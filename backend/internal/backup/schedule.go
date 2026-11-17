package backup

import (
	"context"
	"log/slog"
	"time"

	"github.com/betternas/betternas/internal/notify"
)

// Notifier verschickt Meldungen (implementiert von notify.Service).
type Notifier interface {
	Send(ctx context.Context, m notify.Message) []notify.Result
}

// nextRun liefert den nächsten Zeitpunkt 02:30 (Ortszeit von now) nach now.
func nextRun(now time.Time) time.Time {
	loc := now.Location()
	y, mo, d := now.Date()
	t := time.Date(y, mo, d, 2, 30, 0, 0, loc)
	for !t.After(now) {
		d++
		t = time.Date(y, mo, d, 2, 30, 0, 0, loc)
	}
	return t
}

// runDaily erstellt das nächtliche Backup; bei Fehlern kommt eine Meldung, bestehende Backups bleiben.
func (m *Manager) runDaily(ctx context.Context, n Notifier) {
	if _, err := m.Create(ctx, "system"); err != nil {
		slog.Warn("nächtliches backup", "err", err)
		n.Send(ctx, notify.Message{
			Level: "crit", Title: "Konfigurations-Backup fehlgeschlagen",
			Body: "Das nächtliche Backup der BetterNAS-Konfiguration ist fehlgeschlagen: " + err.Error() + ". Prüfe den freien Platz auf der Systemplatte.",
		})
	}
}

// Run erstellt jede Nacht um 02:30 ein Backup, bis ctx endet.
func (m *Manager) Run(ctx context.Context, n Notifier) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.after(time.Until(nextRun(m.now()))):
		}
		m.runDaily(ctx, n)
	}
}
