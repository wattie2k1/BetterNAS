package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/docker"
	"github.com/betternas/betternas/internal/sysexec"
)

const (
	vmPath = "/srv/appdata/docker/volumes/media_data/_data"
	bkDir  = "/srv/appdata/backups/stacks/media"
)

type backupFake struct {
	cfg   string            // Ausgabe von `compose config --format json`
	sizes map[string]string // Pfad → Bytes
	free  string
	cur   string // lokale Image-ID von web:1
	root  string
}

func backupEnv(t *testing.T, b *backupFake) (Deps, *sysexec.FakeRunner) {
	t.Helper()
	d, f, _ := dockerEnv(t)
	saveStack(t, d, "media")
	if b.free == "" {
		b.free = "10000000000"
	}
	if b.cur == "" {
		b.cur = idOld
	}
	if b.root == "" {
		b.root = "/srv/appdata/docker"
	}
	f.Hook = func(call string) (sysexec.Result, bool) {
		switch {
		case strings.HasSuffix(call, "config --images"):
			return sysexec.Result{Stdout: []byte("web:1\n")}, true
		case strings.HasSuffix(call, "config --format json"):
			return sysexec.Result{Stdout: []byte(b.cfg)}, true
		case call == "docker image inspect --format {{.Id}} web:1":
			return sysexec.Result{Stdout: []byte(b.cur + "\n")}, true
		case call == "docker info --format {{.DockerRootDir}}":
			return sysexec.Result{Stdout: []byte(b.root + "\n")}, true
		case strings.HasPrefix(call, "docker volume inspect --format {{.Mountpoint}} "):
			vol := strings.TrimPrefix(call, "docker volume inspect --format {{.Mountpoint}} ")
			if vol == "media_missing" {
				return sysexec.Result{ExitCode: 1}, true
			}
			return sysexec.Result{Stdout: []byte(b.root + "/volumes/" + vol + "/_data\n")}, true
		case strings.HasPrefix(call, "du -sb -- "):
			var out strings.Builder
			for _, p := range strings.Fields(strings.TrimPrefix(call, "du -sb -- ")) {
				if n, ok := b.sizes[p]; ok {
					out.WriteString(n + "\t" + p + "\n")
				}
			}
			return sysexec.Result{Stdout: []byte(out.String())}, true
		case strings.HasPrefix(call, "df --output=avail -B1 "):
			return sysexec.Result{Stdout: []byte("Avail\n" + b.free + "\n")}, true
		}
		return sysexec.Result{}, false
	}
	return d, f
}

const planCfg = `{
 "services": {
  "app": {"volumes": [
    {"type":"bind","source":"/srv/appdata/media/conf","target":"/conf"},
    {"type":"bind","source":"/srv/appdata/media/conf/sub","target":"/sub"},
    {"type":"bind","source":"/srv/pools/tank/Medien","target":"/media"},
    {"type":"bind","source":"/srv/appdata/media/ro","target":"/ro","read_only":true},
    {"type":"bind","source":"/srv/appdata/stacks/other","target":"/x"},
    {"type":"bind","source":"/srv/appdata/docker/volumes","target":"/y"},
    {"type":"bind","source":"/srv/appdata","target":"/z"},
    {"type":"bind","source":"/etc/localtime","target":"/etc/localtime","read_only":true},
    {"type":"volume","source":"data","target":"/data"},
    {"type":"volume","source":"ext","target":"/ext"},
    {"type":"volume","source":"gone","target":"/gone"},
    {"type":"tmpfs","target":"/tmp"}
  ]},
  "db": {"volumes": [{"type":"volume","source":"data","target":"/again"}]}
 },
 "volumes": {"data":{"name":"media_data"},"ext":{"name":"shared","external":true},"gone":{"name":"media_missing"}}
}`

func TestBackupPlanCollectsOnlyAppData(t *testing.T) {
	d, _ := backupEnv(t, &backupFake{cfg: planCfg, sizes: map[string]string{"/srv/appdata/media/conf": "1000", vmPath: "5000"}})
	plan, err := StackBackupPlan(context.Background(), d, "media")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range plan.Sources {
		got = append(got, s.Kind+":"+s.Path)
	}
	want := "bind:/srv/appdata/media/conf,volume:" + vmPath
	if strings.Join(got, ",") != want {
		t.Fatalf("Quellen %v, erwartet %s", got, want)
	}
	if plan.TotalBytes != 6000 || !plan.Possible || plan.FreeBytes != 10000000000 {
		t.Fatalf("%+v", plan)
	}
	if plan.Sources[1].Name != "media_data" || plan.Sources[0].Name != "/srv/appdata/media/conf" {
		t.Fatalf("Namen: %+v", plan.Sources)
	}
}

func TestBackupPlanNotPossible(t *testing.T) {
	one := `{"services":{"a":{"volumes":[{"type":"bind","source":"/srv/appdata/media/c","target":"/c"}]}}}`
	// zu wenig Platz
	d, _ := backupEnv(t, &backupFake{cfg: one, sizes: map[string]string{"/srv/appdata/media/c": "9000000000"}, free: "9500000000"})
	plan, err := StackBackupPlan(context.Background(), d, "media")
	if err != nil || plan.Possible || !strings.Contains(plan.Reason, "Platz") {
		t.Fatalf("%+v %v", plan, err)
	}
	// nichts zu sichern
	none := `{"services":{"a":{"volumes":[{"type":"bind","source":"/srv/pools/x","target":"/c"}]}}}`
	d2, _ := backupEnv(t, &backupFake{cfg: none})
	plan, err = StackBackupPlan(context.Background(), d2, "media")
	if err != nil || plan.Possible || len(plan.Sources) != 0 || !strings.Contains(plan.Reason, "keine") {
		t.Fatalf("%+v %v", plan, err)
	}
}

func TestBackupPlanRejectsVolumeOutsideDockerRoot(t *testing.T) {
	cfg := `{"services":{"a":{"volumes":[{"type":"volume","source":"data","target":"/d"}]}},"volumes":{"data":{"name":"media_data"}}}`
	d, f := backupEnv(t, &backupFake{cfg: cfg})
	// Der Mountpoint passt nicht zu DockerRootDir/volumes/<name>/_data.
	base := f.Hook
	f.Hook = func(call string) (sysexec.Result, bool) {
		if strings.HasPrefix(call, "docker volume inspect") {
			return sysexec.Result{Stdout: []byte("/etc/_data\n")}, true
		}
		return base(call)
	}
	plan, err := StackBackupPlan(context.Background(), d, "media")
	if err != nil || len(plan.Sources) != 0 {
		t.Fatalf("%+v %v", plan, err)
	}
}

func simpleBackup() *backupFake {
	cfg := `{"services":{"a":{"volumes":[{"type":"bind","source":"/srv/appdata/media/conf","target":"/c"},{"type":"volume","source":"data","target":"/d"}]}},"volumes":{"data":{"name":"media_data"}}}`
	return &backupFake{cfg: cfg, sizes: map[string]string{"/srv/appdata/media/conf": "100", vmPath: "200"}}
}

func TestUpdateWithBackupScriptOrder(t *testing.T) {
	d, f := backupEnv(t, simpleBackup())
	unit, err := RunStackOpts(context.Background(), d, "media", "update", docker.RunOptions{Backup: true})
	if err != nil || unit == "" {
		t.Fatalf("%q %v", unit, err)
	}
	i := f.Index("systemd-run")
	if i < 0 {
		t.Fatalf("%v", f.Calls)
	}
	script := f.Calls[i]
	pull, stop, tar, up := strings.Index(script, "'pull'"), strings.Index(script, "'stop'"), strings.Index(script, "'tar'"), strings.LastIndex(script, "'up'")
	if !(pull >= 0 && pull < stop && stop < tar && tar < up) {
		t.Fatalf("Reihenfolge pull<stop<tar<up verletzt (%d %d %d %d): %s", pull, stop, tar, up, script)
	}
	for _, want := range []string{
		"'-C' '/'", "'-czf' '" + bkDir + "/1700000000.tar.gz.part'", "srv/appdata/media/conf", "srv/appdata/docker/volumes/media_data/_data",
		"'start'", // bei fehlgeschlagener Sicherung wird der Stack wieder gestartet
	} {
		if !strings.Contains(script, want) {
			t.Errorf("Skript ohne %q:\n%s", want, script)
		}
	}
	// Das Manifest liegt neben dem Archiv und nennt die Quellen.
	b, err := d.FS.ReadFile(bkDir + "/1700000000.json")
	var m docker.DataBackup
	if err != nil || json.Unmarshal(b, &m) != nil || m.ID != "1700000000" || len(m.Sources) != 2 {
		t.Fatalf("Manifest: %s %v", b, err)
	}
	// Der Rollback-Stand merkt sich die Sicherung.
	pend, ok := readRollback(d, "media", rollbackPending)
	if !ok || pend.Backup != "1700000000" {
		t.Fatalf("pending: %+v %v", pend, ok)
	}
}

func TestUpdateWithBackupRefusedWhenImpossible(t *testing.T) {
	b := simpleBackup()
	b.free = "10"
	d, f := backupEnv(t, b)
	_, err := RunStackOpts(context.Background(), d, "media", "update", docker.RunOptions{Backup: true})
	if code(err) != apierr.Conflict || !strings.Contains(err.Error(), "Platz") {
		t.Fatalf("%v", err)
	}
	if f.Index("systemd-run") >= 0 {
		t.Fatal("Update darf nicht starten")
	}
	// Ohne Sicherungswunsch läuft das Update wie bisher.
	if _, err := RunStackOpts(context.Background(), d, "media", "update", docker.RunOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestOptionsOnlyForMatchingActions(t *testing.T) {
	d, _ := backupEnv(t, simpleBackup())
	ctx := context.Background()
	if _, err := RunStackOpts(ctx, d, "media", "up", docker.RunOptions{Backup: true}); code(err) != apierr.InvalidParams {
		t.Errorf("Backup bei up: %v", err)
	}
	if _, err := RunStackOpts(ctx, d, "media", "update", docker.RunOptions{RestoreData: true}); code(err) != apierr.InvalidParams {
		t.Errorf("RestoreData bei update: %v", err)
	}
}

// madeBackup führt ein Update mit Sicherung aus, lässt das Image wechseln und übernimmt den Stand.
func madeBackup(t *testing.T, b *backupFake) (Deps, *sysexec.FakeRunner) {
	t.Helper()
	d, f := backupEnv(t, b)
	ctx := context.Background()
	if _, err := RunStackOpts(ctx, d, "media", "update", docker.RunOptions{Backup: true}); err != nil {
		t.Fatal(err)
	}
	// Die Sicherung gilt erst als vorhanden, wenn das Archiv da ist (das schreibt in echt die Unit).
	if err := d.FS.WriteFile(bkDir+"/1700000000.tar.gz", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.cur = idNew
	if err := StackRollbackCommit(ctx, d, "media"); err != nil {
		t.Fatal(err)
	}
	return d, f
}

func TestCommitKeepsBackupWithRollbackState(t *testing.T) {
	d, _ := madeBackup(t, simpleBackup())
	list, _ := ListStacks(context.Background(), d)
	if len(list) != 1 || list[0].Rollback == "" || !list[0].RollbackData {
		t.Fatalf("%+v", list)
	}
	rb, ok := readRollback(d, "media", rollbackFile)
	if !ok || rb.Backup != "1700000000" {
		t.Fatalf("%+v %v", rb, ok)
	}
	if !d.FS.Exists(bkDir + "/1700000000.tar.gz") {
		t.Fatal("Archiv fehlt")
	}
}

func TestCommitWithoutImageChangeDropsBackup(t *testing.T) {
	d, _ := backupEnv(t, simpleBackup())
	ctx := context.Background()
	_, _ = RunStackOpts(ctx, d, "media", "update", docker.RunOptions{Backup: true})
	_ = d.FS.WriteFile(bkDir+"/1700000000.tar.gz", []byte("x"), 0o600)
	if err := StackRollbackCommit(ctx, d, "media"); err != nil { // Image unverändert
		t.Fatal(err)
	}
	if d.FS.Exists(bkDir+"/1700000000.tar.gz") || d.FS.Exists(bkDir+"/1700000000.json") {
		t.Fatal("überflüssige Sicherung bleibt liegen")
	}
}

func TestClearAndDeleteRemoveBackup(t *testing.T) {
	d, _ := madeBackup(t, simpleBackup())
	if err := StackRollbackClear(context.Background(), d, "media"); err != nil {
		t.Fatal(err)
	}
	if d.FS.Exists(bkDir+"/1700000000.tar.gz") || d.FS.Exists(bkDir+"/1700000000.json") {
		t.Fatal("Sicherung bleibt nach Clear")
	}
}

func TestRollbackWithDataRestoresBeforeUp(t *testing.T) {
	d, f := madeBackup(t, simpleBackup())
	f.Calls = nil
	if _, err := RunStackOpts(context.Background(), d, "media", "rollback", docker.RunOptions{RestoreData: true}); err != nil {
		t.Fatal(err)
	}
	i := f.Index("systemd-run")
	script := f.Calls[i]
	stop, del, tar, tag, up := strings.Index(script, "'stop'"), strings.Index(script, "'-delete'"), strings.Index(script, "'-xzpf'"), strings.Index(script, "'tag'"), strings.LastIndex(script, "'up'")
	if !(stop >= 0 && stop < del && del < tar && tar < up && tag < up) {
		t.Fatalf("Reihenfolge stop<delete<extract<up verletzt (%d %d %d %d): %s", stop, del, tar, up, script)
	}
	for _, want := range []string{"'find' '/srv/appdata/media/conf' '-mindepth' '1' '-delete'", "'find' '" + vmPath + "' '-mindepth' '1' '-delete'", "'-C' '/'", bkDir + "/1700000000.tar.gz"} {
		if !strings.Contains(script, want) {
			t.Errorf("Skript ohne %q:\n%s", want, script)
		}
	}
}

func TestRollbackWithoutDataDoesNotTouchData(t *testing.T) {
	d, f := madeBackup(t, simpleBackup())
	f.Calls = nil
	if _, err := RunStackOpts(context.Background(), d, "media", "rollback", docker.RunOptions{}); err != nil {
		t.Fatal(err)
	}
	script := f.Calls[f.Index("systemd-run")]
	if strings.Contains(script, "-delete") || strings.Contains(script, "-xzpf") || strings.Contains(script, "'stop'") {
		t.Fatalf("Daten dürfen nicht angefasst werden:\n%s", script)
	}
}

func TestRollbackWithDataNeedsBackup(t *testing.T) {
	d, _, _ := dockerEnv(t) // ohne Sicherung, aber mit Rollback-Stand
	saveStack(t, d, "media")
	_ = writeRollback(d, "media", rollbackFile, docker.Rollback{CreatedAt: "t", Images: []docker.RollbackImage{{Ref: "web:1", ID: idOld}}})
	_, err := RunStackOpts(context.Background(), d, "media", "rollback", docker.RunOptions{RestoreData: true})
	if code(err) != apierr.NotFound {
		t.Fatalf("%v", err)
	}
}

func TestRestoreRefusesUnsafeManifestPaths(t *testing.T) {
	for _, bad := range []string{"/srv/appdata", "/etc", "/srv/appdata/../etc", "/srv/appdata/stacks/media", "/srv/appdata/backups", "/srv/pools/x", "/srv/appdata/a'b"} {
		d, _ := madeBackup(t, simpleBackup())
		m := docker.DataBackup{ID: "1700000000", Sources: []docker.BackupSource{{Kind: "bind", Name: bad, Path: bad}}}
		b, _ := json.Marshal(m)
		_ = d.FS.WriteFile(bkDir+"/1700000000.json", b, 0o600)
		if _, err := RunStackOpts(context.Background(), d, "media", "rollback", docker.RunOptions{RestoreData: true}); err == nil {
			t.Errorf("%q wurde akzeptiert", bad)
		}
	}
}

func TestFailedUpdateStillMakesRollbackPossible(t *testing.T) {
	// StackRollbackCommit ist auch nach einem fehlgeschlagenen Update sicher aufrufbar (Manager tut das).
	d, _ := madeBackup(t, simpleBackup())
	if err := StackRollbackCommit(context.Background(), d, "media"); err != nil {
		t.Fatal(err)
	}
}
