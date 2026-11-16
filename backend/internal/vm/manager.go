package vm

import (
	"context"
	"database/sql"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
)

// Agent ist der Teil des Agent-Clients, den der Manager braucht.
type Agent interface {
	Call(ctx context.Context, method string, params, result any) error
}

const changedEvent = "vm.changed"

// Manager verwaltet virtuelle Maschinen über den Agent.
type Manager struct {
	db    *sql.DB
	agent Agent
	hub   *events.Hub
}

// NewManager erzeugt den Manager.
func NewManager(d *sql.DB, a Agent, hub *events.Hub) *Manager {
	return &Manager{db: d, agent: a, hub: hub}
}

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

// Status meldet, ob libvirt verfügbar ist.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	var st Status
	err := m.agent.Call(ctx, "vm.status", nil, &st)
	return st, err
}

// List liefert alle VMs.
func (m *Manager) List(ctx context.Context) ([]VM, error) {
	list := []VM{}
	err := m.agent.Call(ctx, "vm.list", nil, &list)
	return list, err
}

// Networks liefert die aktiven libvirt-Netze.
func (m *Manager) Networks(ctx context.Context) ([]string, error) {
	list := []string{}
	err := m.agent.Call(ctx, "vm.networks", nil, &list)
	return list, err
}

// ISOs liefert die verfügbaren ISO-Dateien.
func (m *Manager) ISOs(ctx context.Context) ([]string, error) {
	list := []string{}
	err := m.agent.Call(ctx, "vm.isos", nil, &list)
	return list, err
}

// CreateInput ist die Eingabe zum Anlegen einer VM.
type CreateInput struct {
	Spec
	Start bool `json:"start"`
}

// Create legt eine VM an (und startet sie auf Wunsch).
func (m *Manager) Create(ctx context.Context, actor string, in CreateInput) error {
	if err := in.Spec.Validate(); err != nil {
		return err
	}
	err := m.agent.Call(ctx, "vm.create", in, nil)
	m.record(ctx, actor, "vm.create", in.Name, err)
	return err
}

// Action führt eine Aktion aus der Whitelist aus.
func (m *Manager) Action(ctx context.Context, actor, name, action string) error {
	if !DomainRe.MatchString(name) {
		return apierr.New(apierr.InvalidParams, "Ungültiger VM-Name")
	}
	if !Actions[action] {
		return apierr.New(apierr.InvalidParams, "Unbekannte VM-Aktion")
	}
	err := m.agent.Call(ctx, "vm.action", map[string]string{"name": name, "action": action}, nil)
	m.record(ctx, actor, "vm."+action, name, err)
	return err
}

// Delete entfernt eine ausgeschaltete VM, auf Wunsch mit ihren Festplatten.
func (m *Manager) Delete(ctx context.Context, actor, name string, deleteDisks bool) error {
	if !DomainRe.MatchString(name) {
		return apierr.New(apierr.InvalidParams, "Ungültiger VM-Name")
	}
	err := m.agent.Call(ctx, "vm.delete", map[string]any{"name": name, "deleteDisks": deleteDisks}, nil)
	m.record(ctx, actor, "vm.delete", name, err)
	return err
}
