package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/db"
)

type fakeVerifier map[string]string

func (f fakeVerifier) Verify(_ context.Context, u, p string) (bool, error) {
	return f[u] == p && p != "", nil
}

func admins(names ...string) GroupChecker {
	return func(u, g string) (bool, error) {
		for _, n := range names {
			if n == u && g == AdminGroup {
				return true, nil
			}
		}
		return false, nil
	}
}

type env struct {
	db *sql.DB
	h  *Handlers
	mx http.Handler
}

func newEnv(t *testing.T, ttl time.Duration) *env {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	h := NewHandlers(d, NewStore(d, ttl), fakeVerifier{"alice": "pw-alice", "bob": "pw-bob"}, admins("alice"), false)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/login", h.Login)
	mux.Handle("POST /api/v1/auth/logout", h.RequireSession(http.HandlerFunc(h.Logout)))
	mux.Handle("GET /api/v1/auth/me", h.RequireSession(http.HandlerFunc(h.Me)))
	mux.Handle("/api/v1/echo", h.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(SessionFrom(r.Context()).Username))
	})))
	return &env{db: d, h: h, mx: mux}
}

func (e *env) do(method, path, body, ip string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = ip + ":40000"
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	w := httptest.NewRecorder()
	e.mx.ServeHTTP(w, r)
	return w
}

func (e *env) login(t *testing.T, user, pw string) (*http.Cookie, string) {
	t.Helper()
	w := e.do("POST", "/api/v1/auth/login", `{"username":"`+user+`","password":"`+pw+`"}`, "192.168.1.10", nil, "")
	if w.Code != 200 {
		t.Fatalf("login %d %s", w.Code, w.Body)
	}
	var body struct{ Username, CsrfToken string }
	json.Unmarshal(w.Body.Bytes(), &body)
	for _, c := range w.Result().Cookies() {
		if c.Name == CookieName {
			return c, body.CsrfToken
		}
	}
	t.Fatal("no cookie")
	return nil, ""
}

func TestLoginSuccessSetsCookie(t *testing.T) {
	e := newEnv(t, time.Hour)
	c, csrf := e.login(t, "alice", "pw-alice")
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || csrf == "" {
		t.Fatalf("cookie %+v csrf %q", c, csrf)
	}
	list, _ := audit.List(context.Background(), e.db, 5)
	if len(list) != 1 || list[0].Action != "auth.login" || list[0].Result != "ok" {
		t.Fatalf("%+v", list)
	}
	if w := e.do("GET", "/api/v1/auth/me", "", "192.168.1.10", c, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"alice"`) {
		t.Fatalf("me %d %s", w.Code, w.Body)
	}
}

func TestLoginNonAdminIsGeneric401(t *testing.T) {
	e := newEnv(t, time.Hour)
	wrong := e.do("POST", "/api/v1/auth/login", `{"username":"alice","password":"falsch"}`, "192.168.1.10", nil, "")
	nonAdmin := e.do("POST", "/api/v1/auth/login", `{"username":"bob","password":"pw-bob"}`, "192.168.1.11", nil, "")
	if wrong.Code != 401 || nonAdmin.Code != 401 || wrong.Body.String() != nonAdmin.Body.String() {
		t.Fatalf("%d %s / %d %s", wrong.Code, wrong.Body, nonAdmin.Code, nonAdmin.Body)
	}
	if !strings.Contains(wrong.Body.String(), "Benutzername oder Passwort falsch") {
		t.Fatal(wrong.Body.String())
	}
}

func TestLoginRateLimited(t *testing.T) {
	e := newEnv(t, time.Hour)
	for i := 0; i < 5; i++ {
		e.do("POST", "/api/v1/auth/login", `{"username":"alice","password":"x"}`, "192.168.1.10", nil, "")
	}
	w := e.do("POST", "/api/v1/auth/login", `{"username":"alice","password":"pw-alice"}`, "192.168.1.10", nil, "")
	if w.Code != 429 || !strings.Contains(w.Body.String(), "rate_limited") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := e.do("POST", "/api/v1/auth/login", `{"username":"alice","password":"pw-alice"}`, "192.168.1.20", nil, ""); w.Code != 200 {
		t.Fatalf("other ip blocked: %d", w.Code)
	}
}

func TestCSRFRequiredForMutations(t *testing.T) {
	e := newEnv(t, time.Hour)
	c, csrf := e.login(t, "alice", "pw-alice")
	if w := e.do("POST", "/api/v1/echo", "", "192.168.1.10", c, ""); w.Code != 403 {
		t.Fatalf("no token: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/echo", "", "192.168.1.10", c, "falsch"); w.Code != 403 {
		t.Fatalf("wrong token: %d", w.Code)
	}
	if w := e.do("POST", "/api/v1/echo", "", "192.168.1.10", c, csrf); w.Code != 200 || w.Body.String() != "alice" {
		t.Fatalf("right token: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/echo", "", "192.168.1.10", c, ""); w.Code != 200 {
		t.Fatalf("GET: %d", w.Code)
	}
}

func TestUnauthenticatedIs401(t *testing.T) {
	e := newEnv(t, time.Hour)
	if w := e.do("GET", "/api/v1/echo", "", "192.168.1.10", nil, ""); w.Code != 401 {
		t.Fatalf("%d", w.Code)
	}
}

func TestSessionExpires(t *testing.T) {
	e := newEnv(t, time.Millisecond)
	c, _ := e.login(t, "alice", "pw-alice")
	time.Sleep(20 * time.Millisecond)
	if w := e.do("GET", "/api/v1/echo", "", "192.168.1.10", c, ""); w.Code != 401 {
		t.Fatalf("%d", w.Code)
	}
}

func TestLogoutDeletesSession(t *testing.T) {
	e := newEnv(t, time.Hour)
	c, csrf := e.login(t, "alice", "pw-alice")
	if w := e.do("POST", "/api/v1/auth/logout", "", "192.168.1.10", c, csrf); w.Code != 204 {
		t.Fatalf("%d", w.Code)
	}
	if w := e.do("GET", "/api/v1/echo", "", "192.168.1.10", c, ""); w.Code != 401 {
		t.Fatalf("%d", w.Code)
	}
}

func TestSessionTokenStoredHashed(t *testing.T) {
	e := newEnv(t, time.Hour)
	c, _ := e.login(t, "alice", "pw-alice")
	var n int
	e.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE token_hash = ?`, c.Value).Scan(&n)
	if n != 0 {
		t.Fatal("token stored in plain text")
	}
}

func TestLANOnly(t *testing.T) {
	h := LANOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for addr, want := range map[string]int{
		"192.168.1.5:1234": 200, "[fd00::1]:1": 200, "127.0.0.1:1": 200, "10.1.2.3:5": 200,
		"172.16.0.1:1": 200, "[fe80::1]:1": 200, "[::1]:1": 200,
		"8.8.8.8:1234": 403, "[2001:db8::1]:1": 403, "172.32.0.1:1": 403,
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = addr
		r.Header.Set("X-Forwarded-For", "192.168.1.1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("%s: got %d want %d", addr, w.Code, want)
		}
	}
}
