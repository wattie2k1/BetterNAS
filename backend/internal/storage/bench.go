package storage

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
)

const (
	benchKeep    = 20 // so viele Läufe pro Platte werden aufgehoben
	benchHistory = 10 // so viele Durchschnittswerte gehen an die Oberfläche
)

// BenchSample ist eine Lesemessung an einer Stelle der Platte (0 % = Anfang, 100 % = Ende).
type BenchSample struct {
	Percent int     `json:"percent"`
	MBps    float64 `json:"mbps"`
}

// BenchRun ist ein abgeschlossener Lese-Benchmark.
type BenchRun struct {
	At      time.Time     `json:"at"`
	AvgMBps float64       `json:"avgMBps"`
	MinMBps float64       `json:"minMBps"`
	MaxMBps float64       `json:"maxMBps"`
	Samples []BenchSample `json:"samples"`
}

// BenchPoint ist der Durchschnitt eines früheren Laufs, zum Vergleich über die Zeit.
type BenchPoint struct {
	At      time.Time `json:"at"`
	AvgMBps float64   `json:"avgMBps"`
}

// Bench ist der Benchmark-Zustand einer Platte.
type Bench struct {
	Running bool         `json:"running"`
	Error   string       `json:"error,omitempty"` // der letzte Lauf ist fehlgeschlagen
	Last    *BenchRun    `json:"last,omitempty"`
	History []BenchPoint `json:"history,omitempty"` // ältester zuerst, inklusive des letzten Laufs
}

func (m *Manager) loadBench(ctx context.Context) map[string]*Bench {
	out := map[string]*Bench{}
	rows, err := m.db.QueryContext(ctx, `SELECT disk_id, at, avg, min, max, samples FROM disk_bench ORDER BY id`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, at, samples string
			r := &BenchRun{}
			if rows.Scan(&id, &at, &r.AvgMBps, &r.MinMBps, &r.MaxMBps, &samples) != nil {
				continue
			}
			r.At, _ = time.Parse(time.RFC3339, at)
			_ = json.Unmarshal([]byte(samples), &r.Samples)
			b := out[id]
			if b == nil {
				b = &Bench{}
				out[id] = b
			}
			b.Last = r
			b.History = append(b.History, BenchPoint{At: r.At, AvgMBps: r.AvgMBps})
			if len(b.History) > benchHistory {
				b.History = b.History[1:]
			}
		}
	}
	m.tmu.Lock()
	defer m.tmu.Unlock()
	for id, msg := range m.benchErr {
		b := out[id]
		if b == nil {
			b = &Bench{}
			out[id] = b
		}
		b.Error = msg
	}
	if m.benchRunning != "" {
		b := out[m.benchRunning]
		if b == nil {
			b = &Bench{}
			out[m.benchRunning] = b
		}
		b.Running = true
	}
	return out
}

// StartBench misst im Hintergrund die Leserate einer Platte (rein lesend). Es läuft immer nur ein Benchmark gleichzeitig.
func (m *Manager) StartBench(ctx context.Context, actor, id string) error {
	d, err := m.liveDisk(ctx, id)
	if err != nil {
		return err
	}
	m.tmu.Lock()
	if m.benchRunning != "" {
		m.tmu.Unlock()
		return apierr.New(apierr.Conflict, "Es läuft bereits ein Benchmark.")
	}
	m.benchRunning = id
	delete(m.benchErr, id)
	m.tmu.Unlock()
	_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: "disk.bench", Target: id, Result: "ok"})
	m.hub.Publish(events.Message{Type: "disks.changed"})
	go func() {
		defer func() {
			m.tmu.Lock()
			m.benchRunning = ""
			m.tmu.Unlock()
			m.hub.Publish(events.Message{Type: "disks.changed"})
		}()
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		var res struct {
			Samples []BenchSample `json:"samples"`
		}
		if err := m.agent.Call(cctx, "disk.bench", map[string]string{"device": d.Device}, &res); err != nil {
			slog.Warn("benchmark", "disk", id, "err", err)
			m.tmu.Lock()
			m.benchErr[id] = apierr.From(err).Message
			m.tmu.Unlock()
			return
		}
		if len(res.Samples) == 0 {
			return
		}
		lo, hi, sum := res.Samples[0].MBps, res.Samples[0].MBps, 0.0
		for _, s := range res.Samples {
			sum += s.MBps
			lo, hi = min(lo, s.MBps), max(hi, s.MBps)
		}
		b, _ := json.Marshal(res.Samples)
		if _, err := m.db.Exec(`INSERT INTO disk_bench(disk_id, at, avg, min, max, samples) VALUES (?,?,?,?,?,?)`,
			id, m.now().UTC().Format(time.RFC3339), sum/float64(len(res.Samples)), lo, hi, string(b)); err != nil {
			slog.Warn("benchmark speichern", "disk", id, "err", err)
			return
		}
		_, _ = m.db.Exec(`DELETE FROM disk_bench WHERE disk_id = ? AND id NOT IN (SELECT id FROM disk_bench WHERE disk_id = ? ORDER BY id DESC LIMIT ?)`, id, id, benchKeep)
	}()
	return nil
}
