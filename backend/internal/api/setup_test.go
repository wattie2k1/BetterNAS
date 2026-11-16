package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
)

func TestSetupStatusAndComplete(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	store := auth.NewStore(d, time.Hour)
	token, csrf, _ := store.Create(context.Background(), "anna")
	h := NewServer(Config{WebDir: t.TempDir(), Dev: true}, Deps{DB: d, Jobs: jobs.NewManager(d, hub), Hub: hub, Auth: auth.NewHandlers(d, store, nil, nil, false)})
	do := func(method, path string, withCookie bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.RemoteAddr = "192.168.1.10:1"
		if withCookie {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
			r.Header.Set("X-CSRF-Token", csrf)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	if w := do("GET", "/api/v1/setup", false); w.Code != 401 {
		t.Fatalf("ohne Anmeldung: %d", w.Code)
	}
	w := do("GET", "/api/v1/setup", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"completed":false`) || !strings.Contains(w.Body.String(), `"hostname"`) {
		t.Fatalf("zuerst nicht abgeschlossen: %d %s", w.Code, w.Body)
	}
	if w := do("POST", "/api/v1/setup/complete", true); w.Code != 204 {
		t.Fatalf("complete: %d %s", w.Code, w.Body)
	}
	if w := do("POST", "/api/v1/setup/complete", true); w.Code != 204 {
		t.Fatalf("zweimal abschließen muss funktionieren: %d", w.Code)
	}
	if w := do("GET", "/api/v1/setup", true); !strings.Contains(w.Body.String(), `"completed":true`) {
		t.Fatalf("danach abgeschlossen: %s", w.Body)
	}
	entries, err := audit.List(context.Background(), d, 10)
	if err != nil || len(entries) == 0 || entries[0].Action != "setup.complete" || entries[0].Actor != "anna" {
		t.Fatalf("Audit: %v %+v", err, entries)
	}
}
