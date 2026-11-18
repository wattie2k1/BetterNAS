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
	"github.com/betternas/betternas/internal/notify"
)

// autoAgent spielt eine Docker-Umgebung nach: Stacks mit Update-Stand und je Stack die Container nach einem Update.
type autoAgent struct {
	mu         sync.Mutex
	stacks     map[string][]string // Stack → Images
	outdated   map[string]bool     // Image veraltet?
	afterRun   map[string][]Container
	containers []Container
	runs       []string // „<name>:<action>“
	failRun    map[string]bool
}

func (a *autoAgent) Call(_ context.Context, method string, params, result any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, _ := params.(map[string]string)
	var out any
	switch method {
	case "stack.list":
		list := []StackInfo{}
		for n := range a.stacks {
			list = append(list, StackInfo{Name: n})
		}
		out = list
	case "stack.images":
		out = a.stacks[p["name"]]
	case "image.digests":
		var sts []ImageStatus
		for _, im := range params.(map[string]any)["images"].([]string) {
			sts = append(sts, ImageStatus{Image: im, Outdated: a.outdated[im]})
		}
		out = sts
	case "stack.run":
		if a.failRun[p["name"]+":"+p["action"]] {
			return apierr.New(apierr.Conflict, "läuft schon")
		}
		a.runs = append(a.runs, p["name"]+":"+p["action"])
		if p["action"] == "update" {
			a.containers = a.afterRun[p["name"]]
		}
		out = map[string]string{"unit": "u-" + p["name"]}
	case "stack.runstatus":
		out = RunState{State: "succeeded"}
	case "container.list":
		out = a.containers
	}
	if result != nil && out != nil {
		b, _ := json.Marshal(out)
		return json.Unmarshal(b, result)
	}
	return nil
}

func (a *autoAgent) ran() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return strings.Join(a.runs, ",")
}

type sink struct {
	mu   sync.Mutex
	msgs []notify.Message
}

func (s *sink) Send(_ context.Context, m notify.Message) []notify.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, m)
	return nil
}

func autoManager(t *testing.T, a *autoAgent, cfg AutoUpdateConfig) *Manager {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	m := NewManager(d, a, jobs.NewManager(d, hub), hub)
	m.pollEvery = time.Millisecond
	m.healthSettle = 5 * time.Millisecond
	m.healthTimeout = 200 * time.Millisecond
	if err := m.SetAutoUpdate(context.Background(), "t", cfg); err != nil {
		t.Fatal(err)
	}
	return m
}

type nopLog struct{}

func (nopLog) Printf(string, ...any) {}
func (nopLog) SetProgress(int)       {}

func run(t *testing.T, m *Manager, n Notifier) {
	t.Helper()
	if err := m.autoUpdate(context.Background(), nopLog{}, n); err != nil {
		t.Fatal(err)
	}
}

func okC(stack string) Container {
	return Container{Name: stack + "-web", Stack: stack, State: "running", Status: "Up 1 minute (healthy)", Health: "healthy"}
}

func baseAgent() *autoAgent {
	return &autoAgent{
		stacks:   map[string][]string{"media": {"jf:1"}, "wiki": {"wk:1"}},
		outdated: map[string]bool{"jf:1": true, "wk:1": true},
		afterRun: map[string][]Container{"media": {okC("media")}, "wiki": {okC("wiki")}},
	}
}

func TestAutoUpdateOnlyUpdatesOptedInOutdatedStacks(t *testing.T) {
	a := baseAgent()
	a.outdated["wk:1"] = false
	m := autoManager(t, a, AutoUpdateConfig{Enabled: true, Time: "04:00", Stacks: []string{"media", "wiki"}})
	n := &sink{}
	run(t, m, n)
	if got := a.ran(); got != "media:update" {
		t.Fatalf("Läufe: %q", got)
	}
	if len(n.msgs) != 1 || n.msgs[0].Level != "info" || !strings.Contains(n.msgs[0].Body, "media") {
		t.Fatalf("%+v", n.msgs)
	}
}

func TestAutoUpdateIgnoresStacksWithoutOptIn(t *testing.T) {
	a := baseAgent()
	m := autoManager(t, a, AutoUpdateConfig{Enabled: true, Time: "04:00", Stacks: []string{"wiki"}})
	n := &sink{}
	run(t, m, n)
	if got := a.ran(); got != "wiki:update" {
		t.Fatalf("Läufe: %q", got)
	}
}

func TestAutoUpdateRollsBackWhenContainerUnhealthy(t *testing.T) {
	a := baseAgent()
	bad := okC("media")
	bad.Health, bad.Status = "unhealthy", "Up 1 minute (unhealthy)"
	a.afterRun["media"] = []Container{bad}
	m := autoManager(t, a, AutoUpdateConfig{Enabled: true, Time: "04:00", Stacks: []string{"media"}})
	n := &sink{}
	run(t, m, n)
	if got := a.ran(); got != "media:update,media:rollback" {
		t.Fatalf("Läufe: %q", got)
	}
	if len(n.msgs) != 1 || n.msgs[0].Level != "crit" || !strings.Contains(n.msgs[0].Body, "zurückgerollt") {
		t.Fatalf("%+v", n.msgs)
	}
}

func TestAutoUpdateRollsBackOnRestartLoopAndFailedExit(t *testing.T) {
	for _, c := range []Container{
		{Name: "x", Stack: "media", State: "restarting", Status: "Restarting (1) 3 seconds ago"},
		{Name: "x", Stack: "media", State: "exited", Status: "Exited (137) 3 seconds ago"},
	} {
		a := baseAgent()
		a.afterRun["media"] = []Container{c}
		m := autoManager(t, a, AutoUpdateConfig{Enabled: true, Time: "04:00", Stacks: []string{"media"}})
		run(t, m, &sink{})
		if got := a.ran(); got != "media:update,media:rollback" {
			t.Errorf("%s: Läufe %q", c.Status, got)
		}
	}
}

func TestAutoUpdateAcceptsCleanExitOfInitContainer(t *testing.T) {
	a := baseAgent()
	a.afterRun["media"] = []Container{okC("media"), {Name: "init", Stack: "media", State: "exited", Status: "Exited (0) 10 seconds ago"}}
	m := autoManager(t, a, AutoUpdateConfig{Enabled: true, Time: "04:00", Stacks: []string{"media"}})
	run(t, m, &sink{})
	if got := a.ran(); got != "media:update" {
		t.Fatalf("Läufe: %q", got)
	}
}

func TestAutoUpdateRollsBackWhenHealthNeverSettles(t *testing.T) {
	a := baseAgent()
	slow := okC("media")
	slow.Health = "starting"
	a.afterRun["media"] = []Container{slow}
	m := autoManager(t, a, AutoUpdateConfig{Enabled: true, Time: "04:00", Stacks: []string{"media"}})
	run(t, m, &sink{})
	if got := a.ran(); got != "media:update,media:rollback" {
		t.Fatalf("Läufe: %q", got)
	}
}

func TestAutoUpdateSkipsBusyStackWithoutFailing(t *testing.T) {
	a := baseAgent()
	a.failRun = map[string]bool{"media:update": true}
	m := autoManager(t, a, AutoUpdateConfig{Enabled: true, Time: "04:00", Stacks: []string{"media"}})
	n := &sink{}
	run(t, m, n)
	if got := a.ran(); got != "" {
		t.Fatalf("Läufe: %q", got)
	}
	if len(n.msgs) != 1 || n.msgs[0].Level != "warn" {
		t.Fatalf("%+v", n.msgs)
	}
}

func TestAutoUpdateConfigValidation(t *testing.T) {
	m := autoManager(t, baseAgent(), AutoUpdateConfig{Time: "04:00"})
	ctx := context.Background()
	for _, bad := range []AutoUpdateConfig{
		{Time: "25:00"}, {Time: "4:00"}, {Time: "04:00", Stacks: []string{"../x"}}, {Time: "04:00", Stacks: []string{"Media"}},
	} {
		if err := m.SetAutoUpdate(ctx, "t", bad); err == nil {
			t.Errorf("%+v sollte abgelehnt werden", bad)
		}
	}
	if err := m.SetAutoUpdate(ctx, "t", AutoUpdateConfig{Enabled: true, Time: "03:15", Stacks: []string{"b", "a", "b"}}); err != nil {
		t.Fatal(err)
	}
	c, _ := m.AutoUpdate(ctx)
	if !c.Enabled || c.Time != "03:15" || strings.Join(c.Stacks, ",") != "a,b" {
		t.Fatalf("%+v", c)
	}
}

func TestAutoUpdateDefaultIsOff(t *testing.T) {
	d, _ := db.Open(filepath.Join(t.TempDir(), "b.db"))
	defer d.Close()
	hub := events.NewHub()
	m := NewManager(d, &autoAgent{}, jobs.NewManager(d, hub), hub)
	c, err := m.AutoUpdate(context.Background())
	if err != nil || c.Enabled || c.Time != "04:00" || len(c.Stacks) != 0 {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestNextAutoRun(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 11, 19, 3, 0, 0, 0, loc)
	if got := nextAutoRun(now, 4, 0); !got.Equal(time.Date(2026, 11, 19, 4, 0, 0, 0, loc)) {
		t.Fatal(got)
	}
	if got := nextAutoRun(now, 2, 0); !got.Equal(time.Date(2026, 11, 20, 2, 0, 0, 0, loc)) {
		t.Fatal(got)
	}
}
