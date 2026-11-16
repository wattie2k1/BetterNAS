package docker

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/jobs"
)

type checkAgent struct {
	mu    sync.Mutex
	calls []string
}

func (a *checkAgent) Call(_ context.Context, method string, params, result any) error {
	a.mu.Lock()
	a.calls = append(a.calls, method)
	a.mu.Unlock()
	var out any
	switch method {
	case "stack.list":
		out = []StackInfo{{Name: "media"}, {Name: "broken"}, {Name: "fresh"}}
	case "stack.images":
		b, _ := json.Marshal(params)
		switch {
		case strings.Contains(string(b), "broken"):
			return apierr.New(apierr.InvalidParams, "Die Compose-Datei ist ungültig")
		case strings.Contains(string(b), "fresh"):
			out = []string{"b:1"}
		default:
			out = []string{"a:1", "b:1"}
		}
	case "image.digests":
		b, _ := json.Marshal(params)
		var p struct{ Images []string }
		_ = json.Unmarshal(b, &p)
		var sts []ImageStatus
		for _, im := range p.Images {
			switch im {
			case "a:1":
				sts = append(sts, ImageStatus{Image: im, Outdated: true})
			default:
				sts = append(sts, ImageStatus{Image: im, Error: "Registry nicht erreichbar"})
			}
		}
		out = sts
	}
	if result != nil && out != nil {
		b, _ := json.Marshal(out)
		return json.Unmarshal(b, result)
	}
	return nil
}

func TestCheckUpdatesStoresReportAndSurvivesStackErrors(t *testing.T) {
	m, jm := newTestManager(t, &fakeAgent{})
	m.agent = &checkAgent{}
	id, err := m.CheckUpdates(context.Background(), "anna")
	if err != nil {
		t.Fatal(err)
	}
	if j := waitJob(t, jm, id); j.Status != jobs.StatusSucceeded {
		t.Fatalf("%+v", j)
	}
	rep, err := m.Updates(context.Background())
	if err != nil || rep.CheckedAt == "" {
		t.Fatalf("%+v %v", rep, err)
	}
	if got := rep.Stacks["media"]; len(got.Outdated) != 1 || got.Outdated[0] != "a:1" || len(got.Errors) != 1 {
		t.Errorf("media: %+v", got)
	}
	if got := rep.Stacks["broken"]; len(got.Outdated) != 0 || len(got.Errors) != 1 || !strings.Contains(got.Errors[0], "ungültig") {
		t.Errorf("broken: %+v", got)
	}
	if got := rep.Stacks["fresh"]; len(got.Outdated) != 0 {
		t.Errorf("fresh: %+v", got)
	}
}

func TestSuccessfulUpdateClearsReportEntry(t *testing.T) {
	a := &fakeAgent{states: []RunState{{State: "succeeded"}}}
	m, jm := newTestManager(t, a)
	ctx := context.Background()
	if err := m.saveReport(ctx, UpdateReport{Stacks: map[string]UpdateInfo{"media": {Outdated: []string{"a:1"}}, "other": {Outdated: []string{"c:1"}}}}); err != nil {
		t.Fatal(err)
	}
	id, _ := m.RunStack(ctx, "anna", "media", "update")
	waitJob(t, jm, id)
	rep, _ := m.Updates(ctx)
	if _, ok := rep.Stacks["media"]; ok {
		t.Error("media sollte aus dem Ergebnis verschwinden")
	}
	if _, ok := rep.Stacks["other"]; !ok {
		t.Error("andere Stacks bleiben")
	}
}
