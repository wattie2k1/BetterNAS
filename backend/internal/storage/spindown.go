package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
)

const keySpindown = "power.spindown"

// SpindownChoices sind die erlaubten Ruhezeiten in Minuten (0 = aus).
var SpindownChoices = []int{0, 5, 10, 20, 30, 60, 120, 240}

// SpindownValue rechnet Minuten in den Wert von `hdparm -S` um (≤ 20 min: 5-s-Schritte, darüber 30-min-Schritte).
func SpindownValue(minutes int) (int, error) {
	if !slices.Contains(SpindownChoices, minutes) {
		return 0, apierr.New(apierr.InvalidParams, "Ungültige Ruhezeit.")
	}
	if minutes <= 20 {
		return minutes * 12, nil
	}
	return 240 + minutes/30, nil
}

// Spindown liefert die gespeicherten Ruhezeiten (Platten-ID → Minuten).
func (m *Manager) Spindown(ctx context.Context) (map[string]int, error) {
	out := map[string]int{}
	var raw string
	err := m.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, keySpindown).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out, nil
}

func (m *Manager) saveSpindown(ctx context.Context, v map[string]int) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, keySpindown, string(b))
	return err
}

// SetSpindown stellt die Ruhezeit einer Daten- oder Paritätsplatte ein. Gespeichert wird nur, wenn die Platte sie annimmt.
func (m *Manager) SetSpindown(ctx context.Context, actor, diskID string, minutes int) (err error) {
	value, err := SpindownValue(minutes)
	if err != nil {
		return err
	}
	d, err := m.liveDisk(ctx, diskID)
	if err != nil {
		return err
	}
	_, known, err := m.currentState(ctx)
	if err != nil {
		return err
	}
	if k := known[diskID]; d.System || !d.Manageable || k.ID == "" || (k.Role != "data" && k.Role != "parity") {
		return apierr.New(apierr.Conflict, "Der Ruhezustand ist nur für Daten- und Paritätsplatten einstellbar.")
	}
	defer func() {
		result := "ok"
		if err != nil {
			result = "error"
		}
		_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: "disk.spindown", Target: diskID, Result: result, Details: map[string]any{"minutes": minutes}})
	}()
	if err := m.agent.Call(ctx, "disk.spindown", map[string]any{"device": d.Device, "value": value}, nil); err != nil {
		return err
	}
	cur, err := m.Spindown(ctx)
	if err != nil {
		return err
	}
	if minutes == 0 {
		delete(cur, diskID)
	} else {
		cur[diskID] = minutes
	}
	if err := m.saveSpindown(ctx, cur); err != nil {
		return err
	}
	m.hub.Publish(events.Message{Type: "disks.changed"})
	return nil
}

// ReconcileSpindown wendet die gespeicherten Ruhezeiten erneut an (hdparm behält sie nicht über Neustarts).
// Fehler einzelner Platten werden nur protokolliert.
func (m *Manager) ReconcileSpindown(ctx context.Context) error {
	cur, err := m.Spindown(ctx)
	if err != nil || len(cur) == 0 {
		return err
	}
	live, err := m.liveDisks(ctx)
	if err != nil {
		return err
	}
	for _, d := range live {
		minutes, ok := cur[d.ID]
		if !ok {
			continue
		}
		value, err := SpindownValue(minutes)
		if err != nil {
			continue
		}
		if err := m.agent.Call(ctx, "disk.spindown", map[string]any{"device": d.Device, "value": value}, nil); err != nil {
			slog.Warn("ruhezustand anwenden", "disk", d.ID, "err", err)
		}
	}
	return nil
}
