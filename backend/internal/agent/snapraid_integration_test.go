//go:build integration

// Integrationstest für SnapRAID mit dem echten snapraid-Programm auf Loop-Geräten.
// Braucht root, losetup, mkfs.ext4 und snapraid. systemctl/systemd-run werden ersetzt
// (die Timer-Anbindung prüfen die Unit-Tests); der Test läuft deshalb auch in einem privilegierten Container.
package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/snapraid"
	"github.com/betternas/betternas/internal/storage"
	"github.com/betternas/betternas/internal/sysexec"
)

// noSystemd führt alles echt aus, außer systemctl und systemd-run.
type noSystemd struct{ sysexec.Runner }

func (r noSystemd) Run(ctx context.Context, stdin []byte, name string, args ...string) (sysexec.Result, error) {
	if name == "systemctl" || name == "systemd-run" {
		return sysexec.Result{}, nil
	}
	return r.Runner.Run(ctx, stdin, name, args...)
}

func TestSnapraidLifecycle(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("braucht root")
	}
	if _, err := exec.LookPath("snapraid"); err != nil {
		t.Skip("snapraid ist nicht installiert")
	}
	ctx := context.Background()
	d := Deps{Runner: noSystemd{sysexec.NewRunner()}, FS: sysexec.NewRootFS("/")}
	if existing, _ := d.FS.Glob(filepath.Join(snapraid.ConfDir, "*.conf")); len(existing) > 0 {
		t.Skipf("übersprungen: Es gibt bereits SnapRAID-Konfigurationen (%v)", existing)
	}
	if _, err := os.Stat("/srv/disks/itsd1"); err == nil {
		t.Skip("übersprungen: /srv/disks/itsd1 existiert bereits")
	}

	dir, err := os.MkdirTemp("/var/tmp", "bnas-snap")
	if err != nil {
		t.Fatal(err)
	}
	type disk struct{ id, mount, loop string }
	disks := []*disk{
		{id: "itsd1", mount: storage.DiskPath("itsd1")},
		{id: "itsd2", mount: storage.DiskPath("itsd2")},
		{id: "itsp1", mount: storage.ParityPath("itsp1")},
	}
	t.Cleanup(func() {
		_, _ = ApplySnapraid(ctx, d, snapraid.State{})
		for _, k := range disks {
			_ = exec.Command("umount", k.mount).Run()
			if k.loop != "" {
				_ = exec.Command("losetup", "-d", k.loop).Run()
			}
			os.Remove(k.mount)
		}
		os.RemoveAll(dir)
		os.Remove(snapraid.StatePath("itsnap"))
		os.Remove(snapraid.LogPath("itsnap"))
		os.Remove(snapraid.ContentPath("itsnap"))
	})
	for _, k := range disks {
		img := filepath.Join(dir, k.id+".img")
		sh(t, "truncate", "-s", "300M", img)
		k.loop = sh(t, "losetup", "--find", "--show", img)
		sh(t, "mkfs.ext4", "-q", "-F", "-m", "0", k.loop)
		if err := os.MkdirAll(k.mount, 0o755); err != nil {
			t.Fatal(err)
		}
		sh(t, "mount", k.loop, k.mount)
	}

	st := snapraid.State{Pools: []snapraid.PoolConfig{snapraid.PoolConfig{
		Pool: "itsnap", Data: []string{"itsd1", "itsd2"}, Parity: []string{"itsp1"},
		Settings: snapraid.Settings{DeleteThreshold: 5},
	}.WithDefaults()}}
	if _, err := ApplySnapraid(ctx, d, st); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if out, err := exec.Command("snapraid", "-c", snapraid.ConfPath("itsnap"), "status").CombinedOutput(); err != nil && len(out) == 0 {
		t.Fatalf("snapraid lehnt die erzeugte Konfiguration ab: %v", err)
	}

	// 20 Dateien verteilt auf beide Datenplatten
	for i := 0; i < 20; i++ {
		p := filepath.Join(disks[i%2].mount, fmt.Sprintf("datei%02d.bin", i))
		if err := os.WriteFile(p, []byte(fmt.Sprintf("Inhalt %d %0100d", i, i)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := RunSnapraid(ctx, d, "itsnap", "sync", false); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(disks[2].mount, "snapraid.parity")); err != nil {
		t.Fatalf("keine Paritätsdatei: %v", err)
	}
	status, _ := SnapraidStatus(ctx, d, "itsnap")
	if status.LastSync == nil || status.LastSync.Result != snapraid.ResultOK || status.Diff == nil || status.Diff.Unprotected() != 0 {
		t.Fatalf("%+v", status)
	}

	// Massenlöschung: 6 Dateien weg, Schwelle 5 → blockiert; mit force läuft es
	for i := 0; i < 6; i++ {
		os.Remove(filepath.Join(disks[i%2].mount, fmt.Sprintf("datei%02d.bin", i)))
	}
	err = RunSnapraid(ctx, d, "itsnap", "sync", false)
	if apierr.From(err).Code != apierr.Conflict {
		t.Fatalf("erwartet conflict, bekommen %v", err)
	}
	status, _ = SnapraidStatus(ctx, d, "itsnap")
	if status.LastSync.Result != snapraid.ResultBlocked || status.LastSync.Removed != 6 {
		t.Fatalf("%+v", status.LastSync)
	}
	if err := RunSnapraid(ctx, d, "itsnap", "sync", true); err != nil {
		t.Fatalf("sync --force: %v", err)
	}

	// Scrub
	if err := RunSnapraid(ctx, d, "itsnap", "scrub", false); err != nil {
		t.Fatalf("scrub: %v", err)
	}

	// Fehlende Platte: nichts wird ausgeführt
	sh(t, "umount", disks[1].mount)
	if err := RunSnapraid(ctx, d, "itsnap", "sync", false); apierr.From(err).Code != apierr.Conflict {
		t.Fatalf("sync mit fehlender Platte: %v", err)
	}
	sh(t, "mount", disks[1].loop, disks[1].mount)
}

// TestSnapraidReplaceFix: Eine Datenplatte fällt aus, eine leere Ersatzplatte übernimmt ihren SnapRAID-Namen,
// `fix` holt die Dateien aus der Parität zurück. Vorher sperrt der Marker Sync und Scrub.
func TestSnapraidReplaceFix(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("braucht root")
	}
	if _, err := exec.LookPath("snapraid"); err != nil {
		t.Skip("snapraid ist nicht installiert")
	}
	ctx := context.Background()
	d := Deps{Runner: noSystemd{sysexec.NewRunner()}, FS: sysexec.NewRootFS("/")}
	if existing, _ := d.FS.Glob(filepath.Join(snapraid.ConfDir, "itfix.conf")); len(existing) > 0 {
		t.Skipf("übersprungen: %v existiert bereits", existing)
	}
	for _, id := range []string{"itfd1", "itfd2", "itfn1"} {
		if _, err := os.Stat(storage.DiskPath(id)); err == nil {
			t.Skipf("übersprungen: %s existiert bereits", storage.DiskPath(id))
		}
	}
	dir, err := os.MkdirTemp("/var/tmp", "bnas-fix")
	if err != nil {
		t.Fatal(err)
	}
	type disk struct{ id, mount, loop string }
	disks := map[string]*disk{
		"itfd1": {id: "itfd1", mount: storage.DiskPath("itfd1")},
		"itfd2": {id: "itfd2", mount: storage.DiskPath("itfd2")},
		"itfp1": {id: "itfp1", mount: storage.ParityPath("itfp1")},
		"itfn1": {id: "itfn1", mount: storage.DiskPath("itfn1")},
	}
	t.Cleanup(func() {
		_, _ = ApplySnapraid(ctx, d, snapraid.State{})
		for _, k := range disks {
			_ = exec.Command("umount", k.mount).Run()
			if k.loop != "" {
				_ = exec.Command("losetup", "-d", k.loop).Run()
			}
			os.Remove(k.mount)
		}
		os.RemoveAll(dir)
		for _, f := range []string{snapraid.StatePath("itfix"), snapraid.LogPath("itfix"), snapraid.ContentPath("itfix"), snapraid.FixingPath("itfix")} {
			os.Remove(f)
		}
	})
	attach := func(k *disk) {
		img := filepath.Join(dir, k.id+".img")
		sh(t, "truncate", "-s", "300M", img)
		k.loop = sh(t, "losetup", "--find", "--show", img)
		sh(t, "mkfs.ext4", "-q", "-F", "-m", "0", k.loop)
		if err := os.MkdirAll(k.mount, 0o755); err != nil {
			t.Fatal(err)
		}
		sh(t, "mount", k.loop, k.mount)
	}
	for _, id := range []string{"itfd1", "itfd2", "itfp1"} {
		attach(disks[id])
	}

	st := snapraid.State{Pools: []snapraid.PoolConfig{snapraid.PoolConfig{
		Pool: "itfix", Data: []string{"itfd1", "itfd2"}, Parity: []string{"itfp1"},
	}.WithDefaults()}}
	if _, err := ApplySnapraid(ctx, d, st); err != nil {
		t.Fatalf("apply: %v", err)
	}
	want := map[string]string{}
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("datei%02d.bin", i)
		content := fmt.Sprintf("Inhalt %d %0200d", i, i)
		if err := os.WriteFile(filepath.Join(disks["itfd1"].mount, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		want[name] = content
	}
	for i := 0; i < 5; i++ { // etwas auf der zweiten Platte, damit sie nicht leer ist
		os.WriteFile(filepath.Join(disks["itfd2"].mount, fmt.Sprintf("andere%d.bin", i)), []byte(fmt.Sprintf("x%d", i)), 0o644)
	}
	if err := RunSnapraid(ctx, d, "itfix", "sync", false); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// Platte 1 fällt aus; eine leere Ersatzplatte kommt.
	sh(t, "umount", disks["itfd1"].mount)
	sh(t, "losetup", "-d", disks["itfd1"].loop)
	disks["itfd1"].loop = ""
	attach(disks["itfn1"])

	if err := RecoveryBegin(ctx, d, "itfix", "itfd1"); err != nil {
		t.Fatalf("marker: %v", err)
	}
	if err := RunSnapraid(ctx, d, "itfix", "sync", false); apierr.From(err).Code != apierr.Conflict {
		t.Fatalf("sync trotz Marker: %v", err)
	}
	if status, _ := SnapraidStatus(ctx, d, "itfix"); status.Recovery == nil || status.Recovery.Disk != "itfd1" || status.LastSync.Result != snapraid.ResultBlocked {
		t.Fatalf("%+v", status)
	}

	st2 := snapraid.State{Pools: []snapraid.PoolConfig{snapraid.PoolConfig{
		Pool: "itfix", Data: []string{"itfn1", "itfd2"}, Parity: []string{"itfp1"}, Names: map[string]string{"itfn1": "itfd1"},
	}.WithDefaults()}}
	if _, err := ApplySnapraid(ctx, d, st2); err != nil {
		t.Fatalf("apply mit Alias: %v", err)
	}
	if err := RunSnapraidDisk(ctx, d, "itfix", "fix", false, "itfd1"); err != nil {
		t.Fatalf("fix: %v", err)
	}
	for name, content := range want {
		got, err := os.ReadFile(filepath.Join(disks["itfn1"].mount, name))
		if err != nil || string(got) != content {
			t.Fatalf("%s nicht wiederhergestellt: %v (%q)", name, err, got)
		}
	}
	status, _ := SnapraidStatus(ctx, d, "itfix")
	if status.Recovery != nil || status.LastFix == nil || status.LastFix.Result != snapraid.ResultOK {
		t.Fatalf("%+v", status)
	}
	if err := RunSnapraid(ctx, d, "itfix", "sync", false); err != nil {
		t.Fatalf("sync nach der Wiederherstellung: %v", err)
	}
}
