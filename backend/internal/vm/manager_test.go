package vm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestUploadISO(t *testing.T) {
	m, _, count := newMgr(t, &stubAgent{})
	dir := t.TempDir()
	m.SetISODir(dir)
	ctx := context.Background()
	if err := m.UploadISO(ctx, "anna", "a.iso", strings.NewReader("daten")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.iso")); string(b) != "daten" {
		t.Fatalf("%q", b)
	}
	if err := m.UploadISO(ctx, "anna", "a.iso", strings.NewReader("neu")); apierr.From(err).Code != apierr.Conflict {
		t.Fatalf("%v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.iso")); string(b) != "daten" {
		t.Fatal("Datei wurde überschrieben")
	}
	if err := m.UploadISO(ctx, "anna", "../x.iso", strings.NewReader("x")); apierr.From(err).Code != apierr.InvalidParams {
		t.Fatalf("%v", err)
	}
	if err := m.UploadISO(ctx, "anna", "b.iso", errReader{}); err == nil {
		t.Fatal("Abbruch erwartet")
	}
	if left, _ := os.ReadDir(dir); len(left) != 1 {
		t.Fatalf("Temp-Dateien bleiben liegen: %v", left)
	}
	if count() != 3 { // ok, Konflikt, Abbruch (ungültiger Name wird vorher abgelehnt)
		t.Fatalf("Audit: %d", count())
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("abgebrochen") }

type portAgent struct{ port int }

func (p portAgent) Call(_ context.Context, _ string, _, result any) error {
	b, _ := json.Marshal(map[string]int{"port": p.port})
	return json.Unmarshal(b, result)
}

func TestConsolePortRange(t *testing.T) {
	for port, ok := range map[int]bool{5900: true, 5999: true, 22: false, 0: false, 6000: false} {
		m, _, _ := newMgr(t, portAgent{port})
		_, err := m.ConsolePort(context.Background(), "anna", "debian")
		if (err == nil) != ok {
			t.Errorf("Port %d: %v", port, err)
		}
	}
}
