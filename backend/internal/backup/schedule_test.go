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
		if got := nextRun(c.now, 2, 30); !got.Equal(c.want) {
			t.Errorf("%v: %v statt %v", c.now, got, c.want)
		}
	}
	// Sommerzeit-Lücke (2026-03-29 02:30 gibt es nicht): nie in der Vergangenheit, höchstens ~26 h entfernt
	now := at(2026, 3, 28, 3, 0)
	got := nextRun(now, 2, 30)
	if !got.After(now) || got.Sub(now) > 26*time.Hour {
		t.Errorf("Lücke: %v", got)
	}
}

func TestNextRunCustomTime(t *testing.T) {
	now := time.Date(2026, 11, 17, 4, 15, 0, 0, time.UTC)
	if got, want := nextRun(now, 23, 5), time.Date(2026, 11, 17, 23, 5, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("%v statt %v", got, want)
	}
	if got, want := nextRun(now, 4, 0), time.Date(2026, 11, 18, 4, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("%v statt %v", got, want)
	}
}

func TestConfigDefaultsValidateAndPrune(t *testing.T) {
	e := newMgr(t)
	c, err := e.m.Config(e.ctx)
	if err != nil || c != DefaultConfig() {
		t.Fatalf("%+v %v", c, err)
	}
	for _, bad := range []Config{{Time: "25:00", Keep: 3}, {Time: "2:30", Keep: 3}, {Time: "02:30", Keep: 0}, {Time: "02:30", Keep: 91}} {
		if err := e.m.SetConfig(e.ctx, "anna", bad); err == nil {
			t.Errorf("%+v akzeptiert", bad)
		}
	}
	for i := 0; i < 3; i++ {
		e.m.now = func() time.Time { return time.Date(2026, 11, 17, 2, 30, i, 0, time.UTC) }
		if _, err := e.m.Create(e.ctx, "anna"); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.m.SetConfig(e.ctx, "anna", Config{Enabled: false, Time: "04:00", Keep: 2}); err != nil {
		t.Fatal(err)
	}
	if l, _ := e.m.List(e.ctx); len(l) > 2 {
		t.Fatalf("%d Backups nach Prune", len(l))
	}
	if c, _ := e.m.Config(e.ctx); c.Time != "04:00" || c.Keep != 2 || c.Enabled {
		t.Fatalf("%+v", c)
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
