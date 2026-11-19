package storage

import (
	"context"
	"log/slog"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
)

// FsCheck ist der Zustand der Dateisystem-Prüfung einer Platte. Die Prüfung ist rein lesend und repariert nichts.
type FsCheck struct {
	Running    bool       `json:"running"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Result     string     `json:"result,omitempty"` // ok | errors | failed
	Online     bool       `json:"online,omitempty"` // das Dateisystem war während der Prüfung eingehängt
	Tool       string     `json:"tool,omitempty"`
	Output     string     `json:"output,omitempty"`
}

type agentFsCheck struct {
	Running bool   `json:"running"`
	Known   bool   `json:"known"`
	Result  string `json:"result"`
	Online  bool   `json:"online"`
	Tool    string `json:"tool"`
	Output  string `json:"output"`
}

func (m *Manager) loadFsChecks(ctx context.Context) map[string]*FsCheck {
	out := map[string]*FsCheck{}
	rows, err := m.db.QueryContext(ctx, `SELECT disk_id, started_at, finished_at, result, online, tool, output FROM fs_checks`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, s, f string
			c := &FsCheck{}
			if rows.Scan(&id, &s, &f, &c.Result, &c.Online, &c.Tool, &c.Output) != nil {
				continue
			}
			if t, e := time.Parse(time.RFC3339, s); e == nil {
				c.StartedAt = &t
			}
			if t, e := time.Parse(time.RFC3339, f); e == nil {
				c.FinishedAt = &t
			}
			out[id] = c
		}
	}
	m.tmu.Lock()
	defer m.tmu.Unlock()
	for id, at := range m.fsRunning {
		at := at
		c := out[id]
		if c == nil {
			c = &FsCheck{}
			out[id] = c
		}
		c.Running, c.StartedAt = true, &at
	}
	return out
}

// StartFsCheck startet die schreibgeschützte Dateisystem-Prüfung einer Platte im Hintergrund.
func (m *Manager) StartFsCheck(ctx context.Context, actor, id string) (err error) {
	d, err := m.liveDisk(ctx, id)
	if err != nil {
		return err
	}
	if d.System {
		return apierr.New(apierr.Conflict, "Die Systemplatte lässt sich hier nicht prüfen.")
	}
	m.tmu.Lock()
	if len(m.fsRunning) > 0 {
		m.tmu.Unlock()
		return apierr.New(apierr.Conflict, "Es läuft bereits eine Dateisystem-Prüfung.")
	}
	started := m.now()
	m.fsRunning[id] = started
	m.tmu.Unlock()
	defer func() {
		result := "ok"
		if err != nil {
			result = "error"
			m.tmu.Lock()
			delete(m.fsRunning, id)
			m.tmu.Unlock()
		}
		_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: "disk.fscheck", Target: id, Result: result})
	}()
	if err = m.agent.Call(ctx, "disk.fscheck.start", map[string]string{"device": d.Device}, nil); err != nil {
		return err
	}
	m.hub.Publish(events.Message{Type: "disks.changed"})
	go m.watchFsCheck(id, d.Device, started)
	return nil
}

func (m *Manager) watchFsCheck(id, device string, started time.Time) {
	finish := func(c agentFsCheck) {
		_, err := m.db.Exec(`INSERT INTO fs_checks(disk_id, started_at, finished_at, result, online, tool, output) VALUES (?,?,?,?,?,?,?)
			ON CONFLICT(disk_id) DO UPDATE SET started_at = excluded.started_at, finished_at = excluded.finished_at, result = excluded.result,
			online = excluded.online, tool = excluded.tool, output = excluded.output`,
			id, started.UTC().Format(time.RFC3339), m.now().UTC().Format(time.RFC3339), c.Result, c.Online, c.Tool, c.Output)
		if err != nil {
			slog.Warn("dateisystem-prüfung speichern", "disk", id, "err", err)
		}
		m.tmu.Lock()
		delete(m.fsRunning, id)
		m.tmu.Unlock()
		m.hub.Publish(events.Message{Type: "disks.changed"})
	}
	fails := 0
	for {
		time.Sleep(m.fsPoll)
		var c agentFsCheck
		if err := m.agent.Call(context.Background(), "disk.fscheck.status", map[string]string{"device": device}, &c); err != nil {
			if fails++; fails < 5 {
				continue
			}
			finish(agentFsCheck{Result: "failed", Output: "Der Agent war nicht erreichbar: " + err.Error()})
			return
		}
		fails = 0
		switch {
		case !c.Known:
			finish(agentFsCheck{Result: "failed", Output: "Die Prüfung wurde unterbrochen (der Agent wurde neu gestartet)."})
			return
		case !c.Running:
			finish(c)
			return
		}
	}
}
