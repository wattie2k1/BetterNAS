// Package system enthält Systemfunktionen der Weboberfläche, derzeit die Paket-Updates.
package system

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
)

// Agent ist der Teil des Agent-Clients, den der Manager braucht.
type Agent interface {
	Call(ctx context.Context, method string, params, result any) error
}

// Package ist ein anstehendes Update.
type Package struct {
	Name      string `json:"name"`
	Current   string `json:"current"`
	Candidate string `json:"candidate"`
}

// Run ist der Zustand des letzten Update-Laufs. State "none" = es gab noch keinen.
type Run struct {
	State string `json:"state"` // none | running | succeeded | failed
	Log   string `json:"log"`
}

// AutoRun ist der letzte Lauf der automatischen Updates.
type AutoRun struct {
	At       string   `json:"at"`
	Packages []string `json:"packages"`
	Result   string   `json:"result"` // ok | failed | unknown
}

// Auto ist der Zustand der automatischen Updates (unattended-upgrades).
type Auto struct {
	Enabled bool     `json:"enabled"`
	LastRun *AutoRun `json:"lastRun"`
}

// Overview ist die Antwort für die Updates-Seite.
type Overview struct {
	Packages  []Package `json:"packages"`
	CheckedAt string    `json:"checkedAt"`
	Run       Run       `json:"run"`
	// Channel ist der Update-Kanal der BetterNAS-Paketquelle (stable | testing); leer = keine Quelle oder unbekannt.
	Channel string `json:"channel"`
	// Auto ist der Zustand der automatischen Updates.
	Auto Auto `json:"auto"`
}

const (
	keyPackages  = "updates.packages"
	keyCheckedAt = "updates.checkedAt"
	keyUnit      = "updates.unit"
)

// Updates sucht und installiert Paket-Updates über den Agent.
type Updates struct {
	db    *sql.DB
	agent Agent
	jobs  *jobs.Manager
	hub   *events.Hub
	now   func() time.Time
}

// NewUpdates erzeugt den Manager.
func NewUpdates(d *sql.DB, agent Agent, j *jobs.Manager, hub *events.Hub) *Updates {
	return &Updates{db: d, agent: agent, jobs: j, hub: hub, now: time.Now}
}

func (u *Updates) get(ctx context.Context, key string) (string, error) {
	var v string
	err := u.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (u *Updates) set(ctx context.Context, key, value string) error {
	_, err := u.db.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// Get liefert gespeicherte Paketliste, Zeitpunkt der Suche und Zustand des letzten Laufs.
func (u *Updates) Get(ctx context.Context) (Overview, error) {
	o := Overview{Packages: []Package{}, Run: Run{State: "none"}}
	var ch struct {
		Channel string `json:"channel"`
	}
	if err := u.agent.Call(ctx, "updates.channel", nil, &ch); err == nil {
		o.Channel = ch.Channel
	}
	if err := u.agent.Call(ctx, "updates.auto", nil, &o.Auto); err != nil {
		o.Auto = Auto{}
	}
	raw, err := u.get(ctx, keyPackages)
	if err != nil {
		return o, err
	}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &o.Packages)
	}
	if o.CheckedAt, err = u.get(ctx, keyCheckedAt); err != nil {
		return o, err
	}
	unit, err := u.get(ctx, keyUnit)
	if err != nil || unit == "" {
		return o, err
	}
	// Läuft der Agent gerade neu (Update von BetterNAS selbst), ist der Zustand kurz unbekannt.
	if err := u.agent.Call(ctx, "updates.status", map[string]string{"unit": unit}, &o.Run); err != nil {
		o.Run = Run{State: "running", Log: ""}
	}
	return o, nil
}

// RunStatus liefert nur den Zustand des letzten Laufs (für schnelles Abfragen während der Installation).
func (u *Updates) RunStatus(ctx context.Context) (Run, error) {
	unit, err := u.get(ctx, keyUnit)
	if err != nil || unit == "" {
		return Run{State: "none"}, err
	}
	var r Run
	if err := u.agent.Call(ctx, "updates.status", map[string]string{"unit": unit}, &r); err != nil {
		return Run{State: "running"}, nil
	}
	return r, nil
}

// Check sucht als Job nach Updates (aktualisiert die Paketlisten, dauert einige Sekunden).
func (u *Updates) Check(ctx context.Context, actor string) (string, error) {
	return u.jobs.Start("updates.check", "system", actor, func(ctx context.Context, log jobs.Logger) error {
		log.Printf("Aktualisiere Paketlisten …")
		var pkgs []Package
		err := u.agent.Call(ctx, "updates.check", nil, &pkgs)
		result := "ok"
		if err == nil {
			var b []byte
			if b, err = json.Marshal(pkgs); err == nil {
				if err = u.set(ctx, keyPackages, string(b)); err == nil {
					err = u.set(ctx, keyCheckedAt, u.now().UTC().Format(time.RFC3339))
				}
				if err == nil {
					// Eine neue Suche beginnt frisch: das Protokoll des letzten Laufs wird verworfen.
					err = u.set(ctx, keyUnit, "")
				}
			}
		}
		if err != nil {
			result = "error"
		}
		_ = audit.Record(ctx, u.db, audit.Entry{Actor: actor, Action: "updates.check", Target: "system", Result: result})
		if err == nil {
			log.Printf("%d Updates verfügbar", len(pkgs))
			u.hub.Publish(events.Message{Type: "updates.changed"})
		}
		return err
	}), nil
}

// Install startet die Installation in einer eigenen Unit und liefert deren Namen.
func (u *Updates) Install(ctx context.Context, actor string) (string, error) {
	var res struct {
		Unit string `json:"unit"`
	}
	err := u.agent.Call(ctx, "updates.install", nil, &res)
	result := "ok"
	if err != nil {
		result = "error"
	}
	_ = audit.Record(ctx, u.db, audit.Entry{Actor: actor, Action: "updates.install", Target: res.Unit, Result: result})
	if err != nil {
		return "", err
	}
	if err := u.set(ctx, keyUnit, res.Unit); err != nil {
		return "", err
	}
	_ = u.set(ctx, keyPackages, "[]") // die Liste ist nach dem Update veraltet
	u.hub.Publish(events.Message{Type: "updates.changed"})
	return res.Unit, nil
}

// SetChannel stellt die BetterNAS-Paketquelle auf einen anderen Kanal um. Die gespeicherte Paketliste stammt
// vom alten Kanal und wird verworfen; die nächste Suche füllt sie neu.
func (u *Updates) SetChannel(ctx context.Context, actor, channel string) error {
	if channel != "stable" && channel != "testing" {
		return apierr.New(apierr.InvalidParams, "Ungültiger Update-Kanal")
	}
	err := u.agent.Call(ctx, "updates.setchannel", map[string]string{"channel": channel}, nil)
	result := "ok"
	if err != nil {
		result = "error"
	}
	_ = audit.Record(ctx, u.db, audit.Entry{Actor: actor, Action: "updates.setchannel", Target: channel, Result: result})
	if err != nil {
		return err
	}
	_ = u.set(ctx, keyPackages, "[]")
	u.hub.Publish(events.Message{Type: "updates.changed"})
	return nil
}

// SetAuto schaltet die automatischen Updates ein oder aus.
func (u *Updates) SetAuto(ctx context.Context, actor string, enabled bool) error {
	err := u.agent.Call(ctx, "updates.setauto", map[string]bool{"enabled": enabled}, nil)
	result := "ok"
	if err != nil {
		result = "error"
	}
	target := "aus"
	if enabled {
		target = "ein"
	}
	_ = audit.Record(ctx, u.db, audit.Entry{Actor: actor, Action: "updates.setauto", Target: target, Result: result})
	if err != nil {
		return err
	}
	u.hub.Publish(events.Message{Type: "updates.changed"})
	return nil
}
