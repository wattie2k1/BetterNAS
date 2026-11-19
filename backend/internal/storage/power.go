package storage

import (
	"context"
	"log/slog"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
)

const (
	keyTuning       = "disk.tuning"
	powerKeepDays   = 400
	powerDefaultDay = 30
	dayLayout       = "2006-01-02"
)

// PowerDay ist die Statistik einer Platte für einen Kalendertag (Ortszeit des Geräts).
type PowerDay struct {
	Day        string `json:"day"`
	ActiveMin  int    `json:"activeMin"`
	StandbyMin int    `json:"standbyMin"`
	Spinups    int    `json:"spinups"` // Übergänge von Standby zu aktiv
}

// SamplePower misst den Energiezustand aller drehenden Platten (ohne sie zu wecken) und schreibt die Minute gut.
func (m *Manager) SamplePower(ctx context.Context) error {
	live, err := m.liveDisks(ctx)
	if err != nil {
		return err
	}
	var devs []string
	byDev := map[string]string{}
	for _, d := range live {
		if d.Rotational && d.Device != "" {
			devs = append(devs, d.Device)
			byDev[d.Device] = d.ID
		}
	}
	if len(devs) == 0 {
		return nil
	}
	var states map[string]string
	if err := m.agent.Call(ctx, "disk.powerstate", map[string]any{"devices": devs}, &states); err != nil {
		return err
	}
	now := m.now()
	day := now.Format(dayLayout)
	for dev, st := range states {
		id := byDev[dev]
		if id == "" || (st != "active" && st != "standby") {
			continue
		}
		m.tmu.Lock()
		prev := m.powerPrev[id]
		m.powerPrev[id] = st
		m.tmu.Unlock()
		active, standby, spin := 0, 0, 0
		if st == "active" {
			active = 1
			if prev == "standby" {
				spin = 1
			}
		} else {
			standby = 1
		}
		if _, err := m.db.ExecContext(ctx, `INSERT INTO power_stats(disk_id, day, active_min, standby_min, spinups) VALUES (?,?,?,?,?)
			ON CONFLICT(disk_id, day) DO UPDATE SET active_min = active_min + excluded.active_min, standby_min = standby_min + excluded.standby_min, spinups = spinups + excluded.spinups`,
			id, day, active, standby, spin); err != nil {
			return err
		}
	}
	if now.Minute() == 0 {
		_, err = m.db.ExecContext(ctx, `DELETE FROM power_stats WHERE day < ?`, now.AddDate(0, 0, -powerKeepDays).Format(dayLayout))
	}
	return err
}

// RunPower misst jede Minute den Energiezustand, bis ctx endet.
func (m *Manager) RunPower(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			if err := m.SamplePower(cctx); err != nil {
				slog.Warn("energiezustand messen", "err", err)
			}
			cancel()
		}
	}
}

// PowerStats liefert die Tagesstatistik einer Platte der letzten days Tage (aufsteigend).
func (m *Manager) PowerStats(ctx context.Context, id string, days int) ([]PowerDay, error) {
	if days == 0 {
		days = powerDefaultDay
	}
	if days < 1 || days > powerKeepDays {
		return nil, apierr.New(apierr.InvalidParams, "Ungültiger Zeitraum.")
	}
	rows, err := m.db.QueryContext(ctx, `SELECT day, active_min, standby_min, spinups FROM power_stats WHERE disk_id = ? AND day >= ? ORDER BY day`,
		id, m.now().AddDate(0, 0, -(days-1)).Format(dayLayout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PowerDay{}
	for rows.Next() {
		var p PowerDay
		if err := rows.Scan(&p.Day, &p.ActiveMin, &p.StandbyMin, &p.Spinups); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// --- APM und Schreibcache ---

// TuningSettings sind die gespeicherten Einstellungen einer Platte: APM 1–255 (0 = nicht ändern),
// Schreibcache "on" | "off" | "" (nicht ändern).
type TuningSettings struct {
	APM        int    `json:"apm"`
	WriteCache string `json:"writeCache"`
}

func (t TuningSettings) validate() error {
	if t.APM < 0 || t.APM > 255 || (t.WriteCache != "" && t.WriteCache != "on" && t.WriteCache != "off") {
		return apierr.New(apierr.InvalidParams, "Ungültige Einstellung.")
	}
	return nil
}

// TuningState sind die gespeicherten und die aktuell an der Platte gelesenen Werte.
type TuningState struct {
	Saved   TuningSettings `json:"saved"`
	Current TuningCurrent  `json:"current"`
}

// TuningCurrent entspricht agent.Tuning.
type TuningCurrent struct {
	Standby    bool  `json:"standby,omitempty"`
	APM        *int  `json:"apm,omitempty"`
	WriteCache *bool `json:"writeCache,omitempty"`
}

func (m *Manager) savedTuning(ctx context.Context) (map[string]TuningSettings, error) {
	out := map[string]TuningSettings{}
	return out, m.setting(ctx, keyTuning, &out)
}

// checkTunable lässt wie beim Ruhezustand nur Daten- und Paritätsplatten zu.
func (m *Manager) checkTunable(ctx context.Context, id string) (Disk, error) {
	d, err := m.liveDisk(ctx, id)
	if err != nil {
		return Disk{}, err
	}
	_, known, err := m.currentState(ctx)
	if err != nil {
		return Disk{}, err
	}
	if k := known[id]; d.System || !d.Manageable || k.ID == "" || (k.Role != "data" && k.Role != "parity") {
		return Disk{}, apierr.New(apierr.Conflict, "APM und Schreibcache sind nur für Daten- und Paritätsplatten einstellbar.")
	}
	return d, nil
}

// Tuning liefert gespeicherte und aktuelle Einstellungen. Eine schlafende Platte wird nicht geweckt.
func (m *Manager) Tuning(ctx context.Context, id string) (TuningState, error) {
	d, err := m.checkTunable(ctx, id)
	if err != nil {
		return TuningState{}, err
	}
	saved, err := m.savedTuning(ctx)
	if err != nil {
		return TuningState{}, err
	}
	st := TuningState{Saved: saved[id]}
	if err := m.agent.Call(ctx, "disk.tuning", map[string]string{"device": d.Device}, &st.Current); err != nil {
		return TuningState{}, err
	}
	return st, nil
}

// SetTuning wendet die Einstellungen an der Platte an und speichert sie (gespeichert wird nur, wenn die Platte sie annimmt).
// Leere Einstellungen (0, "") löschen den gespeicherten Wunsch; die Platte behält ihre Werte bis zum nächsten Neustart.
func (m *Manager) SetTuning(ctx context.Context, actor, id string, t TuningSettings) (err error) {
	if err := t.validate(); err != nil {
		return err
	}
	d, err := m.checkTunable(ctx, id)
	if err != nil {
		return err
	}
	defer func() {
		result := "ok"
		if err != nil {
			result = "error"
		}
		_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: "disk.tuning", Target: id, Result: result, Details: map[string]any{"apm": t.APM, "writeCache": t.WriteCache}})
	}()
	if t.APM > 0 || t.WriteCache != "" {
		if err := m.agent.Call(ctx, "disk.tune", map[string]any{"device": d.Device, "apm": t.APM, "writeCache": t.WriteCache}, nil); err != nil {
			return err
		}
	}
	cur, err := m.savedTuning(ctx)
	if err != nil {
		return err
	}
	if t == (TuningSettings{}) {
		delete(cur, id)
	} else {
		cur[id] = t
	}
	if err := m.saveSetting(ctx, keyTuning, cur); err != nil {
		return err
	}
	m.hub.Publish(events.Message{Type: "disks.changed"})
	return nil
}

// ReconcileTuning wendet gespeicherte APM-/Schreibcache-Einstellungen erneut an (hdparm behält sie nicht über Neustarts).
// Fehler einzelner Platten werden nur protokolliert.
func (m *Manager) ReconcileTuning(ctx context.Context) error {
	cur, err := m.savedTuning(ctx)
	if err != nil || len(cur) == 0 {
		return err
	}
	live, err := m.liveDisks(ctx)
	if err != nil {
		return err
	}
	for _, d := range live {
		t, ok := cur[d.ID]
		if !ok {
			continue
		}
		if err := m.agent.Call(ctx, "disk.tune", map[string]any{"device": d.Device, "apm": t.APM, "writeCache": t.WriteCache}, nil); err != nil {
			slog.Warn("apm/schreibcache anwenden", "disk", d.ID, "err", err)
		}
	}
	return nil
}
