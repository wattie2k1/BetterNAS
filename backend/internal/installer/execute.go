package installer

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/betternas/betternas/internal/sysexec"
)

// Progress meldet den aktuellen Schritt und den Fortschritt in Prozent.
type Progress func(step string, percent int)

// Env beschreibt die Umgebung, in der installiert wird.
type Env struct {
	Runner   sysexec.Runner
	FS       sysexec.FS // Wurzel = Wurzel des Live-Systems
	Squashfs string     // Pfad zum rootfs.squashfs
	Target   string     // Mountpoint des Zielsystems, z. B. "/target"
	UEFI     bool       // im UEFI-Modus gestartet
	Arch     string     // "amd64"
	Version  string
	Now      func() time.Time
}

type grubTargets struct{ efi, bios string }

func grubFor(arch string) (grubTargets, error) {
	switch arch {
	case "amd64":
		return grubTargets{efi: "x86_64-efi", bios: "i386-pc"}, nil
	}
	return grubTargets{}, fmt.Errorf("architektur %q wird vom Installer noch nicht unterstützt", arch)
}

type executor struct {
	ctx context.Context
	env Env
	p   Progress
}

// run führt einen Befehl aus. Fehlermeldungen enthalten Befehl und Stderr, nie die Stdin-Daten.
func (x *executor) run(stdin []byte, name string, args ...string) (string, error) {
	r, err := x.env.Runner.Run(x.ctx, stdin, name, args...)
	if err != nil {
		return "", fmt.Errorf("%s: %w", sysexec.CommandLine(name, args...), err)
	}
	if r.ExitCode != 0 {
		return "", fmt.Errorf("%s (Exit %d): %s", sysexec.CommandLine(name, args...), r.ExitCode, strings.TrimSpace(string(r.Stderr)))
	}
	return strings.TrimSpace(string(r.Stdout)), nil
}

func (x *executor) chroot(stdin []byte, args ...string) error {
	_, err := x.run(stdin, "chroot", append([]string{x.env.Target}, args...)...)
	return err
}

func (x *executor) t(p string) string { return path.Join(x.env.Target, p) }

// Execute installiert BetterNAS gemäß Layout und Antworten.
func Execute(ctx context.Context, env Env, l Layout, a Answers, p Progress) (err error) {
	if p == nil {
		p = func(string, int) {}
	}
	if env.Now == nil {
		env.Now = time.Now
	}
	grub, err := grubFor(env.Arch)
	if err != nil {
		return err
	}
	x := &executor{ctx: ctx, env: env, p: p}
	dev := l.Device
	part := func(n int) string { return PartitionDevice(dev, n) }

	p("Partitioniere "+dev, 2)
	if _, err := x.run(nil, "wipefs", "--all", dev); err != nil {
		return err
	}
	if _, err := x.run(nil, "sgdisk", "--zap-all", dev); err != nil {
		return err
	}
	for _, s := range l.Partitions {
		end := "0"
		if s.SizeMiB > 0 {
			end = fmt.Sprintf("+%dM", s.SizeMiB)
		}
		if _, err := x.run(nil, "sgdisk",
			fmt.Sprintf("--new=%d:0:%s", s.Number, end),
			fmt.Sprintf("--typecode=%d:%s", s.Number, s.TypeCode),
			fmt.Sprintf("--change-name=%d:%s", s.Number, s.Name),
			dev); err != nil {
			return err
		}
	}
	if _, err := x.run(nil, "partprobe", dev); err != nil {
		return err
	}
	if _, err := x.run(nil, "udevadm", "settle"); err != nil {
		return err
	}

	p("Erstelle Dateisysteme", 6)
	uuids := map[string]string{}
	for _, s := range l.Partitions {
		switch s.FSType {
		case "vfat":
			_, err = x.run(nil, "mkfs.vfat", "-F", "32", "-n", s.Label, part(s.Number))
		case "ext4":
			args := []string{"-F", "-L", s.Label}
			if s.Mount == "/srv/appdata" {
				args = []string{"-F", "-m", "0", "-L", s.Label}
			}
			_, err = x.run(nil, "mkfs.ext4", append(args, part(s.Number))...)
		default:
			continue
		}
		if err != nil {
			return err
		}
		uuid, err := x.run(nil, "blkid", "-s", "UUID", "-o", "value", part(s.Number))
		if err != nil {
			return err
		}
		uuids[s.Label] = uuid
	}

	if err := env.FS.MkdirAll(env.Target, 0o755); err != nil {
		return err
	}
	if _, err := x.run(nil, "mount", part(3), env.Target); err != nil {
		return err
	}
	defer func() {
		_, _ = x.run(nil, "umount", "-R", env.Target)
		_, _ = x.run(nil, "sync")
	}()
	if err := env.FS.MkdirAll(x.t("/boot/efi"), 0o755); err != nil {
		return err
	}
	if _, err := x.run(nil, "mount", part(2), x.t("/boot/efi")); err != nil {
		return err
	}

	p("Entpacke BetterNAS", 10)
	if _, err := x.run(nil, "unsquashfs", "-f", "-q", "-d", env.Target, env.Squashfs); err != nil {
		return err
	}

	p("Schreibe Konfiguration", 70)
	files, err := RenderFiles(RenderInput{Layout: l, Answers: a, UUIDs: uuids, Version: env.Version, InstalledAt: env.Now()})
	if err != nil {
		return err
	}
	for name, data := range files {
		if err := env.FS.WriteFile(x.t(name), data, 0o644); err != nil {
			return err
		}
	}
	if err := env.FS.Symlink("/usr/share/zoneinfo/"+a.Timezone, x.t("/etc/localtime")); err != nil {
		return err
	}
	if err := env.FS.MkdirAll(x.t("/srv/appdata"), 0o755); err != nil {
		return err
	}

	binds := [][]string{
		{"--bind", "/dev", x.t("/dev")},
		{"--bind", "/dev/pts", x.t("/dev/pts")},
		{"-t", "proc", "proc", x.t("/proc")},
		{"-t", "sysfs", "sysfs", x.t("/sys")},
		{"--bind", "/run", x.t("/run")},
	}
	if env.UEFI {
		binds = append(binds, []string{"-t", "efivarfs", "efivarfs", x.t("/sys/firmware/efi/efivars")})
	}
	for _, b := range binds {
		if _, err := x.run(nil, "mount", b...); err != nil {
			return err
		}
	}

	p("Richte Benutzer ein", 75)
	if err := x.chroot(nil, "ssh-keygen", "-A"); err != nil {
		return err
	}
	if err := x.chroot(nil, "groupadd", "-f", "-r", "betternas-admin"); err != nil {
		return err
	}
	if err := x.chroot(nil, "useradd", "-m", "-s", "/bin/bash", "-G", "betternas-admin,sudo", a.AdminUser); err != nil {
		return err
	}
	if err := x.chroot([]byte(a.AdminUser+":"+a.AdminPassword+"\n"), "chpasswd"); err != nil {
		return err
	}
	if err := x.chroot([]byte(a.AdminPassword+"\n"+a.AdminPassword+"\n"), "smbpasswd", "-a", "-s", a.AdminUser); err != nil {
		return err
	}

	p("Installiere Bootloader", 80)
	efiCommon := []string{"grub-install", "--target=" + grub.efi, "--efi-directory=/boot/efi", "--uefi-secure-boot"}
	if env.UEFI {
		// Debians signiertes GRUB sucht seine Konfiguration unter EFI/debian.
		if err := x.chroot(nil, append(efiCommon, "--bootloader-id=debian")...); err != nil {
			return err
		}
	}
	// Fallback-Pfad EFI/BOOT/BOOTX64.EFI für Mainboards ohne (oder mit verlorenem) Booteintrag.
	if err := x.chroot(nil, append(efiCommon, "--removable", "--no-nvram")...); err != nil {
		return err
	}
	if err := x.chroot(nil, "grub-install", "--target="+grub.bios, dev); err != nil {
		return err
	}

	p("Erzeuge Startdateien", 88)
	if err := x.chroot(nil, "update-initramfs", "-u", "-k", "all"); err != nil {
		return err
	}
	if err := x.chroot(nil, "update-grub"); err != nil {
		return err
	}
	p("Fertig", 100)
	return nil
}
