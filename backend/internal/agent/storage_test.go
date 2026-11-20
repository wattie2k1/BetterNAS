package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/storage"
	"github.com/betternas/betternas/internal/sysexec"
)

const lsblkCall = "lsblk --json --bytes --tree -o " + storage.LsblkColumns

func lsblkFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../storage/testdata/lsblk-basic.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// storageEnv liefert Deps mit FakeRunner (lsblk-Fixture, nichts gemountet) und RootFS.
func storageEnv(t *testing.T) (Deps, *sysexec.FakeRunner, string) {
	t.Helper()
	root := t.TempDir()
	f := &sysexec.FakeRunner{Responses: map[string]sysexec.Result{
		lsblkCall:              {Stdout: lsblkFixture(t)},
		"findmnt --mountpoint": {ExitCode: 1},
		"lsblk --json --bytes -o PATH,TYPE /dev/sda": {Stdout: []byte(`{"blockdevices":[{"path":"/dev/sda","type":"disk","children":[{"path":"/dev/sda1","type":"part"}]}]}`)},
		"blkid -s UUID -o value /dev/sda1":           {Stdout: []byte("aaaa-bbbb\n")},
	}}
	return Deps{Runner: f, FS: sysexec.NewRootFS(root)}, f, root
}

func code(err error) string { return apierr.From(err).Code }

func assertOrder(t *testing.T, f *sysexec.FakeRunner, prefixes ...string) {
	t.Helper()
	last := -1
	for _, p := range prefixes {
		found := -1
		for j := last + 1; j < len(f.Calls); j++ {
			if strings.HasPrefix(f.Calls[j], p) {
				found = j
				break
			}
		}
		if found < 0 {
			t.Fatalf("call %q not found after %d in:\n%s", p, last, strings.Join(f.Calls, "\n"))
		}
		last = found
	}
}

func sdaRequest() FormatRequest {
	return FormatRequest{DiskID: "wwn-0x50014ee2b5d1a2c3", Device: "/dev/sda", SizeBytes: 4000787030016, FSType: "ext4"}
}

func TestFormatCommandSequence(t *testing.T) {
	d, f, _ := storageEnv(t)
	res, err := Format(context.Background(), d, sdaRequest())
	if err != nil {
		t.Fatal(err)
	}
	label := storage.FSLabel("wwn-0x50014ee2b5d1a2c3")
	assertOrder(t, f,
		lsblkCall,
		"wipefs --all /dev/sda",
		"sgdisk --zap-all /dev/sda",
		"sgdisk --new=1:0:0 --typecode=1:8300 --change-name=1:bnas-data /dev/sda",
		"partprobe /dev/sda",
		"udevadm settle",
		"lsblk --json --bytes -o PATH,TYPE /dev/sda",
		"mkfs.ext4 -F -m 0 -L "+label+" /dev/sda1",
		"blkid -s UUID -o value /dev/sda1",
	)
	if res.FSUUID != "aaaa-bbbb" || res.Partition != "/dev/sda1" || res.Label != label || res.FSType != "ext4" {
		t.Fatalf("%+v", res)
	}
}

func TestFormatXFS(t *testing.T) {
	d, f, _ := storageEnv(t)
	req := sdaRequest()
	req.FSType = "xfs"
	if _, err := Format(context.Background(), d, req); err != nil {
		t.Fatal(err)
	}
	if !f.Called("mkfs.xfs -f -L " + storage.FSLabel(req.DiskID) + " /dev/sda1") {
		t.Fatal(f.Calls)
	}
}

func TestFormatRejectsChangedDisk(t *testing.T) {
	cases := map[string]struct {
		mod  func(*FormatRequest, *sysexec.FakeRunner)
		want string
	}{
		"andere größe":  {func(r *FormatRequest, _ *sysexec.FakeRunner) { r.SizeBytes = 1 }, apierr.Conflict},
		"anderes gerät": {func(r *FormatRequest, _ *sysexec.FakeRunner) { r.Device = "/dev/sdb" }, apierr.Conflict},
		"unbekannt":     {func(r *FormatRequest, _ *sysexec.FakeRunner) { r.DiskID = "gibtsnicht" }, apierr.NotFound},
		"systemplatte": {func(r *FormatRequest, _ *sysexec.FakeRunner) {
			*r = FormatRequest{DiskID: "wwn-eui.002538b111b2c3d4", Device: "/dev/nvme0n1", SizeBytes: 512110190592, FSType: "ext4"}
		}, apierr.SystemDisk},
		"gemountet": {func(r *FormatRequest, f *sysexec.FakeRunner) {
			*r = FormatRequest{DiskID: "WDC_WD40EFRX-68N32N0_WD-WCC7K1234567", Device: "/dev/sdb", SizeBytes: 4000787030016, FSType: "ext4"}
			mounted := strings.Replace(string(f.Responses[lsblkCall].Stdout), `"label":"media", "mountpoints":[null]`, `"label":"media", "mountpoints":["/mnt/media"]`, 1)
			f.Responses[lsblkCall] = sysexec.Result{Stdout: []byte(mounted)}
		}, apierr.Conflict},
		"dateisystem": {func(r *FormatRequest, _ *sysexec.FakeRunner) { r.FSType = "ntfs" }, apierr.InvalidParams},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			d, f, _ := storageEnv(t)
			req := sdaRequest()
			c.mod(&req, f)
			_, err := Format(context.Background(), d, req)
			if code(err) != c.want {
				t.Fatalf("want %s got %v", c.want, err)
			}
			if f.Called("wipefs") || f.Called("sgdisk") {
				t.Fatal("disk touched despite rejection")
			}
		})
	}
}

func TestFormatStopsOnError(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses["sgdisk --zap-all"] = sysexec.Result{ExitCode: 2, Stderr: []byte("kaputt")}
	if _, err := Format(context.Background(), d, sdaRequest()); err == nil {
		t.Fatal("expected error")
	}
	if f.Called("mkfs") {
		t.Fatal("continued after failure")
	}
}

// --- Apply ---

func applyState() storage.State {
	return storage.State{
		Disks: []storage.DiskMount{
			{DiskID: "d1", FSUUID: "u1", FSType: "ext4"},
			{DiskID: "d2", FSUUID: "u2", FSType: "ext4"},
		},
		Pools: []storage.Pool{{Name: "media", Branches: []string{"d1", "d2"}, CreatePolicy: "epmfs", MinFree: "20G"}},
	}
}

func TestApplyFreshState(t *testing.T) {
	d, f, root := storageEnv(t)
	res, err := Apply(context.Background(), d, applyState())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changed) != 3 {
		t.Fatalf("%+v", res)
	}
	for _, n := range []string{"srv-disks-d1.mount", "srv-disks-d2.mount", "srv-pools-media.mount"} {
		if _, err := os.Stat(filepath.Join(root, unitDir+"/", n)); err != nil {
			t.Fatal(n, err)
		}
	}
	for _, p := range []string{"srv/disks/d1", "srv/pools/media"} {
		if st, err := os.Stat(filepath.Join(root, p)); err != nil || !st.IsDir() {
			t.Fatal(p, err)
		}
	}
	assertOrder(t, f, "systemctl daemon-reload", "systemctl enable --now srv-disks-d1.mount", "systemctl enable --now srv-pools-media.mount")
	if f.Index("systemctl enable --now srv-disks-d2.mount") > f.Index("systemctl enable --now srv-pools-media.mount") {
		t.Fatal("pool enabled before its disks")
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	d, f, _ := storageEnv(t)
	if _, err := Apply(context.Background(), d, applyState()); err != nil {
		t.Fatal(err)
	}
	f.Calls = nil
	res, err := Apply(context.Background(), d, applyState())
	if err != nil || len(res.Changed) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if f.Called("systemctl") {
		t.Fatalf("unexpected calls: %v", f.Calls)
	}
}

func TestApplyRemovesStale(t *testing.T) {
	d, f, root := storageEnv(t)
	Apply(context.Background(), d, applyState())
	f.Calls = nil
	st := applyState()
	st.Pools = nil
	if _, err := Apply(context.Background(), d, st); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, f, "systemctl stop srv-pools-media.mount", "findmnt --mountpoint /srv/pools/media", "systemctl disable srv-pools-media.mount")
	if _, err := os.Stat(filepath.Join(root, unitDir+"/", "srv-pools-media.mount")); !os.IsNotExist(err) {
		t.Fatal("stale unit not removed")
	}
	if _, err := os.Stat(filepath.Join(root, unitDir+"/", "srv-disks-d1.mount")); err != nil {
		t.Fatal("disk unit removed")
	}
}

func TestApplyIgnoresForeignUnits(t *testing.T) {
	d, f, root := storageEnv(t)
	foreign := filepath.Join(root, unitDir+"/", "srv-disks-foo.mount")
	os.MkdirAll(filepath.Dir(foreign), 0o755)
	os.WriteFile(foreign, []byte("[Mount]\nWhat=/dev/x\n"), 0o644)
	if _, err := Apply(context.Background(), d, applyState()); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(foreign); string(b) != "[Mount]\nWhat=/dev/x\n" || f.Called("systemctl stop srv-disks-foo.mount") {
		t.Fatal("foreign unit touched")
	}
}

func TestApplyRollbackOnBusyPool(t *testing.T) {
	d, f, root := storageEnv(t)
	Apply(context.Background(), d, applyState())
	unit := filepath.Join(root, unitDir+"/", "srv-pools-media.mount")
	before, _ := os.ReadFile(unit)
	f.Calls = nil
	f.Responses["findmnt --mountpoint /srv/pools/media"] = sysexec.Result{ExitCode: 0} // noch eingehängt = belegt
	st := applyState()
	st.Pools[0].CreatePolicy = "mfs"
	_, err := Apply(context.Background(), d, st)
	e := apierr.From(err)
	if e.Code != apierr.PoolBusy || e.Details["unit"] != "srv-pools-media.mount" {
		t.Fatalf("%v", err)
	}
	after, _ := os.ReadFile(unit)
	if string(after) != string(before) {
		t.Fatal("unit file not restored")
	}
	assertOrder(t, f, "systemctl stop srv-pools-media.mount", "systemctl daemon-reload", "systemctl start srv-pools-media.mount")
	if f.Called("systemctl enable --now srv-pools-media.mount") {
		t.Fatal("changed unit started despite rollback")
	}
}

func TestApplyRejectsInvalidState(t *testing.T) {
	d, f, _ := storageEnv(t)
	st := applyState()
	st.Pools[0].Name = "../etc"
	if _, err := Apply(context.Background(), d, st); code(err) != apierr.InvalidParams || f.Called("systemctl") {
		t.Fatal(err)
	}
}

// --- SMART ---

const smartJSON = `{"smart_status":{"passed":true},"temperature":{"current":34},"power_on_time":{"hours":12034}}`

func TestSmartParsesJSON(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses["smartctl --json -H -A -c -l selftest -n standby /dev/sda"] = sysexec.Result{ExitCode: 4, Stdout: []byte(smartJSON)}
	s, err := Smart(context.Background(), d, "/dev/sda")
	if err != nil || !s.Passed || s.TemperatureC != 34 || s.PowerOnHours != 12034 {
		t.Fatalf("%+v %v", s, err)
	}
	f.Responses["smartctl --json -H -A -c -l selftest -n standby /dev/sda"] = sysexec.Result{ExitCode: 2}
	f.Responses["smartctl --json -H -A -c -l selftest -n standby -d sat /dev/sda"] = sysexec.Result{ExitCode: 2}
	if _, err := Smart(context.Background(), d, "/dev/sda"); err == nil {
		t.Fatal("exit bit 1 must fail")
	}
}

func TestSmartRejectsUnknownDevice(t *testing.T) {
	d, f, _ := storageEnv(t)
	if _, err := Smart(context.Background(), d, "/dev/sda1; rm -rf /"); code(err) != apierr.NotFound || f.Called("smartctl") {
		t.Fatal(err)
	}
}

// Fehlt eine Platte, darf mergerfs nicht in den leeren Einhängeordner auf der Systemplatte schreiben.
func TestApplyProtectsEmptyMountpoints(t *testing.T) {
	d, f, _ := storageEnv(t)
	if _, err := Apply(context.Background(), d, applyState()); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, f, "findmnt --mountpoint /srv/disks/d1", "chattr +i /srv/disks/d1", "systemctl enable --now srv-disks-d1.mount")
}

// Auch ein nicht eingehängter Pool darf keine Dateien auf der Systemplatte sammeln (Freigaben schreiben dorthin).
func TestApplyProtectsPoolMountpoint(t *testing.T) {
	d, f, _ := storageEnv(t)
	if _, err := Apply(context.Background(), d, applyState()); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, f, "findmnt --mountpoint /srv/pools/media", "chattr +i /srv/pools/media", "systemctl enable --now srv-pools-media.mount")
}

func TestApplyDoesNotChattrMountedDisk(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses["findmnt --mountpoint /srv/disks/d1"] = sysexec.Result{ExitCode: 0} // bereits eingehängt
	if _, err := Apply(context.Background(), d, applyState()); err != nil {
		t.Fatal(err)
	}
	if f.Called("chattr +i /srv/disks/d1") {
		t.Fatal("chattr on a mounted filesystem root")
	}
}

func TestApplyWritesParityUnitAndProtectsMountpoint(t *testing.T) {
	d, f, root := storageEnv(t)
	st := applyState()
	st.Disks = append(st.Disks, storage.DiskMount{DiskID: "p1", FSUUID: "u3", FSType: "ext4", Role: "parity"})
	if _, err := Apply(context.Background(), d, st); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, unitDir, "srv-parity-p1.mount")); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, f, "chattr +i /srv/parity/p1", "systemctl enable --now srv-parity-p1.mount", "systemctl enable --now srv-pools-media.mount")
	// Zweiter Lauf erkennt die vorhandene Paritäts-Unit (idempotent).
	res, err := Apply(context.Background(), d, st)
	if err != nil || len(res.Changed) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	// Entfällt die Platte, wird die Unit entfernt.
	if _, err := Apply(context.Background(), d, applyState()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, unitDir, "srv-parity-p1.mount")); err == nil {
		t.Fatal("stale parity unit kept")
	}
}

const (
	sdaID = "wwn-0x50014ee2b5d1a2c3"
	sdbID = "WDC_WD40EFRX-68N32N0_WD-WCC7K1234567"
)

func btrfsRequest(profile string) storage.BtrfsCreateRequest {
	return storage.BtrfsCreateRequest{Name: "tresor", Profile: profile, Disks: []storage.BtrfsDisk{
		{DiskID: sdaID, Device: "/dev/sda", SizeBytes: 4000787030016},
		{DiskID: sdbID, Device: "/dev/sdb", SizeBytes: 4000787030016},
	}}
}

func btrfsEnv(t *testing.T) (Deps, *sysexec.FakeRunner) {
	d, f, _ := storageEnv(t)
	f.Responses["lsblk --json --bytes -o PATH,TYPE /dev/sdb"] = sysexec.Result{Stdout: []byte(`{"blockdevices":[{"path":"/dev/sdb","type":"disk","children":[{"path":"/dev/sdb1","type":"part"}]}]}`)}
	f.Responses["blkid -s UUID -o value /dev/sda1"] = sysexec.Result{Stdout: []byte("btrfs-uuid\n")}
	return d, f
}

func TestBtrfsCreateCommandSequence(t *testing.T) {
	d, f := btrfsEnv(t)
	res, err := BtrfsCreate(context.Background(), d, btrfsRequest("raid1"))
	if err != nil {
		t.Fatal(err)
	}
	assertOrder(t, f,
		lsblkCall,
		"wipefs --all /dev/sda", "sgdisk --zap-all /dev/sda", "partprobe /dev/sda",
		"wipefs --all /dev/sdb", "sgdisk --zap-all /dev/sdb", "partprobe /dev/sdb",
		"mkfs.btrfs -f -L bnas-tresor -d raid1 -m raid1 /dev/sda1 /dev/sdb1",
		"udevadm settle",
		"blkid -s UUID -o value /dev/sda1",
	)
	if res.FSUUID != "btrfs-uuid" || res.Label != "bnas-tresor" {
		t.Fatalf("%+v", res)
	}
}

func TestBtrfsMetadataProfile(t *testing.T) {
	cases := []struct {
		profile string
		disks   int
		want    string
	}{{"single", 1, "dup"}, {"single", 3, "raid1"}, {"raid1", 2, "raid1"}, {"raid1c3", 3, "raid1c3"}, {"raid10", 4, "raid10"}}
	for _, c := range cases {
		if got := storage.BtrfsMetadataProfile(c.profile, c.disks); got != c.want {
			t.Errorf("%s/%d: %s", c.profile, c.disks, got)
		}
	}
	d, f := btrfsEnv(t)
	if _, err := BtrfsCreate(context.Background(), d, btrfsRequest("single")); err != nil {
		t.Fatal(err)
	}
	if !f.Called("mkfs.btrfs -f -L bnas-tresor -d single -m raid1 /dev/sda1 /dev/sdb1") {
		t.Fatal(f.Calls)
	}
}

func TestBtrfsCreateRejects(t *testing.T) {
	cases := map[string]func(*storage.BtrfsCreateRequest){
		"Name":     func(r *storage.BtrfsCreateRequest) { r.Name = "../x" },
		"Profil":   func(r *storage.BtrfsCreateRequest) { r.Profile = "raid5" },
		"zu wenig": func(r *storage.BtrfsCreateRequest) { r.Profile = "raid1c3" },
		"doppelt":  func(r *storage.BtrfsCreateRequest) { r.Disks[1] = r.Disks[0] },
		"System": func(r *storage.BtrfsCreateRequest) {
			r.Disks[1] = storage.BtrfsDisk{DiskID: "eui.002538b111b2c3d4", Device: "/dev/nvme0n1", SizeBytes: 512110190592}
		},
		"Gerät":     func(r *storage.BtrfsCreateRequest) { r.Disks[1].Device = "/dev/sdx" },
		"Größe":     func(r *storage.BtrfsCreateRequest) { r.Disks[1].SizeBytes = 1 },
		"unbekannt": func(r *storage.BtrfsCreateRequest) { r.Disks[1].DiskID = "gibtsnicht" },
	}
	for name, mod := range cases {
		d, f := btrfsEnv(t)
		req := btrfsRequest("raid1")
		mod(&req)
		if _, err := BtrfsCreate(context.Background(), d, req); err == nil {
			t.Errorf("%s: kein Fehler", name)
		}
		for _, c := range f.Calls {
			if strings.HasPrefix(c, "wipefs") || strings.HasPrefix(c, "mkfs") {
				t.Errorf("%s: darf nichts löschen, aber: %s", name, c)
			}
		}
	}
}

func TestBtrfsCreateStopsOnMkfsError(t *testing.T) {
	d, f := btrfsEnv(t)
	f.Responses["mkfs.btrfs -f -L bnas-tresor -d raid1 -m raid1 /dev/sda1 /dev/sdb1"] = sysexec.Result{ExitCode: 1, Stderr: []byte("kaputt")}
	if _, err := BtrfsCreate(context.Background(), d, btrfsRequest("raid1")); err == nil {
		t.Fatal("kein Fehler")
	}
	if f.Called("blkid") {
		t.Fatal("nach dem Fehler darf nichts mehr laufen")
	}
}

func TestApplyBtrfsPool(t *testing.T) {
	d, f, root := storageEnv(t)
	const u = "33333333-3333-3333-3333-333333333333"
	st := storage.State{
		Disks: []storage.DiskMount{{DiskID: "a", FSUUID: u, FSType: "btrfs"}, {DiskID: "b", FSUUID: u, FSType: "btrfs"}},
		Pools: []storage.Pool{{Name: "tresor", Type: storage.PoolBtrfs, Branches: []string{"a", "b"}, Profile: "raid1", FSUUID: u, CreatePolicy: "epmfs", MinFree: "20G"}},
	}
	res, err := Apply(context.Background(), d, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changed) != 1 || res.Changed[0] != "srv-pools-tresor.mount" {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "etc/systemd/system/srv-pools-tresor.mount")); err != nil {
		t.Fatal(err)
	}
	if !f.Called("systemctl enable --now srv-pools-tresor.mount") {
		t.Fatal(f.Calls)
	}
}

const (
	testUUID  = "3a1b2c3d-0000-4000-8000-000000000001"
	showTwo   = "Label: 'bnas-tresor'  uuid: " + testUUID + "\n\tTotal devices 2 FS bytes used 114688\n\tdevid    1 size 1000 used 100 path /dev/sda1\n\tdevid    2 size 1000 used 100 path /dev/sdb1\n"
	showGap   = "Label: 'bnas-tresor'  uuid: " + testUUID + "\n\tTotal devices 2 FS bytes used 114688\n\tdevid    1 size 1000 used 100 path /dev/sda1\n\t*** Some devices missing\n"
	usageTwo  = "Overall:\n    Device size:\t\t2000\n    Free (estimated):\t\t900\t(min: 900)\n    Data ratio:\t\t2.00\n\nData,RAID1: Size:1, Used:1 (0.02%)\nMetadata,RAID1: Size:1, Used:1 (0.04%)\n"
	statsRead = "[/dev/sda1].write_io_errs    0\n[/dev/sda1].read_io_errs     4\n[/dev/sdb1].write_io_errs    0\n"
	scrubDone = "UUID:             x\nScrub started:    Fri Nov 20 10:00:00 2026\nStatus:           finished\nDuration:         0:00:01\n\tcsum_errors: 0\n\tuncorrectable_errors: 0\n\tcorrected_errors: 0\n"
)

func TestBtrfsStatusMounted(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses["btrfs filesystem show --raw "+testUUID] = sysexec.Result{Stdout: []byte(showTwo)}
	f.Responses["findmnt --mountpoint /srv/pools/tresor"] = sysexec.Result{}
	f.Responses["btrfs device stats /srv/pools/tresor"] = sysexec.Result{Stdout: []byte(statsRead)}
	f.Responses["btrfs filesystem usage -b /srv/pools/tresor"] = sysexec.Result{Stdout: []byte(usageTwo)}
	f.Responses["btrfs scrub status -R /srv/pools/tresor"] = sysexec.Result{Stdout: []byte(scrubDone)}
	st, err := BtrfsStatus(context.Background(), d, BtrfsStatusRequest{Name: "tresor", FSUUID: testUUID})
	if err != nil {
		t.Fatal(err)
	}
	if !st.Mounted || st.Degraded || len(st.Devices) != 2 || st.Errors != 4 || st.DataProfile != "RAID1" || st.UsableBytes != 1000 || st.FreeBytes != 900 {
		t.Fatalf("%+v", st)
	}
	if st.Scrub == nil || st.Scrub.Result != "finished" || st.Devices[0].Errors.Read != 4 {
		t.Fatalf("%+v", st)
	}
}

func TestBtrfsStatusUnmountedAndMissing(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses["btrfs filesystem show --raw "+testUUID] = sysexec.Result{Stdout: []byte(showGap)}
	st, err := BtrfsStatus(context.Background(), d, BtrfsStatusRequest{Name: "tresor", FSUUID: testUUID})
	if err != nil {
		t.Fatal(err)
	}
	if st.Mounted || !st.Degraded || st.Missing != 1 || st.UsableBytes != 0 {
		t.Fatalf("%+v", st)
	}
	// Ohne eingehängten Pool werden keine Befehle auf dem Mountpoint ausgeführt.
	if f.Called("btrfs device stats") || f.Called("btrfs scrub") {
		t.Fatal(f.Calls)
	}
	// Show schlägt ganz fehl (alle Platten fort): leere Liste statt Fehler.
	f.Responses["btrfs filesystem show --raw "+testUUID] = sysexec.Result{ExitCode: 1, Stderr: []byte("ERROR: no btrfs on")}
	if st, err = BtrfsStatus(context.Background(), d, BtrfsStatusRequest{Name: "tresor", FSUUID: testUUID}); err != nil || len(st.Devices) != 0 {
		t.Fatalf("%+v %v", st, err)
	}
	for _, bad := range []BtrfsStatusRequest{{Name: "../x", FSUUID: testUUID}, {Name: "tresor", FSUUID: "a b"}} {
		if _, err := BtrfsStatus(context.Background(), d, bad); code(err) != "invalid_params" {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

func TestBtrfsScrubStartCancel(t *testing.T) {
	ctx := context.Background()
	d, f, _ := storageEnv(t)
	// nicht eingehängt
	if err := BtrfsScrub(ctx, d, BtrfsScrubRequest{Name: "tresor", Action: "start"}); code(err) != "conflict" {
		t.Fatalf("%v", err)
	}
	f.Responses["findmnt --mountpoint /srv/pools/tresor"] = sysexec.Result{}
	f.Responses["btrfs scrub status -R /srv/pools/tresor"] = sysexec.Result{Stdout: []byte(scrubDone)}
	if err := BtrfsScrub(ctx, d, BtrfsScrubRequest{Name: "tresor", Action: "start"}); err != nil {
		t.Fatal(err)
	}
	if !f.Called("systemctl start --no-block betternas-btrfs-scrub@tresor.service") {
		t.Fatal(f.Calls)
	}
	if err := BtrfsScrub(ctx, d, BtrfsScrubRequest{Name: "tresor", Action: "cancel"}); code(err) != "conflict" {
		t.Fatalf("Abbrechen ohne Lauf: %v", err)
	}
	f.Responses["btrfs scrub status -R /srv/pools/tresor"] = sysexec.Result{Stdout: []byte("Status:           running\n")}
	if err := BtrfsScrub(ctx, d, BtrfsScrubRequest{Name: "tresor", Action: "start"}); code(err) != "conflict" {
		t.Fatalf("doppelter Start: %v", err)
	}
	if err := BtrfsScrub(ctx, d, BtrfsScrubRequest{Name: "tresor", Action: "cancel"}); err != nil || !f.Called("btrfs scrub cancel /srv/pools/tresor") {
		t.Fatalf("%v %v", err, f.Calls)
	}
	if err := BtrfsScrub(ctx, d, BtrfsScrubRequest{Name: "tresor", Action: "x"}); code(err) != "invalid_params" {
		t.Fatalf("%v", err)
	}
}

func TestApplyEnablesScrubTimerAndRemovesIt(t *testing.T) {
	d, f, _ := storageEnv(t)
	const u = "33333333-3333-3333-3333-333333333333"
	st := storage.State{
		Disks: []storage.DiskMount{{DiskID: "a", FSUUID: u, FSType: "btrfs"}, {DiskID: "b", FSUUID: u, FSType: "btrfs"}},
		Pools: []storage.Pool{{Name: "tresor", Type: storage.PoolBtrfs, Branches: []string{"a", "b"}, Profile: "raid1", FSUUID: u, CreatePolicy: "epmfs", MinFree: "20G"}},
	}
	if _, err := Apply(context.Background(), d, st); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, f, "systemctl enable --now srv-pools-tresor.mount", "systemctl enable --now betternas-btrfs-scrub@tresor.timer")
	// Unverändert: Timer wird trotzdem sichergestellt (idempotent).
	f.Calls = nil
	if _, err := Apply(context.Background(), d, st); err != nil {
		t.Fatal(err)
	}
	if !f.Called("systemctl enable --now betternas-btrfs-scrub@tresor.timer") {
		t.Fatal(f.Calls)
	}
	// Pool entfernt: Timer aus.
	f.Calls = nil
	if _, err := Apply(context.Background(), d, storage.State{}); err != nil {
		t.Fatal(err)
	}
	if !f.Called("systemctl disable --now betternas-btrfs-scrub@tresor.timer") {
		t.Fatal(f.Calls)
	}
	// mergerfs-Pools bekommen keinen Timer.
	f.Calls = nil
	if _, err := Apply(context.Background(), d, testStateMergerfs()); err != nil {
		t.Fatal(err)
	}
	if f.Called("systemctl enable --now betternas-btrfs-scrub") {
		t.Fatal(f.Calls)
	}
}

func testStateMergerfs() storage.State {
	return storage.State{
		Disks: []storage.DiskMount{{DiskID: "a", FSUUID: "11111111-1111-1111-1111-111111111111", FSType: "ext4"}},
		Pools: []storage.Pool{{Name: "media", Branches: []string{"a"}, CreatePolicy: "epmfs", MinFree: "20G"}},
	}
}
