package docker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/notify"
)

const keyAutoUpdate = "docker.autoupdate"

var autoTimeRe = regexp.MustCompile(`^([01]\d|2[0-3]):([0-5]\d)$`)

// AutoUpdateConfig sind die Einstellungen der automatischen Updates: Die ausgewählten Stacks werden täglich zur
// eingestellten Uhrzeit aktualisiert, sofern es neue Images gibt.
type AutoUpdateConfig struct {
	Enabled bool     `json:"enabled"`
	Time    string   `json:"time"` // HH:MM, Ortszeit
	Stacks  []string `json:"stacks"`
	// Backup sichert vor jedem Update die Daten der Stacks (Standard: an). Ist das nicht möglich, wird der Stack übersprungen.
	Backup bool `json:"backup"`
}

func defaultAutoUpdate() AutoUpdateConfig {
	return AutoUpdateConfig{Time: "04:00", Stacks: []string{}, Backup: true}
}

// normalize prüft die Einstellungen und liefert die Stacks sortiert und ohne Dubletten.
func (c AutoUpdateConfig) normalize() (AutoUpdateConfig, error) {
	if !autoTimeRe.MatchString(c.Time) {
		return c, apierr.New(apierr.InvalidParams, "Die Uhrzeit muss im Format HH:MM angegeben werden.")
	}
	if len(c.Stacks) > 200 {
		return c, apierr.New(apierr.InvalidParams, "Zu viele Stacks")
	}
	seen := map[string]bool{}
	list := []string{}
	for _, n := range c.Stacks {
		if err := checkName(n); err != nil {
			return c, err
		}
		if !seen[n] {
			seen[n] = true
			list = append(list, n)
		}
	}
	sort.Strings(list)
	c.Stacks = list
	return c, nil
}

// AutoUpdate liefert die gespeicherten Einstellungen (Standard: aus, 04:00, keine Stacks).
func (m *Manager) AutoUpdate(ctx context.Context) (AutoUpdateConfig, error) {
	var raw string
	err := m.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, keyAutoUpdate).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultAutoUpdate(), nil
	}
	if err != nil {
		return defaultAutoUpdate(), err
	}
	c := defaultAutoUpdate() // fehlende Felder behalten ihren Standard
	if json.Unmarshal([]byte(raw), &c) != nil {
		return defaultAutoUpdate(), nil
	}
	if c, err = c.normalize(); err != nil {
		return defaultAutoUpdate(), nil
	}
	return c, nil
}

// SetAutoUpdate speichert die Einstellungen; der Planer richtet sich sofort danach neu aus.
func (m *Manager) SetAutoUpdate(ctx context.Context, actor string, in AutoUpdateConfig) error {
	c, err := in.normalize()
	if err != nil {
		return err
	}
	b, _ := json.Marshal(c)
	_, err = m.db.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, keyAutoUpdate, string(b))
	m.record(ctx, actor, "docker.autoupdate.config", "docker", err)
	if err == nil {
		select {
		case m.wake <- struct{}{}:
		default:
		}
	}
	return err
}

// Notifier verschickt Meldungen (implementiert von notify.Service).
type Notifier interface {
	Send(ctx context.Context, msg notify.Message) []notify.Result
}

// nextAutoRun liefert den nächsten Zeitpunkt hour:min (Ortszeit von now) nach now.
func nextAutoRun(now time.Time, hour, min int) time.Time {
	y, mo, d := now.Date()
	t := time.Date(y, mo, d, hour, min, 0, 0, now.Location())
	for !t.After(now) {
		d++
		t = time.Date(y, mo, d, hour, min, 0, 0, now.Location())
	}
	return t
}

// RunAutoUpdate startet täglich zur eingestellten Uhrzeit die automatischen Updates, bis ctx endet.
func (m *Manager) RunAutoUpdate(ctx context.Context, n Notifier) {
	for {
		cfg, err := m.AutoUpdate(ctx)
		if err != nil {
			slog.Warn("auto-update-einstellungen", "err", err)
		}
		var timer <-chan time.Time
		if cfg.Enabled && len(cfg.Stacks) > 0 {
			h, _ := strconv.Atoi(cfg.Time[:2])
			mi, _ := strconv.Atoi(cfg.Time[3:])
			timer = m.after(time.Until(nextAutoRun(m.now(), h, mi)))
		}
		select {
		case <-ctx.Done():
			return
		case <-m.wake:
			continue
		case <-timer: // abgeschaltet → nil, blockiert
		}
		if _, err := m.StartAutoUpdate(ctx, "system", n); err != nil {
			slog.Warn("auto-update", "err", err)
		}
	}
}

// StartAutoUpdate führt die automatischen Updates sofort als Job aus (auch für „Jetzt ausführen“).
func (m *Manager) StartAutoUpdate(ctx context.Context, actor string, n Notifier) (string, error) {
	cfg, err := m.AutoUpdate(ctx)
	if err != nil {
		return "", err
	}
	if len(cfg.Stacks) == 0 {
		return "", apierr.New(apierr.InvalidParams, "Es sind keine Stacks für automatische Updates ausgewählt.")
	}
	return m.jobs.Start("docker.autoupdate", "docker", actor, func(ctx context.Context, log jobs.Logger) (err error) {
		defer func() { m.record(ctx, actor, "docker.autoupdate", "docker", err) }()
		return m.autoUpdate(ctx, log, n)
	}), nil
}

// autoUpdate prüft auf neue Images, aktualisiert die ausgewählten Stacks nacheinander, prüft danach deren
// Gesundheit und rollt bei Problemen zurück. Zum Schluss geht eine Zusammenfassung an die Benachrichtigungen.
func (m *Manager) autoUpdate(ctx context.Context, log jobs.Logger, n Notifier) error {
	cfg, err := m.AutoUpdate(ctx)
	if err != nil {
		return err
	}
	if err := m.runCheck(ctx, log); err != nil {
		return err
	}
	rep, err := m.loadReport(ctx)
	if err != nil {
		return err
	}
	var lines []string
	level := notify.Level("info")
	raise := func(l notify.Level) {
		if l == "crit" || (l == "warn" && level == "info") {
			level = l
		}
	}
	for _, name := range cfg.Stacks {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if info, ok := rep.Stacks[name]; !ok || len(info.Outdated) == 0 {
			continue
		}
		line, l := m.autoUpdateStack(ctx, log, name, cfg.Backup)
		lines = append(lines, line)
		raise(l)
	}
	if len(lines) == 0 {
		log.Printf("Keine Updates für die ausgewählten Stacks.")
		return nil
	}
	title := "Docker: Stacks automatisch aktualisiert"
	switch level {
	case "crit":
		title = "Docker: automatisches Update mit Problemen"
	case "warn":
		title = "Docker: automatisches Update teilweise übersprungen"
	}
	if n != nil {
		n.Send(ctx, notify.Message{Level: level, Title: title, Body: strings.Join(lines, "\n")})
	}
	return nil
}

// autoUpdateStack aktualisiert einen Stack und liefert eine Zeile für die Zusammenfassung samt Dringlichkeit.
func (m *Manager) autoUpdateStack(ctx context.Context, log jobs.Logger, name string, backup bool) (string, notify.Level) {
	var opts RunOptions
	if backup {
		plan, err := m.BackupPlan(ctx, name)
		switch {
		case err != nil:
			log.Printf("Stack %s übersprungen: Sicherungsplan nicht lesbar (%s)", name, apierr.From(err).Message)
			return fmt.Sprintf("%s: übersprungen (Datensicherung nicht prüfbar: %s)", name, apierr.From(err).Message), "warn"
		case len(plan.Sources) > 0 && !plan.Possible:
			log.Printf("Stack %s übersprungen: %s", name, plan.Reason)
			return fmt.Sprintf("%s: übersprungen (%s)", name, plan.Reason), "warn"
		case len(plan.Sources) > 0:
			opts.Backup = true
		}
	}
	unit, err := m.startStack(ctx, "system", name, "update", opts)
	if err != nil {
		log.Printf("Stack %s übersprungen: %s", name, apierr.From(err).Message)
		return fmt.Sprintf("%s: übersprungen (%s)", name, apierr.From(err).Message), "warn"
	}
	if err := m.waitStack(ctx, log, name, "update", unit); err != nil {
		return fmt.Sprintf("%s: Update fehlgeschlagen (%s)", name, apierr.From(err).Message), "crit"
	}
	ok, reason := m.waitHealthy(ctx, log, name)
	if ok {
		return name + ": aktualisiert", "info"
	}
	log.Printf("Stack %s ist nach dem Update nicht gesund (%s), rolle zurück …", name, reason)
	unit, err = m.startStack(ctx, "system", name, "rollback", RunOptions{RestoreData: opts.Backup})
	if err == nil {
		err = m.waitStack(ctx, log, name, "rollback", unit)
	}
	if err != nil {
		return fmt.Sprintf("%s: nach dem Update nicht gesund (%s), Rollback fehlgeschlagen (%s)", name, reason, apierr.From(err).Message), "crit"
	}
	what := "zurückgerollt auf die vorherige Version"
	if opts.Backup {
		what += " (Daten aus der Sicherung vor dem Update zurückgespielt)"
	}
	return fmt.Sprintf("%s: nach dem Update nicht gesund (%s), %s", name, reason, what), "crit"
}

var exitCodeRe = regexp.MustCompile(`^Exited \((\d+)\)`)

// problem beschreibt, was an einem Container nach einem Update nicht stimmt (leer: in Ordnung);
// pending ist gesetzt, wenn er noch nicht fertig gestartet ist.
func problem(c Container) (bad, pending string) {
	switch {
	case c.State == "restarting":
		return c.Name + " startet dauernd neu", ""
	case c.State == "dead":
		return c.Name + " ist abgestürzt", ""
	case c.State == "exited":
		if m := exitCodeRe.FindStringSubmatch(c.Status); m == nil || m[1] != "0" {
			return c.Name + " wurde beendet (" + c.Status + ")", ""
		}
		return "", ""
	case c.Health == "unhealthy":
		return c.Name + " meldet „fehlerhaft“", ""
	case c.Health == "starting" || c.State == "created":
		return "", c.Name
	}
	return "", ""
}

// waitHealthy wartet, bis alle Container eines Stacks laufen und gesund sind (und das eine Weile bleiben).
// Neustart-Schleifen, Abstürze und „unhealthy“ führen sofort zum Misserfolg, ebenso ein Ablauf der Frist.
func (m *Manager) waitHealthy(ctx context.Context, log jobs.Logger, name string) (bool, string) {
	deadline := time.Now().Add(m.healthTimeout)
	var goodSince time.Time
	for {
		if ctx.Err() != nil {
			return false, "abgebrochen"
		}
		list, err := m.ListContainers(ctx)
		if err == nil {
			var mine, pending []string
			for _, c := range list {
				if c.Stack != name {
					continue
				}
				mine = append(mine, c.Name)
				bad, pend := problem(c)
				if bad != "" {
					return false, bad
				}
				if pend != "" {
					pending = append(pending, pend)
				}
			}
			if len(mine) > 0 && len(pending) == 0 {
				if goodSince.IsZero() {
					goodSince = time.Now()
				}
				if time.Since(goodSince) >= m.healthSettle {
					log.Printf("Stack %s läuft gesund.", name)
					return true, ""
				}
			} else {
				goodSince = time.Time{}
			}
			if time.Now().After(deadline) {
				if len(mine) == 0 {
					return false, "es laufen keine Container"
				}
				return false, "nicht rechtzeitig gestartet: " + strings.Join(pending, ", ")
			}
		} else if time.Now().After(deadline) {
			return false, "Docker nicht erreichbar"
		}
		time.Sleep(m.pollEvery)
	}
}
