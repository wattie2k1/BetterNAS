package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/netcfg"
)

type netAgentStub struct{ calls map[string]any }

func (a *netAgentStub) Call(_ context.Context, method string, params, result any) error {
	if a.calls == nil {
		a.calls = map[string]any{}
	}
	b, _ := json.Marshal(params)
	var p any
	_ = json.Unmarshal(b, &p)
	a.calls[method] = p
	if method == "network.bridge.status" && result != nil {
		b, _ := json.Marshal(netcfg.Status{Active: true, Interface: "eno1", Pending: true, Deadline: 42, Candidates: []string{"eno1"}})
		return json.Unmarshal(b, result)
	}
	return nil
}

func TestNetworkBridgeRoutes(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	store := auth.NewStore(d, time.Hour)
	token, csrf, _ := store.Create(context.Background(), "anna")
	stub := &netAgentStub{}
	h := NewServer(Config{WebDir: t.TempDir(), Dev: true}, Deps{
		DB: d, Jobs: jobs.NewManager(d, hub), Hub: hub,
		Auth:    auth.NewHandlers(d, store, nil, nil, false),
		Network: netcfg.NewManager(d, stub),
	})
	s := &storageAPI{h: h, cookie: &http.Cookie{Name: auth.CookieName, Value: token}, csrf: csrf}
	if w := s.do("GET", "/api/v1/network/bridge", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"pending":true`) || !strings.Contains(w.Body.String(), `"candidates":["eno1"]`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/network/bridge/enable", `{"interface":"eno1"}`); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if p, _ := stub.calls["network.bridge.enable"].(map[string]any); p["interface"] != "eno1" {
		t.Fatalf("%v", stub.calls)
	}
	if w := s.do("POST", "/api/v1/network/bridge/enable", `kaputt`); w.Code != 400 {
		t.Fatalf("%d", w.Code)
	}
	for _, a := range []string{"confirm", "disable"} {
		if w := s.do("POST", "/api/v1/network/bridge/"+a, ""); w.Code != 204 {
			t.Fatalf("%s %d %s", a, w.Code, w.Body)
		}
	}
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM audit WHERE action LIKE 'network.bridge.%'`).Scan(&n)
	if n != 3 {
		t.Fatalf("Audit-Einträge: %d", n)
	}
}

func TestNetworkIPRoutes(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	store := auth.NewStore(d, time.Hour)
	token, csrf, _ := store.Create(context.Background(), "anna")
	stub := &netAgentStub{}
	h := NewServer(Config{WebDir: t.TempDir(), Dev: true}, Deps{
		DB: d, Jobs: jobs.NewManager(d, hub), Hub: hub,
		Auth:    auth.NewHandlers(d, store, nil, nil, false),
		Network: netcfg.NewManager(d, stub),
	})
	s := &storageAPI{h: h, cookie: &http.Cookie{Name: auth.CookieName, Value: token}, csrf: csrf}
	if w := s.do("GET", "/api/v1/network", ""); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	body := `{"mode":"static","address":"192.168.1.10/24","gateway":"192.168.1.1","dns":["192.168.1.1"],"domains":[]}`
	if w := s.do("PUT", "/api/v1/network", body); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if p, _ := stub.calls["network.configure"].(map[string]any); p["address"] != "192.168.1.10/24" || p["mode"] != "static" {
		t.Fatalf("%v", stub.calls)
	}
	if w := s.do("PUT", "/api/v1/network", `kaputt`); w.Code != 400 {
		t.Fatalf("%d", w.Code)
	}
	for _, path := range []string{"/api/v1/network/confirm", "/api/v1/network/revert"} {
		if w := s.do("POST", path, ""); w.Code != 204 {
			t.Fatalf("%s %d", path, w.Code)
		}
	}
	if _, ok := stub.calls["network.confirm"]; !ok {
		t.Fatalf("%v", stub.calls)
	}
}
