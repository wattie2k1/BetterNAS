package agent

import (
	"context"
	"strings"
	"time"
)

// Automatische Updates (unattended-upgrades): Der Schalter ist ein APT-Drop-in, das die Debian-Vorgabe
// aus 20auto-upgrades übersteuert (spätere Dateinamen gewinnen). Der Verlauf stammt aus dem Protokoll von
// unattended-upgrades.
const (
	autoConfPath     = "/etc/apt/apt.conf.d/52betternas-auto-upgrades"
	autoDebianPath   = "/etc/apt/apt.conf.d/20auto-upgrades"
	autoLogPath      = "/var/log/unattended-upgrades/unattended-upgrades.log"
	autoConfHeader   = "// Verwaltet von BetterNAS – nicht manuell bearbeiten"
	autoPeriodicRead = `APT::Periodic::Unattended-Upgrade`
)

// AutoRun ist der letzte Lauf der automatischen Updates.
type AutoRun struct {
	At       string   `json:"at"`       // RFC 3339
	Packages []string `json:"packages"` // installierte Pakete
	Result   string   `json:"result"`   // ok | failed | unknown
}

// AutoUpdates ist der Zustand der automatischen Updates.
type AutoUpdates struct {
	Enabled bool     `json:"enabled"`
	LastRun *AutoRun `json:"lastRun"`
}

// periodicValue liefert den Wert von APT::Periodic::Unattended-Upgrade aus einer Datei ("" = nicht gesetzt).
func periodicValue(content string) string {
	val := ""
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, autoPeriodicRead); ok {
			rest = strings.Trim(strings.TrimSpace(rest), `";`)
			val = strings.TrimSpace(rest)
		}
	}
	return val
}

func autoEnabled(d Deps) bool {
	for _, p := range []string{autoConfPath, autoDebianPath} {
		b, err := d.FS.ReadFile(p)
		if err != nil {
			continue
		}
		if v := periodicValue(string(b)); v != "" {
			return v != "0"
		}
	}
	return false
}

// ParseAutoUpgradeLog liest den letzten Lauf mit installierten Paketen aus dem Protokoll von
// unattended-upgrades (deutsche und englische Meldungen). Ohne solchen Lauf ist das Ergebnis nil.
func ParseAutoUpgradeLog(log string, loc *time.Location) *AutoRun {
	var last *AutoRun
	active := false // gehört die Zeile noch zum Lauf mit den Paketen?
	for _, line := range strings.Split(log, "\n") {
		if len(line) < 24 {
			continue
		}
		ts, level, msg := line[:23], "", strings.TrimSpace(line[23:])
		if f, rest, ok := strings.Cut(msg, " "); ok {
			level, msg = f, strings.TrimSpace(rest)
		}
		for _, prefix := range []string{"Pakete, welche aktualisiert werden:", "Packages that will be upgraded:"} {
			if list, ok := strings.CutPrefix(msg, prefix); ok {
				at, err := time.ParseInLocation("2006-01-02 15:04:05,000", ts, loc)
				if err != nil {
					continue
				}
				last = &AutoRun{At: at.UTC().Format(time.RFC3339), Packages: strings.Fields(list), Result: "unknown"}
				active = true
			}
		}
		if strings.Contains(msg, "wird gestartet") || strings.HasPrefix(msg, "Starting unattended upgrades script") {
			active = false
		}
		if last == nil || !active {
			continue
		}
		switch {
		case strings.HasPrefix(msg, "Alle Systemaktualisierungen installiert"), strings.HasPrefix(msg, "All upgrades installed"):
			last.Result = "ok"
		case level == "ERROR", strings.HasPrefix(msg, "Installation failed"), strings.HasPrefix(msg, "Installation fehlgeschlagen"):
			last.Result = "failed"
		}
	}
	return last
}

// GetAutoUpdates liefert Schalter und letzten Lauf der automatischen Updates.
func GetAutoUpdates(_ context.Context, d Deps) (AutoUpdates, error) {
	out := AutoUpdates{Enabled: autoEnabled(d)}
	if b, err := d.FS.ReadFile(autoLogPath); err == nil {
		out.LastRun = ParseAutoUpgradeLog(string(b), time.Local)
	}
	return out, nil
}

// SetAutoUpdates schaltet die automatischen Updates ein oder aus.
func SetAutoUpdates(_ context.Context, d Deps, enabled bool) error {
	v := "0"
	if enabled {
		v = "1"
	}
	conf := autoConfHeader + "\n" +
		`APT::Periodic::Update-Package-Lists "1";` + "\n" +
		autoPeriodicRead + ` "` + v + `";` + "\n"
	return d.FS.WriteFile(autoConfPath, []byte(conf), 0o644)
}
