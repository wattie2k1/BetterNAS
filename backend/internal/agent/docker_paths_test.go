package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/docker"
)

// useLegacyPaths setzt den Zustand bestehender Installationen (alles unter /srv/appdata) für die Dauer des Tests.
func useLegacyPaths(t *testing.T) {
	t.Helper()
	p := docker.LegacyPaths
	curPaths.Store(&p)
	pathsProblem.Store(nil)
	t.Cleanup(func() { curPaths.Store(nil); pathsProblem.Store(nil) })
}

func resetPaths(t *testing.T) {
	t.Helper()
	curPaths.Store(nil)
	pathsProblem.Store(nil)
	t.Cleanup(func() { curPaths.Store(nil); pathsProblem.Store(nil) })
}

func TestInitPathsFile(t *testing.T) {
	d, _, _ := dockerEnv(t)
	resetPaths(t)
	_ = d.FS.WriteFile(pathsFile, []byte(`{"appdata":"/srv/pools/a/cfg","dockerdata":"/srv/pools/b/dd"}`), 0o644)
	if err := initPaths(d); err != nil {
		t.Fatal(err)
	}
	p, err := requirePaths()
	if err != nil || p.Appdata != "/srv/pools/a/cfg" || p.Dockerdata != "/srv/pools/b/dd" {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestInitPathsFreshIsNil(t *testing.T) {
	d, _, _ := dockerEnv(t)
	resetPaths(t)
	if err := initPaths(d); err != nil {
		t.Fatal(err)
	}
	if _, err := requirePaths(); code(err) != apierr.DockerNotConfigured {
		t.Fatalf("%v", err)
	}
	if d.FS.Exists(pathsFile) {
		t.Fatal("paths.json darf nicht entstehen")
	}
}

func TestInitPathsLegacyByStack(t *testing.T) {
	d, _, _ := dockerEnv(t)
	resetPaths(t)
	_ = d.FS.MkdirAll("/srv/appdata/stacks/emby", 0o700)
	_ = d.FS.WriteFile("/srv/appdata/stacks/emby/compose.yaml", []byte("services: {}\n"), 0o600)
	if err := initPaths(d); err != nil {
		t.Fatal(err)
	}
	p, err := requirePaths()
	if err != nil || p != docker.LegacyPaths || !d.FS.Exists(pathsFile) {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestInitPathsLegacyByDataRoot(t *testing.T) {
	d, _, _ := dockerEnv(t)
	resetPaths(t)
	_ = d.FS.MkdirAll("/etc/docker", 0o755)
	_ = d.FS.WriteFile(dockerDaemonFile, []byte(`{"data-root":"/srv/appdata/docker"}`), 0o644)
	if err := initPaths(d); err != nil {
		t.Fatal(err)
	}
	if p, err := requirePaths(); err != nil || p != docker.LegacyPaths {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestInitPathsCorruptStaysLocked(t *testing.T) {
	d, _, _ := dockerEnv(t)
	resetPaths(t)
	_ = d.FS.MkdirAll("/srv/appdata/stacks/emby", 0o700)
	_ = d.FS.WriteFile("/srv/appdata/stacks/emby/compose.yaml", []byte("x"), 0o600)
	_ = d.FS.WriteFile(pathsFile, []byte(`{x`), 0o644)
	if err := initPaths(d); err == nil {
		t.Fatal("Fehler erwartet")
	}
	_, err := requirePaths()
	if code(err) != apierr.DockerNotConfigured || apierr.From(err).Message == "" {
		t.Fatalf("%v", err)
	}
	if b, _ := d.FS.ReadFile(pathsFile); string(b) != `{x` {
		t.Fatalf("Datei verändert: %s", b)
	}
}

func TestGuardedLocksHandler(t *testing.T) {
	resetPaths(t)
	called := false
	h := guarded(func(context.Context, json.RawMessage) (any, error) { called = true; return 1, nil })
	if _, err := h(context.Background(), nil); code(err) != apierr.DockerNotConfigured || called {
		t.Fatalf("%v %v", err, called)
	}
	useLegacyPaths(t)
	if v, err := h(context.Background(), nil); err != nil || v != 1 || !called {
		t.Fatalf("%v %v", v, err)
	}
}

func TestStackPathsFollowPaths(t *testing.T) {
	resetPaths(t)
	p := docker.Paths{Appdata: "/srv/pools/a/cfg", Dockerdata: "/srv/pools/b/dd"}
	curPaths.Store(&p)
	if got := composePath("x"); got != "/srv/pools/a/cfg/stacks/x/compose.yaml" {
		t.Fatal(got)
	}
	if got := envPath("x"); got != "/srv/pools/a/cfg/stacks/x/.env" {
		t.Fatal(got)
	}
	if got := rollbackPath("x", "r.json"); got != "/srv/pools/a/cfg/stacks/x/r.json" {
		t.Fatal(got)
	}
	if got := proxyNew(); got != "/srv/pools/a/cfg/reverse-proxy/conf/Caddyfile.new" {
		t.Fatal(got)
	}
}

func TestDockerRPCsLockedWhenNotConfigured(t *testing.T) {
	d, _, _ := dockerEnv(t)
	c := rpcClient(t, d) // Register ruft initPaths: frische Installation, nicht eingerichtet
	ctx := context.Background()
	for _, m := range []string{"stack.list", "container.list", "proxy.write", "image.list", "container.stats", "device.list", "stack.images"} {
		if err := c.Call(ctx, m, map[string]string{}, nil); code(err) != apierr.DockerNotConfigured {
			t.Errorf("%s: %v", m, err)
		}
	}
	for _, m := range []string{"docker.status", "folder.list"} {
		if err := c.Call(ctx, m, map[string]string{}, nil); code(err) == apierr.DockerNotConfigured {
			t.Errorf("%s darf nicht gesperrt sein", m)
		}
	}
}
