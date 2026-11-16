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
	"github.com/betternas/betternas/internal/users"
)

type usersAgent struct{ calls []string }

func (a *usersAgent) Call(_ context.Context, method string, _, result any) error {
	a.calls = append(a.calls, method)
	if method == "account.list" && result != nil {
		b, _ := json.Marshal(users.Accounts{
			Users:  []users.User{{Name: "anna", UID: 1000, Admin: true, Groups: []string{}}, {Name: "bob", UID: 10000, Groups: []string{}}},
			Groups: []users.Group{{Name: "familie", GID: 10000, Members: []string{}}},
		})
		return json.Unmarshal(b, result)
	}
	return nil
}

func newUsersAPI(t *testing.T) (func(method, path, body string) *httptest.ResponseRecorder, *events.Hub, *usersAgent) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	store := auth.NewStore(d, time.Hour)
	token, csrf, _ := store.Create(context.Background(), "anna")
	ag := &usersAgent{}
	h := NewServer(Config{WebDir: t.TempDir(), Dev: true}, Deps{
		DB: d, Jobs: jobs.NewManager(d, hub), Hub: hub,
		Auth:  auth.NewHandlers(d, store, nil, nil, false),
		Users: users.NewManager(d, ag, hub),
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
	return do, hub, ag
}

func TestUsersRoutes(t *testing.T) {
	do, hub, _ := newUsersAPI(t)
	sub, cancel := hub.Subscribe()
	defer cancel()

	if w := do("GET", "/api/v1/users", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"bob"`) {
		t.Fatalf("GET users: %d %s", w.Code, w.Body)
	}
	if w := do("GET", "/api/v1/groups", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "familie") {
		t.Fatalf("GET groups: %d %s", w.Code, w.Body)
	}
	if w := do("POST", "/api/v1/users", `{"name":"carl","password":"geheim123"}`); w.Code != 201 {
		t.Fatalf("POST users: %d %s", w.Code, w.Body)
	}
	if m := <-sub; m.Type != "users.changed" {
		t.Fatalf("Ereignis %+v", m)
	}
	if w := do("POST", "/api/v1/users", `{"name":"Carl","password":"geheim123"}`); w.Code != 400 {
		t.Fatalf("ungültiger Name: %d", w.Code)
	}
	if w := do("PUT", "/api/v1/users/bob/password", `{"password":"neues-passwort"}`); w.Code != 204 {
		t.Fatalf("Passwort: %d %s", w.Code, w.Body)
	}
	if w := do("PATCH", "/api/v1/users/bob", `{"fullName":"Bob","groups":["familie"],"admin":false}`); w.Code != 204 {
		t.Fatalf("PATCH: %d %s", w.Code, w.Body)
	}
	if w := do("DELETE", "/api/v1/users/anna", ""); w.Code != 409 {
		t.Fatalf("sich selbst löschen: %d", w.Code)
	}
	if w := do("DELETE", "/api/v1/users/bob", ""); w.Code != 204 {
		t.Fatalf("DELETE: %d %s", w.Code, w.Body)
	}
	if w := do("POST", "/api/v1/groups", `{"name":"kinder","members":["bob"]}`); w.Code != 201 {
		t.Fatalf("POST groups: %d %s", w.Code, w.Body)
	}
	if w := do("PATCH", "/api/v1/groups/kinder", `{"members":[]}`); w.Code != 204 {
		t.Fatalf("PATCH groups: %d", w.Code)
	}
	if w := do("DELETE", "/api/v1/groups/kinder", ""); w.Code != 204 {
		t.Fatalf("DELETE groups: %d", w.Code)
	}
}
