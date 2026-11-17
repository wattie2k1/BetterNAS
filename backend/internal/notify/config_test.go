package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/db"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestLoadConfigDefaults(t *testing.T) {
	c, err := LoadConfig(context.Background(), testDB(t))
	if err != nil {
		t.Fatal(err)
	}
	if c.Mail.Enabled || c.Ntfy.Enabled {
		t.Fatal("Kanäle müssen standardmäßig aus sein")
	}
	if c.Mail.Port != 587 || c.Mail.Security != "starttls" || c.TempWarnC != 55 {
		t.Fatalf("%+v", c)
	}
}

func TestSaveConfigRoundTripKeepsSecret(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	c, _ := LoadConfig(ctx, d)
	c.Mail = Mail{Enabled: true, Host: "smtp.example.org", Port: 587, Security: "starttls", Username: "u", Password: "geheim", From: "nas@example.org", To: []string{"a@example.org"}}
	c.Ntfy = Ntfy{Enabled: true, Server: "https://ntfy.sh", Topic: "nas", Token: "tok-xyz"}
	if err := SaveConfig(ctx, d, c); err != nil {
		t.Fatal(err)
	}
	c.Mail.Password, c.Ntfy.Token = "", ""
	if err := SaveConfig(ctx, d, c); err != nil {
		t.Fatal(err)
	}
	got, _ := LoadConfig(ctx, d)
	if got.Mail.Password != "geheim" || got.Ntfy.Token != "tok-xyz" {
		t.Fatalf("Geheimnis verloren: %+v", got)
	}
	pub := got.Public()
	if !pub.Mail.PasswordSet || !pub.Ntfy.TokenSet {
		t.Fatal("PasswordSet/TokenSet fehlt")
	}
	b, _ := json.Marshal(pub)
	if strings.Contains(string(b), "geheim") || strings.Contains(string(b), "tok-xyz") {
		t.Fatalf("Geheimnis im JSON: %s", b)
	}
}

func TestValidate(t *testing.T) {
	ok := func() Config {
		c := Config{TempWarnC: 55}
		c.Mail = Mail{Enabled: true, Host: "h", Port: 587, Security: "starttls", From: "a@b.de", To: []string{"c@d.de"}}
		c.Ntfy = Ntfy{Enabled: true, Server: "https://ntfy.sh", Topic: "nas"}
		return c
	}
	if err := ok().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Config){
		"adresse":   func(c *Config) { c.Mail.To = []string{"kaputt"} },
		"absender":  func(c *Config) { c.Mail.From = "x" },
		"port0":     func(c *Config) { c.Mail.Port = 0 },
		"port70000": func(c *Config) { c.Mail.Port = 70000 },
		"security":  func(c *Config) { c.Mail.Security = "ssl" },
		"ftp":       func(c *Config) { c.Ntfy.Server = "ftp://x" },
		"topic":     func(c *Config) { c.Ntfy.Topic = "a b" },
		"temp29":    func(c *Config) { c.TempWarnC = 29 },
		"temp91":    func(c *Config) { c.TempWarnC = 91 },
		"mailhost":  func(c *Config) { c.Mail.Host = "" },
		"ntfyserv":  func(c *Config) { c.Ntfy.Server = "" },
		"keinempf":  func(c *Config) { c.Mail.To = nil },
	}
	for name, mut := range cases {
		c := ok()
		mut(&c)
		err := c.Validate()
		if err == nil || apierr.From(err).Code != apierr.InvalidParams {
			t.Errorf("%s: erwartet invalid_params, bekam %v", name, err)
		}
	}
	// deaktivierte Kanäle mit leeren Feldern sind erlaubt
	if err := (Config{TempWarnC: 55}).Validate(); err != nil {
		t.Fatal(err)
	}
}
