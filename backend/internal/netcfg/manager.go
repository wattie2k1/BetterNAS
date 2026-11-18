package netcfg

import (
	"context"
	"database/sql"

	"github.com/betternas/betternas/internal/audit"
)

// Agent ist der Teil des Agent-Clients, den der Manager braucht.
type Agent interface {
	Call(ctx context.Context, method string, params, result any) error
}

// Manager steuert die Bridge br0 über den Agent.
type Manager struct {
	db    *sql.DB
	agent Agent
}

// NewManager erzeugt den Manager.
func NewManager(d *sql.DB, a Agent) *Manager { return &Manager{db: d, agent: a} }

func (m *Manager) record(ctx context.Context, actor, action, target string, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: action, Target: target, Result: result})
}

// Status liefert den Bridge-Zustand.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	st := Status{Candidates: []string{}, Addresses: []string{}}
	err := m.agent.Call(ctx, "network.bridge.status", nil, &st)
	if st.Candidates == nil {
		st.Candidates = []string{}
	}
	if st.Addresses == nil {
		st.Addresses = []string{}
	}
	return st, err
}

// Enable richtet br0 auf der Schnittstelle ein (vorläufig bis zur Bestätigung).
func (m *Manager) Enable(ctx context.Context, actor, iface string) error {
	err := m.agent.Call(ctx, "network.bridge.enable", map[string]string{"interface": iface}, nil)
	m.record(ctx, actor, "network.bridge.enable", iface, err)
	return err
}

// Confirm bestätigt die Bridge und stoppt den automatischen Rückfall.
func (m *Manager) Confirm(ctx context.Context, actor string) error {
	err := m.agent.Call(ctx, "network.bridge.confirm", nil, nil)
	m.record(ctx, actor, "network.bridge.confirm", Name, err)
	return err
}

// Disable nimmt die Bridge zurück.
func (m *Manager) Disable(ctx context.Context, actor string) error {
	err := m.agent.Call(ctx, "network.bridge.disable", nil, nil)
	m.record(ctx, actor, "network.bridge.disable", Name, err)
	return err
}
