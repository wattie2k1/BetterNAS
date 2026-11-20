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

// nextRun liefert den nächsten Zeitpunkt hour:min (Ortszeit von now) nach now.
func nextRun(now time.Time, hour, min int) time.Time {
	loc := now.Location()
	y, mo, d := now.Date()
	t := time.Date(y, mo, d, hour, min, 0, 0, loc)
	for !t.After(now) {
		d++
		t = time.Date(y, mo, d, hour, min, 0, 0, loc)
	}
	return t
}

// runDaily erstellt das nächtliche Backup (Konfiguration und, wenn eingestellt, appdata); bei Fehlern kommt je
// Art eine Meldung, bestehende Backups bleiben.
func (m *Manager) runDaily(ctx context.Context, n Notifier) {
	if _, err := m.Create(ctx, "system"); err != nil {
		slog.Warn("nächtliches backup", "err", err)
		n.Send(ctx, notify.Message{
			Level: "crit", Title: "Konfigurations-Backup fehlgeschlagen",
			Body: "Das nächtliche Backup der BetterNAS-Konfiguration ist fehlgeschlagen: " + err.Error() + ". Prüfe den freien Platz am Sicherungsort.",
		})
	}
	cfg, _ := LoadConfig(ctx, m.db)
	if !cfg.Appdata {
		return
	}
	res, err := m.CreateAppdata(ctx, "system")
	if err != nil {
		slog.Warn("nächtliches appdata-backup", "err", err)
		n.Send(ctx, notify.Message{
			Level: "crit", Title: "appdata-Backup fehlgeschlagen",
			Body: "Das nächtliche Backup von appdata (Einstellungen der Docker-Apps) ist fehlgeschlagen: " + err.Error() + ". Prüfe den freien Platz am Sicherungsort.",
		})
		return
	}
	if res.Changed {
		slog.Info("appdata-backup: Dateien haben sich während der Sicherung geändert", "name", res.Info.Name)
	}
}

// Run erstellt täglich zur eingestellten Uhrzeit ein Backup, bis ctx endet.
// Geänderte Einstellungen (SetConfig) wirken sofort; ist die Sicherung abgeschaltet, wartet Run nur darauf.
func (m *Manager) Run(ctx context.Context, n Notifier) {
	for {
		cfg, err := LoadConfig(ctx, m.db)
		if err != nil {
			slog.Warn("backup-einstellungen", "err", err)
			cfg = DefaultConfig()
		}
		var timer <-chan time.Time
		if cfg.Enabled {
			h, mi := cfg.hm()
			timer = m.after(time.Until(nextRun(m.now(), h, mi)))
		}
		select {
		case <-ctx.Done():
			return
		case <-m.wake:
			continue
		case <-timer: // bei abgeschalteter Sicherung nil → blockiert
		}
		m.runDaily(ctx, n)
	}
}
