package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/system"
)

type updatesAgent struct{ installErr error }

func (a *updatesAgent) Call(_ context.Context, method string, _, result any) error {
	var out any
	switch method {
	case "updates.check":
		out = []system.Package{{Name: "samba", Current: "1", Candidate: "2"}}
	case "updates.install":
		if a.installErr != nil {
			return a.installErr
		}
		out = map[string]string{"unit": "betternas-update-1"}
	case "updates.status":
		out = system.Run{State: "succeeded", Log: "fertig"}
	}
	if result != nil {
		b, _ := json.Marshal(out)
		return json.Unmarshal(b, result)
	}
	return nil
}

func TestUpdatesRoutes(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	jm := jobs.NewManager(d, hub)
	store := auth.NewStore(d, time.Hour)
	token, csrf, _ := store.Create(context.Background(), "anna")
	ag := &updatesAgent{}
	h := NewServer(Config{WebDir: t.TempDir(), Dev: true}, Deps{
		DB: d, Jobs: jm, Hub: hub,
		Auth:    auth.NewHandlers(d, store, nil, nil, false),
		Updates: system.NewUpdates(d, ag, jm, hub),
	})
	do := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.RemoteAddr = "192.168.1.10:1"
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	if w := do("GET", "/api/v1/updates"); w.Code != 200 || !strings.Contains(w.Body.String(), `"packages":[]`) || !strings.Contains(w.Body.String(), `"state":"none"`) {
		t.Fatalf("GET: %d %s", w.Code, w.Body)
	}
	w := do("POST", "/api/v1/updates/check")
	if w.Code != 202 {
		t.Fatalf("check: %d %s", w.Code, w.Body)
	}
	var r struct{ JobID string }
	json.Unmarshal(w.Body.Bytes(), &r)
	for i := 0; i < 300; i++ {
		if j, err := jm.Get(context.Background(), r.JobID); err == nil && j.Status == jobs.StatusSucceeded {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if w := do("GET", "/api/v1/updates"); !strings.Contains(w.Body.String(), `"samba"`) {
		t.Fatalf("nach der Suche: %s", w.Body)
	}
	if w := do("POST", "/api/v1/updates/install"); w.Code != 202 || !strings.Contains(w.Body.String(), "betternas-update-1") {
		t.Fatalf("install: %d %s", w.Code, w.Body)
	}
	if w := do("GET", "/api/v1/updates/run"); w.Code != 200 || !strings.Contains(w.Body.String(), `"fertig"`) {
		t.Fatalf("run: %d %s", w.Code, w.Body)
	}
	ag.installErr = apierr.New(apierr.Conflict, "Ein Update läuft bereits.")
	if w := do("POST", "/api/v1/updates/install"); w.Code != 409 {
		t.Fatalf("Konflikt: %d", w.Code)
	}
}
