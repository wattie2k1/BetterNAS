//go:build integration

// Integrationstest für btrfs-Pools auf einem echten BetterNAS-System.
// Braucht root, btrfs-progs, losetup und systemd. Aufruf: make integration
// Fasst nur eigene Dateien an (Pool „ittest“), läuft also auch neben echten Pools.
package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/storage"
	"github.com/betternas/betternas/internal/sysexec"
)

func TestBtrfsPoolLifecycle(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("braucht root")
	}
	if _, err := exec.LookPath("mkfs.btrfs"); err != nil {
		t.Skip("btrfs-progs fehlt")
	}
	ctx := context.Background()
	d := Deps{Runner: sysexec.NewRunner(), FS: sysexec.NewRootFS("/"), AllowLoop: true}

	dir, err := os.MkdirTemp("/var/tmp", "bnas-btrfs")
	if err != nil {
		t.Fatal(err)
	}
	var loops []string
	for i := 0; i < 2; i++ {
		img := filepath.Join(dir, "disk"+string(rune('a'+i))+".img")
		sh(t, "truncate", "-s", "1G", img)
		loops = append(loops, sh(t, "losetup", "-P", "--find", "--show", img))
	}
	const unit = "srv-pools-ittest.mount"
	t.Cleanup(func() {
		_ = exec.Command("systemctl", "stop", unit).Run()
		_ = exec.Command("systemctl", "disable", unit).Run()
		os.Remove("/etc/systemd/system/" + unit)
		_ = exec.Command("systemctl", "daemon-reload").Run()
		for _, l := range loops {
			_ = exec.Command("losetup", "-d", l).Run()
		}
		os.RemoveAll(dir)
		_ = exec.Command("chattr", "-i", storage.PoolPath("ittest")).Run()
		os.Remove(storage.PoolPath("ittest"))
	})

	disks, err := ListDisks(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	var req storage.BtrfsCreateRequest
	req.Name, req.Profile = "ittest", "raid1"
	for _, l := range loops {
		dk, ok := findDisk(disks, func(x storage.Disk) bool { return x.Device == l })
		if !ok || !dk.Manageable {
			t.Fatalf("%s nicht erkannt oder nicht verwaltbar: %+v", l, dk)
		}
		req.Disks = append(req.Disks, storage.BtrfsDisk{DiskID: dk.ID, Device: dk.Device, SizeBytes: dk.SizeBytes})
	}

	res, err := BtrfsCreate(ctx, d, req)
	if err != nil {
		t.Fatal(err)
	}
	if res.FSUUID == "" || res.Label != "bnas-ittest" {
		t.Fatalf("%+v", res)
	}
	show := sh(t, "btrfs", "filesystem", "show", "--raw", res.FSUUID)
	if strings.Count(show, "devid") != 2 {
		t.Fatalf("zwei Mitglieder erwartet:\n%s", show)
	}

	// Unit genau so einhängen, wie BetterNAS es tut (nur der eigene Pool).
	pool := storage.Pool{Name: "ittest", Type: storage.PoolBtrfs, Branches: []string{req.Disks[0].DiskID, req.Disks[1].DiskID},
		Profile: "raid1", FSUUID: res.FSUUID, Compress: "zstd:1", CreatePolicy: storage.DefaultPolicy, MinFree: storage.DefaultMinFree}
	st := storage.State{
		Disks: []storage.DiskMount{
			{DiskID: pool.Branches[0], FSUUID: res.FSUUID, FSType: "btrfs"},
			{DiskID: pool.Branches[1], FSUUID: res.FSUUID, FSType: "btrfs"},
		},
		Pools: []storage.Pool{pool},
	}
	units, err := storage.RenderUnits(st)
	if err != nil || len(units) != 1 {
		t.Fatalf("%v %d", err, len(units))
	}
	if err := os.WriteFile("/etc/systemd/system/"+unit, units[unit], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(storage.PoolPath("ittest"), 0o755); err != nil {
		t.Fatal(err)
	}
	sh(t, "systemctl", "daemon-reload")
	sh(t, "systemctl", "start", unit)
	if !shOK("findmnt", "--mountpoint", storage.PoolPath("ittest")) {
		t.Fatal("Pool ist nicht eingehängt")
	}
	if err := os.WriteFile(filepath.Join(storage.PoolPath("ittest"), "probe.txt"), []byte("hallo"), 0o644); err != nil {
		t.Fatal(err)
	}
	df := sh(t, "btrfs", "filesystem", "df", storage.PoolPath("ittest"))
	if !strings.Contains(df, "Data, RAID1") || !strings.Contains(df, "Metadata, RAID1") {
		t.Fatalf("Profil RAID1 erwartet:\n%s", df)
	}
	if opts := sh(t, "findmnt", "-no", "OPTIONS", storage.PoolPath("ittest")); !strings.Contains(opts, "compress=zstd:1") {
		t.Fatalf("Kompression fehlt: %s", opts)
	}
}
