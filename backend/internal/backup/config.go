package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/betternas/betternas/internal/docker"
)

const keyConfig = "backup.config"

var timeRe = regexp.MustCompile(`^([01]\d|2[0-3]):([0-5]\d)$`)

// Config sind die Einstellungen der automatischen Sicherung.
type Config struct {
	Enabled bool   `json:"enabled"` // nächtliche Sicherung an/aus
	Time    string `json:"time"`    // HH:MM, Ortszeit
	Keep    int    `json:"keep"`    // Anzahl behaltener Sicherungen (je Art)
	// Dir ist der Ordner der Sicherungen; leer = Systemplatte (Standard).
	Dir string `json:"dir"`
	// Appdata sichert zusätzlich den Ordner appdata (Einstellungen der Docker-Apps).
	Appdata bool `json:"appdata"`
}

// DefaultConfig liefert die Standardwerte (täglich 02:30, 7 behalten).
func DefaultConfig() Config { return Config{Enabled: true, Time: "02:30", Keep: 7} }

// Validate prüft die Einstellungen.
func (c Config) Validate() error {
	if !timeRe.MatchString(c.Time) {
		return invalid("Die Uhrzeit muss im Format HH:MM angegeben werden.")
	}
	if c.Keep < 1 || c.Keep > 90 {
		return invalid("Die Anzahl der behaltenen Sicherungen muss zwischen 1 und 90 liegen.")
	}
	if c.Dir != "" {
		if _, err := dirCheck(c.Dir); err != nil {
			return err
		}
	}
	return nil
}

// dirCheck prüft Sicherungsordner; Tests ersetzen es (ihre Ordner liegen unter /tmp).
var dirCheck = CleanDir

// CleanDir bereinigt einen Sicherungsordner und prüft ihn syntaktisch (wie die Docker-Speicherorte:
// absolut, sichere Zeichen, mindestens zwei Ebenen, keine Systemverzeichnisse).
func CleanDir(p string) (string, error) {
	c, err := docker.CleanRoot(p, false)
	if err != nil {
		return "", invalid("Ungültiger Ordner für die Sicherungen (erlaubt: ein Ordner mit mindestens zwei Ebenen, z. B. /mnt/ssd/sicherung, nicht in Systemverzeichnissen; Buchstaben ohne Umlaute, Ziffern und _ . / + @ -).")
	}
	return c, nil
}

// hm liefert Stunde und Minute (nach Validate).
func (c Config) hm() (int, int) {
	h, _ := strconv.Atoi(c.Time[:2])
	m, _ := strconv.Atoi(c.Time[3:])
	return h, m
}

func (c Config) String() string {
	s := fmt.Sprintf("%s/%d", c.Time, c.Keep)
	if c.Dir != "" {
		s += " " + c.Dir
	}
	if c.Appdata {
		s += " +appdata"
	}
	return s
}

// LoadConfig liest die gespeicherten Einstellungen (Standardwerte, wenn keine existieren oder sie ungültig sind).
func LoadConfig(ctx context.Context, d *sql.DB) (Config, error) {
	c := DefaultConfig()
	var raw string
	err := d.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, keyConfig).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	var in Config
	if json.Unmarshal([]byte(raw), &in) != nil || in.Validate() != nil {
		return DefaultConfig(), nil
	}
	return in, nil
}

// SaveConfig validiert und speichert die Einstellungen.
func SaveConfig(ctx context.Context, d *sql.DB, in Config) error {
	if err := in.Validate(); err != nil {
		return err
	}
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	_, err = d.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, keyConfig, string(b))
	return err
}
