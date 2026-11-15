package system

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
)

type fakeAgent struct {
	mu         sync.Mutex
	calls      []string
	installErr error
	status     Run
}

func (f *fakeAgent) Call(_ context.Context, method string, params, result any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)
	var out any
	switch method {
	case "updates.check":
		out = []Package{{Name: "betternas", Current: "0.0.8-dev", Candidate: "0.1.0"}}
	case "updates.install":
		if f.installErr != nil {
			return f.installErr
		}
		out = map[string]string{"unit": "betternas-update-1700000000"}
	case "updates.status":
		b, _ := json.Marshal(params)
		var p struct{ Unit string }
		_ = json.Unmarshal(b, &p)
		if p.Unit != "betternas-update-1700000000" {
			return apierr.New(apierr.InvalidParams, "falsche Unit")
		}
		out = f.status
	}
	if result != nil {
		b, _ := json.Marshal(out)
		return json.Unmarshal(b, result)
	}
	return nil
}

type env struct {
	u     *Updates
	agent *fakeAgent
	jobs  *jobs.Manager
	ctx   context.Context
}

func newEnv(t *testing.T) (*env, func() []audit.StoredEntry) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	jm := jobs.NewManager(d, hub)
	a := &fakeAgent{status: Run{State: "running", Log: "Unpacking"}}
	e := &env{u: NewUpdates(d, a, jm, hub), agent: a, jobs: jm, ctx: context.Background()}
	return e, func() []audit.StoredEntry { l, _ := audit.List(e.ctx, d, 10); return l }
}

func (e *env) wait(t *testing.T, id string) jobs.Job {
	t.Helper()
	for i := 0; i < 300; i++ {
		j, err := e.jobs.Get(e.ctx, id)
		if err == nil && (j.Status == jobs.StatusSucceeded || j.Status == jobs.StatusFailed) {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Job nicht fertig")
	return jobs.Job{}
}

func TestGetBeforeAnyCheck(t *testing.T) {
	e, _ := newEnv(t)
	o, err := e.u.Get(e.ctx)
	if err != nil || o.CheckedAt != "" || o.Packages == nil || len(o.Packages) != 0 || o.Run.State != "none" {
		t.Fatalf("%v %+v", err, o)
	}
}

func TestCheckStoresPackages(t *testing.T) {
	e, _ := newEnv(t)
	id, err := e.u.Check(e.ctx, "anna")
	if err != nil {
		t.Fatal(err)
	}
	if j := e.wait(t, id); j.Status != jobs.StatusSucceeded {
		t.Fatalf("%s", j.Log)
	}
	o, _ := e.u.Get(e.ctx)
	if len(o.Packages) != 1 || o.Packages[0].Candidate != "0.1.0" || o.CheckedAt == "" {
		t.Fatalf("%+v", o)
	}
}

func TestInstallStoresUnitAndClearsList(t *testing.T) {
	e, entries := newEnv(t)
	id, _ := e.u.Check(e.ctx, "anna")
	e.wait(t, id)
	unit, err := e.u.Install(e.ctx, "anna")
	if err != nil || unit != "betternas-update-1700000000" {
		t.Fatalf("%q %v", unit, err)
	}
	o, _ := e.u.Get(e.ctx)
	if len(o.Packages) != 0 || o.Run.State != "running" || o.Run.Log != "Unpacking" {
		t.Fatalf("%+v", o)
	}
	if l := entries(); len(l) == 0 || l[0].Action != "updates.install" || l[0].Result != "ok" {
		t.Fatalf("Audit: %+v", l)
	}
}

func TestInstallConflictPassesThrough(t *testing.T) {
	e, entries := newEnv(t)
	e.agent.installErr = apierr.New(apierr.Conflict, "Ein Update läuft bereits.")
	if _, err := e.u.Install(e.ctx, "anna"); apierr.From(err).Code != apierr.Conflict {
		t.Fatalf("%v", err)
	}
	if l := entries(); len(l) == 0 || l[0].Result != "error" {
		t.Fatalf("Audit: %+v", l)
	}
}
