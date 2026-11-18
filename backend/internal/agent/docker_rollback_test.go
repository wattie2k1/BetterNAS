package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/sysexec"
)

const (
	idOld = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	idNew = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

// rollbackEnv liefert eine Umgebung, in der der Stack „media“ ein Image (web:1) hat, dessen lokale ID über ids gesteuert wird.
func rollbackEnv(t *testing.T) (Deps, *sysexec.FakeRunner, *string) {
	t.Helper()
	d, f, _ := dockerEnv(t)
	saveStack(t, d, "media")
	cur := idOld
	f.Hook = func(call string) (sysexec.Result, bool) {
		switch {
		case strings.HasSuffix(call, "config --images"):
			return sysexec.Result{Stdout: []byte("web:1\n")}, true
		case call == "docker image inspect --format {{.Id}} web:1":
			return sysexec.Result{Stdout: []byte(cur + "\n")}, true
		}
		return sysexec.Result{}, false
	}
	return d, f, &cur
}

func TestUpdateSnapshotsImagesBeforeSystemdRun(t *testing.T) {
	d, f, _ := rollbackEnv(t)
	if _, err := RunStack(context.Background(), d, "media", "update"); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, f, "docker tag "+idOld+" betternas-rollback/media:p0", "systemd-run")
}

func TestRollbackCommitPromotesWhenImageChanged(t *testing.T) {
	d, f, cur := rollbackEnv(t)
	ctx := context.Background()
	if _, err := RunStack(ctx, d, "media", "update"); err != nil {
		t.Fatal(err)
	}
	*cur = idNew // der Pull hat ein neues Image gebracht
	if err := StackRollbackCommit(ctx, d, "media"); err != nil {
		t.Fatal(err)
	}
	if !f.Called("docker tag " + idOld + " betternas-rollback/media:r0") {
		t.Fatalf("%v", f.Calls)
	}
	list, _ := ListStacks(ctx, d)
	if len(list) != 1 || list[0].Rollback == "" {
		t.Fatalf("%+v", list)
	}
	if d.FS.Exists("/srv/appdata/stacks/media/.rollback.pending.json") {
		t.Fatal("pending-Datei bleibt liegen")
	}
}

func TestRollbackCommitDiscardsWhenNothingChanged(t *testing.T) {
	d, f, _ := rollbackEnv(t)
	ctx := context.Background()
	if _, err := RunStack(ctx, d, "media", "update"); err != nil {
		t.Fatal(err)
	}
	if err := StackRollbackCommit(ctx, d, "media"); err != nil {
		t.Fatal(err)
	}
	if f.Called("docker tag " + idOld + " betternas-rollback/media:r0") {
		t.Fatal("ohne neues Image darf kein Rollback-Stand entstehen")
	}
	if list, _ := ListStacks(ctx, d); list[0].Rollback != "" {
		t.Fatalf("%+v", list)
	}
}

func TestRollbackRunRetagsAndRecreates(t *testing.T) {
	d, f, cur := rollbackEnv(t)
	ctx := context.Background()
	if _, err := RunStack(ctx, d, "media", "update"); err != nil {
		t.Fatal(err)
	}
	*cur = idNew
	if err := StackRollbackCommit(ctx, d, "media"); err != nil {
		t.Fatal(err)
	}
	f.Calls = nil
	if _, err := RunStack(ctx, d, "media", "rollback"); err != nil {
		t.Fatal(err)
	}
	i := f.Index("systemd-run")
	if i < 0 || !strings.Contains(f.Calls[i], "'docker' 'tag' '"+idOld+"' 'web:1' && 'docker' 'compose'") || !strings.Contains(f.Calls[i], "'up' '-d' '--remove-orphans'") {
		t.Fatalf("%v", f.Calls)
	}
}

func TestRollbackRunWithoutSnapshotFails(t *testing.T) {
	d, _, _ := rollbackEnv(t)
	if _, err := RunStack(context.Background(), d, "media", "rollback"); code(err) != apierr.NotFound {
		t.Fatalf("%v", err)
	}
}

func TestRollbackClearRemovesManifestAndTags(t *testing.T) {
	d, f, cur := rollbackEnv(t)
	ctx := context.Background()
	_, _ = RunStack(ctx, d, "media", "update")
	*cur = idNew
	_ = StackRollbackCommit(ctx, d, "media")
	if err := StackRollbackClear(ctx, d, "media"); err != nil {
		t.Fatal(err)
	}
	if !f.Called("docker rmi betternas-rollback/media:r0") {
		t.Fatalf("%v", f.Calls)
	}
	if list, _ := ListStacks(ctx, d); list[0].Rollback != "" {
		t.Fatalf("%+v", list)
	}
}

func TestDeleteStackDropsRollbackFiles(t *testing.T) {
	d, _, cur := rollbackEnv(t)
	ctx := context.Background()
	_, _ = RunStack(ctx, d, "media", "update")
	*cur = idNew
	_ = StackRollbackCommit(ctx, d, "media")
	// Der Update-Lauf ist laut Fake-Runner „fertig“ (kein laufender Vorgang).
	if err := DeleteStack(ctx, d, "media"); err != nil {
		t.Fatal(err)
	}
	if d.FS.Exists("/srv/appdata/stacks/media") {
		t.Fatal("Stack-Verzeichnis bleibt liegen")
	}
}
