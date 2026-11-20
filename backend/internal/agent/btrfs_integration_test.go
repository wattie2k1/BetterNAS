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
	"time"

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
		_ = exec.Command("systemctl", "disable", "--now", scrubTimer("ittest")).Run()
		_ = exec.Command("systemctl", "stop", "betternas-btrfs-op-ittest.service").Run()
		_ = exec.Command("systemctl", "stop", unit).Run()
		_ = exec.Command("umount", storage.PoolPath("ittest")).Run()
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

	// Status: vollständig, RAID1, zwei Platten, keine Fehler.
	status := func() storage.BtrfsStatus {
		t.Helper()
		st, err := BtrfsStatus(ctx, d, BtrfsStatusRequest{Name: "ittest", FSUUID: res.FSUUID})
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	bst := status()
	if !bst.Mounted || bst.Degraded || len(bst.Devices) != 2 || bst.Errors != 0 || bst.DataProfile != "RAID1" || bst.UsableBytes == 0 || bst.FreeBytes == 0 || bst.UsableBytes > bst.RawBytes {
		t.Fatalf("%+v", bst)
	}

	// Prüf-Timer wird eingeschaltet (die Vorlage liefert das Paket).
	syncScrubTimers(ctx, d, []storage.Pool{pool}, nil)
	if !shOK("systemctl", "is-enabled", scrubTimer("ittest")) {
		t.Fatal("Timer nicht aktiv")
	}

	// Prüfung starten und auf das Ende warten.
	if err := BtrfsScrub(ctx, d, BtrfsScrubRequest{Name: "ittest", Action: "start"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		bst = status()
		if bst.Scrub != nil && bst.Scrub.Result == "finished" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Prüfung nicht fertig: %+v", bst.Scrub)
		}
		time.Sleep(time.Second)
	}
	if bst.Scrub.Uncorrectable != 0 || bst.Scrub.Errors != 0 {
		t.Fatalf("%+v", bst.Scrub)
	}

	// Platte „ziehen“: Pool aus, eine Loop-Platte weg → unvollständig.
	sh(t, "systemctl", "stop", unit)
	sh(t, "losetup", "-d", loops[1])
	_ = exec.Command("btrfs", "device", "scan", "--forget").Run()
	bst = status()
	if bst.Mounted || !bst.Degraded || bst.Missing != 1 {
		t.Fatalf("unvollständiger Pool nicht erkannt: %+v", bst)
	}

	t.Logf("show (unvollständig):\n%s", sh(t, "btrfs", "filesystem", "show", "--raw", res.FSUUID))

	// Degradiert einhängen: schreibbar, Status zeigt „unvollständig“ bei eingehängtem Pool.
	if err := BtrfsMountDegraded(ctx, d, storage.BtrfsMountRequest{Name: "ittest", FSUUID: res.FSUUID, Compress: "zstd:1"}); err != nil {
		t.Fatal(err)
	}
	bst = status()
	if !bst.Mounted || !bst.Degraded || bst.Missing != 1 {
		t.Fatalf("degradiert eingehängt: %+v", bst)
	}
	missingDev := 2
	for _, dv := range bst.Devices {
		if dv.Missing {
			missingDev = dv.DevID
		}
	}
	t.Logf("fehlende devid: %d (Geräte: %+v)", missingDev, bst.Devices)

	newLoop := func(n string) storage.BtrfsDisk {
		img := filepath.Join(dir, n+".img")
		sh(t, "truncate", "-s", "1G", img)
		l := sh(t, "losetup", "-P", "--find", "--show", img)
		loops = append(loops, l)
		all, err := ListDisks(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		dk, ok := findDisk(all, func(x storage.Disk) bool { return x.Device == l })
		if !ok || !dk.Manageable {
			t.Fatalf("%s nicht verwaltbar", l)
		}
		return storage.BtrfsDisk{DiskID: dk.ID, Device: dk.Device, SizeBytes: dk.SizeBytes}
	}
	waitReplace := func() {
		t.Helper()
		end := time.Now().Add(60 * time.Second)
		for {
			r, err := BtrfsReplaceStatus(ctx, d, "ittest")
			if err != nil {
				t.Fatal(err)
			}
			if r.State == "finished" {
				return
			}
			if r.State == "canceled" || time.Now().After(end) {
				t.Fatalf("Ersatz: %+v", r)
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	waitOp := func() {
		t.Helper()
		end := time.Now().Add(60 * time.Second)
		for {
			op, err := BtrfsOpStatus(ctx, d, "ittest")
			if err != nil {
				t.Fatal(err)
			}
			if !op.Running {
				if op.Result != "ok" {
					t.Fatalf("Wartung: %+v", op)
				}
				return
			}
			if time.Now().After(end) {
				t.Fatalf("Wartung hängt: %+v", op)
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	// Fehlende Platte ersetzen.
	if err := BtrfsReplaceStart(ctx, d, storage.BtrfsReplaceRequest{Name: "ittest", FSUUID: res.FSUUID, DevID: missingDev, Target: newLoop("c")}); err != nil {
		t.Fatal(err)
	}
	waitReplace()
	if err := BtrfsResize(ctx, d, storage.BtrfsResizeRequest{Name: "ittest", DevID: missingDev}); err != nil {
		t.Fatal(err)
	}
	bst = status()
	if bst.Degraded || len(bst.Devices) != 2 {
		t.Fatalf("nach dem Ersatz: %+v", bst)
	}

	// Dritte Platte hinzufügen, Balance, dann Platte wieder entfernen.
	if err := BtrfsDeviceAdd(ctx, d, storage.BtrfsAddRequest{Name: "ittest", Target: newLoop("e")}); err != nil {
		t.Fatal(err)
	}
	if bst = status(); len(bst.Devices) != 3 {
		t.Fatalf("nach dem Hinzufügen: %+v", bst)
	}
	if err := BtrfsOpStart(ctx, d, storage.BtrfsOpRequest{Name: "ittest", Op: storage.OpBalance, Usage: 90}); err != nil {
		t.Fatal(err)
	}
	waitOp()
	maxDev := 0
	for _, dv := range bst.Devices {
		maxDev = max(maxDev, dv.DevID)
	}
	if err := BtrfsOpStart(ctx, d, storage.BtrfsOpRequest{Name: "ittest", Op: storage.OpRemove, DevID: maxDev}); err != nil {
		t.Fatal(err)
	}
	waitOp()
	if bst = status(); len(bst.Devices) != 2 {
		t.Fatalf("nach dem Entfernen: %+v", bst)
	}

	// Profilwechsel raid1 → single und zurück.
	if err := BtrfsOpStart(ctx, d, storage.BtrfsOpRequest{Name: "ittest", Op: storage.OpConvert, Profile: "single", Metadata: "raid1"}); err != nil {
		t.Fatal(err)
	}
	waitOp()
	if bst = status(); bst.DataProfile != "single" && bst.DataProfile != "Single" {
		t.Fatalf("Profil: %+v", bst)
	}
	if err := BtrfsOpStart(ctx, d, storage.BtrfsOpRequest{Name: "ittest", Op: storage.OpConvert, Profile: "raid1", Metadata: "raid1"}); err != nil {
		t.Fatal(err)
	}
	waitOp()
	if bst = status(); bst.DataProfile != "RAID1" {
		t.Fatalf("Profil: %+v", bst)
	}

	// Fehlerzähler zurücksetzen.
	if err := BtrfsResetStats(ctx, d, "ittest"); err != nil {
		t.Fatal(err)
	}
}
