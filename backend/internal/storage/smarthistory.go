package storage

import (
	"context"
	"log/slog"
	"time"

	"github.com/betternas/betternas/internal/apierr"
)

const (
	historyKeepDays   = 365
	historyEvery      = time.Hour
	historyMaxPoints  = 240
	historyDefaultDay = 30
)

// HistoryPoint ist ein Messpunkt im SMART-Verlauf einer Platte.
type HistoryPoint struct {
	TS           int64 `json:"ts"` // Unix-Sekunden
	TemperatureC int   `json:"temperatureC"`
	Reallocated  int   `json:"reallocatedSectors"`
	Pending      int   `json:"pendingSectors"`
	PowerOnHours int   `json:"powerOnHours"`
}

// RecordSmart legt für jede wache Platte einen Messpunkt ab und löscht alte. Schlafende Platten werden übergangen,
// weil ihre Werte nur der zwischengespeicherte Stand wären.
func (m *Manager) RecordSmart(ctx context.Context) error {
	disks, err := m.ListDisks(ctx)
	if err != nil {
		return err
	}
	now := m.now().Unix()
	for _, d := range disks {
		s := d.Smart
		if d.State != "online" || s == nil || s.Standby {
			continue
		}
		if _, err := m.db.ExecContext(ctx, `INSERT OR REPLACE INTO smart_history(disk_id, ts, temp_c, reallocated, pending, power_on_hours) VALUES (?,?,?,?,?,?)`,
			d.ID, now, s.TemperatureC, s.ReallocatedSectors, s.PendingSectors, s.PowerOnHours); err != nil {
			return err
		}
	}
	_, err = m.db.ExecContext(ctx, `DELETE FROM smart_history WHERE ts < ?`, m.now().AddDate(0, 0, -historyKeepDays).Unix())
	return err
}

// RunHistory zeichnet alle Stunde die SMART-Werte auf, bis ctx endet.
func (m *Manager) RunHistory(ctx context.Context) {
	delay := 2 * time.Minute
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if err := m.RecordSmart(ctx); err != nil {
			slog.Warn("smart-verlauf", "err", err)
		}
		delay = historyEvery
	}
}

// History liefert den Verlauf einer Platte der letzten days Tage (aufsteigend), auf höchstens historyMaxPoints Punkte
// verdichtet (Temperatur: Höchstwert, Zähler: letzter Wert je Abschnitt).
func (m *Manager) History(ctx context.Context, diskID string, days int) ([]HistoryPoint, error) {
	if days == 0 {
		days = historyDefaultDay
	}
	if days < 1 || days > historyKeepDays {
		return nil, apierr.New(apierr.InvalidParams, "Ungültiger Zeitraum.")
	}
	rows, err := m.db.QueryContext(ctx, `SELECT ts, temp_c, reallocated, pending, power_on_hours FROM smart_history WHERE disk_id = ? AND ts >= ? ORDER BY ts`,
		diskID, m.now().AddDate(0, 0, -days).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all := []HistoryPoint{}
	for rows.Next() {
		var p HistoryPoint
		if err := rows.Scan(&p.TS, &p.TemperatureC, &p.Reallocated, &p.Pending, &p.PowerOnHours); err != nil {
			return nil, err
		}
		all = append(all, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return downsample(all, historyMaxPoints), nil
}

func downsample(in []HistoryPoint, limit int) []HistoryPoint {
	if len(in) <= limit {
		return in
	}
	step := (len(in) + limit - 1) / limit
	out := make([]HistoryPoint, 0, limit)
	for i := 0; i < len(in); i += step {
		end := min(i+step, len(in))
		p := in[end-1]
		for _, q := range in[i:end] {
			p.TemperatureC = max0(p.TemperatureC, q.TemperatureC)
		}
		out = append(out, p)
	}
	return out
}

func max0(a, b int) int {
	if b > a {
		return b
	}
	return a
}

// SectorGrowth liefert je Platte, um wie viele defekte (neu zugewiesene + instabile) Sektoren sie in den letzten days
// Tagen zugenommen hat. Platten ohne zwei Messpunkte im Zeitraum fehlen in der Antwort.
func (m *Manager) SectorGrowth(ctx context.Context, days int) (map[string]int, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT disk_id, reallocated + pending FROM smart_history WHERE ts >= ? ORDER BY disk_id, ts`,
		m.now().AddDate(0, 0, -days).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	first, last, n := map[string]int{}, map[string]int{}, map[string]int{}
	for rows.Next() {
		var id string
		var v int
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		if n[id] == 0 {
			first[id] = v
		}
		last[id] = v
		n[id]++
	}
	out := map[string]int{}
	for id, c := range n {
		if c >= 2 {
			out[id] = last[id] - first[id]
		}
	}
	return out, rows.Err()
}
