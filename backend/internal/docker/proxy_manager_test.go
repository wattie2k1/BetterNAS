package docker

import (
	"context"
	"encoding/json"
	"fmt"
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

// stepAgent protokolliert alle Aufrufe in Reihenfolge („stack.run:pull“, „proxy.validate“, …).
type stepAgent struct {
	mu        sync.Mutex
	steps     []string
	written   ProxyConfig
	validate  error
	reloadErr []error // Antworten auf proxy.reload nacheinander
	reloads   int
}

func (a *stepAgent) Call(_ context.Context, method string, params, result any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch method {
	case "stack.run":
		b, _ := json.Marshal(params)
		var p map[string]any
		_ = json.Unmarshal(b, &p)
		a.steps = append(a.steps, fmt.Sprintf("stack.run:%v:%v", p["name"], p["action"]))
		return fill(result, map[string]string{"unit": "u"})
	case "stack.runstatus":
		return fill(result, RunState{State: "succeeded"})
	case "proxy.write":
		b, _ := json.Marshal(params)
		_ = json.Unmarshal(b, &a.written)
		a.steps = append(a.steps, method)
	case "proxy.validate":
		a.steps = append(a.steps, method)
		return a.validate
	case "proxy.reload":
		a.steps = append(a.steps, method)
		var err error
		if a.reloads < len(a.reloadErr) {
			err = a.reloadErr[a.reloads]
		}
		a.reloads++
		return err
	default:
		a.steps = append(a.steps, method)
	}
	return nil
}

func fill(result, v any) error {
	if result == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return json.Unmarshal(b, result)
}

func (a *stepAgent) order() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return strings.Join(a.steps, " > ")
}

func proxyManager(t *testing.T, a *stepAgent) (*Manager, *jobs.Manager) {
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
	m.proxyRetryEvery = time.Millisecond
	return m, jm
}

func TestProxyConfigIsStoredNormalized(t *testing.T) {
	m, _ := proxyManager(t, &stepAgent{})
	ctx := context.Background()
	if c, err := m.Proxy(ctx); err != nil || c.Enabled || c.HTTPPort != 8080 || len(c.Sites) != 0 {
		t.Fatalf("Standard: %+v %v", c, err)
	}
	in := validCfg()
	in.Sites[0].Domain = "Jellyfin.Example.DE"
	out, err := m.SetProxy(ctx, "anna", in)
	if err != nil || out.Sites[0].Domain != "cloud.example.de" {
		t.Fatalf("%+v %v", out, err)
	}
	got, _ := m.Proxy(ctx)
	if len(got.Sites) != 3 || got.Email != "admin@example.de" {
		t.Fatalf("%+v", got)
	}
	bad := validCfg()
	bad.HTTPSPort = 443
	if _, err := m.SetProxy(ctx, "anna", bad); apierr.From(err).Code != apierr.InvalidParams {
		t.Fatalf("%v", err)
	}
	if again, _ := m.Proxy(ctx); again.HTTPSPort != 8443 {
		t.Fatalf("ungültige Einstellung gespeichert: %+v", again)
	}
}

func TestApplyProxyRunsStepsInOrder(t *testing.T) {
	a := &stepAgent{}
	m, jm := proxyManager(t, a)
	_, _ = m.SetProxy(context.Background(), "anna", validCfg())
	id, err := m.ApplyProxy(context.Background(), "anna")
	if err != nil {
		t.Fatal(err)
	}
	if j := waitJob(t, jm, id); j.Status != jobs.StatusSucceeded {
		t.Fatalf("%+v", j)
	}
	want := "proxy.write > stack.run:reverse-proxy:pull > proxy.validate > proxy.commit > stack.run:reverse-proxy:up > proxy.reload"
	if got := a.order(); got != want {
		t.Fatalf("Ablauf:\n%s\nerwartet:\n%s", got, want)
	}
	if len(a.written.Sites) != 3 {
		t.Fatalf("Agent bekam %+v", a.written)
	}
}

func TestApplyProxyStopsWhenCaddyRejectsConfig(t *testing.T) {
	a := &stepAgent{validate: apierr.New(apierr.InvalidParams, "Caddy lehnt die Konfiguration ab: unrecognized directive")}
	m, jm := proxyManager(t, a)
	_, _ = m.SetProxy(context.Background(), "anna", validCfg())
	id, _ := m.ApplyProxy(context.Background(), "anna")
	j := waitJob(t, jm, id)
	if j.Status != jobs.StatusFailed || !strings.Contains(j.Log, "Caddy lehnt") {
		t.Fatalf("%+v", j)
	}
	got := a.order()
	if strings.Contains(got, "proxy.commit") || strings.Contains(got, ":up") || strings.Contains(got, "proxy.reload") {
		t.Fatalf("nach abgelehnter Konfiguration darf nichts übernommen werden: %s", got)
	}
}

func TestApplyProxyDisabledStopsStack(t *testing.T) {
	a := &stepAgent{}
	m, jm := proxyManager(t, a)
	cfg := validCfg()
	cfg.Enabled = false
	_, _ = m.SetProxy(context.Background(), "anna", cfg)
	id, _ := m.ApplyProxy(context.Background(), "anna")
	if j := waitJob(t, jm, id); j.Status != jobs.StatusSucceeded {
		t.Fatalf("%+v", j)
	}
	if got := a.order(); got != "proxy.write > stack.run:reverse-proxy:down" {
		t.Fatalf("%s", got)
	}
}

func TestApplyProxyRetriesReloadWhileCaddyStarts(t *testing.T) {
	a := &stepAgent{reloadErr: []error{apierr.New(apierr.Conflict, "noch nicht"), apierr.New(apierr.Conflict, "noch nicht"), nil}}
	m, jm := proxyManager(t, a)
	_, _ = m.SetProxy(context.Background(), "anna", validCfg())
	id, _ := m.ApplyProxy(context.Background(), "anna")
	if j := waitJob(t, jm, id); j.Status != jobs.StatusSucceeded || a.reloads != 3 {
		t.Fatalf("%+v Reloads=%d", j, a.reloads)
	}
	// Dauerhaft kaputt: Job schlägt nach den Versuchen fehl.
	b := &stepAgent{reloadErr: []error{apierr.New(apierr.Conflict, "x"), apierr.New(apierr.Conflict, "x"), apierr.New(apierr.Conflict, "x"), apierr.New(apierr.Conflict, "x"), apierr.New(apierr.Conflict, "x"), apierr.New(apierr.Conflict, "x")}}
	m2, jm2 := proxyManager(t, b)
	_, _ = m2.SetProxy(context.Background(), "anna", validCfg())
	id, _ = m2.ApplyProxy(context.Background(), "anna")
	if j := waitJob(t, jm2, id); j.Status != jobs.StatusFailed {
		t.Fatalf("%+v", j)
	}
}
