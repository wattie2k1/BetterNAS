package api

import (
	"context"
	"encoding/json"
	"net"
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
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/version"
)

type testEnv struct {
	srv    http.Handler
	cookie *http.Cookie
	store  *auth.Store
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	// Agent-Attrappe über das echte RPC-Protokoll
	sockDir, _ := os.MkdirTemp("", "api")
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "a.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	as := rpc.NewServer(nil)
	as.Handle("system.info", func(context.Context, json.RawMessage) (any, error) {
		return map[string]any{"hostname": "betternas", "cpuCount": 4}, nil
	})
	go as.Serve(l)

	web := filepath.Join(dir, "web")
	os.MkdirAll(filepath.Join(web, "assets"), 0o755)
	os.WriteFile(filepath.Join(web, "index.html"), []byte("<html>BetterNAS</html>"), 0o644)
	os.WriteFile(filepath.Join(web, "assets", "app.js"), []byte("console.log(1)"), 0o644)

	hub := events.NewHub()
	store := auth.NewStore(d, time.Hour)
	agent := rpc.NewClient(sock)
	h := NewServer(Config{WebDir: web, Dev: true}, Deps{
		DB: d, Agent: agent, Jobs: jobs.NewManager(d, hub), Hub: hub,
		Auth: auth.NewHandlers(d, store, auth.AgentVerifier{Agent: agent}, auth.SystemGroupChecker, false),
	})
	token, _, err := store.Create(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	return &testEnv{srv: h, store: store, cookie: &http.Cookie{Name: auth.CookieName, Value: token}}
}

func (e *testEnv) get(path string, withCookie bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	r.RemoteAddr = "192.168.1.10:5000"
	if withCookie {
		r.AddCookie(e.cookie)
	}
	w := httptest.NewRecorder()
	e.srv.ServeHTTP(w, r)
	return w
}

func TestUnauthenticatedAPIIs401(t *testing.T) {
	e := newTestEnv(t)
	for _, p := range []string{"/api/v1/system/info", "/api/v1/jobs", "/api/v1/ws"} {
		if w := e.get(p, false); w.Code != 401 {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
}

func TestSPAFallback(t *testing.T) {
	e := newTestEnv(t)
	if w := e.get("/speicher/pools", false); w.Code != 200 || !strings.Contains(w.Body.String(), "BetterNAS") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := e.get("/assets/app.js", false); w.Code != 200 || w.Body.String() != "console.log(1)" {
		t.Fatalf("%d", w.Code)
	}
	w := e.get("/api/v1/unknown", true)
	if w.Code != 404 || !strings.Contains(w.Body.String(), `"not_found"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestStaticPathTraversal(t *testing.T) {
	e := newTestEnv(t)
	if w := e.get("/../b.db", false); strings.Contains(w.Body.String(), "SQLite") {
		t.Fatal("path traversal")
	}
}

func TestSystemInfoIncludesVersion(t *testing.T) {
	e := newTestEnv(t)
	w := e.get("/api/v1/system/info", true)
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != 200 || body["hostname"] != "betternas" || body["version"] != version.Version {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestJobsList(t *testing.T) {
	e := newTestEnv(t)
	if w := e.get("/api/v1/jobs", true); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := e.get("/api/v1/jobs/unbekannt", true); w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}
}

func TestPublicAddressForbidden(t *testing.T) {
	e := newTestEnv(t)
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "8.8.8.8:1"
	w := httptest.NewRecorder()
	e.srv.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("%d", w.Code)
	}
}

func TestEnsureSelfSignedCertIdempotent(t *testing.T) {
	dir := t.TempDir()
	cert, key, err := EnsureSelfSignedCert(dir, "betternas")
	if err != nil {
		t.Fatal(err)
	}
	st1, _ := os.Stat(cert)
	if ks, _ := os.Stat(key); ks.Mode().Perm() != 0o600 {
		t.Fatalf("key perms %v", ks.Mode())
	}
	time.Sleep(10 * time.Millisecond)
	if _, _, err := EnsureSelfSignedCert(dir, "betternas"); err != nil {
		t.Fatal(err)
	}
	st2, _ := os.Stat(cert)
	if !st1.ModTime().Equal(st2.ModTime()) {
		t.Fatal("certificate regenerated")
	}
}
