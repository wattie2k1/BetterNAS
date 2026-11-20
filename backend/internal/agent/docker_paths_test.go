package agent

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/betternas/betternas/internal/sysexec"
	"os"
	"reflect"
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
	if err != nil || !samePaths(p, docker.LegacyPaths) || !d.FS.Exists(pathsFile) {
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
	if p, err := requirePaths(); err != nil || !samePaths(p, docker.LegacyPaths) {
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
	owner         map[string]string // Pfad → „uid mode“ (Vorgabe „0 755“)
	ftype         map[string]string // Pfad → Dateityp (Vorgabe „directory“)
}

func samePaths(a, b docker.Paths) bool { return reflect.DeepEqual(a, b) }

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
		case strings.HasPrefix(call, "stat -c %u %a -- "):
			p := strings.TrimPrefix(call, "stat -c %u %a -- ")
			if v, ok := pf.owner[p]; ok {
				return sysexec.Result{Stdout: []byte(v + "\n")}, true
			}
			return sysexec.Result{Stdout: []byte("0 755\n")}, true
		case strings.HasPrefix(call, "stat -c %F -- "):
			p := strings.TrimPrefix(call, "stat -c %F -- ")
			if v, ok := pf.ftype[p]; ok {
				return sysexec.Result{Stdout: []byte(v + "\n")}, true
			}
			return sysexec.Result{Stdout: []byte("directory\n")}, true
		case call == "systemctl enable docker.socket docker.service" || call == "systemctl restart docker.service":
			if pf.failSystemctl {
				return sysexec.Result{ExitCode: 1, Stderr: []byte("boom")}, true
			}
			return sysexec.Result{}, true
		case call == "systemctl daemon-reload":
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
	if json.Unmarshal(b, &got) != nil || !samePaths(got, twoPools) {
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
	for _, want := range []string{"systemctl daemon-reload", "systemctl enable docker.socket docker.service", "systemctl restart docker.service"} {
		if !f.Called(want) {
			t.Fatalf("%s fehlt: %v", want, f.Calls)
		}
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
		if strings.HasPrefix(c, "systemctl") && c != "systemctl daemon-reload" { // daemon-reload: neue Mount-Abhängigkeit
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

type failFS struct {
	sysexec.FS
	failPath string
}

func (f failFS) WriteFile(p string, data []byte, perm os.FileMode) error {
	if p == f.failPath {
		return errors.New("Platte voll")
	}
	return f.FS.WriteFile(p, data, perm)
}

func TestSetPathsRejectsForeignOrWritableRoot(t *testing.T) {
	for name, owner := range map[string]string{"fremder Besitzer": "1000 755", "Gruppe schreibbar": "0 775", "alle schreibbar": "0 777"} {
		d, _ := pathsEnv(t, &pathsFake{owner: map[string]string{"/srv/pools/a/cfg": owner}})
		if _, err := SetPaths(context.Background(), d, twoPools); code(err) != apierr.InvalidParams {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestSetPathsRejectsSymlinkedSubdir(t *testing.T) {
	for _, sub := range []string{"/srv/pools/a/cfg/stacks", "/srv/pools/a/cfg/reverse-proxy", "/srv/pools/a/cfg/backups", "/srv/pools/b/dd/docker"} {
		d, _ := pathsEnv(t, &pathsFake{ftype: map[string]string{sub: "symbolic link"}})
		if _, err := SetPaths(context.Background(), d, twoPools); code(err) != apierr.InvalidParams {
			t.Errorf("%s: %v", sub, err)
		}
		if d.FS.Exists(pathsFile) {
			t.Errorf("%s: paths.json darf nicht entstehen", sub)
		}
	}
}

func TestSetPathsStoresResolvedPath(t *testing.T) {
	d, _ := pathsEnv(t, &pathsFake{links: map[string]string{"/srv/pools/a/cfg": "/srv/pools/a/real"}})
	info, err := SetPaths(context.Background(), d, twoPools)
	if err != nil || info.Appdata != "/srv/pools/a/real" {
		t.Fatalf("%+v %v", info, err)
	}
	b, _ := d.FS.ReadFile(pathsFile)
	if !strings.Contains(string(b), "/srv/pools/a/real") || strings.Contains(string(b), "a/cfg") {
		t.Fatalf("%s", b)
	}
}

func TestSetPathsWritesMountDropIn(t *testing.T) {
	d, f := pathsEnv(t, &pathsFake{})
	if _, err := SetPaths(context.Background(), d, twoPools); err != nil {
		t.Fatal(err)
	}
	b, err := d.FS.ReadFile("/etc/systemd/system/docker.service.d/betternas-paths.conf")
	if err != nil || !strings.Contains(string(b), "[Unit]\nRequiresMountsFor=/srv/pools/a/cfg /srv/pools/b/dd\n") {
		t.Fatalf("%s %v", b, err)
	}
	if f.Index("systemctl daemon-reload") > f.Index("systemctl enable docker.socket") {
		t.Fatalf("daemon-reload muss vor dem Start laufen: %v", f.Calls)
	}
}

func TestSetPathsAddsDataRootToExistingDaemonJson(t *testing.T) {
	d, _ := pathsEnv(t, &pathsFake{})
	_ = d.FS.MkdirAll("/etc/docker", 0o755)
	_ = d.FS.WriteFile(dockerDaemonFile, []byte(`{"log-driver":"local","registry-mirrors":["https://m.example"]}`), 0o644)
	if _, err := SetPaths(context.Background(), d, twoPools); err != nil {
		t.Fatal(err)
	}
	m, err := readDaemon(d)
	if err != nil || m["data-root"] != "/srv/pools/b/dd/docker" || m["log-driver"] != "local" || m["registry-mirrors"] == nil {
		t.Fatalf("%v %v", m, err)
	}
}

func TestSetPathsFailedSecondSaveKeepsOldConfig(t *testing.T) {
	d, _ := pathsEnv(t, &pathsFake{})
	if _, err := SetPaths(context.Background(), d, twoPools); err != nil {
		t.Fatal(err)
	}
	before, _ := d.FS.ReadFile(pathsFile)
	d2 := Deps{Runner: d.Runner, FS: failFS{d.FS, pathsFile}}
	if _, err := SetPaths(context.Background(), d2, docker.Paths{Appdata: twoPools.Appdata, Dockerdata: "/srv/pools/c/neu"}); err == nil {
		t.Fatal("Fehler erwartet")
	}
	p, err := requirePaths()
	if err != nil || p.Dockerdata != twoPools.Dockerdata {
		t.Fatalf("%+v %v", p, err)
	}
	if after, _ := d.FS.ReadFile(pathsFile); string(after) != string(before) {
		t.Fatalf("paths.json verändert: %s", after)
	}
}

func TestSetPathsRemembersPreviousRoots(t *testing.T) {
	d, _ := pathsEnv(t, &pathsFake{})
	curPaths.Store(&docker.Paths{Appdata: "/srv/appdata", Dockerdata: "/srv/appdata"})
	if _, err := SetPaths(context.Background(), d, docker.Paths{Appdata: "/srv/appdata", Dockerdata: "/srv/pools/b/dd"}); err != nil {
		t.Fatal(err)
	}
	p, _ := requirePaths()
	if !reflect.DeepEqual(p.Previous, []string{"/srv/appdata"}) {
		t.Fatalf("%+v", p)
	}
	b, _ := d.FS.ReadFile(pathsFile)
	if !strings.Contains(string(b), `"previous"`) {
		t.Fatalf("%s", b)
	}
	// Ein weiterer Wechsel hält die Liste ohne Doppelte
	if _, err := SetPaths(context.Background(), d, docker.Paths{Appdata: "/srv/appdata", Dockerdata: "/srv/pools/c/dd"}); err != nil {
		t.Fatal(err)
	}
	p, _ = requirePaths()
	if !reflect.DeepEqual(p.Previous, []string{"/srv/pools/b/dd", "/srv/appdata"}) && !reflect.DeepEqual(p.Previous, []string{"/srv/appdata", "/srv/pools/b/dd"}) {
		t.Fatalf("%+v", p)
	}
}

func TestSetPathsRefusedWhileFileCorrupt(t *testing.T) {
	d, _ := pathsEnv(t, &pathsFake{})
	_ = d.FS.MkdirAll("/etc/betternas", 0o755)
	_ = d.FS.WriteFile(pathsFile, []byte(`{x`), 0o644)
	_ = initPaths(d)
	if _, err := SetPaths(context.Background(), d, twoPools); code(err) != apierr.Conflict {
		t.Fatalf("%v", err)
	}
	if b, _ := d.FS.ReadFile(pathsFile); string(b) != `{x` {
		t.Fatalf("Datei überschrieben: %s", b)
	}
}

func TestGetPathsReportsProblemAndRereadsRepairedFile(t *testing.T) {
	d, _ := pathsEnv(t, &pathsFake{})
	_ = d.FS.MkdirAll("/etc/betternas", 0o755)
	_ = d.FS.WriteFile(pathsFile, []byte(`{x`), 0o644)
	_ = initPaths(d)
	info, err := GetPaths(context.Background(), d)
	if err != nil || info.Configured || !strings.Contains(info.Problem, "paths.json") {
		t.Fatalf("%+v %v", info, err)
	}
	_ = d.FS.Remove(pathsFile)
	_ = d.FS.WriteFile(pathsFile, []byte(`{"appdata":"/srv/pools/a/cfg","dockerdata":"/srv/pools/b/dd"}`), 0o644)
	info, err = GetPaths(context.Background(), d)
	if err != nil || !info.Configured || info.Problem != "" {
		t.Fatalf("%+v %v", info, err)
	}
}

func TestInitPathsLegacyByDockerDir(t *testing.T) {
	d, _, _ := dockerEnv(t)
	resetPaths(t)
	_ = d.FS.MkdirAll("/srv/appdata/docker", 0o710)
	if err := initPaths(d); err != nil {
		t.Fatal(err)
	}
	if p, err := requirePaths(); err != nil || !samePaths(p, docker.LegacyPaths) {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestInitPathsCleansValuesFromFile(t *testing.T) {
	d, _, _ := dockerEnv(t)
	resetPaths(t)
	_ = d.FS.WriteFile(pathsFile, []byte(`{"appdata":"/srv/pools/a/cfg/","dockerdata":"/srv/pools/b//dd"}`), 0o644)
	if err := initPaths(d); err != nil {
		t.Fatal(err)
	}
	if p, _ := requirePaths(); p.Appdata != "/srv/pools/a/cfg" || p.Dockerdata != "/srv/pools/b/dd" {
		t.Fatalf("%+v", p)
	}
}

func TestDockerSettingsAndServiceLockedWhenNotConfigured(t *testing.T) {
	d, _, _ := dockerEnv(t)
	resetPaths(t)
	c := rpcClient(t, d)
	if err := c.Call(context.Background(), "docker.settings.set", DockerSettings{LogMaxSize: "10m", LogMaxFile: 3}, nil); code(err) != apierr.DockerNotConfigured {
		t.Fatalf("docker.settings.set: %v", err)
	}
	if d.FS.Exists(dockerDaemonFile) {
		t.Fatal("daemon.json darf nicht entstehen")
	}
	for _, a := range []string{"start", "restart"} {
		if err := ServiceAction(context.Background(), d, "docker", a); code(err) != apierr.DockerNotConfigured {
			t.Errorf("docker %s: %v", a, err)
		}
	}
	if err := ServiceAction(context.Background(), d, "docker", "stop"); err != nil {
		t.Errorf("stop muss möglich bleiben: %v", err)
	}
}

func TestSafeBindPathPreviousRoots(t *testing.T) {
	resetPaths(t)
	p := docker.Paths{Appdata: "/srv/pools/a/cfg", Dockerdata: "/srv/pools/b/dd", Previous: []string{"/srv/appdata"}}
	curPaths.Store(&p)
	for path, want := range map[string]bool{"/srv/appdata/emby/config": true, "/srv/appdata/stacks/x": false, "/srv/appdata/docker/volumes": false, "/srv/appdata": false} {
		if got := safeBindPath(path); got != want {
			t.Errorf("safeBindPath(%q) = %v", path, got)
		}
	}
}

func TestShQuoteEscapesSingleQuotes(t *testing.T) {
	if got := shQuote([]string{"a'b", "c d"}); got != `'a'\''b' 'c d'` {
		t.Fatal(got)
	}
}
