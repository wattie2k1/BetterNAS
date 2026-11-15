package shares

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/users"
)

type fakeAgent struct {
	mu       sync.Mutex
	applied  []State
	applyErr error
}

func (f *fakeAgent) Call(_ context.Context, method string, params, result any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch method {
	case "account.list":
		b, _ := json.Marshal(users.Accounts{
			Users:  []users.User{{Name: "anna", UID: 1000}, {Name: "bob", UID: 10000}},
			Groups: []users.Group{{Name: "familie", GID: 10000}},
		})
		return json.Unmarshal(b, result)
	case "share.apply":
		if f.applyErr != nil {
			return f.applyErr
		}
		b, _ := json.Marshal(params)
		var st State
		_ = json.Unmarshal(b, &st)
		f.applied = append(f.applied, st)
		return nil
	}
	return nil
}

func (f *fakeAgent) lastApplied() State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.applied[len(f.applied)-1]
}

func (f *fakeAgent) applyCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.applied)
}

type env struct {
	m     *Manager
	agent *fakeAgent
	jobs  *jobs.Manager
	ctx   context.Context
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(`INSERT INTO pools(name, create_policy, min_free, created_at) VALUES ('media','epmfs','20G','now'),('daten','epmfs','20G','now')`); err != nil {
		t.Fatal(err)
	}
	hub := events.NewHub()
	e := &env{agent: &fakeAgent{}, jobs: jobs.NewManager(d, hub), ctx: context.Background()}
	e.m = NewManager(d, e.agent, e.jobs, hub)
	e.m.mounted = func(string) bool { return true }
	e.m.hostname = func() string { return "nas" }
	return e
}

func (e *env) wait(t *testing.T, id string) jobs.Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		j, err := e.jobs.Get(e.ctx, id)
		if err == nil && (j.Status == jobs.StatusSucceeded || j.Status == jobs.StatusFailed) {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Job wurde nicht fertig")
	return jobs.Job{}
}

func (e *env) mustSucceed(t *testing.T, id string, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if j := e.wait(t, id); j.Status != jobs.StatusSucceeded {
		t.Fatalf("Job fehlgeschlagen: %s", j.Log)
	}
}

func code(err error) string {
	var ae *apierr.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

func filme() Share {
	return Share{Name: "Filme", Pool: "media", Subdir: "Filme", Browsable: true, Recycle: true,
		ACL: []ACL{{"user", "anna", "rw"}, {"group", "familie", "ro"}}}
}

func TestCreateShareAppliesThenCommits(t *testing.T) {
	e := newEnv(t)
	id, err := e.m.Create(e.ctx, "anna", filme())
	e.mustSucceed(t, id, err)
	st := e.agent.lastApplied()
	if len(st.Shares) != 1 || st.Shares[0].Name != "Filme" {
		t.Fatalf("angewendet: %+v", st)
	}
	list, err := e.m.List(e.ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("%v %+v", err, list)
	}
	v := list[0]
	if v.Path != "/srv/pools/media/Filme" || !v.PoolMounted || v.UNCPath != `\\nas\Filme` || len(v.ACL) != 2 || v.ACL[1].Name != "familie" {
		t.Fatalf("%+v", v)
	}
}

func TestCreateShareValidatesSynchronously(t *testing.T) {
	e := newEnv(t)
	cases := map[string]func(*Share){
		"unbekannter Benutzer": func(s *Share) { s.ACL = []ACL{{"user", "ghost", "rw"}} },
		"unbekannte Gruppe":    func(s *Share) { s.ACL = []ACL{{"group", "anna", "rw"}} },
		"unbekannter Pool":     func(s *Share) { s.Pool = "weg" },
		"ungültiger Ordner":    func(s *Share) { s.Subdir = "../x" },
	}
	for name, mod := range cases {
		s := filme()
		mod(&s)
		if _, err := e.m.Create(e.ctx, "anna", s); code(err) != apierr.InvalidParams {
			t.Errorf("%s: %v", name, err)
		}
	}
	if e.agent.applyCount() != 0 {
		t.Fatal("Agent darf nicht aufgerufen werden")
	}
}

func TestCreateShareRejectsDuplicateName(t *testing.T) {
	e := newEnv(t)
	id, err := e.m.Create(e.ctx, "anna", filme())
	e.mustSucceed(t, id, err)
	dup := filme()
	dup.Name = "FILME"
	if _, err := e.m.Create(e.ctx, "anna", dup); code(err) != apierr.Conflict {
		t.Fatalf("%v", err)
	}
}

func TestApplyFailureLeavesDBUnchanged(t *testing.T) {
	e := newEnv(t)
	e.agent.applyErr = apierr.New(apierr.InvalidParams, "Der Pool media ist nicht eingehängt.")
	id, err := e.m.Create(e.ctx, "anna", filme())
	if err != nil {
		t.Fatal(err)
	}
	if j := e.wait(t, id); j.Status != jobs.StatusFailed {
		t.Fatalf("Job soll fehlschlagen: %+v", j)
	}
	if list, _ := e.m.List(e.ctx); len(list) != 0 {
		t.Fatalf("DB geändert: %+v", list)
	}
}

func TestUpdateAndDeleteShare(t *testing.T) {
	e := newEnv(t)
	id, err := e.m.Create(e.ctx, "anna", filme())
	e.mustSucceed(t, id, err)

	upd := filme()
	upd.Name = "ignoriert"
	upd.Comment = "neu"
	upd.ACL = []ACL{{"user", "bob", "rw"}}
	id, err = e.m.Update(e.ctx, "anna", "Filme", upd)
	e.mustSucceed(t, id, err)
	v, err := e.m.Get(e.ctx, "filme") // Schreibweise egal
	if err != nil || v.Name != "Filme" || v.Comment != "neu" || len(v.ACL) != 1 || v.ACL[0].Name != "bob" {
		t.Fatalf("%v %+v", err, v)
	}

	if _, err := e.m.Update(e.ctx, "anna", "gibtsnicht", filme()); code(err) != apierr.NotFound {
		t.Fatalf("%v", err)
	}
	id, err = e.m.Delete(e.ctx, "anna", "Filme")
	e.mustSucceed(t, id, err)
	if len(e.agent.lastApplied().Shares) != 0 {
		t.Fatal("Freigabe nicht aus dem Zustand entfernt")
	}
	if list, _ := e.m.List(e.ctx); len(list) != 0 {
		t.Fatal("Freigabe noch in der DB")
	}
}

func TestDependents(t *testing.T) {
	e := newEnv(t)
	id, err := e.m.Create(e.ctx, "anna", filme())
	e.mustSucceed(t, id, err)
	if got, _ := e.m.PoolDependents(e.ctx, "media"); !slices.Equal(got, []string{"share:Filme"}) {
		t.Fatalf("Pool media: %v", got)
	}
	if got, _ := e.m.PoolDependents(e.ctx, "daten"); len(got) != 0 {
		t.Fatalf("Pool daten: %v", got)
	}
	for kind, name := range map[string]string{"user": "anna", "group": "familie"} {
		if got, _ := e.m.PrincipalUsage(e.ctx, kind, name); !slices.Equal(got, []string{"share:Filme"}) {
			t.Errorf("%s %s: %v", kind, name, got)
		}
	}
	if got, _ := e.m.PrincipalUsage(e.ctx, "user", "bob"); len(got) != 0 {
		t.Fatalf("bob: %v", got)
	}
	if got, _ := e.m.PrincipalUsage(e.ctx, "group", "anna"); len(got) != 0 {
		t.Fatalf("Typ muss beachtet werden: %v", got)
	}
}

func TestReconcileAppliesStoredState(t *testing.T) {
	e := newEnv(t)
	id, err := e.m.Create(e.ctx, "anna", filme())
	e.mustSucceed(t, id, err)
	before := e.agent.applyCount()
	if err := e.m.Reconcile(e.ctx); err != nil {
		t.Fatal(err)
	}
	if e.agent.applyCount() != before+1 || len(e.agent.lastApplied().Shares) != 1 {
		t.Fatal("Reconcile hat den gespeicherten Zustand nicht angewendet")
	}
}
