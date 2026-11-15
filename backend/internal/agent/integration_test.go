//go:build integration

// Integrationstest für Formatieren und Storage-Apply auf einem echten BetterNAS-System.
// Braucht root, mergerfs, losetup und systemd. Aufruf: make integration
package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/storage"
	"github.com/betternas/betternas/internal/sysexec"
)

func sh(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func shOK(name string, args ...string) bool { return exec.Command(name, args...).Run() == nil }

func TestStorageLifecycle(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("braucht root")
	}
	ctx := context.Background()
	d := Deps{Runner: sysexec.NewRunner(), FS: sysexec.NewRootFS("/"), AllowLoop: true}

	// Schutz: Der Test entfernt am Ende alle verwalteten Units – nur auf einem System ohne eigene Pools laufen.
	if existing, _ := managedUnits(d); len(existing) > 0 {
		t.Fatalf("Abbruch: Auf diesem System gibt es bereits verwaltete Mount-Units (%d). Test nur ohne eigene Pools ausführen.", len(existing))
	}

	// Zwei Loop-Geräte à 1 GiB
	dir, err := os.MkdirTemp("/var/tmp", "bnas-it")
	if err != nil {
		t.Fatal(err)
	}
	var loops []string
	for i := 0; i < 2; i++ {
		img := filepath.Join(dir, "disk"+string(rune('a'+i))+".img")
		sh(t, "truncate", "-s", "1G", img)
		loops = append(loops, sh(t, "losetup", "-P", "--find", "--show", img))
	}
	t.Cleanup(func() {
		_, _ = Apply(ctx, d, storage.State{})
		for _, l := range loops {
			_ = exec.Command("losetup", "-d", l).Run()
		}
		os.RemoveAll(dir)
		for _, l := range loops {
			os.Remove(storage.DiskPath("loop-" + filepath.Base(l)))
		}
		os.Remove(storage.PoolPath("ittest"))
	})

	// 1. Erkennung
	disks, err := ListDisks(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	var mine []storage.Disk
	for _, l := range loops {
		dk, ok := findDisk(disks, func(x storage.Disk) bool { return x.Device == l })
		if !ok || !dk.Manageable {
			t.Fatalf("%s nicht erkannt oder nicht verwaltbar: %+v", l, dk)
		}
		mine = append(mine, dk)
	}

	// 2. Formatieren (ext4 und xfs)
	var mounts []storage.DiskMount
	for i, dk := range mine {
		fs := []string{"ext4", "xfs"}[i]
		res, err := Format(ctx, d, FormatRequest{DiskID: dk.ID, Device: dk.Device, SizeBytes: dk.SizeBytes, FSType: fs})
		if err != nil {
			t.Fatalf("format %s: %v", dk.Device, err)
		}
		if got := sh(t, "blkid", "-s", "LABEL", "-o", "value", res.Partition); got != storage.FSLabel(dk.ID) {
			t.Fatalf("label %q", got)
		}
		mounts = append(mounts, storage.DiskMount{DiskID: dk.ID, FSUUID: res.FSUUID, FSType: fs})
	}

	// 3. Pool anlegen und Datei darüber schreiben
	pool := storage.Pool{Name: "ittest", Branches: []string{mine[0].ID, mine[1].ID}, CreatePolicy: "epmfs", MinFree: "1M"}
	st := storage.State{Disks: mounts, Pools: []storage.Pool{pool}}
	if _, err := Apply(ctx, d, st); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !shOK("findmnt", "-t", "fuse.mergerfs", storage.PoolPath("ittest")) {
		t.Fatal("pool nicht eingehängt")
	}
	if err := os.WriteFile(filepath.Join(storage.PoolPath("ittest"), "hallo.txt"), []byte("BetterNAS"), 0o644); err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, dk := range mine {
		if _, err := os.Stat(filepath.Join(storage.DiskPath(dk.ID), "hallo.txt")); err == nil {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("Datei auf %d Platten statt genau einer", found)
	}

	// 4. Pool auf eine Platte verkleinern
	st.Pools[0].Branches = []string{mine[1].ID}
	if _, err := Apply(ctx, d, st); err != nil {
		t.Fatalf("verkleinern: %v", err)
	}
	unit := filepath.Join(unitDir, storage.UnitName(storage.PoolPath("ittest")))
	if b, _ := os.ReadFile(unit); !strings.Contains(string(b), "What="+storage.DiskPath(mine[1].ID)+"\n") {
		t.Fatalf("unit nicht verkleinert:\n%s", b)
	}
	os.WriteFile(filepath.Join(storage.PoolPath("ittest"), "neu.txt"), []byte("x"), 0o644)
	if _, err := os.Stat(filepath.Join(storage.DiskPath(mine[1].ID), "neu.txt")); err != nil {
		t.Fatal("neue Datei nicht auf verbleibender Platte")
	}

	// 5. Belegter Pool: Änderung wird abgelehnt und zurückgesetzt
	before, _ := os.ReadFile(unit)
	f, err := os.Open(filepath.Join(storage.PoolPath("ittest"), "neu.txt"))
	if err != nil {
		t.Fatal(err)
	}
	st.Pools[0].CreatePolicy = "mfs"
	_, err = Apply(ctx, d, st)
	if apierr.From(err).Code != apierr.PoolBusy {
		f.Close()
		t.Fatalf("erwartet pool_busy, bekommen %v", err)
	}
	after, _ := os.ReadFile(unit)
	if string(after) != string(before) || !shOK("findmnt", storage.PoolPath("ittest")) {
		f.Close()
		t.Fatal("nach pool_busy nicht sauber zurückgesetzt")
	}
	f.Close()
	if _, err := Apply(ctx, d, st); err != nil {
		t.Fatalf("nach dem Schließen: %v", err)
	}

	// 6. Units überstehen daemon-reload und Neustart
	sh(t, "systemctl", "daemon-reload")
	sh(t, "systemctl", "restart", storage.UnitName(storage.PoolPath("ittest")))
	if !shOK("findmnt", "-t", "fuse.mergerfs", storage.PoolPath("ittest")) {
		t.Fatal("pool nach restart nicht eingehängt")
	}

	// 7. Alles entfernen
	if _, err := Apply(ctx, d, storage.State{}); err != nil {
		t.Fatalf("entfernen: %v", err)
	}
	if left, _ := managedUnits(d); len(left) != 0 {
		t.Fatalf("verwaltete Units übrig: %v", left)
	}
	if shOK("findmnt", storage.PoolPath("ittest")) {
		t.Fatal("pool noch eingehängt")
	}
}
