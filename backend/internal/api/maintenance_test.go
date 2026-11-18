package api

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/backup"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/rpc"
)

// maintAgent startet einen echten RPC-Server auf einem Unix-Socket und zeichnet Aufrufe auf.
type maintAgent struct {
	*rpc.Client
	mu    sync.Mutex
	calls []string
}

func newMaintAgent(t *testing.T) *maintAgent {
	t.Helper()
	dir, _ := os.MkdirTemp("", "api")
	t.Cleanup(func() { os.RemoveAll(dir) })
	l, err := net.Listen("unix", filepath.Join(dir, "a.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	a := &maintAgent{}
	srv := rpc.NewServer(nil)
	rec := func(name string, res string) {
		srv.Handle(name, func(context.Context, json.RawMessage) (any, error) {
			a.mu.Lock()
			a.calls = append(a.calls, name)
			a.mu.Unlock()
			return json.RawMessage(res), nil
		})
	}
	rec("logs.read", `{"text":"2026-11-18 api gestartet\n"}`)
	rec("support.collect", `{"system/uname.txt":"Linux\n","logs/api.log":"x\n"}`)
	rec("system.restart-api", `{"ok":true}`)
	go srv.Serve(l)
	a.Client = rpc.NewClient(filepath.Join(dir, "a.sock"))
	return a
}

func TestMaintenanceRoutes(t *testing.T) {
	tmp := t.TempDir()
	d, err := db.Open(filepath.Join(tmp, "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	store := auth.NewStore(d, time.Hour)
	token, csrf, _ := store.Create(context.Background(), "anna")
	ag := newMaintAgent(t)
	bm := backup.NewManager(d, ag.Client, filepath.Join(tmp, "backups"), "test", hub)
	h := NewServer(Config{WebDir: t.TempDir(), Dev: true}, Deps{
		DB: d, Agent: ag.Client, Jobs: jobs.NewManager(d, hub), Hub: hub, Backup: bm,
		Auth: auth.NewHandlers(d, store, nil, nil, false),
	})
	s := &storageAPI{h: h, cookie: &http.Cookie{Name: auth.CookieName, Value: token}, csrf: csrf}

	if w := s.do("GET", "/api/v1/logs/api?lines=50&download=1", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "api gestartet") || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("%d %s %v", w.Code, w.Body, w.Header())
	}
	if w := s.do("GET", "/api/v1/logs/shadow", ""); w.Code != 404 {
		t.Fatalf("unbekannte Quelle %d", w.Code)
	}
	if w := s.do("GET", "/api/v1/logs/audit", ""); w.Code != 200 || !strings.HasPrefix(strings.TrimSpace(w.Body.String()), "[") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}

	w := s.do("GET", "/api/v1/support-bundle", "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/gzip" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	gz, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	got := map[string]string{}
	for {
		hd, err := tr.Next()
		if err != nil {
			break
		}
		b, _ := io.ReadAll(tr)
		got[hd.Name] = string(b)
	}
	for _, n := range []string{"betternas-support/info.txt", "betternas-support/audit.json", "betternas-support/system/uname.txt", "betternas-support/logs/api.log"} {
		if _, ok := got[n]; !ok {
			t.Errorf("%s fehlt: %v", n, got)
		}
	}
	for n := range got {
		if strings.Contains(n, "settings") {
			t.Errorf("Einstellungen dürfen nicht ins Paket: %s", n)
		}
	}

	// Werkseinstellungen nur mit Bestätigungswort; danach Marker + API-Neustart.
	if w := s.do("POST", "/api/v1/system/factory-reset", `{"confirm":"ja"}`); w.Code != 400 {
		t.Fatalf("%d", w.Code)
	}
	ag.mu.Lock()
	ag.calls = nil
	ag.mu.Unlock()
	if w := s.do("POST", "/api/v1/system/factory-reset", `{"confirm":"ZURÜCKSETZEN"}`); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(ag.calls) != 1 || ag.calls[0] != "system.restart-api" {
		t.Fatalf("%v", ag.calls)
	}
	ok, err := backup.ApplyFactoryReset(filepath.Join(tmp, "restore"), filepath.Join(tmp, "b.db"), 2)
	if !ok || err != nil {
		t.Fatalf("Marker nicht gesetzt: %v %v", ok, err)
	}
}
