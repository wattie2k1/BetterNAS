package agent

import (
	"context"
	"encoding/json"
	"path"
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

var unguardedMethods = map[string]bool{"docker.status": true, "folder.list": true, "folder.create": true}

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
