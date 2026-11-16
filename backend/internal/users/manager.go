package users

import (
	"context"
	"database/sql"
	"slices"
	"sync"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
)

// Agent ist der Teil des Agent-Clients, den der Manager braucht.
type Agent interface {
	Call(ctx context.Context, method string, params, result any) error
}

// UsageFunc liefert, was ein Benutzer (kind "user") oder eine Gruppe (kind "group") verwendet,
// z. B. "share:filme". Verwendete Konten lassen sich nicht löschen.
type UsageFunc func(ctx context.Context, kind, name string) ([]string, error)

// UserInput beschreibt ein neues Konto.
type UserInput struct {
	Name     string   `json:"name"`
	FullName string   `json:"fullName"`
	Password string   `json:"password"`
	Groups   []string `json:"groups"`
	Admin    bool     `json:"admin"`
}

// UserUpdate ändert ein Konto (ohne Passwort).
type UserUpdate struct {
	FullName string   `json:"fullName"`
	Groups   []string `json:"groups"`
	Admin    bool     `json:"admin"`
}

// GroupInput beschreibt eine neue Gruppe.
type GroupInput struct {
	Name    string   `json:"name"`
	Members []string `json:"members"`
}

// Manager verwaltet Benutzer und Gruppen über den Agent.
type Manager struct {
	db    *sql.DB
	agent Agent
	hub   *events.Hub
	usage []UsageFunc
	mu    sync.Mutex // serialisiert Änderungen (Prüfungen wie „letzter Admin“ bleiben gültig)
}

// NewManager erzeugt einen Manager.
func NewManager(d *sql.DB, agent Agent, hub *events.Hub, usage ...UsageFunc) *Manager {
	return &Manager{db: d, agent: agent, hub: hub, usage: usage}
}

// List liefert alle Benutzer und BetterNAS-Gruppen.
func (m *Manager) List(ctx context.Context) (Accounts, error) {
	var a Accounts
	if err := m.agent.Call(ctx, "account.list", nil, &a); err != nil {
		return Accounts{}, err
	}
	if a.Users == nil {
		a.Users = []User{}
	}
	if a.Groups == nil {
		a.Groups = []Group{}
	}
	return a, nil
}

// finish schreibt das Audit-Protokoll (nie mit Passwörtern) und meldet die Änderung.
func (m *Manager) finish(ctx context.Context, actor, action, target string, err error) error {
	result := "ok"
	if err != nil {
		result = "error"
	}
	_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: action, Target: target, Result: result})
	if err == nil {
		m.hub.Publish(events.Message{Type: "users.changed"})
	}
	return err
}

func (m *Manager) usedBy(ctx context.Context, kind, name string) error {
	var used []string
	for _, f := range m.usage {
		u, err := f(ctx, kind, name)
		if err != nil {
			return err
		}
		used = append(used, u...)
	}
	if len(used) > 0 {
		return apierr.New(apierr.Conflict, "Wird noch in Freigaben verwendet und kann nicht gelöscht werden.").With("usedBy", used)
	}
	return nil
}

func adminCount(a Accounts) int {
	n := 0
	for _, u := range a.Users {
		if u.Admin {
			n++
		}
	}
	return n
}

func findUser(a Accounts, name string) (User, bool) {
	i := slices.IndexFunc(a.Users, func(u User) bool { return u.Name == name })
	if i < 0 {
		return User{}, false
	}
	return a.Users[i], true
}

// CreateUser legt ein Konto an.
func (m *Manager) CreateUser(ctx context.Context, actor string, in UserInput) error {
	if err := ValidateName(in.Name); err != nil {
		return err
	}
	if err := ValidatePassword(in.Name, in.Password); err != nil {
		return err
	}
	if err := ValidateFullName(in.FullName); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	err := m.agent.Call(ctx, "user.create", in, nil)
	return m.finish(ctx, actor, "user.create", in.Name, err)
}

// UpdateUser ändert vollständigen Namen, Gruppen und Admin-Recht.
func (m *Manager) UpdateUser(ctx context.Context, actor, name string, in UserUpdate) error {
	if err := ValidateFullName(in.FullName); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	a, err := m.List(ctx)
	if err != nil {
		return err
	}
	u, ok := findUser(a, name)
	if !ok {
		return apierr.New(apierr.NotFound, "Benutzer nicht gefunden")
	}
	if u.Admin && !in.Admin && adminCount(a) <= 1 {
		return apierr.New(apierr.Conflict, "Der letzte Administrator kann nicht herabgestuft werden.")
	}
	if in.Groups == nil {
		in.Groups = []string{}
	}
	err = m.agent.Call(ctx, "user.update", map[string]any{"name": name, "fullName": in.FullName, "groups": in.Groups, "admin": in.Admin}, nil)
	return m.finish(ctx, actor, "user.update", name, err)
}

// SetPassword setzt ein neues Passwort (Linux und Samba).
func (m *Manager) SetPassword(ctx context.Context, actor, name, pw string) error {
	if err := ValidatePassword(name, pw); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	err := m.agent.Call(ctx, "user.password", map[string]string{"name": name, "password": pw}, nil)
	return m.finish(ctx, actor, "user.password", name, err)
}

// DeleteUser entfernt ein Konto.
func (m *Manager) DeleteUser(ctx context.Context, actor, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if actor == name {
		return apierr.New(apierr.Conflict, "Das eigene Konto kann nicht gelöscht werden.")
	}
	a, err := m.List(ctx)
	if err != nil {
		return err
	}
	u, ok := findUser(a, name)
	if !ok {
		return apierr.New(apierr.NotFound, "Benutzer nicht gefunden")
	}
	if u.Admin && adminCount(a) <= 1 {
		return apierr.New(apierr.Conflict, "Der letzte Administrator kann nicht gelöscht werden.")
	}
	if err := m.usedBy(ctx, "user", name); err != nil {
		return err
	}
	err = m.agent.Call(ctx, "user.delete", map[string]string{"name": name}, nil)
	return m.finish(ctx, actor, "user.delete", name, err)
}

// CreateGroup legt eine Gruppe an und setzt ihre Mitglieder.
func (m *Manager) CreateGroup(ctx context.Context, actor string, in GroupInput) error {
	if err := ValidateName(in.Name); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	err := m.agent.Call(ctx, "group.create", map[string]string{"name": in.Name}, nil)
	if err == nil && len(in.Members) > 0 {
		if err = m.agent.Call(ctx, "group.members", map[string]any{"name": in.Name, "members": in.Members}, nil); err != nil {
			_ = m.agent.Call(ctx, "group.delete", map[string]string{"name": in.Name}, nil)
		}
	}
	return m.finish(ctx, actor, "group.create", in.Name, err)
}

// SetMembers ersetzt die Mitglieder einer Gruppe.
func (m *Manager) SetMembers(ctx context.Context, actor, name string, members []string) error {
	if members == nil {
		members = []string{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	err := m.agent.Call(ctx, "group.members", map[string]any{"name": name, "members": members}, nil)
	return m.finish(ctx, actor, "group.members", name, err)
}

// DeleteGroup entfernt eine Gruppe.
func (m *Manager) DeleteGroup(ctx context.Context, actor, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.usedBy(ctx, "group", name); err != nil {
		return err
	}
	err := m.agent.Call(ctx, "group.delete", map[string]string{"name": name}, nil)
	return m.finish(ctx, actor, "group.delete", name, err)
}
