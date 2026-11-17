// Package notify verschickt Meldungen per Mail und ntfy und führt den Verlauf.
package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/mail"
	"net/url"
	"regexp"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
)

const keyConfig = "notify.config"

var topicRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Mail ist der SMTP-Kanal.
type Mail struct {
	Enabled  bool     `json:"enabled"`
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Security string   `json:"security"` // none | starttls | tls
	Username string   `json:"username"`
	Password string   `json:"password,omitempty"`
	From     string   `json:"from"`
	To       []string `json:"to"`
}

// Ntfy ist der ntfy-Kanal.
type Ntfy struct {
	Enabled bool   `json:"enabled"`
	Server  string `json:"server"`
	Topic   string `json:"topic"`
	Token   string `json:"token,omitempty"`
}

// Config ist die gesamte Benachrichtigungs-Konfiguration.
type Config struct {
	Mail      Mail `json:"mail"`
	Ntfy      Ntfy `json:"ntfy"`
	TempWarnC int  `json:"tempWarnC"`
}

// PublicMail ist Mail ohne Passwort.
type PublicMail struct {
	Mail
	PasswordSet bool `json:"passwordSet"`
}

// PublicNtfy ist Ntfy ohne Token.
type PublicNtfy struct {
	Ntfy
	TokenSet bool `json:"tokenSet"`
}

// PublicConfig ist die Config, wie sie die API ausliefert (ohne Geheimnisse).
type PublicConfig struct {
	Mail      PublicMail `json:"mail"`
	Ntfy      PublicNtfy `json:"ntfy"`
	TempWarnC int        `json:"tempWarnC"`
}

func defaults() Config {
	return Config{Mail: Mail{Port: 587, Security: "starttls", To: []string{}}, TempWarnC: 55}
}

// LoadConfig liest die gespeicherte Konfiguration (Standardwerte, wenn keine existiert).
func LoadConfig(ctx context.Context, d *sql.DB) (Config, error) {
	c := defaults()
	var raw string
	err := d.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, keyConfig).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return defaults(), nil
	}
	if c.Mail.To == nil {
		c.Mail.To = []string{}
	}
	return c, nil
}

// SaveConfig validiert und speichert; ein leeres Passwort/Token behält den gespeicherten Wert.
func SaveConfig(ctx context.Context, d *sql.DB, in Config) error {
	if err := in.Validate(); err != nil {
		return err
	}
	old, err := LoadConfig(ctx, d)
	if err != nil {
		return err
	}
	if in.Mail.Password == "" {
		in.Mail.Password = old.Mail.Password
	}
	if in.Ntfy.Token == "" {
		in.Ntfy.Token = old.Ntfy.Token
	}
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	_, err = d.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, keyConfig, string(b))
	return err
}

// Public entfernt die Geheimnisse.
func (c Config) Public() PublicConfig {
	p := PublicConfig{TempWarnC: c.TempWarnC}
	p.Mail = PublicMail{Mail: c.Mail, PasswordSet: c.Mail.Password != ""}
	p.Mail.Mail.Password = ""
	p.Ntfy = PublicNtfy{Ntfy: c.Ntfy, TokenSet: c.Ntfy.Token != ""}
	p.Ntfy.Ntfy.Token = ""
	if p.Mail.To == nil {
		p.Mail.To = []string{}
	}
	return p
}

func invalid(msg string) error { return apierr.New(apierr.InvalidParams, msg) }

// Validate prüft die Konfiguration; deaktivierte Kanäle werden nur grob geprüft.
func (c Config) Validate() error {
	if c.TempWarnC < 30 || c.TempWarnC > 90 {
		return invalid("Die Temperaturschwelle muss zwischen 30 und 90 °C liegen.")
	}
	if m := c.Mail; m.Enabled {
		if strings.TrimSpace(m.Host) == "" {
			return invalid("Mail: Server fehlt.")
		}
		if m.Port < 1 || m.Port > 65535 {
			return invalid("Mail: Port muss zwischen 1 und 65535 liegen.")
		}
		switch m.Security {
		case "none", "starttls", "tls":
		default:
			return invalid("Mail: Sicherheit muss none, starttls oder tls sein.")
		}
		if _, err := mail.ParseAddress(m.From); err != nil || strings.ContainsAny(m.From, "\r\n") {
			return invalid("Mail: Absenderadresse ist ungültig.")
		}
		if len(m.To) == 0 {
			return invalid("Mail: Mindestens ein Empfänger nötig.")
		}
		for _, a := range m.To {
			if _, err := mail.ParseAddress(a); err != nil || strings.ContainsAny(a, "\r\n") {
				return invalid("Mail: Empfängeradresse ist ungültig: " + a)
			}
		}
	}
	if n := c.Ntfy; n.Enabled {
		u, err := url.Parse(n.Server)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return invalid("ntfy: Server muss mit http:// oder https:// beginnen.")
		}
		if !topicRe.MatchString(n.Topic) {
			return invalid("ntfy: Topic darf nur Buchstaben, Ziffern, _ und - enthalten (max. 64).")
		}
	}
	return nil
}
