package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
)

type fakeAgent struct {
	mu    sync.Mutex
	calls []string
	err   error
	hook  func(method string)
}

func (f *fakeAgent) Call(_ context.Context, method string, _, _ any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)
	if f.hook != nil {
		f.hook(method)
	}
	return f.err
}

type mgrEnv struct {
	m     *Manager
	agent *fakeAgent
	root  string
	ctx   context.Context
}

func newMgr(t *testing.T) *mgrEnv {
	t.Helper()
	d := testDB(t)
	root := t.TempDir()
	a := &fakeAgent{}
	m := NewManager(d, a, filepath.Join(root, "backups"), "0.5.0-test", events.NewHub())
	return &mgrEnv{m: m, agent: a, root: root, ctx: context.Background()}
}

func (e *mgrEnv) actions(t *testing.T) []string {
	t.Helper()
	list, err := audit.List(e.ctx, e.m.db, 50)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, a := range list {
		out = append(out, a.Action+":"+a.Target+":"+a.Actor)
	}
	return out
}

func TestManagerCreateListDeleteAudit(t *testing.T) {
	e := newMgr(t)
	info, err := e.m.Create(e.ctx, "anna")
	if err != nil {
		t.Fatal(err)
	}
	l, err := e.m.List(e.ctx)
	if err != nil || len(l) != 1 || l[0].Name != info.Name {
		t.Fatalf("%+v %v", l, err)
	}
	if err := e.m.Delete(e.ctx, "anna", info.Name); err != nil {
		t.Fatal(err)
	}
	if l, _ := e.m.List(e.ctx); len(l) != 0 {
		t.Fatalf("%+v", l)
	}
	got := strings.Join(e.actions(t), " ")
	if !strings.Contains(got, "backup.create:"+info.Name+":anna") || !strings.Contains(got, "backup.delete:"+info.Name+":anna") {
		t.Fatalf("Audit: %s", got)
	}
}

func TestManagerCreatePrunesToSeven(t *testing.T) {
	e := newMgr(t)
	for i := 0; i < 9; i++ {
		if _, err := e.m.Create(e.ctx, "anna"); err != nil {
			t.Fatal(err)
		}
	}
	if l, _ := e.m.List(e.ctx); len(l) != 7 {
		t.Fatalf("%d Backups statt 7", len(l))
	}
}

func TestManagerOpenRejectsBadName(t *testing.T) {
	e := newMgr(t)
	for _, n := range []string{"../etc/passwd", "x.tar.gz", ""} {
		if _, err := e.m.Open(n); apierr.From(err).Code != apierr.InvalidParams {
			t.Errorf("%q: %v", n, err)
		}
	}
}

func TestRestoreNeedsValidToken(t *testing.T) {
	e := newMgr(t)
	if err := e.m.Restore(e.ctx, "anna", "gibtsnicht"); apierr.From(err).Code != apierr.NotFound {
		t.Fatalf("%v", err)
	}
	info, _ := e.m.Create(e.ctx, "anna")
	p, err := e.m.PreviewStored(e.ctx, info.Name)
	if err != nil {
		t.Fatal(err)
	}
	e.m.now = func() time.Time { return time.Now().Add(16 * time.Minute) }
	if err := e.m.Restore(e.ctx, "anna", p.Token); apierr.From(err).Code != apierr.Conflict {
		t.Fatalf("abgelaufen: %v", err)
	}
	if len(e.agent.calls) != 0 {
		t.Fatalf("Agent aufgerufen: %v", e.agent.calls)
	}
}

func TestRestoreStagesThenRestarts(t *testing.T) {
	e := newMgr(t)
	info, _ := e.m.Create(e.ctx, "anna")
	p, err := e.m.PreviewStored(e.ctx, info.Name)
	if err != nil || p.Manifest.Counts["pools"] != 1 {
		t.Fatalf("%+v %v", p, err)
	}
	var stagedAtCall bool
	e.agent.hook = func(string) {
		_, err := os.Stat(filepath.Join(e.root, "restore", "pending.db"))
		stagedAtCall = err == nil
	}
	if err := e.m.Restore(e.ctx, "anna", p.Token); err != nil {
		t.Fatal(err)
	}
	if !stagedAtCall || len(e.agent.calls) != 1 || e.agent.calls[0] != "system.restart-api" {
		t.Fatalf("staged=%v calls=%v", stagedAtCall, e.agent.calls)
	}
	if !strings.Contains(strings.Join(e.actions(t), " "), "backup.restore:"+info.Name) && !strings.Contains(strings.Join(e.actions(t), " "), "backup.restore") {
		t.Fatalf("kein Audit: %v", e.actions(t))
	}
	// Token ist verbraucht
	if err := e.m.Restore(e.ctx, "anna", p.Token); apierr.From(err).Code != apierr.NotFound {
		t.Fatalf("%v", err)
	}
}

func TestRestoreAgentFailureUnstages(t *testing.T) {
	e := newMgr(t)
	info, _ := e.m.Create(e.ctx, "anna")
	p, _ := e.m.PreviewStored(e.ctx, info.Name)
	e.agent.err = errors.New("systemd weg")
	if err := e.m.Restore(e.ctx, "anna", p.Token); err == nil {
		t.Fatal("Fehler erwartet")
	}
	for _, n := range []string{"pending.db", "pending.json"} {
		if _, err := os.Stat(filepath.Join(e.root, "restore", n)); err == nil {
			t.Errorf("%s blieb liegen", n)
		}
	}
}

func TestPreviewUploadRejectsGarbage(t *testing.T) {
	e := newMgr(t)
	_, err := e.m.PreviewUpload(e.ctx, strings.NewReader("das ist kein backup"))
	if apierr.From(err).Code != apierr.InvalidParams {
		t.Fatalf("%v", err)
	}
	// nichts übrig, laufende DB unverändert
	ents, _ := os.ReadDir(filepath.Join(e.root, "restore"))
	for _, en := range ents {
		t.Errorf("Rest im Restore-Verzeichnis: %s", en.Name())
	}
	var n int
	e.m.db.QueryRow(`SELECT COUNT(*) FROM pools`).Scan(&n)
	if n != 1 {
		t.Fatal("DB verändert")
	}
}

func TestPreviewWarnsOnOtherVersion(t *testing.T) {
	e := newMgr(t)
	info, _ := e.m.Create(e.ctx, "anna")
	e.m.version = "0.6.0"
	p, err := e.m.PreviewStored(e.ctx, info.Name)
	if err != nil || len(p.Warnings) == 0 || !strings.Contains(p.Warnings[0], "0.5.0-test") {
		t.Fatalf("%+v %v", p, err)
	}
}
