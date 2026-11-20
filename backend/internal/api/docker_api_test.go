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
	"github.com/betternas/betternas/internal/docker"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
)

type dockerAgentStub struct{}

func (dockerAgentStub) Call(_ context.Context, method string, _, result any) error {
	var out any
	switch method {
	case "stack.run":
		out = map[string]string{"unit": "betternas-stack-media-1700000000"}
	case "stack.runstatus":
		out = docker.RunState{State: "succeeded", Log: "ok"}
	case "stack.list":
		out = []docker.StackInfo{{Name: "media", Running: 1, Total: 2}}
	case "stack.get":
		out = docker.StackFiles{Name: "media", Compose: "services: {}"}
	case "stack.validate":
		out = docker.StackCheck{OK: true}
	case "container.list":
		out = []docker.Container{{ID: "0123456789ab", Name: "web", Ports: []string{}}}
	case "stack.images":
		out = []string{"nginx:stable"}
	case "image.digests":
		out = []docker.ImageStatus{{Image: "nginx:stable", Outdated: true}}
	case "image.list":
		out = []docker.ImageInfo{{ID: "aaaaaaaaaaaa", Ref: "nginx:stable", Repository: "nginx", Tag: "stable"}}
	case "network.list":
		out = []docker.NetworkInfo{{ID: "111111111111", Name: "bridge", Builtin: true}}
	case "volume.list":
		out = []docker.VolumeInfo{{Name: "data", Unused: true}}
	case "resource.prune":
		out = docker.PruneResult{Reclaimed: "1.5GB"}
	case "stack.backupplan":
		out = docker.BackupPlan{Sources: []docker.BackupSource{{Kind: "bind", Name: "/srv/appdata/media/c", Path: "/srv/appdata/media/c", Bytes: 1234}}, TotalBytes: 1234, FreeBytes: 99999, Possible: true}
	case "proxy.write", "proxy.validate", "proxy.commit", "proxy.reload":
		out = map[string]bool{"ok": true}
	case "device.list":
		out = []docker.HostDevice{{Kind: "gpu", Path: "/dev/dri/renderD128", Name: "Intel HD", GID: 991, Group: "render"}}
	case "folder.create":
		out = docker.Folder{Name: "Filme", Path: "/srv/pools/tank/Filme"}
	case "folder.list":
		out = docker.FolderList{Path: "/srv/pools", Dirs: []docker.Folder{{Name: "tank", Path: "/srv/pools/tank"}}}
	case "docker.status":
		out = docker.Status{Installed: true, Active: true, Version: "26"}
	}
	if result != nil && out != nil {
		b, _ := json.Marshal(out)
		return json.Unmarshal(b, result)
	}
	return nil
}

func newDockerAPI(t *testing.T) *storageAPI {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	jm := jobs.NewManager(d, hub)
	store := auth.NewStore(d, time.Hour)
	token, csrf, _ := store.Create(context.Background(), "anna")
	h := NewServer(Config{WebDir: t.TempDir(), Dev: true}, Deps{
		DB: d, Jobs: jm, Hub: hub,
		Auth:   auth.NewHandlers(d, store, nil, nil, false),
		Docker: docker.NewManager(d, dockerAgentStub{}, jm, hub),
	})
	return &storageAPI{h: h, cookie: &http.Cookie{Name: auth.CookieName, Value: token}, csrf: csrf, jobs: jm}
}

func TestDockerRoutes(t *testing.T) {
	s := newDockerAPI(t)
	if w := s.do("GET", "/api/v1/docker/status", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"active":true`) {
		t.Fatalf("status %d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/stacks", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"media"`) {
		t.Fatalf("list %d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/stacks/media", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"compose":"services: {}"`) {
		t.Fatalf("get %d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/stacks", `{"name":"media","compose":"services: {}"}`); w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	if w := s.do("PUT", "/api/v1/stacks/media", `{"compose":"services: {}","env":"A=1"}`); w.Code != 204 {
		t.Fatalf("update %d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/stacks/media/validate", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("validate %d %s", w.Code, w.Body)
	}
	w := s.do("POST", "/api/v1/stacks/media/up", "")
	if w.Code != 202 {
		t.Fatalf("up %d %s", w.Code, w.Body)
	}
	s.waitJob(t, w.Body.Bytes())
	if w := s.do("DELETE", "/api/v1/stacks/media", ""); w.Code != 204 {
		t.Fatalf("delete %d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/containers", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"web"`) {
		t.Fatalf("containers %d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/containers/0123456789ab/restart", ""); w.Code != 204 {
		t.Fatalf("restart %d %s", w.Code, w.Body)
	}
}

func TestDockerInvalidInput(t *testing.T) {
	s := newDockerAPI(t)
	if w := s.do("POST", "/api/v1/stacks", `{"name":"../x","compose":"a: 1"}`); w.Code != 400 {
		t.Fatalf("name %d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/stacks", `kaputt`); w.Code != 400 {
		t.Fatalf("json %d", w.Code)
	}
	if w := s.do("POST", "/api/v1/stacks/A/up", ""); w.Code != 400 {
		t.Fatalf("run name %d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/containers/zzz/stop", ""); w.Code != 400 {
		t.Fatalf("container id %d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/containers/0123456789ab/exec", ""); w.Code != 404 && w.Code != 405 {
		t.Fatalf("container action %d", w.Code)
	}
	big := `{"name":"big","compose":"` + strings.Repeat("a", 600<<10) + `"}`
	if w := s.do("POST", "/api/v1/stacks", big); w.Code != 400 {
		t.Fatalf("zu groß %d", w.Code)
	}
}

func TestDockerEndpointsRequireAuthAndCSRF(t *testing.T) {
	s := newDockerAPI(t)
	for _, c := range [][2]string{{"GET", "/api/v1/stacks"}, {"POST", "/api/v1/stacks"}, {"DELETE", "/api/v1/stacks/media"},
		{"POST", "/api/v1/stacks/media/up"}, {"GET", "/api/v1/containers"}, {"POST", "/api/v1/containers/0123456789ab/stop"}} {
		r := httptest.NewRequest(c[0], c[1], strings.NewReader(`{}`))
		r.RemoteAddr = "192.168.1.10:1"
		w := httptest.NewRecorder()
		s.h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Errorf("%s %s ohne Sitzung: %d", c[0], c[1], w.Code)
		}
		r = httptest.NewRequest(c[0], c[1], strings.NewReader(`{}`))
		r.RemoteAddr = "192.168.1.10:1"
		r.AddCookie(s.cookie)
		w = httptest.NewRecorder()
		s.h.ServeHTTP(w, r)
		if c[0] != "GET" && w.Code != 403 {
			t.Errorf("%s %s ohne CSRF: %d", c[0], c[1], w.Code)
		}
	}
}

func TestDockerUpdatesRoutes(t *testing.T) {
	s := newDockerAPI(t)
	if w := s.do("GET", "/api/v1/docker/updates", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"stacks":{}`) {
		t.Fatalf("leer %d %s", w.Code, w.Body)
	}
	w := s.do("POST", "/api/v1/docker/updates/check", "")
	if w.Code != 202 {
		t.Fatalf("check %d %s", w.Code, w.Body)
	}
	s.waitJob(t, w.Body.Bytes())
	if w := s.do("GET", "/api/v1/docker/updates", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"outdated":["nginx:stable"]`) {
		t.Fatalf("Ergebnis %d %s", w.Code, w.Body)
	}
}

func TestDockerResourceRoutes(t *testing.T) {
	s := newDockerAPI(t)
	for path, want := range map[string]string{
		"/api/v1/docker/images":   `"ref":"nginx:stable"`,
		"/api/v1/docker/networks": `"builtin":true`,
		"/api/v1/docker/volumes":  `"unused":true`,
	} {
		if w := s.do("GET", path, ""); w.Code != 200 || !strings.Contains(w.Body.String(), want) {
			t.Fatalf("%s %d %s", path, w.Code, w.Body)
		}
	}
	if w := s.do("DELETE", "/api/v1/docker/images?ref=ghcr.io/x/y:1", ""); w.Code != 204 {
		t.Fatalf("rm image %d %s", w.Code, w.Body)
	}
	if w := s.do("DELETE", "/api/v1/docker/volumes", ""); w.Code != 400 {
		t.Fatalf("ohne ref %d", w.Code)
	}
	if w := s.do("POST", "/api/v1/docker/images/prune", `{"all":true}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"reclaimed":"1.5GB"`) {
		t.Fatalf("prune %d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/docker/volumes/prune", ``); w.Code != 200 {
		t.Fatalf("prune ohne Body %d %s", w.Code, w.Body)
	}
}

func TestDockerAutoUpdateRoutes(t *testing.T) {
	s := newDockerAPI(t)
	if w := s.do("GET", "/api/v1/docker/autoupdate", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":false`) || !strings.Contains(w.Body.String(), `"stacks":[]`) {
		t.Fatalf("Standard %d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/docker/autoupdate/run", ""); w.Code != 400 {
		t.Fatalf("ohne Stacks %d %s", w.Code, w.Body)
	}
	if w := s.do("PUT", "/api/v1/docker/autoupdate", `{"enabled":true,"time":"25:00","stacks":[]}`); w.Code != 400 {
		t.Fatalf("ungültige Zeit %d %s", w.Code, w.Body)
	}
	w := s.do("PUT", "/api/v1/docker/autoupdate", `{"enabled":true,"time":"03:30","stacks":["gone"]}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"time":"03:30"`) || !strings.Contains(w.Body.String(), `"stacks":["gone"]`) {
		t.Fatalf("speichern %d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/docker/autoupdate/run", ""); w.Code != 202 {
		t.Fatalf("run %d %s", w.Code, w.Body)
	} else {
		s.waitJob(t, w.Body.Bytes())
	}
}

func TestDockerFolderRoute(t *testing.T) {
	s := newDockerAPI(t)
	if w := s.do("GET", "/api/v1/docker/folders?path=%2Fsrv%2Fpools", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"/srv/pools/tank"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/docker/folders", ""); w.Code != 200 {
		t.Fatalf("Übersicht %d %s", w.Code, w.Body)
	}
	for _, bad := range []string{"%2Fsrv%2Fpools%2F..%2F..%2Fetc", "relativ"} { // Wurzelprüfung (z. B. /etc) macht der Agent
		if w := s.do("GET", "/api/v1/docker/folders?path="+bad, ""); w.Code != 400 {
			t.Errorf("%s: %d %s", bad, w.Code, w.Body)
		}
	}
}

func TestDockerDevicesRoute(t *testing.T) {
	s := newDockerAPI(t)
	if w := s.do("GET", "/api/v1/docker/devices", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"gid":991`) || !strings.Contains(w.Body.String(), `"/dev/dri/renderD128"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestDockerBackupRoutes(t *testing.T) {
	s := newDockerAPI(t)
	if w := s.do("GET", "/api/v1/stacks/media/backup-plan", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"possible":true`) || !strings.Contains(w.Body.String(), `"totalBytes":1234`) {
		t.Fatalf("Plan %d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/stacks/Böse/backup-plan", ""); w.Code != 400 {
		t.Fatalf("Name %d %s", w.Code, w.Body)
	}
	// Ohne Body, mit Optionen und mit kaputtem Body.
	if w := s.do("POST", "/api/v1/stacks/media/update", ""); w.Code != 202 {
		t.Fatalf("ohne Body %d %s", w.Code, w.Body)
	} else {
		s.waitJob(t, w.Body.Bytes())
	}
	if w := s.do("POST", "/api/v1/stacks/media/update", `{"backup":true}`); w.Code != 202 {
		t.Fatalf("mit Option %d %s", w.Code, w.Body)
	} else {
		s.waitJob(t, w.Body.Bytes())
	}
	if w := s.do("POST", "/api/v1/stacks/media/update", `{kaputt`); w.Code != 400 {
		t.Fatalf("kaputt %d %s", w.Code, w.Body)
	}
}

func TestDockerCreateFolderRoute(t *testing.T) {
	s := newDockerAPI(t)
	w := s.do("POST", "/api/v1/docker/folders", `{"path":"/srv/pools/tank","name":"Filme"}`)
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"/srv/pools/tank/Filme"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	for _, bad := range []string{`{"path":"/srv/pools/../../etc","name":"x"}`, `{kaputt`} {
		if w := s.do("POST", "/api/v1/docker/folders", bad); w.Code != 400 {
			t.Errorf("%s: %d %s", bad, w.Code, w.Body)
		}
	}
}

func TestDockerProxyRoutes(t *testing.T) {
	s := newDockerAPI(t)
	if w := s.do("GET", "/api/v1/docker/proxy", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":false`) || !strings.Contains(w.Body.String(), `"httpsPort":8443`) || !strings.Contains(w.Body.String(), `"sites":[]`) {
		t.Fatalf("Standard %d %s", w.Code, w.Body)
	}
	body := `{"enabled":true,"email":"a@example.de","httpPort":8080,"httpsPort":8443,"sites":[{"domain":"Jf.Example.de","host":"","port":8096,"https":false,"public":false}]}`
	w := s.do("PUT", "/api/v1/docker/proxy", body)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"domain":"jf.example.de"`) {
		t.Fatalf("speichern %d %s", w.Code, w.Body)
	}
	for _, bad := range []string{
		`{"enabled":true,"httpPort":8080,"httpsPort":443,"sites":[]}`,
		`{"enabled":true,"httpPort":8080,"httpsPort":8443,"sites":[{"domain":"x y.de","port":1}]}`,
		`{"enabled":true,"httpPort":8080,"httpsPort":8443,"sites":[{"domain":"a.example.de","host":"8.8.8.8","port":80}]}`,
		`{kaputt`,
	} {
		if w := s.do("PUT", "/api/v1/docker/proxy", bad); w.Code != 400 {
			t.Errorf("%s: %d %s", bad, w.Code, w.Body)
		}
	}
	// Anwenden mit ausgeschaltetem Proxy: nur ein Stack-Schritt, damit der Test schnell bleibt.
	if w := s.do("PUT", "/api/v1/docker/proxy", `{"enabled":false,"httpPort":8080,"httpsPort":8443,"sites":[]}`); w.Code != 200 {
		t.Fatalf("ausschalten %d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/docker/proxy/apply", ""); w.Code != 202 {
		t.Fatalf("apply %d %s", w.Code, w.Body)
	} else {
		s.waitJob(t, w.Body.Bytes())
	}
}
