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
	f.Responses["smartctl --json -H -A /dev/sda"] = sysexec.Result{ExitCode: 4, Stdout: []byte(smartJSON)}
	s, err := Smart(context.Background(), d, "/dev/sda")
	if err != nil || !s.Passed || s.TemperatureC != 34 || s.PowerOnHours != 12034 {
		t.Fatalf("%+v %v", s, err)
	}
	f.Responses["smartctl --json -H -A /dev/sda"] = sysexec.Result{ExitCode: 2}
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
	if f.Called("chattr +i /srv/pools/media") {
		t.Fatal("pool mountpoint must not be made immutable")
	}
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
