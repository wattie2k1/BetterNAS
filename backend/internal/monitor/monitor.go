// Package monitor leitet aus Platten-, Pool- und SnapRAID-Zustand Befunde ab und meldet Änderungen.
package monitor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/betternas/betternas/internal/docker"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/notify"
	"github.com/betternas/betternas/internal/snapraid"
	"github.com/betternas/betternas/internal/storage"
)

// Source liefert den beobachteten Zustand (implementiert von storage.Manager).
type Source interface {
	ListDisks(ctx context.Context) ([]storage.DiskView, error)
	ListPools(ctx context.Context) ([]storage.PoolView, error)
	SnapraidStatus(ctx context.Context, pool string) (snapraid.Status, error)
}

// DockerSource liefert den Zustand der Container (implementiert von docker.Manager).
type DockerSource interface {
	Status(ctx context.Context) (docker.Status, error)
	ListContainers(ctx context.Context) ([]docker.Container, error)
}

// Notifier verschickt Meldungen (implementiert von notify.Service).
type Notifier interface {
	Send(ctx context.Context, m notify.Message) []notify.Result
}

// Finding ist ein aktuell bestehendes Problem.
type Finding struct {
	Key         string
	Level       notify.Level
	Title, Body string
	// Confirm ist die Zahl aufeinanderfolgender Prüfungen, in denen der Befund bestehen muss, bevor er gemeldet wird
	// (0/1: sofort). Das filtert kurze Zacken, z. B. einen einzelnen Neustart.
	Confirm int
}

// Active ist ein gemerkter, noch nicht behobener Befund.
type Active struct {
	Key   string       `json:"key"`
	Level notify.Level `json:"level"`
	Title string       `json:"title"`
	Body  string       `json:"body"`
	Since time.Time    `json:"since"`
}

// Monitor gleicht Befunde mit den gemerkten Alarmen ab.
type Monitor struct {
	db  *sql.DB
	src Source
	n   Notifier
	hub *events.Hub

	docker DockerSource // optional
	mu     sync.Mutex
	seen   map[string]int // Befund → Zahl aufeinanderfolgender Beobachtungen (nur mit Confirm > 1)
}

// New erzeugt einen Monitor; hub darf nil sein.
func New(d *sql.DB, src Source, n Notifier, hub *events.Hub) *Monitor {
	return &Monitor{db: d, src: src, n: n, hub: hub}
}

// SetDocker schaltet die Überwachung der Docker-Container zu (optional, vor dem Start von Run aufrufen).
func (m *Monitor) SetDocker(d DockerSource) { m.docker = d }

// dockerFindings meldet Container mit fehlschlagendem Healthcheck und solche in einer Neustart-Schleife.
// Absichtlich gestoppte oder beendete Container sind kein Befund.
func (m *Monitor) dockerFindings(ctx context.Context) (fs []Finding, unknown []string, err error) {
	if m.docker == nil {
		return nil, nil, nil
	}
	st, serr := m.docker.Status(ctx)
	if serr != nil || !st.Installed || !st.Active {
		// Ohne laufendes Docker gibt es nichts zu beurteilen; bestehende Alarme bleiben, es gibt keine Fehlermeldung.
		return nil, []string{"container-"}, nil
	}
	list, lerr := m.docker.ListContainers(ctx)
	if lerr != nil {
		return nil, []string{"container-"}, fmt.Errorf("container: %w", lerr)
	}
	for _, c := range list {
		where := ""
		if c.Stack != "" {
			where = " (Stack " + c.Stack + ")"
		}
		switch {
		case c.State == "restarting":
			fs = append(fs, Finding{Key: "container-restarting:" + c.Name, Level: "crit", Confirm: 2,
				Title: "Container startet dauernd neu: " + c.Name,
				Body:  "Der Container „" + c.Name + "“" + where + " stürzt immer wieder ab und wird neu gestartet. Prüfe die Logs unter Docker → Container."})
		case c.Health == "unhealthy":
			fs = append(fs, Finding{Key: "container-unhealthy:" + c.Name, Level: "warn", Confirm: 2,
				Title: "Container fehlerhaft: " + c.Name,
				Body:  "Der Healthcheck des Containers „" + c.Name + "“" + where + " schlägt fehl; der Dienst antwortet vermutlich nicht. Prüfe die Logs unter Docker → Container."})
		}
	}
	return fs, nil, nil
}

// confirmed zählt aufeinanderfolgende Beobachtungen und lässt Befunde mit Confirm erst nach genug Wiederholungen durch.
func (m *Monitor) confirmed(fs []Finding) []Finding {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := map[string]int{}
	out := make([]Finding, 0, len(fs))
	for _, f := range fs {
		if f.Confirm <= 1 {
			out = append(out, f)
			continue
		}
		n := m.seen[f.Key] + 1
		next[f.Key] = n
		if n >= f.Confirm {
			out = append(out, f)
		}
	}
	m.seen = next
	return out
}

func diskLabel(d storage.DiskView) string {
	switch {
	case d.Model != "" && d.Serial != "":
		return d.Model + " (" + d.Serial + ")"
	case d.Model != "":
		return d.Model
	}
	return d.ID
}

// collect liefert die Befunde und die Schlüssel-Präfixe, die wegen Quellfehlern nicht beurteilt werden konnten.
func (m *Monitor) collect(ctx context.Context) (fs []Finding, unknown []string, err error) {
	cfg, cerr := notify.LoadConfig(ctx, m.db)
	if cerr != nil {
		return nil, nil, cerr
	}
	add := func(key string, lvl notify.Level, title, body string) {
		fs = append(fs, Finding{Key: key, Level: lvl, Title: title, Body: body})
	}
	disks, derr := m.src.ListDisks(ctx)
	if derr != nil {
		unknown = append(unknown, "smart:", "smart-sectors:", "temp:", "disk-missing:")
		err = errors.Join(err, fmt.Errorf("platten: %w", derr))
	}
	for _, d := range disks {
		label := diskLabel(d)
		if d.State == "missing" {
			add("disk-missing:"+d.ID, "crit", "Platte fehlt: "+label, "Die Platte ist nicht mehr erreichbar. Prüfe Kabel und Stromversorgung.")
			continue
		}
		s := d.Smart
		if s == nil {
			continue
		}
		noValues := s.Standby && !s.Passed && s.TemperatureC == 0 && s.PowerOnHours == 0
		if !s.Passed && !noValues {
			add("smart:"+d.ID, "crit", "SMART-Fehler: "+label, "Die Platte meldet einen SMART-Fehler. Sichere die Daten und tausche die Platte aus.")
		}
		if s.PendingSectors > 0 || s.ReallocatedSectors > 0 {
			add("smart-sectors:"+d.ID, "warn", "Defekte Sektoren: "+label,
				fmt.Sprintf("Neu zugewiesene Sektoren: %d, instabile Sektoren: %d. Beobachte die Platte und plane einen Austausch.", s.ReallocatedSectors, s.PendingSectors))
		}
		if s.TemperatureC > 0 && s.TemperatureC >= cfg.TempWarnC {
			add("temp:"+d.ID, "warn", "Platte zu warm: "+label, fmt.Sprintf("Die Platte ist %d °C warm (Schwelle %d °C). Prüfe die Belüftung.", s.TemperatureC, cfg.TempWarnC))
		}
	}
	pools, perr := m.src.ListPools(ctx)
	if perr != nil {
		unknown = append(unknown, "pool-unmounted:", "snapraid-sync:", "snapraid-scrub:", "snapraid-recovery:")
		err = errors.Join(err, fmt.Errorf("pools: %w", perr))
	}
	for _, p := range pools {
		if !p.Mounted {
			add("pool-unmounted:"+p.Name, "crit", "Pool nicht eingehängt: "+p.Name, "Der Speicherpool ist nicht verfügbar; Freigaben und Container, die ihn nutzen, funktionieren nicht.")
		}
		st, serr := m.src.SnapraidStatus(ctx, p.Name)
		if serr != nil {
			unknown = append(unknown, "snapraid-sync:"+p.Name, "snapraid-scrub:"+p.Name, "snapraid-recovery:"+p.Name)
			err = errors.Join(err, fmt.Errorf("snapraid %s: %w", p.Name, serr))
			continue
		}
		if !st.Enabled {
			continue
		}
		if rec := st.Recovery; rec != nil {
			detail := "Die Dateien der ersetzten Platte " + rec.Disk + " sind noch nicht wiederhergestellt; Synchronisierung und Prüfung pausieren."
			if st.LastFix != nil && strings.TrimSpace(st.LastFix.Message) != "" {
				detail += " Letztes Ergebnis: " + st.LastFix.Message
			}
			add("snapraid-recovery:"+p.Name, "crit", "SnapRAID-Wiederherstellung offen: "+p.Name, detail+" Wiederhole sie oder brich sie unter „Speicherpools“ ab.")
		}
		if r := st.LastSync; r != nil && (r.Result == snapraid.ResultFailed || r.Result == snapraid.ResultBlocked) {
			add("snapraid-sync:"+p.Name, "crit", "SnapRAID-Synchronisierung fehlgeschlagen: "+p.Name, orDefault(r.Message, "Die letzte Synchronisierung ist nicht durchgelaufen."))
		}
		if r := st.LastScrub; r != nil && (r.Result == snapraid.ResultFailed || r.Result == snapraid.ResultErrors) {
			add("snapraid-scrub:"+p.Name, "crit", "SnapRAID-Prüfung meldet Fehler: "+p.Name, orDefault(r.Message, "Die letzte Prüfung ist fehlgeschlagen."))
		}
	}
	dfs, dunknown, derr2 := m.dockerFindings(ctx)
	fs = append(fs, dfs...)
	unknown = append(unknown, dunknown...)
	err = errors.Join(err, derr2)
	return fs, unknown, err
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// Findings leitet die aktuellen Befunde ab (reine Ableitung, ohne Seiteneffekte).
func (m *Monitor) Findings(ctx context.Context) ([]Finding, error) {
	fs, _, err := m.collect(ctx)
	return fs, err
}

// Active liefert die gemerkten, noch nicht behobenen Befunde.
func (m *Monitor) Active(ctx context.Context) ([]Active, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT key, level, title, body, since FROM alerts ORDER BY since, key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Active{}
	for rows.Next() {
		var a Active
		var lvl, since string
		if err := rows.Scan(&a.Key, &lvl, &a.Title, &a.Body, &since); err != nil {
			return nil, err
		}
		a.Level = notify.Level(lvl)
		a.Since, _ = time.Parse(time.RFC3339, since)
		out = append(out, a)
	}
	return out, rows.Err()
}

func isUnknown(key string, unknown []string) bool {
	for _, p := range unknown {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// Check gleicht die Befunde mit den gemerkten Alarmen ab: neue werden gemeldet, behobene einmal als „Behoben“.
// Konnte ein Teil des Zustands nicht gelesen werden, bleiben dessen Alarme unangetastet; der Fehler wird zurückgegeben.
func (m *Monitor) Check(ctx context.Context) error {
	fs, unknown, srcErr := m.collect(ctx)
	if fs == nil && srcErr != nil && len(unknown) == 0 {
		return srcErr
	}
	active, err := m.Active(ctx)
	if err != nil {
		return err
	}
	known := map[string]Active{}
	for _, a := range active {
		known[a.Key] = a
	}
	fs = m.confirmed(fs) // auch bei leerer Liste: setzt die Zähler zurück
	changed := false
	now := time.Now().UTC().Format(time.RFC3339)
	current := map[string]bool{}
	for _, f := range fs {
		current[f.Key] = true
		if _, ok := known[f.Key]; ok {
			continue
		}
		if _, err := m.db.ExecContext(ctx, `INSERT INTO alerts(key, level, title, body, since) VALUES (?,?,?,?,?)`, f.Key, string(f.Level), f.Title, f.Body, now); err != nil {
			return err
		}
		changed = true
		m.n.Send(ctx, notify.Message{Level: f.Level, Title: f.Title, Body: f.Body})
	}
	for _, a := range active {
		if current[a.Key] || isUnknown(a.Key, unknown) {
			continue
		}
		if _, err := m.db.ExecContext(ctx, `DELETE FROM alerts WHERE key = ?`, a.Key); err != nil {
			return err
		}
		changed = true
		m.n.Send(ctx, notify.Message{Level: "info", Title: "Behoben: " + a.Title, Body: "Das Problem besteht nicht mehr."})
	}
	if changed && m.hub != nil {
		m.hub.Publish(events.Message{Type: "alerts.changed"})
	}
	return srcErr
}

// Run prüft 30 s nach dem Start und danach alle 5 Minuten, bis ctx endet.
func (m *Monitor) Run(ctx context.Context) {
	delay := 30 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		cctx, cancel := context.WithTimeout(ctx, time.Minute)
		if err := m.Check(cctx); err != nil {
			slog.Warn("überwachung", "err", err)
		}
		cancel()
		delay = 5 * time.Minute
	}
}
