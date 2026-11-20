package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/storage"
	"github.com/betternas/betternas/internal/sysexec"
)

const opShowCall = "systemctl show -p LoadState -p ActiveState -p SubState -p Result -p Description betternas-btrfs-op-tresor.service"

func mountedEnv(t *testing.T) (Deps, *sysexec.FakeRunner) {
	d, f := btrfsEnv(t)
	f.Responses["findmnt --mountpoint /srv/pools/tresor"] = sysexec.Result{}
	return d, f
}

func opShow(f *sysexec.FakeRunner, props string) {
	f.Responses[opShowCall] = sysexec.Result{Stdout: []byte(props)}
}

func TestBtrfsOpStartCommands(t *testing.T) {
	cases := map[string]struct {
		req  storage.BtrfsOpRequest
		want string
	}{
		"balance voll":   {storage.BtrfsOpRequest{Name: "tresor", Op: "balance"}, "balance start /srv/pools/tresor"},
		"balance Usage":  {storage.BtrfsOpRequest{Name: "tresor", Op: "balance", Usage: 60}, "balance start -dusage=60 -musage=60 /srv/pools/tresor"},
		"convert":        {storage.BtrfsOpRequest{Name: "tresor", Op: "convert", Profile: "raid1", Metadata: "raid1"}, "balance start -f -dconvert=raid1 -mconvert=raid1 /srv/pools/tresor"},
		"convert dup":    {storage.BtrfsOpRequest{Name: "tresor", Op: "convert", Profile: "single", Metadata: "dup"}, "balance start -f -dconvert=single -mconvert=dup /srv/pools/tresor"},
		"remove":         {storage.BtrfsOpRequest{Name: "tresor", Op: "remove", DevID: 2}, "device remove 2 /srv/pools/tresor"},
		"remove missing": {storage.BtrfsOpRequest{Name: "tresor", Op: "remove", Missing: true}, "device remove missing /srv/pools/tresor"},
	}
	for name, c := range cases {
		d, f := mountedEnv(t)
		opShow(f, "LoadState=not-found\n")
		if err := BtrfsOpStart(context.Background(), d, c.req); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := "systemd-run --unit=betternas-btrfs-op-tresor.service --description=BetterNAS btrfs " + c.req.Op + " --property=RemainAfterExit=yes /usr/bin/btrfs " + c.want
		if !f.Called(want) {
			t.Errorf("%s: erwartet %q in\n%s", name, want, strings.Join(f.Calls, "\n"))
		}
		assertOrder(t, f, "systemctl stop betternas-btrfs-op-tresor.service", "systemctl reset-failed betternas-btrfs-op-tresor.service", "systemd-run")
	}
}

func TestBtrfsOpStartRejects(t *testing.T) {
	bad := map[string]storage.BtrfsOpRequest{
		"Name":        {Name: "../x", Op: "balance"},
		"Op":          {Name: "tresor", Op: "format"},
		"Usage":       {Name: "tresor", Op: "balance", Usage: 101},
		"Profil":      {Name: "tresor", Op: "convert", Profile: "raid5", Metadata: "raid5"},
		"Metadaten":   {Name: "tresor", Op: "convert", Profile: "raid1", Metadata: "x; rm"},
		"ohne Platte": {Name: "tresor", Op: "remove"},
	}
	for name, req := range bad {
		d, f := mountedEnv(t)
		opShow(f, "LoadState=not-found\n")
		if err := BtrfsOpStart(context.Background(), d, req); err == nil {
			t.Errorf("%s: kein Fehler", name)
		}
		if f.Called("systemd-run") {
			t.Errorf("%s: darf nichts starten", name)
		}
	}
	// nicht eingehängt
	d, f, _ := storageEnv(t)
	if err := BtrfsOpStart(context.Background(), d, storage.BtrfsOpRequest{Name: "tresor", Op: "balance"}); code(err) != "conflict" || f.Called("systemd-run") {
		t.Fatalf("%v", err)
	}
	// läuft schon
	d, f = mountedEnv(t)
	opShow(f, "LoadState=loaded\nActiveState=active\nSubState=running\nResult=success\nDescription=BetterNAS btrfs balance\n")
	if err := BtrfsOpStart(context.Background(), d, storage.BtrfsOpRequest{Name: "tresor", Op: "balance"}); code(err) != "conflict" || f.Called("systemd-run") {
		t.Fatalf("%v", err)
	}
}

func TestBtrfsOpStatus(t *testing.T) {
	ctx := context.Background()
	d, f := mountedEnv(t)
	if op, _ := BtrfsOpStatus(ctx, d, "tresor"); op.Op != "" || op.Running {
		t.Fatalf("nie gelaufen: %+v", op)
	}
	opShow(f, "LoadState=not-found\n")
	if op, _ := BtrfsOpStatus(ctx, d, "tresor"); op.Op != "" || op.Progress != -1 {
		t.Fatalf("%+v", op)
	}
	opShow(f, "LoadState=loaded\nActiveState=active\nSubState=running\nResult=success\nDescription=BetterNAS btrfs balance\n")
	f.Responses["/usr/bin/btrfs balance status /srv/pools/tresor"] = sysexec.Result{Stdout: []byte("Balance on '/srv/pools/tresor' is running\n2 out of about 4 chunks balanced (2 considered),  50% left\n")}
	if op, _ := BtrfsOpStatus(ctx, d, "tresor"); !op.Running || op.Op != "balance" || op.Progress != 50 {
		t.Fatalf("%+v", op)
	}
	opShow(f, "LoadState=loaded\nActiveState=active\nSubState=exited\nResult=success\nDescription=BetterNAS btrfs remove\n")
	if op, _ := BtrfsOpStatus(ctx, d, "tresor"); op.Running || op.Result != "ok" || op.Op != "remove" || op.Progress != 100 {
		t.Fatalf("%+v", op)
	}
	opShow(f, "LoadState=loaded\nActiveState=failed\nSubState=failed\nResult=exit-code\nDescription=BetterNAS btrfs remove\n")
	f.Responses["journalctl -u betternas-btrfs-op-tresor.service"] = sysexec.Result{Stdout: []byte("ERROR: error removing device: No space left on device\n")}
	if op, _ := BtrfsOpStatus(ctx, d, "tresor"); op.Result != "failed" || !strings.Contains(op.Message, "No space left") {
		t.Fatalf("%+v", op)
	}
}

func TestBtrfsReplaceStart(t *testing.T) {
	d, f := mountedEnv(t)
	opShow(f, "LoadState=not-found\n")
	req := storage.BtrfsReplaceRequest{Name: "tresor", FSUUID: testUUID, DevID: 2, Target: storage.BtrfsDisk{DiskID: sdbID, Device: "/dev/sdb", SizeBytes: 4000787030016}}
	if err := BtrfsReplaceStart(context.Background(), d, req); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, f, lsblkCall, "wipefs --all /dev/sdb", "sgdisk --zap-all /dev/sdb", "partprobe /dev/sdb", "/usr/bin/btrfs replace start -f 2 /dev/sdb1 /srv/pools/tresor")

	// Ungültige Eingaben und geänderte Platte: nichts wird gelöscht.
	for name, mod := range map[string]func(*storage.BtrfsReplaceRequest){
		"devid": func(r *storage.BtrfsReplaceRequest) { r.DevID = 0 },
		"uuid":  func(r *storage.BtrfsReplaceRequest) { r.FSUUID = "a b" },
		"Gerät": func(r *storage.BtrfsReplaceRequest) { r.Target.Device = "/dev/sdx" },
		"System": func(r *storage.BtrfsReplaceRequest) {
			r.Target = storage.BtrfsDisk{DiskID: "eui.002538b111b2c3d4", Device: "/dev/nvme0n1", SizeBytes: 512110190592}
		},
	} {
		d, f := mountedEnv(t)
		opShow(f, "LoadState=not-found\n")
		r := req
		mod(&r)
		if err := BtrfsReplaceStart(context.Background(), d, r); err == nil {
			t.Errorf("%s: kein Fehler", name)
		}
		if f.Called("wipefs") || f.Called("/usr/bin/btrfs replace start") {
			t.Errorf("%s: darf nichts löschen", name)
		}
	}
	// Ersatz läuft schon
	d, f = mountedEnv(t)
	opShow(f, "LoadState=not-found\n")
	f.Responses["/usr/bin/btrfs replace status -1 /srv/pools/tresor"] = sysexec.Result{Stdout: []byte("5.0% done, 0 write errs, 0 uncorr. read errs\n")}
	if err := BtrfsReplaceStart(context.Background(), d, req); code(err) != "conflict" || f.Called("wipefs") {
		t.Fatalf("%v", err)
	}
}

func TestBtrfsDeviceAddAndResize(t *testing.T) {
	d, f := mountedEnv(t)
	opShow(f, "LoadState=not-found\n")
	if err := BtrfsDeviceAdd(context.Background(), d, storage.BtrfsAddRequest{Name: "tresor", Target: storage.BtrfsDisk{DiskID: sdbID, Device: "/dev/sdb", SizeBytes: 4000787030016}}); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, f, "wipefs --all /dev/sdb", "/usr/bin/btrfs device add -f /dev/sdb1 /srv/pools/tresor")
	if err := BtrfsResize(context.Background(), d, storage.BtrfsResizeRequest{Name: "tresor", DevID: 3}); err != nil || !f.Called("/usr/bin/btrfs filesystem resize 3:max /srv/pools/tresor") {
		t.Fatalf("%v %v", err, f.Calls)
	}
	if err := BtrfsResize(context.Background(), d, storage.BtrfsResizeRequest{Name: "tresor", DevID: 0}); err == nil {
		t.Fatal("devid 0 muss scheitern")
	}
}

func TestBtrfsMountDegraded(t *testing.T) {
	d, f, _ := storageEnv(t)
	req := storage.BtrfsMountRequest{Name: "tresor", FSUUID: testUUID, Compress: "zstd:1"}
	if err := BtrfsMountDegraded(context.Background(), d, req); err != nil {
		t.Fatal(err)
	}
	if !f.Called("mount -t btrfs -o noatime,nofail,compress=zstd:1,degraded /dev/disk/by-uuid/" + testUUID + " /srv/pools/tresor") {
		t.Fatal(f.Calls)
	}
	for _, bad := range []storage.BtrfsMountRequest{{Name: "../x", FSUUID: testUUID}, {Name: "tresor", FSUUID: "a b"}, {Name: "tresor", FSUUID: testUUID, Compress: "lzo"}} {
		if err := BtrfsMountDegraded(context.Background(), d, bad); code(err) != "invalid_params" {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	d, f = mountedEnv(t)
	if err := BtrfsMountDegraded(context.Background(), d, req); code(err) != "conflict" || f.Called("mount -t") {
		t.Fatalf("schon eingehängt: %v", err)
	}
}

func TestBtrfsResetStats(t *testing.T) {
	d, f := mountedEnv(t)
	if err := BtrfsResetStats(context.Background(), d, "tresor"); err != nil || !f.Called("/usr/bin/btrfs device stats -z /srv/pools/tresor") {
		t.Fatalf("%v %v", err, f.Calls)
	}
}
