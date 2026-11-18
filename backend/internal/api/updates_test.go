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

type updatesAgent struct {
	installErr error
	channel    string
	setCalls   []string
}

func (a *updatesAgent) Call(_ context.Context, method string, params, result any) error {
	var out any
	switch method {
	case "updates.check":
		out = []system.Package{{Name: "samba", Current: "1", Candidate: "2"}}
	case "updates.install":
		if a.installErr != nil {
			return a.installErr
		}
		out = map[string]string{"unit": "betternas-update-1"}
	case "updates.channel":
		out = map[string]string{"channel": a.channel}
	case "updates.setchannel":
		b, _ := json.Marshal(params)
		var p struct{ Channel string }
		_ = json.Unmarshal(b, &p)
		a.channel = p.Channel
		a.setCalls = append(a.setCalls, p.Channel)
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

func TestUpdatesChannelRoute(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	jm := jobs.NewManager(d, hub)
	store := auth.NewStore(d, time.Hour)
	token, csrf, _ := store.Create(context.Background(), "anna")
	ag := &updatesAgent{channel: "stable"}
	h := NewServer(Config{WebDir: t.TempDir(), Dev: true}, Deps{
		DB: d, Jobs: jm, Hub: hub,
		Auth:    auth.NewHandlers(d, store, nil, nil, false),
		Updates: system.NewUpdates(d, ag, jm, hub),
	})
	do := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "192.168.1.10:1"
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	if w := do("GET", "/api/v1/updates", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"channel":"stable"`) {
		t.Fatalf("GET: %d %s", w.Code, w.Body)
	}
	if w := do("PUT", "/api/v1/updates/channel", `{"channel":"testing"}`); w.Code != 204 {
		t.Fatalf("PUT: %d %s", w.Code, w.Body)
	}
	if len(ag.setCalls) != 1 || ag.setCalls[0] != "testing" {
		t.Fatalf("Agent-Aufrufe: %v", ag.setCalls)
	}
	if w := do("GET", "/api/v1/updates", ""); !strings.Contains(w.Body.String(), `"channel":"testing"`) {
		t.Fatalf("nach Wechsel: %s", w.Body)
	}
	for _, body := range []string{`{"channel":"beta"}`, `{"channel":""}`, `kaputt`, `{}`} {
		if w := do("PUT", "/api/v1/updates/channel", body); w.Code != 400 {
			t.Errorf("%s: %d", body, w.Code)
		}
	}
	if len(ag.setCalls) != 1 {
		t.Fatalf("ungültige Kanäle dürfen den Agent nicht erreichen: %v", ag.setCalls)
	}
}
