package docker

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/rpc"
)

// Agent ist der Teil des Agent-Clients, den der Manager braucht.
type Agent interface {
	Call(ctx context.Context, method string, params, result any) error
}

// StackInput sind die Eingaben zum Anlegen und Ändern eines Stacks.
type StackInput struct {
	Name    string `json:"name"`
	Compose string `json:"compose"`
	Env     string `json:"env"`
}

// Manager verwaltet Stacks und Container über den Agent.
type Manager struct {
	db        *sql.DB
	agent     Agent
	jobs      *jobs.Manager
	hub       *events.Hub
	pollEvery time.Duration
}

// NewManager erzeugt den Manager.
func NewManager(d *sql.DB, a Agent, j *jobs.Manager, hub *events.Hub) *Manager {
	return &Manager{db: d, agent: a, jobs: j, hub: hub, pollEvery: 2 * time.Second}
}

const (
	maxRunTime     = 2 * time.Hour
	maxPollErrors  = 30 // Agent-Neustart während eines Laufs überbrücken
	changedEvent   = "docker.changed"
	stackNameError = "Ungültiger Stackname (erlaubt: Kleinbuchstaben, Ziffern, - und _, bis 40 Zeichen)"
)

func (m *Manager) record(ctx context.Context, actor, action, target string, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: action, Target: target, Result: result})
	if err == nil {
		m.hub.Publish(events.Message{Type: changedEvent})
	}
}

func checkName(name string) error {
	if !StackNameRe.MatchString(name) {
		return apierr.New(apierr.InvalidParams, stackNameError)
	}
	return nil
}

// Status meldet, ob Docker installiert und aktiv ist.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	var st Status
	err := m.agent.Call(ctx, "docker.status", nil, &st)
	return st, err
}

// ListStacks liefert alle Stacks mit Laufzustand.
func (m *Manager) ListStacks(ctx context.Context) ([]StackInfo, error) {
	list := []StackInfo{}
	err := m.agent.Call(ctx, "stack.list", nil, &list)
	return list, err
}

// GetStack liefert compose.yaml und .env.
func (m *Manager) GetStack(ctx context.Context, name string) (StackFiles, error) {
	if err := checkName(name); err != nil {
		return StackFiles{}, err
	}
	var sf StackFiles
	err := m.agent.Call(ctx, "stack.get", map[string]string{"name": name}, &sf)
	return sf, err
}

func (m *Manager) save(ctx context.Context, actor string, in StackInput, create bool) error {
	if err := checkName(in.Name); err != nil {
		return err
	}
	req := map[string]any{"name": in.Name, "compose": in.Compose, "env": in.Env, "create": create}
	err := m.agent.Call(ctx, "stack.save", req, nil)
	m.record(ctx, actor, "stack.save", in.Name, err)
	return err
}

// CreateStack legt einen neuen Stack an (Konflikt, wenn der Name existiert).
func (m *Manager) CreateStack(ctx context.Context, actor string, in StackInput) error {
	return m.save(ctx, actor, in, true)
}

// UpdateStack überschreibt compose.yaml und .env eines vorhandenen Stacks.
func (m *Manager) UpdateStack(ctx context.Context, actor string, in StackInput) error {
	if _, err := m.GetStack(ctx, in.Name); err != nil {
		return err
	}
	return m.save(ctx, actor, in, false)
}

// ValidateStack prüft den gespeicherten Stack mit `docker compose config`.
func (m *Manager) ValidateStack(ctx context.Context, name string) (StackCheck, error) {
	if err := checkName(name); err != nil {
		return StackCheck{}, err
	}
	var c StackCheck
	err := m.agent.Call(ctx, "stack.validate", map[string]string{"name": name}, &c)
	return c, err
}

// DeleteStack fährt den Stack herunter (Volumes bleiben) und löscht seine Dateien.
func (m *Manager) DeleteStack(ctx context.Context, actor, name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	err := m.agent.Call(ctx, "stack.delete", map[string]string{"name": name}, nil)
	m.record(ctx, actor, "stack.delete", name, err)
	return err
}

// RunStack startet eine Stack-Aktion und begleitet sie als Job, bis die Unit des Agents fertig ist.
func (m *Manager) RunStack(ctx context.Context, actor, name, action string) (string, error) {
	if err := checkName(name); err != nil {
		return "", err
	}
	if !StackActions[action] {
		return "", apierr.New(apierr.InvalidParams, "Unbekannte Stack-Aktion")
	}
	var res struct {
		Unit string `json:"unit"`
	}
	// Der Agent prüft Existenz, Gültigkeit und laufende Vorgänge synchron; Fehler gehen direkt an den Aufrufer.
	err := m.agent.Call(ctx, "stack.run", map[string]string{"name": name, "action": action}, &res)
	m.record(ctx, actor, "stack."+action, name, err)
	if err != nil {
		return "", err
	}
	return m.jobs.Start("stack."+action, name, actor, func(ctx context.Context, log jobs.Logger) error {
		defer m.hub.Publish(events.Message{Type: changedEvent})
		log.Printf("Stack %s: %s …", name, action)
		deadline, shown, errs := time.Now().Add(maxRunTime), "", 0
		for {
			time.Sleep(m.pollEvery)
			var st RunState
			if err := m.agent.Call(ctx, "stack.runstatus", map[string]string{"unit": res.Unit}, &st); err != nil {
				if errs++; errs >= maxPollErrors {
					return err
				}
				continue
			}
			errs = 0
			if st.Log != shown {
				if rest, ok := strings.CutPrefix(st.Log, shown); ok {
					log.Printf("%s", strings.TrimLeft(rest, "\n"))
				} else {
					log.Printf("%s", st.Log)
				}
				shown = st.Log
			}
			switch st.State {
			case "succeeded":
				return nil
			case "failed":
				return apierr.New(apierr.Conflict, "Der Vorgang ist fehlgeschlagen (Protokoll siehe oben).")
			}
			if time.Now().After(deadline) {
				return apierr.New(apierr.Internal, "Der Vorgang dauert zu lange.")
			}
		}
	}), nil
}

// ListContainers liefert alle Container.
func (m *Manager) ListContainers(ctx context.Context) ([]Container, error) {
	list := []Container{}
	err := m.agent.Call(ctx, "container.list", nil, &list)
	return list, err
}

// ContainerAction führt start, stop, restart oder remove aus.
func (m *Manager) ContainerAction(ctx context.Context, actor, id, action string) error {
	if !ContainerIDRe.MatchString(id) {
		return apierr.New(apierr.InvalidParams, "Ungültige Container-ID")
	}
	if !ContainerActions[action] {
		return apierr.New(apierr.InvalidParams, "Unbekannte Container-Aktion")
	}
	err := m.agent.Call(ctx, "container.action", map[string]string{"id": id, "action": action}, nil)
	m.record(ctx, actor, "container."+action, id, err)
	return err
}

// SessionOpener ist der Teil des Agent-Clients für bidirektionale Sitzungen (*rpc.Client).
type SessionOpener interface {
	OpenSession(ctx context.Context, method string, params any) (*rpc.Session, error)
}

// OpenSession öffnet eine Sitzung (Logs, Shell) beim Agent.
func (m *Manager) OpenSession(ctx context.Context, method string, params any) (*rpc.Session, error) {
	so, ok := m.agent.(SessionOpener)
	if !ok {
		return nil, apierr.New(apierr.Internal, "Sitzungen sind nicht verfügbar")
	}
	return so.OpenSession(ctx, method, params)
}

// Stats liefert CPU und Speicher der laufenden Container.
func (m *Manager) Stats(ctx context.Context) ([]ContainerStats, error) {
	list := []ContainerStats{}
	err := m.agent.Call(ctx, "container.stats", nil, &list)
	return list, err
}

// AuditShell hält fest, dass ein Administrator eine Shell in einem Container geöffnet hat.
func (m *Manager) AuditShell(ctx context.Context, actor, id string) {
	_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: "container.shell", Target: id, Result: "ok"})
}
