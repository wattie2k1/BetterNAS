package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path"
	"regexp"
	"strconv"
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

// loadPathsFile liest und prüft paths.json und liefert die bereinigten Werte.
func loadPathsFile(d Deps) (docker.Paths, string) {
	b, err := d.FS.ReadFile(pathsFile)
	if err != nil {
		return docker.Paths{}, "paths.json konnte nicht gelesen werden: " + err.Error()
	}
	var p docker.Paths
	if err := json.Unmarshal(b, &p); err != nil {
		return docker.Paths{}, "paths.json ist ungültig (kein gültiges JSON). Docker bleibt gesperrt, bis die Datei repariert oder entfernt ist."
	}
	if err := p.Validate(true); err != nil {
		return docker.Paths{}, "paths.json enthält ungültige Speicherorte: " + apierr.From(err).Message
	}
	a, _ := docker.CleanRoot(p.Appdata, true)
	dd, _ := docker.CleanRoot(p.Dockerdata, true)
	out := docker.Paths{Appdata: a, Dockerdata: dd}
	for _, r := range p.Previous {
		if c, err := docker.CleanRoot(r, true); err == nil {
			out.Previous = append(out.Previous, c)
		}
	}
	return out, ""
}

// initPaths lädt paths.json. Fehlt sie, gilt eine Installation mit vorhandenen Stacks, mit /srv/appdata/docker oder mit
// data-root unter /srv/appdata als bestehend (Legacy: beide Wurzeln /srv/appdata, die Datei wird angelegt); sonst ist
// Docker nicht eingerichtet. Diese Kriterien entsprechen dem Docker-Block in postinst.sh. Eine kaputte Datei lässt
// Docker gesperrt und wird nie überschrieben.
func initPaths(d Deps) error {
	curPaths.Store(nil)
	pathsProblem.Store(nil)
	fail := func(msg string) error {
		pathsProblem.Store(&msg)
		return apierr.New(apierr.Conflict, msg)
	}
	if d.FS.Exists(pathsFile) {
		p, problem := loadPathsFile(d)
		if problem != "" {
			return fail(problem)
		}
		curPaths.Store(&p)
		return nil
	}
	legacy := d.FS.Exists(docker.LegacyPaths.DataRoot())
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
	if curPaths.Load() == nil && pathsProblem.Load() != nil {
		_ = initPaths(d) // eine von Hand reparierte Datei wirkt sofort
	}
	p := curPaths.Load()
	if p == nil {
		if msg := pathsProblem.Load(); msg != nil {
			info.Problem = *msg
		}
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

const dockerMountDropIn = "/etc/systemd/system/docker.service.d/betternas-paths.conf"

// pathMode zerlegt die Ausgabe von `stat -c "%u %a"`.
func pathOwnerMode(ctx context.Context, d Deps, p string) (uid int, mode int64, err error) {
	res, e := d.Runner.Run(ctx, nil, "stat", "-c", "%u %a", "--", p)
	if e != nil || res.ExitCode != 0 {
		return 0, 0, errors.New("stat fehlgeschlagen")
	}
	f := strings.Fields(string(res.Stdout))
	if len(f) != 2 {
		return 0, 0, errors.New("unerwartete Ausgabe von stat")
	}
	u, e1 := strconv.Atoi(f[0])
	m, e2 := strconv.ParseInt(f[1], 8, 32)
	if e1 != nil || e2 != nil {
		return 0, 0, errors.New("unerwartete Ausgabe von stat")
	}
	return u, m, nil
}

// isPlainDir sagt, ob p ein echter Ordner ist (kein Link).
func isPlainDir(ctx context.Context, d Deps, p string) bool {
	res, err := d.Runner.Run(ctx, nil, "stat", "-c", "%F", "--", p)
	return err == nil && res.ExitCode == 0 && strings.TrimSpace(string(res.Stdout)) == "directory"
}

// withPrevious ergänzt die früheren Wurzeln um die bisher geltenden Werte, die sich ändern.
func withPrevious(cur *docker.Paths, next docker.Paths) []string {
	var prev []string
	add := func(r string) {
		if r == "" {
			return
		}
		for _, x := range prev {
			if x == r {
				return
			}
		}
		prev = append(prev, r)
	}
	if cur != nil {
		for _, r := range cur.Previous {
			add(r)
		}
		if cur.Appdata != next.Appdata {
			add(cur.Appdata)
		}
		if cur.Dockerdata != next.Dockerdata {
			add(cur.Dockerdata)
		}
	}
	if len(prev) > 8 {
		prev = prev[len(prev)-8:]
	}
	return prev
}

// SetPaths prüft und speichert die Docker-Speicherorte. Bei der Ersteinrichtung schreibt es außerdem die
// daemon.json (neu oder um den data-root ergänzt) und startet Docker neu. Spätere Änderungen gelten nur für Neues:
// weder daemon.json noch laufende Stacks werden angefasst.
func SetPaths(ctx context.Context, d Deps, in docker.Paths) (docker.PathsInfo, error) {
	mutate.Lock()
	defer mutate.Unlock()
	if msg := pathsProblem.Load(); msg != nil {
		return docker.PathsInfo{}, apierr.New(apierr.Conflict, *msg+" (Speichern ist erst danach möglich, damit nichts überschrieben wird.)")
	}
	cur := curPaths.Load()
	allowLegacy := cur != nil && (isLegacyRoot(cur.Appdata) || isLegacyRoot(cur.Dockerdata))
	if err := in.Validate(allowLegacy); err != nil {
		return docker.PathsInfo{}, err
	}
	a, _ := docker.CleanRoot(in.Appdata, allowLegacy)
	dd, _ := docker.CleanRoot(in.Dockerdata, allowLegacy)
	next := docker.Paths{Appdata: a, Dockerdata: dd}
	// Stacks, Reverse-Proxy und Sicherungen liegen in dockerdata: bei einem Wechsel würden vorhandene Stacks aus der
	// Liste verschwinden. appdata enthält nur App-Einstellungen und lässt sich jederzeit ändern (für Neues).
	if cur != nil && cur.Dockerdata != next.Dockerdata {
		if files, _ := d.FS.Glob(path.Join(cur.StacksDir(), "*", docker.ComposeFile)); len(files) > 0 {
			return docker.PathsInfo{}, apierr.New(apierr.Conflict, "Unter "+cur.Dockerdata+" gibt es noch Stacks. Der Speicherort für Daten lässt sich erst ändern, wenn keine Stacks mehr vorhanden sind (vorhandene Daten werden nicht verschoben).")
		}
	}
	var warns []string
	roots := []struct {
		dst        *string
		label      string
		dockerRoot bool
	}{{&next.Appdata, "Einstellungen (appdata)", false}, {&next.Dockerdata, "Daten (dockerdata)", true}}
	for _, r := range roots {
		res, err := d.Runner.Run(ctx, nil, "realpath", "-e", "--", *r.dst)
		if err != nil || res.ExitCode != 0 {
			return docker.PathsInfo{}, apierr.New(apierr.NotFound, "Der Ordner für "+r.label+" wurde nicht gefunden. Lege ihn zuerst an.")
		}
		real, err := docker.CleanRoot(strings.TrimSpace(string(res.Stdout)), allowLegacy)
		if err != nil {
			return docker.PathsInfo{}, apierr.New(apierr.InvalidParams, "Der Ordner für "+r.label+" führt über einen Link aus den erlaubten Bereichen hinaus oder enthält unzulässige Zeichen.")
		}
		uid, mode, err := pathOwnerMode(ctx, d, real)
		if err != nil || uid != 0 || mode&0o022 != 0 {
			return docker.PathsInfo{}, apierr.New(apierr.InvalidParams, "Der Ordner für "+r.label+" muss root gehören und darf für Gruppe und andere nicht schreibbar sein. Lege ihn im Ordnerfenster neu an.")
		}
		w, err := checkRoot(ctx, d, real, r.label, r.dockerRoot)
		if err != nil {
			return docker.PathsInfo{}, err
		}
		if w != "" {
			warns = append(warns, w)
		}
		*r.dst = real // gespeichert wird der aufgelöste Pfad
	}
	if err := next.Validate(allowLegacy); err != nil {
		return docker.PathsInfo{}, err
	}
	next.Previous = withPrevious(cur, next)

	first := cur == nil
	subdirs := []struct {
		p    string
		perm os.FileMode
	}{{next.StacksDir(), 0o700}, {next.ProxyDir(), 0o755}, {path.Dir(next.BackupBase()), 0o755}}
	if first {
		subdirs = append(subdirs, struct {
			p    string
			perm os.FileMode
		}{next.DataRoot(), 0o710})
	}
	for _, dir := range subdirs {
		if err := d.FS.MkdirAll(dir.p, dir.perm); err != nil {
			return docker.PathsInfo{}, apierr.New(apierr.Internal, "Ordner konnte nicht angelegt werden: "+dir.p).With("detail", err.Error())
		}
		if !isPlainDir(ctx, d, dir.p) {
			return docker.PathsInfo{}, apierr.New(apierr.InvalidParams, dir.p+" ist ein Link oder kein Ordner. BetterNAS legt dort Daten ab und verlangt echte Ordner.")
		}
	}

	oldPaths, hadPaths := d.FS.ReadFile(pathsFile)
	oldDaemon, errDaemon := d.FS.ReadFile(dockerDaemonFile)
	hadDaemon := errDaemon == nil
	oldDropIn, errDrop := d.FS.ReadFile(dockerMountDropIn)
	hadDropIn := errDrop == nil
	rollback := func() {
		if first {
			_ = d.FS.Remove(pathsFile)
			curPaths.Store(nil)
		} else {
			if hadPaths == nil {
				_ = d.FS.WriteFile(pathsFile, oldPaths, 0o644)
			}
			curPaths.Store(cur)
		}
		if hadDaemon {
			_ = d.FS.WriteFile(dockerDaemonFile, oldDaemon, 0o644)
		} else {
			_ = d.FS.Remove(dockerDaemonFile)
		}
		if hadDropIn {
			_ = d.FS.WriteFile(dockerMountDropIn, oldDropIn, 0o644)
		} else {
			_ = d.FS.Remove(dockerMountDropIn)
		}
		_, _ = run(ctx, d.Runner, nil, "systemctl", "daemon-reload")
	}
	fail := func(msg string, err error) (docker.PathsInfo, error) {
		rollback()
		return docker.PathsInfo{}, apierr.New(apierr.Internal, msg).With("detail", err.Error())
	}

	if first {
		m := map[string]any{}
		if hadDaemon {
			var err error
			if m, err = readDaemon(d); err != nil {
				return docker.PathsInfo{}, err
			}
		} else {
			m = map[string]any{"log-driver": "json-file", "log-opts": map[string]string{"max-size": "10m", "max-file": "3"}}
		}
		if _, ok := m["data-root"]; !ok {
			m["data-root"] = next.DataRoot()
			b, err := json.MarshalIndent(m, "", "  ")
			if err == nil {
				err = d.FS.MkdirAll(path.Dir(dockerDaemonFile), 0o755)
			}
			if err == nil {
				err = d.FS.WriteFile(dockerDaemonFile, append(b, '\n'), 0o644)
			}
			if err != nil {
				return fail("daemon.json konnte nicht geschrieben werden", err)
			}
		}
	}
	if err := writePathsFile(d, next); err != nil {
		return fail("paths.json konnte nicht geschrieben werden", err)
	}
	curPaths.Store(&next)
	// Docker erst starten, wenn die Speicherorte eingehängt sind (Pools und Platten hängen mit nofail ein).
	dropIn := "[Unit]\nRequiresMountsFor=" + next.Appdata + " " + next.Dockerdata + "\n"
	if err := d.FS.MkdirAll(path.Dir(dockerMountDropIn), 0o755); err != nil {
		return fail("Docker-Abhängigkeit konnte nicht angelegt werden", err)
	}
	if err := d.FS.WriteFile(dockerMountDropIn, []byte(dropIn), 0o644); err != nil {
		return fail("Docker-Abhängigkeit konnte nicht angelegt werden", err)
	}
	if _, err := run(ctx, d.Runner, nil, "systemctl", "daemon-reload"); err != nil {
		return fail("systemd konnte nicht neu geladen werden", err)
	}
	if first {
		if _, err := run(ctx, d.Runner, nil, "systemctl", "enable", "docker.socket", "docker.service"); err != nil {
			return fail("Docker konnte nicht aktiviert werden", err)
		}
		// restart statt start: war Docker schon (ohne diesen data-root) gestartet, muss es neu laden.
		if _, err := run(ctx, d.Runner, nil, "systemctl", "restart", "docker.service"); err != nil {
			return fail("Docker konnte nicht gestartet werden", err)
		}
	}
	if warns == nil {
		warns = []string{}
	}
	return docker.PathsInfo{Configured: true, Appdata: next.Appdata, Dockerdata: next.Dockerdata, Warnings: warns, StaleStacks: countStaleStacks(d, next)}, nil
}
