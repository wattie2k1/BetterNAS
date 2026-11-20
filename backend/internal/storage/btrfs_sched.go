package storage

import (
	"context"
	"fmt"
	"slices"

	"github.com/betternas/betternas/internal/events"
)

const keyScrubSchedule = "btrfs.scrub.schedule"

// BtrfsScrubSchedule legt fest, wie oft btrfs-Pools geprüft werden (Prüfsummen aller Daten lesen und Fehler reparieren).
type BtrfsScrubSchedule struct {
	Interval string `json:"interval"` // off | weekly | monthly | quarterly
	Hour     int    `json:"hour"`     // 0–23
}

// ScrubIntervals sind die erlaubten Intervalle.
var ScrubIntervals = []string{"off", "weekly", "monthly", "quarterly"}

func (s BtrfsScrubSchedule) validate() error {
	if !slices.Contains(ScrubIntervals, s.Interval) {
		return invalid("Intervall muss off, weekly, monthly oder quarterly sein")
	}
	if s.Hour < 0 || s.Hour > 23 {
		return invalid("Stunde muss zwischen 0 und 23 liegen")
	}
	return nil
}

// calendar liefert den OnCalendar-Ausdruck ("off" = aus).
func (s BtrfsScrubSchedule) calendar() string {
	switch s.Interval {
	case "weekly":
		return fmt.Sprintf("Sun *-*-* %02d:00:00", s.Hour)
	case "monthly":
		return fmt.Sprintf("*-*-01 %02d:00:00", s.Hour)
	case "quarterly":
		return fmt.Sprintf("*-01,04,07,10-01 %02d:00:00", s.Hour)
	}
	return "off"
}

// BtrfsScrubSchedule liefert den gespeicherten Zeitplan (Standard: monatlich, 3 Uhr).
func (m *Manager) BtrfsScrubSchedule(ctx context.Context) (BtrfsScrubSchedule, error) {
	s := BtrfsScrubSchedule{Interval: "monthly", Hour: 3}
	if err := m.setting(ctx, keyScrubSchedule, &s); err != nil || s.validate() != nil {
		s = BtrfsScrubSchedule{Interval: "monthly", Hour: 3}
	}
	return s, nil
}

// scrubCalendar liefert den Zeitplan für den Soll-Zustand: "" = Standard, solange nichts gespeichert ist.
func (m *Manager) scrubCalendar(ctx context.Context) string {
	var s BtrfsScrubSchedule
	if err := m.setting(ctx, keyScrubSchedule, &s); err != nil || s.Interval == "" || s.validate() != nil {
		return ""
	}
	return s.calendar()
}

// SetBtrfsScrubSchedule speichert den Zeitplan und schaltet die Prüf-Timer sofort um.
func (m *Manager) SetBtrfsScrubSchedule(ctx context.Context, actor string, s BtrfsScrubSchedule) error {
	if err := s.validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.saveSetting(ctx, keyScrubSchedule, s); err != nil {
		return err
	}
	st, _, err := m.currentState(ctx)
	if err == nil {
		err = m.agent.Call(ctx, "storage.apply", st, nil)
	}
	m.auditSimple(ctx, actor, "pool.scrubschedule", s.Interval, err)
	if err != nil {
		return err
	}
	m.hub.Publish(events.Message{Type: "pools.changed"})
	return nil
}
