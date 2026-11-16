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
