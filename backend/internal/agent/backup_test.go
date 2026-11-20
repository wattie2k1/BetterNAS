package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/sysexec"
)

func backupFakeEnv(t *testing.T, realdir string) (Deps, *sysexec.FakeRunner) {
	t.Helper()
	d, f, _ := dockerEnv(t) // legt auch Legacy-Pfade fest (appdata = /srv/appdata)
	f.Hook = func(call string) (sysexec.Result, bool) {
		switch {
		case strings.HasPrefix(call, "realpath -e -- "):
			return sysexec.Result{Stdout: []byte(realdir + "\n")}, true
		case strings.HasPrefix(call, "df --output=avail -B1 "):
			return sysexec.Result{Stdout: []byte("Avail\n5000000000\n")}, true
		case strings.HasPrefix(call, "du -sb -- "):
			return sysexec.Result{Stdout: []byte("1000000\t/srv/appdata\n")}, true
		case strings.HasPrefix(call, "stat -c %s -- "):
			return sysexec.Result{Stdout: []byte("4242\n")}, true
		}
		return sysexec.Result{}, false
	}
	return d, f
}

func TestBackupPrepare(t *testing.T) {
	d, f := backupFakeEnv(t, "/mnt/ssd/sicherung")
	ctx := context.Background()
	tg, err := BackupPrepare(ctx, d, "/mnt/ssd/sicherung/")
	if err != nil {
		t.Fatal(err)
	}
	if tg.Dir != "/mnt/ssd/sicherung" || tg.Free != 5000000000 || tg.Appdata != "/srv/appdata" {
		t.Fatalf("%+v", tg)
	}
	for _, want := range []string{
		"mkdir -p -m 755 -- /mnt/ssd/sicherung",
		"install -m 0640 -o root -g betternas /dev/null /mnt/ssd/sicherung/.betternas-probe",
		"rm -f -- /mnt/ssd/sicherung/.betternas-probe",
	} {
		if !f.Called(want) {
			t.Errorf("fehlt: %s\n%v", want, f.Calls)
		}
	}
	for _, bad := range []string{"/etc/x", "relativ", "/mnt", "/mnt/a b", "/srv/pools"} {
		if _, err := BackupPrepare(ctx, d, bad); apierr.From(err).Code != apierr.InvalidParams {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestBackupDirResolvingIntoSystemIsRejected(t *testing.T) {
	d, _ := backupFakeEnv(t, "/etc/geheim")
	if _, err := BackupPrepare(context.Background(), d, "/mnt/link/x"); apierr.From(err).Code != apierr.InvalidParams {
		t.Fatalf("%v", err)
	}
}

func TestBackupPrepareProbeFailure(t *testing.T) {
	d, f := backupFakeEnv(t, "/mnt/nfs/b")
	prev := f.Hook
	f.Hook = func(call string) (sysexec.Result, bool) {
		if strings.HasPrefix(call, "install ") {
			return sysexec.Result{ExitCode: 1, Stderr: []byte("Operation not permitted")}, true
		}
		return prev(call)
	}
	_, err := BackupPrepare(context.Background(), d, "/mnt/nfs/b")
	if apierr.From(err).Code != apierr.InvalidParams || !strings.Contains(apierr.From(err).Message, "lassen sich keine Sicherungen ablegen") {
		t.Fatalf("%v", err)
	}
}

func TestBackupStore(t *testing.T) {
	d, f := backupFakeEnv(t, "/mnt/ssd/b")
	ctx := context.Background()
	src := "/var/lib/betternas/staging/betternas-backup-20261120-020000.tar.gz"
	if err := BackupStore(ctx, d, src, "/mnt/ssd/b"); err != nil {
		t.Fatal(err)
	}
	i1 := f.Index("install -m 0640 -o root -g betternas -- " + src + " /mnt/ssd/b/.betternas-backup-20261120-020000.tar.gz.part")
	i2 := f.Index("mv -f -- /mnt/ssd/b/.betternas-backup-20261120-020000.tar.gz.part /mnt/ssd/b/betternas-backup-20261120-020000.tar.gz")
	i3 := f.Index("rm -f -- " + src)
	if i1 < 0 || i2 < i1 || i3 < i2 {
		t.Fatalf("Reihenfolge: %d %d %d\n%v", i1, i2, i3, f.Calls)
	}
	for _, bad := range []string{
		"/etc/passwd",
		"/var/lib/betternas/betternas.db",
		"/var/lib/betternas/staging/../betternas-backup-20261120-020000.tar.gz",
		"/var/lib/betternas/staging/x.tar.gz",
		"/var/lib/betternas/staging/betternas-appdata-20261120-020000.tar.gz",
	} {
		if err := BackupStore(ctx, d, bad, "/mnt/ssd/b"); apierr.From(err).Code != apierr.InvalidParams {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestBackupDelete(t *testing.T) {
	d, f := backupFakeEnv(t, "/mnt/ssd/b")
	ctx := context.Background()
	if err := BackupDelete(ctx, d, "/mnt/ssd/b", "betternas-appdata-20261120-020000.tar.gz"); err != nil {
		t.Fatal(err)
	}
	if !f.Called("rm -- /mnt/ssd/b/betternas-appdata-20261120-020000.tar.gz") {
		t.Fatal(f.Calls)
	}
	for _, bad := range []string{"../x", "foo.tar.gz", "", "betternas-backup-20261120-020000.tar.gz/../../x"} {
		if err := BackupDelete(ctx, d, "/mnt/ssd/b", bad); apierr.From(err).Code != apierr.InvalidParams {
			t.Errorf("%q: %v", bad, err)
		}
	}
	prev := f.Hook
	f.Hook = func(call string) (sysexec.Result, bool) {
		if strings.HasPrefix(call, "test -f ") {
			return sysexec.Result{ExitCode: 1}, true
		}
		return prev(call)
	}
	if err := BackupDelete(ctx, d, "/mnt/ssd/b", "betternas-backup-20261120-020000.tar.gz"); apierr.From(err).Code != apierr.NotFound {
		t.Fatal(err)
	}
}

func TestBackupPruneKeepsNewestPerKind(t *testing.T) {
	d, f := backupFakeEnv(t, "/mnt/ssd/b")
	prev := f.Hook
	f.Hook = func(call string) (sysexec.Result, bool) {
		if strings.HasPrefix(call, "find /mnt/ssd/b -maxdepth 1 -type f") {
			return sysexec.Result{Stdout: []byte(strings.Join([]string{
				"betternas-backup-20261101-020000.tar.gz", "betternas-backup-20261102-020000.tar.gz", "betternas-backup-20261103-020000.tar.gz",
				"betternas-appdata-20261101-023000.tar.gz", "betternas-appdata-20261102-023000.tar.gz",
				"fremd.txt", ".betternas-backup-20261101-020000.tar.gz.part", "",
			}, "\n"))}, true
		}
		return prev(call)
	}
	if err := BackupPrune(context.Background(), d, "/mnt/ssd/b", 2); err != nil {
		t.Fatal(err)
	}
	var removed []string
	for _, c := range f.Calls {
		if strings.HasPrefix(c, "rm -f -- ") {
			removed = append(removed, strings.TrimPrefix(c, "rm -f -- "))
		}
	}
	if len(removed) != 1 || removed[0] != "/mnt/ssd/b/betternas-backup-20261101-020000.tar.gz" {
		t.Fatalf("%v", removed)
	}
	if err := BackupPrune(context.Background(), d, "/mnt/ssd/b", 0); apierr.From(err).Code != apierr.InvalidParams {
		t.Fatal(err)
	}
}

func TestBackupAppdata(t *testing.T) {
	d, f := backupFakeEnv(t, "/mnt/ssd/b")
	d.FS.MkdirAll("/srv/appdata", 0o755)
	res, err := BackupAppdata(context.Background(), d, "/mnt/ssd/b")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Name, "betternas-appdata-") || res.Size != 4242 || res.Changed {
		t.Fatalf("%+v", res)
	}
	var tarCall string
	for _, c := range f.Calls {
		if strings.HasPrefix(c, "tar ") {
			tarCall = c
		}
	}
	part := "/mnt/ssd/b/." + res.Name + ".part"
	if tarCall != "tar --numeric-owner -czpf "+part+" -C /srv/appdata ." {
		t.Fatalf("%q", tarCall)
	}
	i1, i2, i3 := f.Index("chown root:betternas -- "+part), f.Index("chmod 0640 -- "+part), f.Index("mv -f -- "+part+" /mnt/ssd/b/"+res.Name)
	if i1 < 0 || i2 < i1 || i3 < i2 {
		t.Fatalf("Reihenfolge %d %d %d", i1, i2, i3)
	}
}

func TestBackupAppdataExcludesTargetInsideAppdata(t *testing.T) {
	d, f := backupFakeEnv(t, "/srv/appdata/sicherung")
	d.FS.MkdirAll("/srv/appdata", 0o755)
	if _, err := BackupAppdata(context.Background(), d, "/srv/appdata/sicherung"); err != nil {
		t.Fatal(err)
	}
	if !f.Called("tar --numeric-owner -czpf") {
		t.Fatal(f.Calls)
	}
	found := false
	for _, c := range f.Calls {
		if strings.HasPrefix(c, "tar ") && strings.Contains(c, "--exclude=./sicherung") {
			found = true
		}
	}
	if !found {
		t.Fatal(f.Calls)
	}
}

func TestBackupAppdataFailures(t *testing.T) {
	ctx := context.Background()
	// zu wenig Platz
	d, f := backupFakeEnv(t, "/mnt/ssd/b")
	d.FS.MkdirAll("/srv/appdata", 0o755)
	prev := f.Hook
	f.Hook = func(call string) (sysexec.Result, bool) {
		if strings.HasPrefix(call, "df ") {
			return sysexec.Result{Stdout: []byte("Avail\n1000\n")}, true
		}
		return prev(call)
	}
	if _, err := BackupAppdata(ctx, d, "/mnt/ssd/b"); apierr.From(err).Code != apierr.Conflict {
		t.Fatalf("Platz: %v", err)
	}
	// tar scheitert → .part wird entfernt, nichts umbenannt
	d, f = backupFakeEnv(t, "/mnt/ssd/b")
	d.FS.MkdirAll("/srv/appdata", 0o755)
	prev = f.Hook
	f.Hook = func(call string) (sysexec.Result, bool) {
		if strings.HasPrefix(call, "tar ") {
			return sysexec.Result{ExitCode: 2, Stderr: []byte("Cannot write")}, true
		}
		return prev(call)
	}
	if _, err := BackupAppdata(ctx, d, "/mnt/ssd/b"); apierr.From(err).Code != apierr.Internal {
		t.Fatalf("tar: %v", err)
	}
	if f.Called("mv ") || !f.Called("rm -f -- /mnt/ssd/b/.betternas-appdata-") {
		t.Fatal(f.Calls)
	}
	// Dateien geändert (Exit 1) ist kein Fehler
	d, f = backupFakeEnv(t, "/mnt/ssd/b")
	d.FS.MkdirAll("/srv/appdata", 0o755)
	prev = f.Hook
	f.Hook = func(call string) (sysexec.Result, bool) {
		if strings.HasPrefix(call, "tar ") {
			return sysexec.Result{ExitCode: 1}, true
		}
		return prev(call)
	}
	if r, err := BackupAppdata(ctx, d, "/mnt/ssd/b"); err != nil || !r.Changed {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestBackupAppdataNeedsDocker(t *testing.T) {
	d, _ := backupFakeEnv(t, "/mnt/ssd/b")
	curPaths.Store(nil)
	if _, err := BackupAppdata(context.Background(), d, "/mnt/ssd/b"); apierr.From(err).Code != apierr.DockerNotConfigured {
		t.Fatal(err)
	}
}
