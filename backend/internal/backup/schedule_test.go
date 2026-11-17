package backup

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/notify"
)

type fakeNotifier struct {
	mu   sync.Mutex
	sent []notify.Message
}

func (f *fakeNotifier) Send(_ context.Context, m notify.Message) []notify.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	return nil
}

func TestNextRun(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("keine Zeitzonen-Daten")
	}
	at := func(y int, mo time.Month, d, h, mi int) time.Time { return time.Date(y, mo, d, h, mi, 0, 0, berlin) }
	cases := []struct{ now, want time.Time }{
		{at(2026, 11, 17, 1, 0), at(2026, 11, 17, 2, 30)},
		{at(2026, 11, 17, 2, 30), at(2026, 11, 18, 2, 30)}, // genau 02:30 → morgen
		{at(2026, 11, 17, 3, 0), at(2026, 11, 18, 2, 30)},
		{at(2026, 12, 31, 12, 0), at(2027, 1, 1, 2, 30)},
		{at(2026, 2, 28, 23, 0), at(2026, 3, 1, 2, 30)},
	}
	for _, c := range cases {
		if got := nextRun(c.now); !got.Equal(c.want) {
			t.Errorf("%v: %v statt %v", c.now, got, c.want)
		}
	}
	// Sommerzeit-Lücke (2026-03-29 02:30 gibt es nicht): nie in der Vergangenheit, höchstens ~26 h entfernt
	now := at(2026, 3, 28, 3, 0)
	got := nextRun(now)
	if !got.After(now) || got.Sub(now) > 26*time.Hour {
		t.Errorf("Lücke: %v", got)
	}
}

func TestRunDailyCreatesBackup(t *testing.T) {
	e := newMgr(t)
	n := &fakeNotifier{}
	e.m.runDaily(e.ctx, n)
	if l, _ := e.m.List(e.ctx); len(l) != 1 {
		t.Fatalf("%d Backups", len(l))
	}
	if len(n.sent) != 0 {
		t.Fatalf("Meldung bei Erfolg: %v", n.sent)
	}
}

func TestRunDailyNotifiesOnFailureAndKeepsOld(t *testing.T) {
	e := newMgr(t)
	if _, err := e.m.Create(e.ctx, "anna"); err != nil {
		t.Fatal(err)
	}
	// Backup-Verzeichnis „verschwindet“: an seiner Stelle liegt eine Datei → Create scheitert
	blocker := filepath.Join(e.root, "blocker")
	os.WriteFile(blocker, []byte("x"), 0o600)
	old := e.m.dir
	e.m.dir = filepath.Join(blocker, "backups")
	n := &fakeNotifier{}
	e.m.runDaily(e.ctx, n)
	if len(n.sent) != 1 || n.sent[0].Level != "crit" || n.sent[0].Title != "Konfigurations-Backup fehlgeschlagen" {
		t.Fatalf("%+v", n.sent)
	}
	e.m.dir = old
	if l, _ := e.m.List(e.ctx); len(l) != 1 {
		t.Fatalf("alte Backups angetastet: %d", len(l))
	}
}

func TestRunStopsOnContext(t *testing.T) {
	e := newMgr(t)
	e.m.after = func(time.Duration) <-chan time.Time { return make(chan time.Time) } // feuert nie
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.m.Run(ctx, &fakeNotifier{}); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run endet nicht")
	}
}

func TestRunRunsWhenTimerFires(t *testing.T) {
	e := newMgr(t)
	fire := make(chan time.Time, 1)
	calls := 0
	e.m.after = func(time.Duration) <-chan time.Time {
		calls++
		if calls == 1 {
			fire <- time.Now()
			return fire
		}
		return make(chan time.Time)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go e.m.Run(ctx, &fakeNotifier{})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if l, _ := e.m.List(e.ctx); len(l) == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("kein Backup nach Timer")
}
