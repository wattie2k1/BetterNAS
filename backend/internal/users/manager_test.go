package users

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/events"
)

type fakeAgent struct {
	mu       sync.Mutex
	accounts Accounts
	calls    []string
}

func (f *fakeAgent) Call(_ context.Context, method string, params, result any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)
	if method == "account.list" && result != nil {
		b, _ := json.Marshal(f.accounts)
		return json.Unmarshal(b, result)
	}
	return nil
}

func (f *fakeAgent) called(m string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == m {
			return true
		}
	}
	return false
}

type env struct {
	m     *Manager
	agent *fakeAgent
	hub   *events.Hub
	ctx   context.Context
	sub   <-chan events.Message
}

func newEnv(t *testing.T, usage ...UsageFunc) *env {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	ch, cancel := hub.Subscribe()
	t.Cleanup(cancel)
	a := &fakeAgent{accounts: Accounts{
		Users: []User{
			{Name: "wattie", UID: 1000, Admin: true, System: true, Groups: []string{}},
			{Name: "anna", UID: 10000, Groups: []string{"familie"}},
		},
		Groups: []Group{{Name: "familie", GID: 10000, Members: []string{"anna"}}},
	}}
	return &env{m: NewManager(d, a, hub, usage...), agent: a, hub: hub, ctx: context.Background(), sub: ch}
}

func code(err error) string {
	if err == nil {
		return ""
	}
	return apierr.From(err).Code
}

func TestCreateUserValidatesBeforeAgent(t *testing.T) {
	e := newEnv(t)
	for _, in := range []UserInput{
		{Name: "Bad Name", Password: "geheim123"},
		{Name: "bob", Password: "kurz"},
		{Name: "bob", Password: "geheim123", FullName: "a:b"},
	} {
		if code(e.m.CreateUser(e.ctx, "wattie", in)) != apierr.InvalidParams {
			t.Errorf("%+v sollte abgelehnt werden", in)
		}
	}
	if e.agent.called("user.create") {
		t.Fatal("Agent darf nicht aufgerufen werden")
	}
}

func TestCreateUserPublishesAndAudits(t *testing.T) {
	e := newEnv(t)
	if err := e.m.CreateUser(e.ctx, "wattie", UserInput{Name: "bob", Password: "streng-geheim-1"}); err != nil {
		t.Fatal(err)
	}
	if !e.agent.called("user.create") {
		t.Fatal("user.create fehlt")
	}
	if m := <-e.sub; m.Type != "users.changed" {
		t.Fatalf("Ereignis: %+v", m)
	}
}

func TestAuditHasNoPassword(t *testing.T) {
	e := newEnv(t)
	_ = e.m.CreateUser(e.ctx, "wattie", UserInput{Name: "bob", Password: "streng-geheim-1"})
	_ = e.m.SetPassword(e.ctx, "wattie", "anna", "streng-geheim-2")
	entries, err := audit.List(e.ctx, e.m.db, 10)
	if err != nil || len(entries) != 2 {
		t.Fatalf("%v %+v", err, entries)
	}
	for _, en := range entries {
		b, _ := json.Marshal(en)
		if strings.Contains(string(b), "streng-geheim") {
			t.Fatalf("Passwort im Audit: %s", b)
		}
	}
}

func TestCannotRemoveLastAdminOrSelf(t *testing.T) {
	e := newEnv(t)
	if code(e.m.DeleteUser(e.ctx, "wattie", "wattie")) != apierr.Conflict {
		t.Error("sich selbst löschen")
	}
	if code(e.m.DeleteUser(e.ctx, "anna", "wattie")) != apierr.Conflict {
		t.Error("letzten Admin löschen")
	}
	if code(e.m.UpdateUser(e.ctx, "wattie", "wattie", UserUpdate{Admin: false})) != apierr.Conflict {
		t.Error("letzten Admin degradieren")
	}
	// Mit einem zweiten Admin ist es erlaubt.
	e.agent.accounts.Users[1].Admin = true
	if err := e.m.UpdateUser(e.ctx, "anna", "wattie", UserUpdate{Admin: false}); err != nil {
		t.Fatal(err)
	}
	e.agent.accounts.Users[0].Admin = true
	if err := e.m.DeleteUser(e.ctx, "wattie", "anna"); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteBlockedWhenUsed(t *testing.T) {
	e := newEnv(t, func(_ context.Context, kind, name string) ([]string, error) {
		if kind == "user" && name == "anna" || kind == "group" && name == "familie" {
			return []string{"share:filme"}, nil
		}
		return nil, nil
	})
	err := e.m.DeleteUser(e.ctx, "wattie", "anna")
	if code(err) != apierr.Conflict || apierr.From(err).Details["usedBy"] == nil {
		t.Fatalf("%v", err)
	}
	if code(e.m.DeleteGroup(e.ctx, "wattie", "familie")) != apierr.Conflict {
		t.Fatal("Gruppe in Freigabe")
	}
	if e.agent.called("user.delete") || e.agent.called("group.delete") {
		t.Fatal("Agent darf nicht aufgerufen werden")
	}
}

func TestGroupFlow(t *testing.T) {
	e := newEnv(t)
	if err := e.m.CreateGroup(e.ctx, "wattie", GroupInput{Name: "kinder", Members: []string{"anna"}}); err != nil {
		t.Fatal(err)
	}
	if !e.agent.called("group.create") || !e.agent.called("group.members") {
		t.Fatalf("%v", e.agent.calls)
	}
	if code(e.m.CreateGroup(e.ctx, "wattie", GroupInput{Name: "root"})) != apierr.InvalidParams {
		t.Fatal("reservierter Name")
	}
}
