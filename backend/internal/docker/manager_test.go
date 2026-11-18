package docker

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
)

type fakeAgent struct {
	mu       sync.Mutex
	calls    []string
	runErr   error
	states   []RunState // Antworten von stack.runstatus nacheinander
	statusAt int
	saved    map[string]any
}

func (f *fakeAgent) Call(_ context.Context, method string, params, result any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)
	var out any
	switch method {
	case "stack.run":
		if f.runErr != nil {
			return f.runErr
		}
		out = map[string]string{"unit": "betternas-stack-media-1700000000"}
	case "stack.runstatus":
		i := min(f.statusAt, len(f.states)-1)
		f.statusAt++
		out = f.states[i]
	case "stack.save":
		b, _ := json.Marshal(params)
		_ = json.Unmarshal(b, &f.saved)
		if f.saved["create"] == true && f.saved["name"] == "dup" {
			return apierr.New(apierr.Conflict, "existiert")
		}
	case "stack.get":
		out = StackFiles{Name: "media", Compose: "x: 1"}
	}
	if result != nil && out != nil {
		b, _ := json.Marshal(out)
		return json.Unmarshal(b, result)
	}
	return nil
}

func newTestManager(t *testing.T, a *fakeAgent) (*Manager, *jobs.Manager) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	jm := jobs.NewManager(d, hub)
	m := NewManager(d, a, jm, hub)
	m.pollEvery = time.Millisecond
	return m, jm
}

func waitJob(t *testing.T, jm *jobs.Manager, id string) jobs.Job {
	t.Helper()
	for i := 0; i < 500; i++ {
		j, _ := jm.Get(context.Background(), id)
		if j.Status == jobs.StatusSucceeded || j.Status == jobs.StatusFailed {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timeout")
	return jobs.Job{}
}

func TestRunStackTracksJobToSuccess(t *testing.T) {
	a := &fakeAgent{states: []RunState{{State: "running", Log: "Pulling"}, {State: "succeeded", Log: "Pulling\nDone"}}}
	m, jm := newTestManager(t, a)
	id, err := m.RunStack(context.Background(), "anna", "media", "up")
	if err != nil {
		t.Fatal(err)
	}
	j := waitJob(t, jm, id)
	if j.Status != jobs.StatusSucceeded || !strings.Contains(j.Log, "Pulling") || !strings.Contains(j.Log, "Done") {
		t.Fatalf("%+v", j)
	}
	if strings.Count(j.Log, "Pulling") != 1 {
		t.Errorf("Protokoll doppelt: %q", j.Log)
	}
}

func TestRunStackFailureMarksJobFailed(t *testing.T) {
	a := &fakeAgent{states: []RunState{{State: "failed", Log: "boom"}}}
	m, jm := newTestManager(t, a)
	id, _ := m.RunStack(context.Background(), "anna", "media", "up")
	if j := waitJob(t, jm, id); j.Status != jobs.StatusFailed || !strings.Contains(j.Log, "boom") {
		t.Fatalf("%+v", j)
	}
}

func TestRunStackAgentRefusalIsReturnedDirectly(t *testing.T) {
	a := &fakeAgent{runErr: apierr.New(apierr.InvalidParams, "ungültig")}
	m, _ := newTestManager(t, a)
	if _, err := m.RunStack(context.Background(), "anna", "media", "up"); err == nil {
		t.Fatal("Fehler erwartet")
	}
}

func TestRunStackValidatesInput(t *testing.T) {
	m, _ := newTestManager(t, &fakeAgent{})
	ctx := context.Background()
	if _, err := m.RunStack(ctx, "a", "../x", "up"); err == nil {
		t.Error("Name")
	}
	if _, err := m.RunStack(ctx, "a", "media", "exec"); err == nil {
		t.Error("Aktion")
	}
	if err := m.ContainerAction(ctx, "a", "../x", "stop"); err == nil {
		t.Error("ID")
	}
	if len(m.agent.(*fakeAgent).calls) != 0 {
		t.Error("ungültige Eingaben dürfen den Agent nicht erreichen")
	}
}

func TestCreateStackConflictAndAudit(t *testing.T) {
	a := &fakeAgent{}
	m, _ := newTestManager(t, a)
	ctx := context.Background()
	if err := m.CreateStack(ctx, "anna", StackInput{Name: "dup", Compose: "x: 1"}); err == nil {
		t.Fatal("Konflikt erwartet")
	}
	if err := m.CreateStack(ctx, "anna", StackInput{Name: "media", Compose: "x: 1", Env: "A=1"}); err != nil {
		t.Fatal(err)
	}
	if a.saved["create"] != true || a.saved["env"] != "A=1" {
		t.Fatalf("%v", a.saved)
	}
	var n int
	if err := m.db.QueryRow(`SELECT COUNT(*) FROM audit WHERE action = 'stack.save' AND target = 'media' AND result = 'ok'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("Audit: %d %v", n, err)
	}
	if err := m.db.QueryRow(`SELECT COUNT(*) FROM audit WHERE action = 'stack.save' AND target = 'dup' AND result = 'error'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("Audit Fehler: %d %v", n, err)
	}
}

func TestUpdateStackSavesWithoutCreate(t *testing.T) {
	a := &fakeAgent{}
	m, _ := newTestManager(t, a)
	if err := m.UpdateStack(context.Background(), "anna", StackInput{Name: "media", Compose: "y: 2"}); err != nil {
		t.Fatal(err)
	}
	if a.saved["create"] != false {
		t.Fatalf("%v", a.saved)
	}
}

func hasCall(a *fakeAgent, method string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range a.calls {
		if c == method {
			return true
		}
	}
	return false
}

func TestRollbackBookkeepingAfterRuns(t *testing.T) {
	cases := []struct{ action, method string }{
		{"update", "stack.rollback.commit"},
		{"rollback", "stack.rollback.clear"},
	}
	for _, c := range cases {
		a := &fakeAgent{states: []RunState{{State: "succeeded"}}}
		m, jm := newTestManager(t, a)
		id, err := m.RunStack(context.Background(), "anna", "media", c.action)
		if err != nil {
			t.Fatal(err)
		}
		waitJob(t, jm, id)
		if !hasCall(a, c.method) {
			t.Errorf("%s: %s fehlt: %v", c.action, c.method, a.calls)
		}
	}
	// Schlägt der Lauf fehl, bleibt der Rollback-Stand unangetastet.
	a := &fakeAgent{states: []RunState{{State: "failed"}}}
	m, jm := newTestManager(t, a)
	id, _ := m.RunStack(context.Background(), "anna", "media", "rollback")
	waitJob(t, jm, id)
	if hasCall(a, "stack.rollback.clear") {
		t.Error("clear nach fehlgeschlagenem Rollback")
	}
}
