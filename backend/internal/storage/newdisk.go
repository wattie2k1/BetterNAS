package storage

import (
	"context"
	"log/slog"
	"time"

	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
)

const keySeenInit = "disk.seen.initialized"

// Zustände in disk_seen: new (gerade erkannt, noch nicht eingerichtet), seen (bekannt), ignored (vom Nutzer abgewiesen).
const (
	seenNew     = "new"
	seenSeen    = "seen"
	seenIgnored = "ignored"
)

func (m *Manager) loadSeen(ctx context.Context) (map[string]string, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT disk_id, state FROM disk_seen`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, st string
		if err := rows.Scan(&id, &st); err != nil {
			return nil, err
		}
		out[id] = st
	}
	return out, rows.Err()
}

// trackNew merkt sich erstmals gesehene, verwaltbare Platten. Beim allerersten Lauf gelten alle angeschlossenen Platten als bekannt,
// damit nach einem Update nicht jede vorhandene Platte als „neu“ gemeldet wird. Liefert die Zustände je Platten-ID.
func (m *Manager) trackNew(ctx context.Context, live []Disk) map[string]string {
	seen, err := m.loadSeen(ctx)
	if err != nil {
		slog.Warn("neue platten erkennen", "err", err)
		return nil
	}
	var inited bool
	_ = m.setting(ctx, keySeenInit, &inited)
	now := m.now().UTC().Format(time.RFC3339)
	for _, d := range live {
		if d.System || !d.Manageable {
			continue
		}
		if _, ok := seen[d.ID]; ok {
			continue
		}
		st := seenNew
		if !inited {
			st = seenSeen
		}
		if _, err := m.db.ExecContext(ctx, `INSERT OR IGNORE INTO disk_seen(disk_id, first_seen, state) VALUES (?,?,?)`, d.ID, now, st); err != nil {
			slog.Warn("neue platten erkennen", "err", err)
			continue
		}
		seen[d.ID] = st
	}
	if !inited {
		_ = m.saveSetting(ctx, keySeenInit, true)
	}
	return seen
}

// DismissNew blendet den Hinweis „Neue Platte erkannt“ für eine Platte dauerhaft aus.
func (m *Manager) DismissNew(ctx context.Context, actor, id string) error {
	if _, err := m.liveDisk(ctx, id); err != nil {
		return err
	}
	_, err := m.db.ExecContext(ctx, `INSERT INTO disk_seen(disk_id, first_seen, state) VALUES (?,?,?)
		ON CONFLICT(disk_id) DO UPDATE SET state = excluded.state`, id, m.now().UTC().Format(time.RFC3339), seenIgnored)
	result := "ok"
	if err != nil {
		result = "error"
	}
	_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: "disk.dismiss", Target: id, Result: result})
	if err != nil {
		return err
	}
	m.hub.Publish(events.Message{Type: "disks.changed"})
	return nil
}
