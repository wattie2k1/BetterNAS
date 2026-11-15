package installer

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/sysexec"
)

var update = flag.Bool("update", false, "Golden-Files aktualisieren")

const secret = "Geheim!Passwort42"

func testEnv(t *testing.T, uefi bool) (Env, *sysexec.FakeRunner, string) {
	root := t.TempDir()
	f := &sysexec.FakeRunner{Responses: map[string]sysexec.Result{
		"blkid -s UUID -o value": {Stdout: []byte("UUID-X\n")},
	}}
	return Env{
		Runner: f, FS: sysexec.NewRootFS(root), Squashfs: "/run/live/medium/betternas/rootfs.squashfs",
		Target: "/target", UEFI: uefi, Arch: "amd64", Version: "0.0.1",
		Now: func() time.Time { return time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC) },
	}, f, root
}

func testAnswers() Answers {
	a := Defaults()
	a.AdminUser, a.AdminPassword = "anna", secret
	return a
}

func nvmeLayout(t *testing.T) Layout {
	l, err := PlanLayout(Target{Device: "/dev/nvme0n1", SizeBytes: 500_000_000_000}, Defaults())
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func assertOrder(t *testing.T, f *sysexec.FakeRunner, prefixes ...string) {
	t.Helper()
	last := -1
	for _, p := range prefixes {
		i := -1
		for j := last + 1; j < len(f.Calls); j++ {
			if strings.HasPrefix(f.Calls[j], p) {
				i = j
				break
			}
		}
		if i < 0 {
			t.Fatalf("call %q not found after index %d in:\n%s", p, last, strings.Join(f.Calls, "\n"))
		}
		last = i
	}
}

func TestExecuteCommandSequenceUEFI(t *testing.T) {
	env, f, root := testEnv(t, true)
	if err := Execute(context.Background(), env, nvmeLayout(t), testAnswers(), nil); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, f,
		"wipefs --all /dev/nvme0n1",
		"sgdisk --zap-all /dev/nvme0n1",
		"sgdisk --new=1:0:+1M --typecode=1:EF02 --change-name=1:BIOS /dev/nvme0n1",
		"sgdisk --new=2:0:+512M --typecode=2:EF00",
		"sgdisk --new=3:0:+32768M --typecode=3:8300",
		"sgdisk --new=4:0:0 --typecode=4:8300",
		"partprobe /dev/nvme0n1",
		"udevadm settle",
		"mkfs.vfat -F 32 -n EFI /dev/nvme0n1p2",
		"mkfs.ext4 -F -L bnas-root /dev/nvme0n1p3",
		"mkfs.ext4 -F -m 0 -L bnas-appdata /dev/nvme0n1p4",
		"mount /dev/nvme0n1p3 /target",
		"mount /dev/nvme0n1p2 /target/boot/efi",
		"unsquashfs -f -q -d /target /run/live/medium/betternas/rootfs.squashfs",
		"mount -t efivarfs efivarfs /target/sys/firmware/efi/efivars",
		"chroot /target ssh-keygen -A",
		"chroot /target useradd -m -s /bin/bash -G betternas-admin,sudo anna",
		"chroot /target chpasswd",
		"chroot /target smbpasswd -a -s anna",
		"chroot /target grub-install --target=x86_64-efi --efi-directory=/boot/efi --uefi-secure-boot --bootloader-id=debian",
		"chroot /target grub-install --target=x86_64-efi --efi-directory=/boot/efi --uefi-secure-boot --removable --no-nvram",
		"chroot /target grub-install --target=i386-pc /dev/nvme0n1",
		"chroot /target update-initramfs -u -k all",
		"chroot /target update-grub",
		"umount -R /target",
	)
	if _, err := os.Stat(filepath.Join(root, "target/etc/fstab")); err != nil {
		t.Fatal("fstab not written")
	}
	if l, _ := os.Readlink(filepath.Join(root, "target/etc/localtime")); l != "/usr/share/zoneinfo/Europe/Berlin" {
		t.Fatalf("localtime -> %q", l)
	}
}

func TestExecuteBIOS(t *testing.T) {
	env, f, _ := testEnv(t, false)
	if err := Execute(context.Background(), env, nvmeLayout(t), testAnswers(), nil); err != nil {
		t.Fatal(err)
	}
	if f.Called("mount -t efivarfs") || f.Called("chroot /target grub-install --target=x86_64-efi --efi-directory=/boot/efi --uefi-secure-boot --bootloader-id") {
		t.Fatal("NVRAM/efivars used in BIOS mode")
	}
	if !f.Called("chroot /target grub-install --target=i386-pc") || !f.Called("chroot /target grub-install --target=x86_64-efi --efi-directory=/boot/efi --uefi-secure-boot --removable --no-nvram") {
		t.Fatal("bootloaders missing")
	}
}

func TestExecuteRegeneratesIdentity(t *testing.T) {
	env, f, root := testEnv(t, true)
	if err := Execute(context.Background(), env, nvmeLayout(t), testAnswers(), nil); err != nil {
		t.Fatal(err)
	}
	if !f.Called("chroot /target ssh-keygen -A") {
		t.Fatal("ssh host keys not regenerated")
	}
	if _, err := os.Stat(filepath.Join(root, "target/etc/machine-id")); err == nil {
		t.Fatal("installer must not write machine-id")
	}
}

func TestExecuteNeverLeaksPassword(t *testing.T) {
	env, f, _ := testEnv(t, true)
	f.Responses["chroot /target smbpasswd"] = sysexec.Result{ExitCode: 1, Stderr: []byte("smbpasswd kaputt")}
	var texts []string
	err := Execute(context.Background(), env, nvmeLayout(t), testAnswers(), func(s string, _ int) { texts = append(texts, s) })
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(strings.Join(texts, " "), secret) {
		t.Fatal("password leaked")
	}
	for _, c := range f.Calls {
		if strings.Contains(c, secret) {
			t.Fatalf("password on command line: %s", c)
		}
	}
	if !f.Called("umount -R /target") {
		t.Fatal("target not unmounted after failure")
	}
}

func TestExecuteStopsAfterFailure(t *testing.T) {
	env, f, _ := testEnv(t, true)
	f.Responses["mkfs.ext4"] = sysexec.Result{ExitCode: 1}
	if err := Execute(context.Background(), env, nvmeLayout(t), testAnswers(), nil); err == nil {
		t.Fatal("expected error")
	}
	if f.Called("unsquashfs") || f.Called("mount") {
		t.Fatal("continued after failure")
	}
}

func TestExecuteUnsupportedArch(t *testing.T) {
	env, f, _ := testEnv(t, true)
	env.Arch = "arm64"
	if err := Execute(context.Background(), env, nvmeLayout(t), testAnswers(), nil); err == nil || len(f.Calls) != 0 {
		t.Fatal("arm64 must be rejected before touching disks")
	}
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", "golden", name)
	if *update {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (mit -update erzeugen)", err)
	}
	if string(want) != string(got) {
		t.Fatalf("%s differs:\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

func TestRenderFilesGolden(t *testing.T) {
	uuids := map[string]string{"EFI": "ABCD-1234", "bnas-root": "11111111-2222-3333-4444-555555555555", "bnas-appdata": "66666666-7777-8888-9999-000000000000"}
	in := RenderInput{Layout: nvmeLayout(t), Answers: testAnswers(), UUIDs: uuids, Version: "0.0.1", InstalledAt: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)}
	files, err := RenderFiles(in)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "fstab", files["/etc/fstab"])
	golden(t, "dhcp.network", files["/etc/systemd/network/10-betternas.network"])
	golden(t, "keyboard", files["/etc/default/keyboard"])
	golden(t, "installed.json", files["/etc/betternas/installed.json"])

	in.Answers.Net = NetConfig{Interface: "enp1s0", Address: "192.168.1.20/24", Gateway: "192.168.1.1", DNS: []string{"192.168.1.1", "9.9.9.9"}}
	in.Answers.Keymap = "de-nodeadkeys"
	files, _ = RenderFiles(in)
	golden(t, "static.network", files["/etc/systemd/network/10-betternas.network"])
	if !strings.Contains(string(files["/etc/default/keyboard"]), `XKBVARIANT="nodeadkeys"`) {
		t.Fatal("nodeadkeys variant missing")
	}

	small, _ := PlanLayout(Target{Device: "/dev/sda", SizeBytes: 40 << 30}, Defaults())
	in.Layout = small
	files, _ = RenderFiles(in)
	if strings.Contains(string(files["/etc/fstab"]), "/srv/appdata") {
		t.Fatal("appdata in fstab without partition")
	}
}
