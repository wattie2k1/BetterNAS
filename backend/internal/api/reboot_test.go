package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/system"
)

func TestRebootRequiredRoute(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	store := auth.NewStore(d, time.Hour)
	token, csrf, _ := store.Create(context.Background(), "anna")
	mod := filepath.Join(t.TempDir(), "modules")
	for _, k := range []string{"6.1.0-27-amd64", "6.1.0-28-amd64"} {
		if err := os.MkdirAll(filepath.Join(mod, k), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	checker := &system.RebootChecker{Running: func() (string, error) { return "6.1.0-27-amd64", nil }, ModulesDir: mod}
	h := NewServer(Config{WebDir: t.TempDir(), Dev: true}, Deps{
		DB: d, Jobs: jobs.NewManager(d, hub), Hub: hub,
		Auth:          auth.NewHandlers(d, store, nil, nil, false),
		RebootChecker: checker,
	})
	get := func(withCookie bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/v1/system/reboot-required", nil)
		r.RemoteAddr = "192.168.1.10:1"
		if withCookie {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
			r.Header.Set("X-CSRF-Token", csrf)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := get(false); w.Code != 401 {
		t.Fatalf("ohne Anmeldung: %d", w.Code)
	}
	w := get(true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"required":true`) || !strings.Contains(w.Body.String(), "6.1.0-28-amd64") {
		t.Fatalf("GET: %d %s", w.Code, w.Body)
	}
}
