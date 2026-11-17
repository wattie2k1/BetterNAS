// Package snapraid beschreibt die SnapRAID-Konfiguration eines Pools (Einstellungen, Dateien, Status).
// Das Paket importiert bewusst nicht internal/storage (storage importiert dieses Paket);
// consistency_test.go sichert die duplizierten Werte ab.
package snapraid

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/betternas/betternas/internal/apierr"
)

// Verzeichnisse und Dateien.
const (
	ConfDir  = "/etc/betternas/snapraid"
	StateDir = "/var/lib/betternas/snapraid"
	LockDir  = "/run/betternas"
	UnitDir  = "/etc/systemd/system"

	// ManagedHeader steht in der ersten Zeile jeder erzeugten Datei (= storage.ManagedHeader).
	ManagedHeader = "# Verwaltet von BetterNAS – nicht manuell bearbeiten"
)

// Standardwerte.
const (
	DefaultSync            = "*-*-* 03:00:00"
	DefaultScrub           = "Sun *-*-* 05:00:00"
	DefaultScrubPercent    = 8
	DefaultScrubOlderDays  = 10
	DefaultDeleteThreshold = 500
)

var (
	// PoolNameRe entspricht storage.PoolNameRe.
	PoolNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	diskIDRe   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)
	calendarRe = regexp.MustCompile(`^[A-Za-z0-9 *:,./~-]{1,64}$`)
)

// DiskPath und ParityPath entsprechen storage.DiskPath und storage.ParityPath.
func DiskPath(id string) string   { return "/srv/disks/" + id }
func ParityPath(id string) string { return "/srv/parity/" + id }

func ConfPath(pool string) string     { return ConfDir + "/" + pool + ".conf" }
func SettingsPath(pool string) string { return ConfDir + "/" + pool + ".settings" }
func ContentPath(pool string) string  { return StateDir + "/" + pool + ".content" }
func StatePath(pool string) string    { return StateDir + "/" + pool + ".state.json" }
func LogPath(pool string) string      { return StateDir + "/" + pool + ".log" }
func LockPath(pool string) string     { return LockDir + "/snapraid-" + pool + ".lock" }

// FixingPath ist der Marker „Wiederherstellung offen“ (Zeile 1: SnapRAID-Name der Platte, Zeile 2: Startzeit).
func FixingPath(pool string) string { return StateDir + "/" + pool + ".fixing" }

// TimerPath liefert die Timer-Unit einer Aktion (sync | scrub).
func TimerPath(action, pool string) string {
	return UnitDir + "/betternas-snapraid-" + action + "-" + pool + ".timer"
}

func invalid(msg string) error { return apierr.New(apierr.InvalidParams, msg) }

// Settings sind die einstellbaren Werte eines SnapRAID-Pools.
type Settings struct {
	SyncSchedule    string `json:"syncSchedule"`
	ScrubSchedule   string `json:"scrubSchedule"`
	ScrubPercent    int    `json:"scrubPercent"`
	ScrubOlderDays  int    `json:"scrubOlderDays"`
	DeleteThreshold int    `json:"deleteThreshold"`
}

// WithDefaults ersetzt leere Werte (Text leer, Zahl 0) durch die Standardwerte.
func (s Settings) WithDefaults() Settings {
	if s.SyncSchedule == "" {
		s.SyncSchedule = DefaultSync
	}
	if s.ScrubSchedule == "" {
		s.ScrubSchedule = DefaultScrub
	}
	if s.ScrubPercent == 0 {
		s.ScrubPercent = DefaultScrubPercent
	}
	if s.ScrubOlderDays == 0 {
		s.ScrubOlderDays = DefaultScrubOlderDays
	}
	if s.DeleteThreshold == 0 {
		s.DeleteThreshold = DefaultDeleteThreshold
	}
	return s
}

// Validate prüft die Werte.
func (s Settings) Validate() error {
	if !calendarRe.MatchString(s.SyncSchedule) {
		return invalid("Zeitplan für die Synchronisierung ist ungültig (systemd-OnCalendar, z. B. *-*-* 03:00:00)")
	}
	if !calendarRe.MatchString(s.ScrubSchedule) {
		return invalid("Zeitplan für die Prüfung ist ungültig (systemd-OnCalendar, z. B. Sun *-*-* 05:00:00)")
	}
	if s.ScrubPercent < 1 || s.ScrubPercent > 100 {
		return invalid("Prüfumfang muss zwischen 1 und 100 Prozent liegen")
	}
	if s.ScrubOlderDays < 0 || s.ScrubOlderDays > 3650 {
		return invalid("Mindestalter für die Prüfung muss zwischen 0 und 3650 Tagen liegen")
	}
	if s.DeleteThreshold < 0 || s.DeleteThreshold > 1000000 {
		return invalid("Löschschwelle muss zwischen 0 und 1000000 Dateien liegen")
	}
	return nil
}

// PoolConfig ist die SnapRAID-Konfiguration eines Pools. Data und Parity sind Disk-IDs (Parität: Level 1, 2).
type PoolConfig struct {
	Pool   string   `json:"pool"`
	Data   []string `json:"data"`
	Parity []string `json:"parity"`
	// Names ordnet Platten-IDs einen abweichenden SnapRAID-Namen zu (ersetzte Platten behalten den Namen der alten).
	Names map[string]string `json:"names,omitempty"`
	Settings
}

// Name liefert den SnapRAID-Namen einer Datenplatte (ohne Alias: die ID).
func (p PoolConfig) Name(id string) string {
	if n, ok := p.Names[id]; ok {
		return n
	}
	return id
}

// WithDefaults füllt leere Einstellungen mit Standardwerten.
func (p PoolConfig) WithDefaults() PoolConfig {
	p.Settings = p.Settings.WithDefaults()
	return p
}

// Validate prüft Namen, Anzahl und Eindeutigkeit der Datenträger sowie die Einstellungen.
func (p PoolConfig) Validate() error {
	if !PoolNameRe.MatchString(p.Pool) {
		return invalid("Ungültiger Poolname: " + p.Pool)
	}
	if len(p.Data) == 0 {
		return invalid("SnapRAID braucht mindestens eine Datenplatte")
	}
	if len(p.Parity) < 1 || len(p.Parity) > 2 {
		return invalid("SnapRAID braucht eine oder zwei Paritätsplatten")
	}
	seen := map[string]bool{}
	for _, id := range append(append([]string{}, p.Data...), p.Parity...) {
		if !diskIDRe.MatchString(id) {
			return invalid("Ungültige Datenträger-Kennung: " + id)
		}
		if seen[id] {
			return invalid("Datenträger doppelt verwendet: " + id)
		}
		seen[id] = true
	}
	isData := map[string]bool{}
	for _, id := range p.Data {
		isData[id] = true
	}
	for id, name := range p.Names {
		if !isData[id] {
			return invalid("Alias für eine Platte, die keine Datenplatte des Pools ist: " + id)
		}
		if !diskIDRe.MatchString(name) {
			return invalid("Ungültiger SnapRAID-Name: " + name)
		}
	}
	names := map[string]bool{}
	for _, id := range p.Data {
		n := p.Name(id)
		if names[n] {
			return invalid("SnapRAID-Name doppelt: " + n)
		}
		names[n] = true
	}
	return p.Settings.Validate()
}

// State ist die SnapRAID-Konfiguration aller Pools.
type State struct {
	Pools []PoolConfig `json:"pools"`
}

// Validate prüft alle Pools; ein Datenträger darf nur in einem Pool vorkommen.
func (s State) Validate() error {
	names := map[string]bool{}
	disks := map[string]string{}
	for _, p := range s.Pools {
		if err := p.Validate(); err != nil {
			return err
		}
		if names[p.Pool] {
			return invalid("Pool doppelt: " + p.Pool)
		}
		names[p.Pool] = true
		for _, id := range append(append([]string{}, p.Data...), p.Parity...) {
			if other, ok := disks[id]; ok {
				return invalid(fmt.Sprintf("Datenträger %s gehört bereits zu Pool %s", id, other))
			}
			disks[id] = p.Pool
		}
	}
	return nil
}

// Ergebnisse eines Laufs.
const (
	ResultOK      = "ok"
	ResultErrors  = "errors"
	ResultFailed  = "failed"
	ResultBlocked = "blocked"
)

// RunState ist das Ergebnis des letzten Laufs einer Aktion.
type RunState struct {
	Action  string    `json:"action"`
	Result  string    `json:"result"`
	Message string    `json:"message,omitempty"`
	At      time.Time `json:"at"`
	Removed int       `json:"removed,omitempty"` // bei blocked: Anzahl gelöschter Dateien
	// Unrecoverable ist bei fix die Zahl der nicht wiederherstellbaren Dateien.
	Unrecoverable int `json:"unrecoverable,omitempty"`
}

// Recovery beschreibt eine offene Wiederherstellung (Marker vorhanden).
type Recovery struct {
	Disk  string    `json:"disk"`
	State string    `json:"state"` // open
	Since time.Time `json:"since"`
}

// Status ist der Zustand von SnapRAID für einen Pool.
type Status struct {
	Enabled   bool       `json:"enabled"`
	Running   bool       `json:"running"`
	LastSync  *RunState  `json:"lastSync,omitempty"`
	LastScrub *RunState  `json:"lastScrub,omitempty"`
	LastFix   *RunState  `json:"lastFix,omitempty"`
	Recovery  *Recovery  `json:"recovery,omitempty"`
	Diff      *Diff      `json:"diff,omitempty"`
	DiffAt    *time.Time `json:"diffAt,omitempty"`
	Log       string     `json:"log,omitempty"`
}

// Diff fasst die Ausgabe von `snapraid diff` zusammen.
type Diff struct {
	Equal   int `json:"equal"`
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Updated int `json:"updated"`
	Moved   int `json:"moved"`
	Copied  int `json:"copied"`
}

// Unprotected ist die Anzahl noch nicht durch Parität gesicherter Änderungen.
func (d Diff) Unprotected() int { return d.Added + d.Removed + d.Updated + d.Moved + d.Copied }

var diffLineRe = regexp.MustCompile(`^\s*(\d+) (equal|added|removed|updated|moved|copied|restored)\s*$`)

// ParseDiff liest die Zusammenfassungszeilen von `snapraid diff`.
func ParseDiff(out []byte) Diff {
	var d Diff
	for _, line := range strings.Split(string(out), "\n") {
		m := diffLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var n int
		fmt.Sscanf(m[1], "%d", &n)
		switch m[2] {
		case "equal":
			d.Equal = n
		case "added":
			d.Added = n
		case "removed":
			d.Removed = n
		case "updated":
			d.Updated = n
		case "moved":
			d.Moved = n
		case "copied":
			d.Copied = n
		}
	}
	return d
}

// ParseSettings liest die Einstellungsdatei <pool>.settings (nur die vom Lauf benötigten Zahlen).
func ParseSettings(b []byte) (Settings, error) {
	var s Settings
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return s, invalid("Ungültige Einstellungsdatei")
		}
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
			return s, invalid("Ungültiger Wert in der Einstellungsdatei: " + k)
		}
		switch k {
		case "delete_threshold":
			s.DeleteThreshold = n
		case "scrub_percent":
			s.ScrubPercent = n
		case "scrub_older_days":
			s.ScrubOlderDays = n
		}
	}
	return s, nil
}

var (
	recoveredRe   = regexp.MustCompile(`(?m)^\s*(\d+) recovered errors?\s*$`)
	unrecoveredRe = regexp.MustCompile(`(?m)^\s*(\d+) unrecoverable errors?\s*$`)
	unrecovLineRe = regexp.MustCompile(`(?m)^unrecoverable\b`)
)

// ParseFix liest aus der Ausgabe von `snapraid fix` die Zahl wiederhergestellter und nicht wiederherstellbarer Dateien.
func ParseFix(out []byte) (recovered, unrecoverable int) {
	if m := recoveredRe.FindSubmatch(out); m != nil {
		recovered, _ = strconv.Atoi(string(m[1]))
	}
	if m := unrecoveredRe.FindSubmatch(out); m != nil {
		unrecoverable, _ = strconv.Atoi(string(m[1]))
	} else {
		unrecoverable = len(unrecovLineRe.FindAll(out, -1))
	}
	return recovered, unrecoverable
}
