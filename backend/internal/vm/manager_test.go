package vm

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/events"
)

type stubAgent struct {
	err   error
	calls []string
}

func (s *stubAgent) Call(_ context.Context, method string, _, _ any) error {
	s.calls = append(s.calls, method)
	return s.err
}

func newMgr(t *testing.T, a Agent) (*Manager, *events.Hub, func() int) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	count := func() int {
		var n int
		_ = d.QueryRow(`SELECT COUNT(*) FROM audit WHERE action LIKE 'vm.%'`).Scan(&n)
		return n
	}
	return NewManager(d, a, hub), hub, count
}

func TestManagerAudits(t *testing.T) {
	a := &stubAgent{}
	m, hub, count := newMgr(t, a)
	ch, cancel := hub.Subscribe()
	defer cancel()
	ctx := context.Background()
	if err := m.Action(ctx, "anna", "debian", "start"); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-ch:
		if msg.Type != "vm.changed" {
			t.Fatal(msg.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("Ereignis fehlt")
	}
	a.err = errors.New("boom")
	if err := m.Delete(ctx, "anna", "debian", true); err == nil {
		t.Fatal("Fehler erwartet")
	}
	select {
	case msg := <-ch:
		t.Fatalf("bei Fehler kein Ereignis: %v", msg)
	case <-time.After(50 * time.Millisecond):
	}
	if count() != 2 {
		t.Fatalf("Audit-Einträge: %d", count())
	}
}

func TestManagerRejectsInvalidBeforeAgent(t *testing.T) {
	a := &stubAgent{}
	m, _, _ := newMgr(t, a)
	ctx := context.Background()
	for _, err := range []error{
		m.Action(ctx, "a", "-x", "start"), m.Action(ctx, "a", "debian", "undefine"),
		m.Delete(ctx, "a", "../x", false), m.Create(ctx, "a", CreateInput{Spec: Spec{Name: "../x"}}),
	} {
		if apierr.From(err).Code != apierr.InvalidParams {
			t.Fatalf("invalid_params erwartet: %v", err)
		}
	}
	if len(a.calls) != 0 {
		t.Fatalf("Agent darf nicht aufgerufen werden: %v", a.calls)
	}
}
