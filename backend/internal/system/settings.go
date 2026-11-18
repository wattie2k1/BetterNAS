package system

import (
	"context"
	"database/sql"

	"github.com/betternas/betternas/internal/audit"
)

// Settings verwaltet Hostname, Zeit und Energie über den Agent.
type Settings struct {
	db    *sql.DB
	agent Agent
}

// NewSettings erzeugt den Manager.
func NewSettings(d *sql.DB, a Agent) *Settings { return &Settings{db: d, agent: a} }

func (s *Settings) record(ctx context.Context, actor, action, target string, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	_ = audit.Record(ctx, s.db, audit.Entry{Actor: actor, Action: action, Target: target, Result: result})
}

// Get liefert Hostname, Zeitzone und NTP-Einstellungen.
func (s *Settings) Get(ctx context.Context) (map[string]any, error) {
	out := map[string]any{}
	err := s.agent.Call(ctx, "system.settings.get", nil, &out)
	return out, err
}

// Timezones liefert alle gültigen Zeitzonen.
func (s *Settings) Timezones(ctx context.Context) ([]string, error) {
	out := []string{}
	err := s.agent.Call(ctx, "system.timezones", nil, &out)
	return out, err
}

// Set ändert Hostname, Zeitzone, NTP und NTP-Server (nur gesetzte Felder).
func (s *Settings) Set(ctx context.Context, actor string, change map[string]any) error {
	err := s.agent.Call(ctx, "system.settings.set", change, nil)
	target := ""
	for k := range change {
		target += k + " "
	}
	s.record(ctx, actor, "system.settings.set", target, err)
	return err
}

// Power startet neu oder fährt herunter (action: reboot | poweroff).
func (s *Settings) Power(ctx context.Context, actor, action string) error {
	err := s.agent.Call(ctx, "system.power", map[string]string{"action": action}, nil)
	s.record(ctx, actor, "system.power", action, err)
	return err
}
