package agent

import (
	"context"
	"encoding/json"
	"github.com/betternas/betternas/internal/sysexec"
	"strings"
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

// pathsEnv richtet Runner-Antworten für SetPaths ein: realpath (mit Links), findmnt (Dateisystemtyp je Pfad) und stat (Gerätenummer).
type pathsFake struct {
	links         map[string]string // Pfad → realpath-Ergebnis (sonst identisch); "-" heißt: nicht vorhanden
	fstype        map[string]string // Pfad → Dateisystemtyp (Vorgabe ext4)
	dev           map[string]string // Pfad → Gerätenummer (Vorgabe 99, "/" ist 1)
	failSystemctl bool
}

func pathsEnv(t *testing.T, pf *pathsFake) (Deps, *sysexec.FakeRunner) {
	t.Helper()
	d, f, _ := dockerEnv(t)
	resetPaths(t)
	f.Hook = func(call string) (sysexec.Result, bool) {
		switch {
		case strings.HasPrefix(call, "realpath -e -- "):
			p := strings.TrimPrefix(call, "realpath -e -- ")
			if r, ok := pf.links[p]; ok {
				if r == "-" {
					return sysexec.Result{ExitCode: 1}, true
				}
				return sysexec.Result{Stdout: []byte(r + "\n")}, true
			}
			return sysexec.Result{Stdout: []byte(p + "\n")}, true
		case strings.HasPrefix(call, "findmnt -n -o FSTYPE -T "):
			p := strings.TrimPrefix(call, "findmnt -n -o FSTYPE -T ")
			if v, ok := pf.fstype[p]; ok {
				return sysexec.Result{Stdout: []byte(v + "\n")}, true
			}
			return sysexec.Result{Stdout: []byte("ext4\n")}, true
		case strings.HasPrefix(call, "stat -c %d -- "):
			p := strings.TrimPrefix(call, "stat -c %d -- ")
			if v, ok := pf.dev[p]; ok {
				return sysexec.Result{Stdout: []byte(v + "\n")}, true
			}
			if p == "/" {
				return sysexec.Result{Stdout: []byte("1\n")}, true
			}
			return sysexec.Result{Stdout: []byte("99\n")}, true
		case call == "systemctl enable --now docker.socket docker.service":
			if pf.failSystemctl {
				return sysexec.Result{ExitCode: 1, Stderr: []byte("boom")}, true
			}
			return sysexec.Result{}, true
		}
		return sysexec.Result{}, false
	}
	return d, f
}

var twoPools = docker.Paths{Appdata: "/srv/pools/a/cfg", Dockerdata: "/srv/pools/b/dd"}

func TestSetPathsFirstSetupWritesFilesAndStartsDocker(t *testing.T) {
	d, f := pathsEnv(t, &pathsFake{})
	info, err := SetPaths(context.Background(), d, twoPools)
	if err != nil || !info.Configured || info.Appdata != twoPools.Appdata || info.Dockerdata != twoPools.Dockerdata {
		t.Fatalf("%+v %v", info, err)
	}
	b, _ := d.FS.ReadFile(pathsFile)
	var got docker.Paths
	if json.Unmarshal(b, &got) != nil || got != twoPools {
		t.Fatalf("paths.json: %s", b)
	}
	m, err := readDaemon(d)
	if err != nil || m["data-root"] != "/srv/pools/b/dd/docker" || m["log-driver"] != "json-file" {
		t.Fatalf("daemon.json: %v %v", m, err)
	}
	for _, dir := range []string{"/srv/pools/a/cfg/stacks", "/srv/pools/a/cfg/reverse-proxy", "/srv/pools/a/cfg/backups", "/srv/pools/b/dd/docker"} {
		if !d.FS.Exists(dir) {
			t.Errorf("%s fehlt", dir)
		}
	}
	if !f.Called("systemctl enable --now docker.socket docker.service") {
		t.Fatalf("%v", f.Calls)
	}
	if _, err := requirePaths(); err != nil {
		t.Fatal(err)
	}
}

func TestSetPathsKeepsExistingDaemonJson(t *testing.T) {
	d, _ := pathsEnv(t, &pathsFake{})
	_ = d.FS.MkdirAll("/etc/docker", 0o755)
	_ = d.FS.WriteFile(dockerDaemonFile, []byte(`{"data-root":"/mnt/eigen"}`), 0o644)
	if _, err := SetPaths(context.Background(), d, twoPools); err != nil {
		t.Fatal(err)
	}
	if b, _ := d.FS.ReadFile(dockerDaemonFile); string(b) != `{"data-root":"/mnt/eigen"}` {
		t.Fatalf("daemon.json verändert: %s", b)
	}
}

func TestSetPathsRejectsMergerfsForDockerdataWarnsForAppdata(t *testing.T) {
	d, _ := pathsEnv(t, &pathsFake{fstype: map[string]string{"/srv/pools/b/dd": "fuse.mergerfs"}})
	if _, err := SetPaths(context.Background(), d, twoPools); code(err) != apierr.InvalidParams {
		t.Fatalf("dockerdata auf mergerfs: %v", err)
	}
	if _, err := requirePaths(); err == nil {
		t.Fatal("darf nicht eingerichtet sein")
	}
	d, _ = pathsEnv(t, &pathsFake{fstype: map[string]string{"/srv/pools/a/cfg": "fuse.mergerfs"}})
	info, err := SetPaths(context.Background(), d, twoPools)
	if err != nil || len(info.Warnings) != 1 || !strings.Contains(info.Warnings[0], "mergerfs") {
		t.Fatalf("%+v %v", info, err)
	}
}

func TestSetPathsRejectsNetworkFS(t *testing.T) {
	for _, fs := range []string{"nfs4", "cifs", "fuse.sshfs"} {
		d, _ := pathsEnv(t, &pathsFake{fstype: map[string]string{"/srv/pools/a/cfg": fs}})
		if _, err := SetPaths(context.Background(), d, twoPools); code(err) != apierr.InvalidParams {
			t.Errorf("%s: %v", fs, err)
		}
	}
}

func TestSetPathsWarnsSameDeviceAsRoot(t *testing.T) {
	d, _ := pathsEnv(t, &pathsFake{dev: map[string]string{"/srv/pools/a/cfg": "1"}})
	info, err := SetPaths(context.Background(), d, twoPools)
	if err != nil || len(info.Warnings) != 1 || !strings.Contains(info.Warnings[0], "Systemplatte") {
		t.Fatalf("%+v %v", info, err)
	}
}

func TestSetPathsRejectsInvalid(t *testing.T) {
	d, _ := pathsEnv(t, &pathsFake{links: map[string]string{"/srv/pools/a/link": "/etc", "/srv/pools/a/fehlt": "-"}})
	for name, in := range map[string]docker.Paths{
		"verschachtelt": {Appdata: "/srv/pools/a/cfg", Dockerdata: "/srv/pools/a/cfg/dd"},
		"gleich":        {Appdata: "/srv/pools/a/cfg", Dockerdata: "/srv/pools/a/cfg"},
		"außerhalb":     {Appdata: "/etc", Dockerdata: "/srv/pools/b/dd"},
		"link":          {Appdata: "/srv/pools/a/link", Dockerdata: "/srv/pools/b/dd"},
		"fehlt":         {Appdata: "/srv/pools/a/fehlt", Dockerdata: "/srv/pools/b/dd"},
		"leer":          {},
		"srv-appdata":   {Appdata: "/srv/appdata", Dockerdata: "/srv/pools/b/dd"}, // nur bei bestehenden Installationen
	} {
		if _, err := SetPaths(context.Background(), d, in); err == nil {
			t.Errorf("%s akzeptiert", name)
		}
	}
	if d.FS.Exists(pathsFile) {
		t.Fatal("paths.json darf nicht entstehen")
	}
}

func TestSetPathsRollbackWhenSystemctlFails(t *testing.T) {
	d, _ := pathsEnv(t, &pathsFake{failSystemctl: true})
	if _, err := SetPaths(context.Background(), d, twoPools); err == nil {
		t.Fatal("Fehler erwartet")
	}
	if _, err := requirePaths(); code(err) != apierr.DockerNotConfigured {
		t.Fatalf("%v", err)
	}
	if d.FS.Exists(pathsFile) || d.FS.Exists(dockerDaemonFile) {
		t.Fatal("Dateien müssen zurückgenommen sein")
	}
}

func TestSetPathsChangeLaterDoesNotTouchDaemonJsonOrSystemd(t *testing.T) {
	d, f := pathsEnv(t, &pathsFake{})
	if _, err := SetPaths(context.Background(), d, twoPools); err != nil {
		t.Fatal(err)
	}
	before, _ := d.FS.ReadFile(dockerDaemonFile)
	n := len(f.Calls)
	if _, err := SetPaths(context.Background(), d, docker.Paths{Appdata: twoPools.Appdata, Dockerdata: "/srv/pools/c/neu"}); err != nil {
		t.Fatal(err)
	}
	after, _ := d.FS.ReadFile(dockerDaemonFile)
	if string(before) != string(after) {
		t.Fatalf("daemon.json verändert: %s", after)
	}
	for _, c := range f.Calls[n:] {
		if strings.HasPrefix(c, "systemctl") {
			t.Fatalf("systemctl bei Änderung: %s", c)
		}
	}
	if p, _ := requirePaths(); p.Dockerdata != "/srv/pools/c/neu" {
		t.Fatalf("%+v", p)
	}
}

func TestSetPathsRefusesAppdataChangeWhileStacksExist(t *testing.T) {
	d, _ := pathsEnv(t, &pathsFake{})
	if _, err := SetPaths(context.Background(), d, twoPools); err != nil {
		t.Fatal(err)
	}
	saveStack(t, d, "emby")
	_, err := SetPaths(context.Background(), d, docker.Paths{Appdata: "/srv/pools/c/cfg2", Dockerdata: twoPools.Dockerdata})
	if code(err) != apierr.Conflict {
		t.Fatalf("%v", err)
	}
}

func TestSetPathsLegacyKeepsSrvAppdata(t *testing.T) {
	d, _ := pathsEnv(t, &pathsFake{})
	curPaths.Store(&docker.Paths{Appdata: "/srv/appdata", Dockerdata: "/srv/appdata"})
	info, err := SetPaths(context.Background(), d, docker.Paths{Appdata: "/srv/appdata", Dockerdata: "/srv/pools/b/dd"})
	if err != nil || info.Dockerdata != "/srv/pools/b/dd" {
		t.Fatalf("%+v %v", info, err)
	}
	// erneutes Speichern mit demselben Wert bleibt möglich
	if _, err := SetPaths(context.Background(), d, docker.Paths{Appdata: "/srv/appdata", Dockerdata: "/srv/pools/b/dd"}); err != nil {
		t.Fatal(err)
	}
}

func TestGetPathsStaleStacks(t *testing.T) {
	d, _ := pathsEnv(t, &pathsFake{})
	if info, err := GetPaths(context.Background(), d); err != nil || info.Configured {
		t.Fatalf("%+v %v", info, err)
	}
	if _, err := SetPaths(context.Background(), d, twoPools); err != nil {
		t.Fatal(err)
	}
	_ = d.FS.MkdirAll("/srv/pools/a/cfg/stacks/alt", 0o700)
	_ = d.FS.WriteFile("/srv/pools/a/cfg/stacks/alt/compose.yaml", []byte("services:\n  a:\n    volumes:\n      - /srv/appdata/alt/config:/config\n"), 0o600)
	_ = d.FS.MkdirAll("/srv/pools/a/cfg/stacks/neu", 0o700)
	_ = d.FS.WriteFile("/srv/pools/a/cfg/stacks/neu/compose.yaml", []byte("services:\n  a:\n    volumes:\n      - /srv/pools/a/cfg/neu:/config\n      - /srv/pools/Medien:/media\n"), 0o600)
	info, err := GetPaths(context.Background(), d)
	if err != nil || !info.Configured || info.StaleStacks != 1 || info.Warnings == nil {
		t.Fatalf("%+v %v", info, err)
	}
}

func TestDockerStatusReportsConfigured(t *testing.T) {
	d, _ := pathsEnv(t, &pathsFake{})
	if st, _ := DockerStatus(context.Background(), d); st.Configured {
		t.Fatal("frisch: nicht eingerichtet")
	}
	curPaths.Store(&twoPools)
	if st, _ := DockerStatus(context.Background(), d); !st.Configured {
		t.Fatal("eingerichtet erwartet")
	}
}

func TestPathsRPCsWorkWhenNotConfigured(t *testing.T) {
	d, _ := pathsEnv(t, &pathsFake{})
	c := rpcClient(t, d)
	var info docker.PathsInfo
	if err := c.Call(context.Background(), "docker.paths.get", nil, &info); err != nil || info.Configured {
		t.Fatalf("%+v %v", info, err)
	}
	if err := c.Call(context.Background(), "docker.paths.set", twoPools, &info); err != nil || !info.Configured {
		t.Fatalf("%+v %v", info, err)
	}
	if err := c.Call(context.Background(), "stack.list", nil, nil); err != nil {
		t.Fatalf("nach der Einrichtung entsperrt: %v", err)
	}
}
