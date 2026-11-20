package agent

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/docker"
	"github.com/betternas/betternas/internal/rpc"
)

// pathsFile hält die eingerichteten Docker-Speicherorte (docker.Paths als JSON).
const pathsFile = "/etc/betternas/paths.json"

// curPaths sind die eingerichteten Speicherorte; nil, solange Docker nicht eingerichtet ist.
var curPaths atomic.Pointer[docker.Paths]

// pathsProblem merkt sich, warum paths.json nicht geladen werden konnte (Docker bleibt dann gesperrt).
var pathsProblem atomic.Pointer[string]

func notConfigured() *apierr.Error {
	if msg := pathsProblem.Load(); msg != nil {
		return apierr.New(apierr.DockerNotConfigured, *msg)
	}
	return apierr.New(apierr.DockerNotConfigured, "Docker ist noch nicht eingerichtet. Lege unter Einstellungen → Docker die Speicherorte fest.")
}

// requirePaths liefert die Speicherorte oder docker_not_configured.
func requirePaths() (docker.Paths, error) {
	if p := curPaths.Load(); p != nil {
		return *p, nil
	}
	return docker.Paths{}, notConfigured()
}

// guarded sperrt einen Handler, solange Docker nicht eingerichtet ist.
func guarded(h rpc.Handler) rpc.Handler {
	return func(ctx context.Context, p json.RawMessage) (any, error) {
		if _, err := requirePaths(); err != nil {
			return nil, err
		}
		return h(ctx, p)
	}
}

// stackDir ist das Verzeichnis eines Stacks. Nur nach requirePaths aufrufen (die Docker-RPCs sind gesperrt, bis
// Pfade vorhanden sind); ohne Einrichtung zeigt der Pfad bewusst ins Leere.
func stackDir(name string) string { return path.Join(stacksDir(), name) }

func stacksDir() string {
	if p := curPaths.Load(); p != nil {
		return p.StacksDir()
	}
	return "/nonexistent/docker-not-configured/stacks"
}

func proxyDir() string {
	if p := curPaths.Load(); p != nil {
		return p.ProxyDir()
	}
	return "/nonexistent/docker-not-configured/reverse-proxy"
}

// dockerReg registriert Docker-Methoden und sperrt sie, solange Docker nicht eingerichtet ist.
// Ausgenommen sind Status und Ordnerauswahl (für die Einrichtung nötig).
type dockerReg struct{ s *rpc.Server }

var unguardedMethods = map[string]bool{"docker.status": true, "folder.list": true, "folder.create": true, "docker.paths.get": true, "docker.paths.set": true}

func (r dockerReg) Handle(method string, h rpc.Handler) {
	if !unguardedMethods[method] {
		h = guarded(h)
	}
	r.s.Handle(method, h)
}

func (r dockerReg) HandleSession(method string, h rpc.SessionHandler) {
	r.s.HandleSession(method, func(ctx context.Context, p json.RawMessage, in <-chan rpc.Event, emit func(rpc.Event) error) error {
		if _, err := requirePaths(); err != nil {
			return err
		}
		return h(ctx, p, in, emit)
	})
}

// initPaths lädt paths.json. Fehlt sie, gilt eine Installation mit vorhandenen Stacks oder mit data-root unter
// /srv/appdata als bestehend (Legacy: beide Wurzeln /srv/appdata, die Datei wird angelegt); sonst ist Docker
// nicht eingerichtet. Eine kaputte Datei lässt Docker gesperrt und wird nie überschrieben.
func initPaths(d Deps) error {
	curPaths.Store(nil)
	pathsProblem.Store(nil)
	fail := func(msg string) error {
		pathsProblem.Store(&msg)
		return apierr.New(apierr.Conflict, msg)
	}
	if d.FS.Exists(pathsFile) {
		b, err := d.FS.ReadFile(pathsFile)
		if err != nil {
			return fail("paths.json konnte nicht gelesen werden: " + err.Error())
		}
		var p docker.Paths
		if err := json.Unmarshal(b, &p); err != nil {
			return fail("paths.json ist ungültig (kein gültiges JSON). Docker bleibt gesperrt, bis die Datei repariert ist.")
		}
		if err := p.Validate(true); err != nil {
			return fail("paths.json enthält ungültige Speicherorte: " + apierr.From(err).Message)
		}
		curPaths.Store(&p)
		return nil
	}
	legacy := false
	if files, err := d.FS.Glob(path.Join(docker.LegacyPaths.StacksDir(), "*", docker.ComposeFile)); err == nil && len(files) > 0 {
		legacy = true
	}
	if m, err := readDaemon(d); err == nil {
		if v, _ := m["data-root"].(string); v == docker.LegacyPaths.DataRoot() {
			legacy = true
		}
	}
	if !legacy {
		return nil
	}
	if err := writePathsFile(d, docker.LegacyPaths); err != nil {
		return fail("paths.json konnte nicht angelegt werden: " + err.Error())
	}
	p := docker.LegacyPaths
	curPaths.Store(&p)
	return nil
}

func writePathsFile(d Deps, p docker.Paths) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := d.FS.MkdirAll(path.Dir(pathsFile), 0o755); err != nil {
		return err
	}
	return d.FS.WriteFile(pathsFile, append(b, '\n'), 0o644)
}

func isLegacyRoot(p string) bool {
	return p == "/srv/appdata" || strings.HasPrefix(p, "/srv/appdata/")
}

var srvPathRe = regexp.MustCompile(`/srv/[A-Za-z0-9_.+@/-]+`)

// countStaleStacks zählt Stacks, deren compose.yaml Pfade unter /srv enthält, die weder unter den eingerichteten
// Speicherorten noch unter Pools, Platten oder VMs liegen (sie stammen aus einer früheren Einstellung).
func countStaleStacks(d Deps, p docker.Paths) int {
	files, err := d.FS.Glob(path.Join(p.StacksDir(), "*", docker.ComposeFile))
	if err != nil {
		return 0
	}
	roots := []string{p.Appdata, p.Dockerdata, "/srv/pools", "/srv/disks", "/srv/vms"}
	n := 0
	for _, f := range files {
		b, err := d.FS.ReadFile(f)
		if err != nil {
			continue
		}
		for _, m := range srvPathRe.FindAllString(string(b), -1) {
			if !underAny(roots, path.Clean(m)) {
				n++
				break
			}
		}
	}
	return n
}

// checkRoot prüft das Dateisystem eines Speicherorts. Harte Fehler: Netzwerkfreigaben und (für dockerdata)
// mergerfs. Weiche Hinweise kommen als Warnung zurück.
func checkRoot(ctx context.Context, d Deps, real, label string, dockerRoot bool) (string, error) {
	var warns []string
	if res, err := d.Runner.Run(ctx, nil, "findmnt", "-n", "-o", "FSTYPE", "-T", real); err == nil && res.ExitCode == 0 {
		switch fs := strings.TrimSpace(string(res.Stdout)); fs {
		case "nfs", "nfs4", "cifs", "smb3", "fuse.sshfs":
			return "", apierr.New(apierr.InvalidParams, "Der Speicherort für "+label+" liegt auf einer Netzwerkfreigabe ("+fs+"). Wähle einen Ordner auf einer lokalen Platte oder einem lokalen Pool.")
		case "fuse.mergerfs":
			if dockerRoot {
				return "", apierr.New(apierr.InvalidParams, "Der Speicherort für "+label+" liegt auf einem mergerfs-Pool. Docker läuft darauf nicht zuverlässig. Wähle einen btrfs-Pool oder eine einzelne Platte.")
			}
			warns = append(warns, "Der Speicherort für "+label+" liegt auf einem mergerfs-Pool. Datenbanken und Sperrdateien laufen darauf nicht immer zuverlässig; besser ist ein btrfs-Pool oder eine einzelne Platte.")
		}
	}
	dev := func(p string) string {
		res, err := d.Runner.Run(ctx, nil, "stat", "-c", "%d", "--", p)
		if err != nil || res.ExitCode != 0 {
			return ""
		}
		return strings.TrimSpace(string(res.Stdout))
	}
	if a, b := dev(real), dev("/"); a != "" && a == b {
		warns = append(warns, "Der Speicherort für "+label+" liegt auf der Systemplatte. Bereite für Docker besser eine eigene Platte oder einen Pool vor.")
	}
	return strings.Join(warns, " "), nil
}

// GetPaths liefert den Zustand der Docker-Speicherorte.
func GetPaths(ctx context.Context, d Deps) (docker.PathsInfo, error) {
	info := docker.PathsInfo{Warnings: []string{}}
	p := curPaths.Load()
	if p == nil {
		return info, nil
	}
	info.Configured, info.Appdata, info.Dockerdata = true, p.Appdata, p.Dockerdata
	for _, r := range []struct {
		path, label string
		dockerRoot  bool
	}{{p.Appdata, "Einstellungen (appdata)", false}, {p.Dockerdata, "Daten (dockerdata)", true}} {
		if w, err := checkRoot(ctx, d, r.path, r.label, r.dockerRoot); err == nil && w != "" {
			info.Warnings = append(info.Warnings, w)
		} else if err != nil {
			info.Warnings = append(info.Warnings, apierr.From(err).Message)
		}
	}
	info.StaleStacks = countStaleStacks(d, *p)
	return info, nil
}

// SetPaths prüft und speichert die Docker-Speicherorte. Bei der Ersteinrichtung schreibt es außerdem die
// daemon.json (nur wenn keine vorhanden ist) und startet Docker. Spätere Änderungen gelten nur für Neues:
// weder daemon.json noch laufende Stacks werden angefasst.
func SetPaths(ctx context.Context, d Deps, in docker.Paths) (docker.PathsInfo, error) {
	mutate.Lock()
	defer mutate.Unlock()
	cur := curPaths.Load()
	allowLegacy := cur != nil && (isLegacyRoot(cur.Appdata) || isLegacyRoot(cur.Dockerdata))
	if err := in.Validate(allowLegacy); err != nil {
		return docker.PathsInfo{}, err
	}
	a, _ := docker.CleanRoot(in.Appdata, allowLegacy)
	dd, _ := docker.CleanRoot(in.Dockerdata, allowLegacy)
	next := docker.Paths{Appdata: a, Dockerdata: dd}
	if cur != nil && cur.Appdata != next.Appdata {
		if files, _ := d.FS.Glob(path.Join(cur.StacksDir(), "*", docker.ComposeFile)); len(files) > 0 {
			return docker.PathsInfo{}, apierr.New(apierr.Conflict, "Unter "+cur.Appdata+" gibt es noch Stacks. Der Speicherort für Einstellungen lässt sich erst ändern, wenn keine Stacks mehr vorhanden sind (vorhandene Daten werden nicht verschoben).")
		}
	}
	var warns []string
	for _, r := range []struct {
		path, label string
		dockerRoot  bool
	}{{next.Appdata, "Einstellungen (appdata)", false}, {next.Dockerdata, "Daten (dockerdata)", true}} {
		res, err := d.Runner.Run(ctx, nil, "realpath", "-e", "--", r.path)
		if err != nil || res.ExitCode != 0 {
			return docker.PathsInfo{}, apierr.New(apierr.NotFound, "Der Ordner für "+r.label+" wurde nicht gefunden. Lege ihn zuerst an.")
		}
		real := strings.TrimSpace(string(res.Stdout))
		if _, err := docker.CleanRoot(real, allowLegacy); err != nil {
			return docker.PathsInfo{}, apierr.New(apierr.InvalidParams, "Der Ordner für "+r.label+" führt über einen Link aus den erlaubten Bereichen hinaus.")
		}
		w, err := checkRoot(ctx, d, real, r.label, r.dockerRoot)
		if err != nil {
			return docker.PathsInfo{}, err
		}
		if w != "" {
			warns = append(warns, w)
		}
	}
	first := cur == nil
	dirs := []struct {
		p    string
		perm os.FileMode
	}{{next.StacksDir(), 0o700}, {next.ProxyDir(), 0o755}, {path.Dir(next.BackupBase()), 0o755}}
	if first {
		dirs = append(dirs, struct {
			p    string
			perm os.FileMode
		}{next.DataRoot(), 0o710})
	}
	for _, dir := range dirs {
		if err := d.FS.MkdirAll(dir.p, dir.perm); err != nil {
			return docker.PathsInfo{}, apierr.New(apierr.Internal, "Ordner konnte nicht angelegt werden: "+dir.p).With("detail", err.Error())
		}
	}
	wroteDaemon := false
	undo := func() {
		_ = d.FS.Remove(pathsFile)
		if wroteDaemon {
			_ = d.FS.Remove(dockerDaemonFile)
		}
		curPaths.Store(nil)
	}
	if first && !d.FS.Exists(dockerDaemonFile) {
		b, err := json.MarshalIndent(map[string]any{
			"data-root":  next.DataRoot(),
			"log-driver": "json-file",
			"log-opts":   map[string]string{"max-size": "10m", "max-file": "3"},
		}, "", "  ")
		if err == nil {
			err = d.FS.MkdirAll(path.Dir(dockerDaemonFile), 0o755)
		}
		if err == nil {
			err = d.FS.WriteFile(dockerDaemonFile, append(b, '\n'), 0o644)
		}
		if err != nil {
			return docker.PathsInfo{}, apierr.New(apierr.Internal, "daemon.json konnte nicht geschrieben werden").With("detail", err.Error())
		}
		wroteDaemon = true
	}
	if err := writePathsFile(d, next); err != nil {
		undo()
		return docker.PathsInfo{}, apierr.New(apierr.Internal, "paths.json konnte nicht geschrieben werden").With("detail", err.Error())
	}
	curPaths.Store(&next)
	pathsProblem.Store(nil)
	if first {
		if _, err := run(ctx, d.Runner, nil, "systemctl", "enable", "--now", "docker.socket", "docker.service"); err != nil {
			undo()
			return docker.PathsInfo{}, apierr.New(apierr.Internal, "Docker konnte nicht gestartet werden").With("detail", err.Error())
		}
	}
	if warns == nil {
		warns = []string{}
	}
	return docker.PathsInfo{Configured: true, Appdata: next.Appdata, Dockerdata: next.Dockerdata, Warnings: warns, StaleStacks: countStaleStacks(d, next)}, nil
}
