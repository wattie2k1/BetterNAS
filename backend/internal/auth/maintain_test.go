package auth

import (
	"context"
	"testing"
	"time"
)

func TestSweepRemovesExpiredSessionsAndStaleFailures(t *testing.T) {
	e := newEnv(t, time.Hour)
	ctx := context.Background()
	now := time.Now()
	e.h.store.now = func() time.Time { return now }
	e.h.limiter.now = func() time.Time { return now }

	if _, _, err := e.h.store.Create(ctx, "alice"); err != nil { // läuft in 1 h ab
		t.Fatal(err)
	}
	e.h.limiter.Fail("10.0.0.1")

	e.h.sweep(ctx) // nichts abgelaufen
	if n := count(t, e, `SELECT COUNT(*) FROM sessions`); n != 1 {
		t.Fatalf("Sitzung zu früh entfernt: %d", n)
	}

	now = now.Add(2 * time.Hour)
	e.h.sweep(ctx)
	if n := count(t, e, `SELECT COUNT(*) FROM sessions`); n != 0 {
		t.Fatalf("abgelaufene Sitzung übrig: %d", n)
	}
	e.h.limiter.mu.Lock()
	left := len(e.h.limiter.failures)
	e.h.limiter.mu.Unlock()
	if left != 0 {
		t.Fatalf("alte Fehlversuche übrig: %d", left)
	}
}

func count(t *testing.T, e *env, q string) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
