package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
)

const (
	keySelfTestSchedule = "disk.selftest.schedule"
	keySelfTestLast     = "disk.selftest.last"
)

// SelfTestSchedule legt fest, wie oft die Platten Selbsttests fahren (jeweils zur Stunde Hour, nacheinander).
type SelfTestSchedule struct {
	Short string `json:"short"` // off | weekly | monthly
	Long  string `json:"long"`  // off | monthly | quarterly
	Hour  int    `json:"hour"`  // 0–23
}

var selfTestIntervals = map[string]time.Duration{
	"weekly": 7 * 24 * time.Hour, "monthly": 30 * 24 * time.Hour, "quarterly": 90 * 24 * time.Hour,
}

func (s SelfTestSchedule) validate() error {
	okShort := s.Short == "off" || s.Short == "weekly" || s.Short == "monthly"
	okLong := s.Long == "off" || s.Long == "monthly" || s.Long == "quarterly"
	if !okShort || !okLong || s.Hour < 0 || s.Hour > 23 {
		return apierr.New(apierr.InvalidParams, "Ungültiger Zeitplan für die Selbsttests.")
	}
	return nil
}

func (m *Manager) setting(ctx context.Context, key string, v any) error {
	var raw string
	err := m.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_ = json.Unmarshal([]byte(raw), v)
	return nil
}

func (m *Manager) saveSetting(ctx context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, string(b))
	return err
}

// SelfTestSchedule liefert den gespeicherten Zeitplan (Standard: aus, 2 Uhr).
func (m *Manager) SelfTestSchedule(ctx context.Context) (SelfTestSchedule, error) {
	s := SelfTestSchedule{Short: "off", Long: "off", Hour: 2}
	err := m.setting(ctx, keySelfTestSchedule, &s)
	if s.validate() != nil {
		s = SelfTestSchedule{Short: "off", Long: "off", Hour: 2}
	}
	return s, err
}

// SetSelfTestSchedule speichert den Zeitplan.
func (m *Manager) SetSelfTestSchedule(ctx context.Context, actor string, s SelfTestSchedule) error {
	if err := s.validate(); err != nil {
		return err
	}
	err := m.saveSetting(ctx, keySelfTestSchedule, s)
	result := "ok"
	if err != nil {
		result = "error"
	}
	_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: "disk.selftest.schedule", Result: result, Details: map[string]any{"short": s.Short, "long": s.Long, "hour": s.Hour}})
	return err
}

func (m *Manager) invalidateSmart(device string) {
	m.tmu.Lock()
	delete(m.smart, device)
	m.tmu.Unlock()
}

// StartSelfTest startet einen Selbsttest (short | long) auf einer erkannten Platte.
func (m *Manager) StartSelfTest(ctx context.Context, actor, id, kind string) (err error) {
	if kind != "short" && kind != "long" {
		return apierr.New(apierr.InvalidParams, "Ungültige Testart.")
	}
	d, err := m.liveDisk(ctx, id)
	if err != nil {
		return err
	}
	defer func() {
		result := "ok"
		if err != nil {
			result = "error"
		}
		_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: "disk.selftest.start", Target: id, Result: result, Details: map[string]any{"kind": kind}})
	}()
	if err := m.agent.Call(ctx, "disk.selftest.start", map[string]string{"device": d.Device, "kind": kind}, nil); err != nil {
		return err
	}
	m.invalidateSmart(d.Device)
	m.hub.Publish(events.Message{Type: "disks.changed"})
	return nil
}

// AbortSelfTest bricht einen laufenden Selbsttest ab.
func (m *Manager) AbortSelfTest(ctx context.Context, actor, id string) (err error) {
	d, err := m.liveDisk(ctx, id)
	if err != nil {
		return err
	}
	defer func() {
		result := "ok"
		if err != nil {
			result = "error"
		}
		_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: "disk.selftest.abort", Target: id, Result: result})
	}()
	if err := m.agent.Call(ctx, "disk.selftest.abort", map[string]string{"device": d.Device}, nil); err != nil {
		return err
	}
	m.invalidateSmart(d.Device)
	m.hub.Publish(events.Message{Type: "disks.changed"})
	return nil
}

// waitSelfTest wartet, bis die Platte keinen Selbsttest mehr fährt (oder timeout/ctx endet).
func (m *Manager) waitSelfTest(ctx context.Context, d Disk, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(m.selfTestPoll):
		}
		m.invalidateSmart(d.Device)
		if s := m.smartFor(ctx, d.Device); s != nil && (s.SelfTest == nil || !s.SelfTest.Running) {
			return
		}
	}
}

// runSelfTests fährt die Tests nacheinander auf allen Platten mit SMART-Werten (schont das Netzteil).
func (m *Manager) runSelfTests(ctx context.Context, kind string) {
	disks, err := m.ListDisks(ctx)
	if err != nil {
		slog.Warn("selbsttest", "err", err)
		return
	}
	timeout := 30 * time.Minute
	if kind == "long" {
		timeout = 30 * time.Hour
	}
	for _, d := range disks {
		if ctx.Err() != nil {
			return
		}
		if d.State != "online" || d.Smart == nil || (d.Smart.SelfTest != nil && d.Smart.SelfTest.Running) {
			continue
		}
		if err := m.StartSelfTest(ctx, "system", d.ID, kind); err != nil {
			slog.Warn("selbsttest starten", "disk", d.ID, "err", err)
			continue
		}
		m.waitSelfTest(ctx, d.Disk, timeout)
	}
}

// selfTestTick startet fällige Tests: zur eingestellten Stunde, wenn der letzte Lauf länger als das Intervall her ist.
// Sind Kurz- und Langtest fällig, läuft nur der lange.
func (m *Manager) selfTestTick(ctx context.Context) {
	s, err := m.SelfTestSchedule(ctx)
	if err != nil || (s.Short == "off" && s.Long == "off") {
		return
	}
	now := m.now()
	if now.Hour() != s.Hour || !m.stRunning.CompareAndSwap(false, true) {
		return
	}
	last := map[string]time.Time{}
	_ = m.setting(ctx, keySelfTestLast, &last)
	due := func(kind, every string) bool {
		iv, ok := selfTestIntervals[every]
		return ok && now.Sub(last[kind]) >= iv-time.Hour
	}
	kind := ""
	switch {
	case due("long", s.Long):
		kind = "long"
	case due("short", s.Short):
		kind = "short"
	}
	if kind == "" {
		m.stRunning.Store(false)
		return
	}
	last[kind] = now
	if kind == "long" {
		last["short"] = now // der lange Test deckt den kurzen ab
	}
	_ = m.saveSetting(ctx, keySelfTestLast, last)
	go func() {
		defer m.stRunning.Store(false)
		m.runSelfTests(ctx, kind)
	}()
}

// RunSelfTests prüft alle 10 Minuten, ob Selbsttests fällig sind, bis ctx endet.
func (m *Manager) RunSelfTests(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.selfTestTick(ctx)
		}
	}
}
