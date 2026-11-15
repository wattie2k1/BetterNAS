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

	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/shares"
	"github.com/betternas/betternas/internal/users"
)

type sharesAgent struct{}

func (sharesAgent) Call(_ context.Context, method string, _, result any) error {
	if method == "account.list" && result != nil {
		b, _ := json.Marshal(users.Accounts{Users: []users.User{{Name: "anna", UID: 1000}}, Groups: []users.Group{}})
		return json.Unmarshal(b, result)
	}
	return nil
}

func TestSharesRoutes(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(`INSERT INTO pools(name, create_policy, min_free, created_at) VALUES ('media','epmfs','20G','now')`); err != nil {
		t.Fatal(err)
	}
	hub := events.NewHub()
	jm := jobs.NewManager(d, hub)
	store := auth.NewStore(d, time.Hour)
	token, csrf, _ := store.Create(context.Background(), "anna")
	h := NewServer(Config{WebDir: t.TempDir(), Dev: true}, Deps{
		DB: d, Jobs: jm, Hub: hub,
		Auth:   auth.NewHandlers(d, store, nil, nil, false),
		Shares: shares.NewManager(d, sharesAgent{}, jm, hub),
	})
	do := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "192.168.1.10:1"
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	waitJob := func(w *httptest.ResponseRecorder) {
		t.Helper()
		var r struct{ JobID string }
		json.Unmarshal(w.Body.Bytes(), &r)
		for i := 0; i < 300; i++ {
			j, err := jm.Get(context.Background(), r.JobID)
			if err == nil && (j.Status == jobs.StatusSucceeded || j.Status == jobs.StatusFailed) {
				if j.Status != jobs.StatusSucceeded {
					t.Fatalf("Job fehlgeschlagen: %s", j.Log)
				}
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("Job nicht fertig")
	}

	body := `{"name":"Filme","pool":"media","subdir":"Filme","browsable":true,"acl":[{"type":"user","name":"anna","access":"rw"}]}`
	w := do("POST", "/api/v1/shares", body)
	if w.Code != 202 {
		t.Fatalf("POST: %d %s", w.Code, w.Body)
	}
	waitJob(w)
	if w := do("POST", "/api/v1/shares", `{"name":"x","pool":"media","subdir":"../x","acl":[]}`); w.Code != 400 {
		t.Fatalf("ungültig: %d", w.Code)
	}
	if w := do("POST", "/api/v1/shares", body); w.Code != 409 {
		t.Fatalf("doppelt: %d", w.Code)
	}
	if w := do("GET", "/api/v1/shares", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"uncPath"`) || !strings.Contains(w.Body.String(), `"Filme"`) {
		t.Fatalf("GET: %d %s", w.Code, w.Body)
	}
	if w := do("GET", "/api/v1/shares/filme", ""); w.Code != 200 {
		t.Fatalf("GET eine: %d", w.Code)
	}
	if w := do("GET", "/api/v1/shares/gibtsnicht", ""); w.Code != 404 {
		t.Fatalf("404 erwartet: %d", w.Code)
	}
	w = do("PUT", "/api/v1/shares/Filme", `{"pool":"media","subdir":"Filme","comment":"neu","browsable":true,"acl":[{"type":"user","name":"anna","access":"ro"}]}`)
	if w.Code != 202 {
		t.Fatalf("PUT: %d %s", w.Code, w.Body)
	}
	waitJob(w)
	w = do("DELETE", "/api/v1/shares/Filme", "")
	if w.Code != 202 {
		t.Fatalf("DELETE: %d", w.Code)
	}
	waitJob(w)
}
