package tlsmgr

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/providers/dns/desec"
	"github.com/go-acme/lego/v4/providers/dns/digitalocean"
	"github.com/go-acme/lego/v4/providers/dns/duckdns"
	"github.com/go-acme/lego/v4/providers/dns/godaddy"
	"github.com/go-acme/lego/v4/providers/dns/hetzner"
	"github.com/go-acme/lego/v4/providers/dns/inwx"
	"github.com/go-acme/lego/v4/providers/dns/netcup"
	"github.com/go-acme/lego/v4/providers/dns/porkbun"
	"github.com/go-acme/lego/v4/registration"
)

// Logger nimmt Protokollzeilen entgegen (jobs.Logger erfüllt das).
type Logger interface {
	Printf(format string, a ...any)
}

// Field beschreibt ein Zugangsdaten-Feld eines DNS-Anbieters.
type Field struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Secret   bool   `json:"secret"`
	Optional bool   `json:"optional,omitempty"`
}

// Provider ist ein unterstützter DNS-Anbieter für die DNS-01-Challenge.
type Provider struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	Help   string  `json:"help"`
	Fields []Field `json:"fields"`
	build  func(c map[string]string) (challenge.Provider, error)
}

var providers = []Provider{
	{
		ID: "duckdns", Label: "DuckDNS (kostenlose Subdomain)",
		Help:   "Kostenlos auf duckdns.org: Subdomain anlegen, dort als IP die LAN-Adresse dieses NAS eintragen. Den Token findest du oben auf der DuckDNS-Seite.",
		Fields: []Field{{Key: "token", Label: "Token", Secret: true}},
		build: func(c map[string]string) (challenge.Provider, error) {
			cfg := duckdns.NewDefaultConfig()
			cfg.Token = c["token"]
			return duckdns.NewDNSProviderConfig(cfg)
		},
	},
	{
		ID: "desec", Label: "deSEC (kostenlose Subdomain)",
		Help:   "Kostenlos auf desec.io: Konto mit Domain (z. B. name.dedyn.io) anlegen und unter „Token Management“ einen Token erzeugen. Den A-Eintrag auf die LAN-Adresse dieses NAS setzen.",
		Fields: []Field{{Key: "token", Label: "API-Token", Secret: true}},
		build: func(c map[string]string) (challenge.Provider, error) {
			cfg := desec.NewDefaultConfig()
			cfg.Token = c["token"]
			return desec.NewDNSProviderConfig(cfg)
		},
	},
	{
		ID: "cloudflare", Label: "Cloudflare",
		Help:   "API-Token mit der Berechtigung „Zone → DNS → Bearbeiten“ (und „Zone → Zone → Lesen“) für die Zone der Domain.",
		Fields: []Field{{Key: "token", Label: "API-Token", Secret: true}},
		build: func(c map[string]string) (challenge.Provider, error) {
			cfg := cloudflare.NewDefaultConfig()
			cfg.AuthToken = c["token"]
			return cloudflare.NewDNSProviderConfig(cfg)
		},
	},
	{
		ID: "hetzner", Label: "Hetzner DNS",
		Help:   "API-Token der Hetzner-DNS-Konsole.",
		Fields: []Field{{Key: "token", Label: "API-Token", Secret: true}},
		build: func(c map[string]string) (challenge.Provider, error) {
			cfg := hetzner.NewDefaultConfig()
			cfg.APIToken = c["token"]
			return hetzner.NewDNSProviderConfig(cfg)
		},
	},
	{
		ID: "inwx", Label: "INWX",
		Help: "Zugangsdaten des INWX-Kontos (am besten ein eigener Benutzer nur für DNS).",
		Fields: []Field{
			{Key: "username", Label: "Benutzername"},
			{Key: "password", Label: "Passwort", Secret: true},
			{Key: "sharedSecret", Label: "2FA-Geheimnis (falls aktiv)", Secret: true, Optional: true},
		},
		build: func(c map[string]string) (challenge.Provider, error) {
			cfg := inwx.NewDefaultConfig()
			cfg.Username, cfg.Password, cfg.SharedSecret = c["username"], c["password"], c["sharedSecret"]
			return inwx.NewDNSProviderConfig(cfg)
		},
	},
	{
		ID: "netcup", Label: "netcup",
		Help: "Kundennummer sowie API-Schlüssel und -Passwort aus dem netcup-Kundenkontrollpanel.",
		Fields: []Field{
			{Key: "customer", Label: "Kundennummer"},
			{Key: "key", Label: "API-Schlüssel", Secret: true},
			{Key: "password", Label: "API-Passwort", Secret: true},
		},
		build: func(c map[string]string) (challenge.Provider, error) {
			cfg := netcup.NewDefaultConfig()
			cfg.Customer, cfg.Key, cfg.Password = c["customer"], c["key"], c["password"]
			return netcup.NewDNSProviderConfig(cfg)
		},
	},
	{
		ID: "porkbun", Label: "Porkbun",
		Help: "API-Schlüssel und Secret; die API-Nutzung muss für die Domain bei Porkbun aktiviert sein.",
		Fields: []Field{
			{Key: "apiKey", Label: "API-Schlüssel", Secret: true},
			{Key: "secretApiKey", Label: "Secret", Secret: true},
		},
		build: func(c map[string]string) (challenge.Provider, error) {
			cfg := porkbun.NewDefaultConfig()
			cfg.APIKey, cfg.SecretAPIKey = c["apiKey"], c["secretApiKey"]
			return porkbun.NewDNSProviderConfig(cfg)
		},
	},
	{
		ID: "godaddy", Label: "GoDaddy",
		Help: "Produktions-API-Schlüssel und Secret von developer.godaddy.com.",
		Fields: []Field{
			{Key: "apiKey", Label: "API-Schlüssel", Secret: true},
			{Key: "apiSecret", Label: "Secret", Secret: true},
		},
		build: func(c map[string]string) (challenge.Provider, error) {
			cfg := godaddy.NewDefaultConfig()
			cfg.APIKey, cfg.APISecret = c["apiKey"], c["apiSecret"]
			return godaddy.NewDNSProviderConfig(cfg)
		},
	},
	{
		ID: "digitalocean", Label: "DigitalOcean",
		Help:   "Personal Access Token mit Schreibrechten.",
		Fields: []Field{{Key: "token", Label: "Token", Secret: true}},
		build: func(c map[string]string) (challenge.Provider, error) {
			cfg := digitalocean.NewDefaultConfig()
			cfg.AuthToken = c["token"]
			return digitalocean.NewDNSProviderConfig(cfg)
		},
	},
}

// Providers liefert die unterstützten DNS-Anbieter.
func Providers() []Provider { return providers }

func findProvider(id string) (Provider, bool) {
	for _, p := range providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// Issuer holt ein Zertifikat von einer ACME-Stelle (austauschbar für Tests).
type Issuer interface {
	Obtain(ctx context.Context, cfg ACMEConfig, dir string, log Logger) (certPEM, keyPEM []byte, err error)
}

// legoIssuer ist der echte Issuer auf Basis von lego.
type legoIssuer struct{}

type acmeUser struct {
	email string
	reg   *registration.Resource
	key   crypto.PrivateKey
}

func (u *acmeUser) GetEmail() string                        { return u.email }
func (u *acmeUser) GetRegistration() *registration.Resource { return u.reg }
func (u *acmeUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

func acmeEnv(staging bool) string {
	if staging {
		return "staging"
	}
	return "production"
}

// loadAccount liest oder erzeugt Konto-Schlüssel (und gespeicherte Registrierung) je Umgebung.
func loadAccount(dir, env, email string) (*acmeUser, string, error) {
	base := filepath.Join(dir, "acme")
	if err := os.MkdirAll(base, 0o750); err != nil {
		return nil, "", err
	}
	keyFile := filepath.Join(base, "account-"+env+".key")
	regFile := filepath.Join(base, "account-"+env+".json")
	u := &acmeUser{email: email}
	if b, err := os.ReadFile(keyFile); err == nil {
		blk, _ := pem.Decode(b)
		if blk == nil {
			return nil, "", errors.New("Konto-Schlüssel nicht lesbar")
		}
		k, err := x509.ParseECPrivateKey(blk.Bytes)
		if err != nil {
			return nil, "", err
		}
		u.key = k
		if rb, err := os.ReadFile(regFile); err == nil {
			var reg registration.Resource
			if json.Unmarshal(rb, &reg) == nil && reg.URI != "" {
				u.reg = &reg
			}
		}
		return u, regFile, nil
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", err
	}
	der, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		return nil, "", err
	}
	if err := writeAtomic(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return nil, "", err
	}
	u.key = k
	return u, regFile, nil
}

// Obtain fordert über die DNS-01-Challenge ein Zertifikat an.
func (legoIssuer) Obtain(ctx context.Context, cfg ACMEConfig, dir string, log Logger) ([]byte, []byte, error) {
	prov, ok := findProvider(cfg.Provider)
	if !ok {
		return nil, nil, fmt.Errorf("unbekannter DNS-Anbieter %q", cfg.Provider)
	}
	dp, err := prov.build(cfg.Credentials)
	if err != nil {
		return nil, nil, fmt.Errorf("Zugangsdaten für %s: %w", prov.Label, err)
	}
	env := acmeEnv(cfg.Staging)
	user, regFile, err := loadAccount(dir, env, cfg.Email)
	if err != nil {
		return nil, nil, err
	}
	lc := lego.NewConfig(user)
	lc.CADirURL = lego.LEDirectoryProduction
	if cfg.Staging {
		lc.CADirURL = lego.LEDirectoryStaging
	}
	lc.Certificate.KeyType = certcrypto.EC256
	lc.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	client, err := lego.NewClient(lc)
	if err != nil {
		return nil, nil, err
	}
	if err := client.Challenge.SetDNS01Provider(dp); err != nil {
		return nil, nil, err
	}
	if user.reg == nil {
		log.Printf("Konto bei Let's Encrypt (%s) wird angelegt …", env)
		reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: cfg.AgreedTerms})
		if err != nil {
			return nil, nil, fmt.Errorf("Registrierung bei Let's Encrypt: %w", err)
		}
		user.reg = reg
		if b, err := json.Marshal(reg); err == nil {
			_ = writeAtomic(regFile, b, 0o600)
		}
	}
	log.Printf("Zertifikat für %v wird angefordert (DNS-01, das dauert bis zu einige Minuten) …", cfg.Domains)
	type result struct {
		res *certificate.Resource
		err error
	}
	ch := make(chan result, 1)
	go func() {
		res, err := client.Certificate.Obtain(certificate.ObtainRequest{Domains: cfg.Domains, Bundle: true})
		ch <- result{res, err}
	}()
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, nil, r.err
		}
		return r.res.Certificate, r.res.PrivateKey, nil
	}
}
