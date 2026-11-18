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

// IPStatus liefert die Netzwerkkonfiguration des Hosts.
func (m *Manager) IPStatus(ctx context.Context) (IPStatus, error) {
	st := IPStatus{}
	err := m.agent.Call(ctx, "network.status", nil, &st)
	return st, err
}

// IPApply schreibt die IP-Konfiguration (vorläufig bis zur Bestätigung).
func (m *Manager) IPApply(ctx context.Context, actor string, cfg IPConfig) error {
	err := m.agent.Call(ctx, "network.configure", cfg, nil)
	m.record(ctx, actor, "network.configure", cfg.Mode+" "+cfg.Address, err)
	return err
}

// IPConfirm bestätigt die neue IP-Konfiguration.
func (m *Manager) IPConfirm(ctx context.Context, actor string) error {
	err := m.agent.Call(ctx, "network.confirm", nil, nil)
	m.record(ctx, actor, "network.confirm", "ip", err)
	return err
}

// IPRevert nimmt eine unbestätigte IP-Änderung zurück.
func (m *Manager) IPRevert(ctx context.Context, actor string) error {
	err := m.agent.Call(ctx, "network.revert", nil, nil)
	m.record(ctx, actor, "network.revert", "ip", err)
	return err
}
