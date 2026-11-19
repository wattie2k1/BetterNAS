package tlsmgr

import (
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/notify"
)

const keyConfig = "tls.config"

// Mode sagt, woher das aktive Zertifikat stammt.
type Mode string

const (
	ModeSelfSigned Mode = "selfsigned" // einfaches selbstsigniertes Zertifikat
	ModeLocalCA    Mode = "localca"    // von der lokalen CA ausgestellt, wird automatisch erneuert
	ModeACME       Mode = "acme"       // Let's Encrypt, wird automatisch erneuert
	ModeCustom     Mode = "custom"     // vom Nutzer hochgeladen
)

// ACMEConfig sind die Einstellungen für Let's Encrypt.
type ACMEConfig struct {
	Provider    string            `json:"provider"`
	Domains     []string          `json:"domains"`
	Email       string            `json:"email"`
	Staging     bool              `json:"staging"`
	AgreedTerms bool              `json:"agreedTerms"`
	Credentials map[string]string `json:"credentials,omitempty"`
	LastIssued  *time.Time        `json:"lastIssued,omitempty"`
	LastError   string            `json:"lastError,omitempty"`
	LastErrorAt *time.Time        `json:"lastErrorAt,omitempty"`
}

// LocalConfig sind die Einstellungen der lokalen CA.
type LocalConfig struct {
	Names []string `json:"names"` // zusätzliche Namen/IPs im Zertifikat
}

// Config ist die gespeicherte Gesamtkonfiguration.
type Config struct {
	Mode  Mode        `json:"mode"`
	Local LocalConfig `json:"local"`
	ACME  ACMEConfig  `json:"acme"`
}

// Store ist das aktive Zertifikat der Weboberfläche (api.CertStore).
type Store interface {
	Install(certPEM, keyPEM []byte) error
	Leaf() *x509.Certificate
}

// Notifier verschickt Meldungen (notify.Service).
type Notifier interface {
	Send(ctx context.Context, m notify.Message) []notify.Result
}

// Manager verwaltet die Zertifikatsquellen.
type Manager struct {
	db       *sql.DB
	dir      string
	store    Store
	notifier Notifier
	issuer   Issuer

	// Hostname und LocalIPs sind für Tests austauschbar.
	Hostname func() string
	LocalIPs func() []net.IP
	Now      func() time.Time

	mu         sync.Mutex
	acmeBusy   bool
	lastNotify time.Time
}

// New erzeugt einen Manager. n darf nil sein.
func New(d *sql.DB, dir string, store Store, n Notifier) *Manager {
	return &Manager{
		db: d, dir: dir, store: store, notifier: n, issuer: legoIssuer{},
		Hostname: func() string { h, _ := os.Hostname(); return h },
		LocalIPs: defaultLocalIPs,
		Now:      time.Now,
	}
}

// SetIssuer ersetzt die Gegenstelle für Let's Encrypt (für Tests).
func (m *Manager) SetIssuer(i Issuer) { m.issuer = i }

func invalid(msg string) error { return apierr.New(apierr.InvalidParams, msg) }

func (m *Manager) loadConfig(ctx context.Context) (Config, error) {
	var c Config
	var raw string
	err := m.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, keyConfig).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Config{}, nil
	}
	return c, nil
}

func (m *Manager) saveConfig(ctx context.Context, c Config) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, keyConfig, string(b))
	return err
}

// effectiveMode ergänzt eine fehlende Betriebsart aus dem aktiven Zertifikat.
func (m *Manager) effectiveMode(c Config) Mode {
	if c.Mode != "" {
		return c.Mode
	}
	if l := m.store.Leaf(); l != nil && l.Subject.String() != l.Issuer.String() {
		return ModeCustom
	}
	return ModeSelfSigned
}

// SetMode merkt sich die Betriebsart nach dem Hochladen (custom) oder Neuerzeugen (selfsigned).
func (m *Manager) SetMode(ctx context.Context, mode Mode) error {
	if mode != ModeSelfSigned && mode != ModeCustom {
		return fmt.Errorf("Betriebsart %q kann nicht direkt gesetzt werden", mode)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := m.loadConfig(ctx)
	if err != nil {
		return err
	}
	c.Mode = mode
	return m.saveConfig(ctx, c)
}

// ---- Namen ----

var (
	labelRe    = `[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?`
	dnsNameRe  = regexp.MustCompile(`^` + labelRe + `(\.` + labelRe + `)*$`)
	emailRe    = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	bannedTLDs = map[string]bool{"local": true, "lan": true, "localhost": true, "internal": true, "home": true, "localdomain": true, "arpa": true, "intranet": true, "private": true, "corp": true}
)

// splitNames prüft zusätzliche Namen/IPs für die lokale CA.
func splitNames(names []string) (dns []string, ips []net.IP, clean []string, err error) {
	if len(names) > 20 {
		return nil, nil, nil, invalid("Es sind höchstens 20 zusätzliche Namen möglich.")
	}
	seen := map[string]bool{}
	for _, raw := range names {
		n := strings.ToLower(strings.TrimSpace(raw))
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		if ip := net.ParseIP(n); ip != nil {
			ips = append(ips, ip)
		} else if len(n) <= 253 && dnsNameRe.MatchString(n) {
			dns = append(dns, n)
		} else {
			return nil, nil, nil, invalid(fmt.Sprintf("„%s“ ist weder ein gültiger Name noch eine IP-Adresse.", raw))
		}
		clean = append(clean, n)
	}
	if clean == nil {
		clean = []string{}
	}
	return dns, ips, clean, nil
}

func defaultLocalIPs() []net.IP {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		skip := false
		for _, p := range []string{"docker", "br-", "veth", "virbr", "vnet"} {
			if strings.HasPrefix(ifc.Name, p) {
				skip = true
			}
		}
		if skip {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if v4 := ipn.IP.To4(); v4 != nil && !v4.IsLinkLocalUnicast() {
				out = append(out, v4)
			}
		}
	}
	return out
}

// localNames sammelt alle Namen und Adressen für das Zertifikat der lokalen CA.
func (m *Manager) localNames(extra []string) (dns []string, ips []net.IP, err error) {
	xd, xi, _, err := splitNames(extra)
	if err != nil {
		return nil, nil, err
	}
	host := strings.ToLower(strings.TrimSpace(m.Hostname()))
	if host == "" || !dnsNameRe.MatchString(host) {
		host = "betternas"
	}
	seenD, seenI := map[string]bool{}, map[string]bool{}
	addD := func(n string) {
		if !seenD[n] {
			seenD[n] = true
			dns = append(dns, n)
		}
	}
	addI := func(ip net.IP) {
		if !seenI[ip.String()] {
			seenI[ip.String()] = true
			ips = append(ips, ip)
		}
	}
	addD(host)
	if !strings.HasSuffix(host, ".local") {
		addD(host + ".local")
	}
	addD("localhost")
	for _, n := range xd {
		addD(n)
	}
	addI(net.ParseIP("127.0.0.1"))
	addI(net.ParseIP("::1"))
	for _, ip := range m.LocalIPs() {
		addI(ip)
	}
	for _, ip := range xi {
		addI(ip)
	}
	return dns, ips, nil
}

// ---- Lokale CA ----

// EnableLocalCA richtet die lokale CA ein (falls nötig), stellt das Zertifikat aus und aktiviert es.
// resetCA ersetzt eine vorhandene CA; alle Geräte müssen dann die neue CA erneut installieren.
func (m *Manager) EnableLocalCA(ctx context.Context, extra []string, resetCA bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, _, clean, err := splitNames(extra)
	if err != nil {
		return err
	}
	dns, ips, err := m.localNames(clean)
	if err != nil {
		return err
	}
	ca, err := LoadLocalCA(m.dir)
	if resetCA || errors.Is(err, os.ErrNotExist) {
		ca, err = CreateLocalCA(m.dir, m.hostnameOrDefault())
	}
	if err != nil {
		return err
	}
	certPEM, keyPEM, err := ca.Issue(dns, ips)
	if err != nil {
		return err
	}
	if err := m.store.Install(certPEM, keyPEM); err != nil {
		return err
	}
	c, err := m.loadConfig(ctx)
	if err != nil {
		return err
	}
	c.Mode, c.Local.Names = ModeLocalCA, clean
	return m.saveConfig(ctx, c)
}

func (m *Manager) hostnameOrDefault() string {
	if h := strings.TrimSpace(m.Hostname()); h != "" {
		return h
	}
	return "betternas"
}

// CACert liefert das Root-Zertifikat der lokalen CA (PEM) zum Installieren auf Geräten.
func (m *Manager) CACert() ([]byte, error) {
	ca, err := LoadLocalCA(m.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, apierr.New(apierr.NotFound, "Die lokale Zertifizierungsstelle ist noch nicht eingerichtet.")
		}
		return nil, err
	}
	return ca.CertPEM, nil
}

// renewLocal stellt das Zertifikat neu aus, wenn Adressen fehlen oder es bald abläuft.
func (m *Manager) renewLocal(ctx context.Context, c Config) error {
	ca, err := LoadLocalCA(m.dir)
	if err != nil {
		return err
	}
	dns, ips, err := m.localNames(c.Local.Names)
	if err != nil {
		return err
	}
	need, why := needsLocalReissue(m.store.Leaf(), ca, dns, ips, m.Now())
	if !need {
		return nil
	}
	slog.Info("lokales Zertifikat wird neu ausgestellt", "grund", why)
	certPEM, keyPEM, err := ca.Issue(dns, ips)
	if err != nil {
		return err
	}
	return m.store.Install(certPEM, keyPEM)
}

// ---- Let's Encrypt ----

// ACMEInput ist die Eingabe der GUI; leere Geheimnisse behalten den gespeicherten Wert.
type ACMEInput struct {
	Provider    string            `json:"provider"`
	Domains     []string          `json:"domains"`
	Email       string            `json:"email"`
	Staging     bool              `json:"staging"`
	AgreedTerms bool              `json:"agreedTerms"`
	Credentials map[string]string `json:"credentials"`
}

func cleanDomains(in []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, raw := range in {
		d := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		if strings.HasPrefix(d, "*.") {
			return nil, invalid("Platzhalter-Domains (*.…) werden nicht unterstützt.")
		}
		if net.ParseIP(d) != nil {
			return nil, invalid("Let's Encrypt stellt keine Zertifikate für IP-Adressen aus – trage einen Domainnamen ein.")
		}
		labels := strings.Split(d, ".")
		if len(d) > 253 || len(labels) < 2 || !dnsNameRe.MatchString(d) {
			return nil, invalid(fmt.Sprintf("„%s“ ist kein gültiger öffentlicher Domainname (z. B. meinnas.duckdns.org).", raw))
		}
		if bannedTLDs[labels[len(labels)-1]] {
			return nil, invalid(fmt.Sprintf("„%s“ endet auf eine nicht öffentliche Domain – dafür stellt Let's Encrypt nichts aus.", raw))
		}
		out = append(out, d)
	}
	if len(out) == 0 {
		return nil, invalid("Bitte mindestens eine Domain eintragen.")
	}
	if len(out) > 5 {
		return nil, invalid("Es sind höchstens 5 Domains möglich.")
	}
	return out, nil
}

// SaveACME speichert die Let's-Encrypt-Einstellungen (ohne etwas anzufordern).
func (m *Manager) SaveACME(ctx context.Context, in ACMEInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	prov, ok := findProvider(in.Provider)
	if !ok {
		return invalid("Bitte einen DNS-Anbieter wählen.")
	}
	domains, err := cleanDomains(in.Domains)
	if err != nil {
		return err
	}
	email := strings.TrimSpace(in.Email)
	if email != "" && !emailRe.MatchString(email) {
		return invalid("Die E-Mail-Adresse ist ungültig.")
	}
	if !in.AgreedTerms {
		return invalid("Bitte den Nutzungsbedingungen von Let's Encrypt zustimmen.")
	}
	c, err := m.loadConfig(ctx)
	if err != nil {
		return err
	}
	old := map[string]string{}
	if c.ACME.Provider == prov.ID {
		old = c.ACME.Credentials
	}
	creds := map[string]string{}
	for _, f := range prov.Fields {
		v := strings.TrimSpace(in.Credentials[f.Key])
		if v == "" {
			v = old[f.Key]
		}
		if v == "" && !f.Optional {
			return invalid(fmt.Sprintf("Bitte „%s“ ausfüllen.", f.Label))
		}
		if v != "" {
			creds[f.Key] = v
		}
	}
	prev := c.ACME
	c.ACME = ACMEConfig{
		Provider: prov.ID, Domains: domains, Email: email, Staging: in.Staging, AgreedTerms: true, Credentials: creds,
		LastIssued: prev.LastIssued, LastError: prev.LastError, LastErrorAt: prev.LastErrorAt,
	}
	return m.saveConfig(ctx, c)
}

// RemoveACME löscht Einstellungen und Zugangsdaten. Das aktive Zertifikat bleibt, wird aber nicht mehr erneuert.
func (m *Manager) RemoveACME(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := m.loadConfig(ctx)
	if err != nil {
		return err
	}
	c.ACME = ACMEConfig{}
	if m.effectiveMode(c) == ModeACME {
		c.Mode = ModeCustom
	}
	return m.saveConfig(ctx, c)
}

// ErrBusy zeigt an, dass schon eine Anforderung läuft.
var ErrBusy = errors.New("es läuft bereits eine Zertifikatsanforderung")

// IssueACME fordert das Zertifikat an und aktiviert es. Bei Fehlern bleibt das alte Zertifikat aktiv.
func (m *Manager) IssueACME(ctx context.Context, log Logger) error {
	m.mu.Lock()
	c, err := m.loadConfig(ctx)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	if m.acmeBusy {
		m.mu.Unlock()
		return ErrBusy
	}
	if c.ACME.Provider == "" || len(c.ACME.Domains) == 0 {
		m.mu.Unlock()
		return invalid("Let's Encrypt ist noch nicht eingerichtet.")
	}
	m.acmeBusy = true
	cfg := c.ACME
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.acmeBusy = false; m.mu.Unlock() }()

	certPEM, keyPEM, err := m.issuer.Obtain(ctx, cfg, m.dir, log)
	if err == nil {
		err = m.checkIssued(certPEM, cfg.Domains)
	}
	if err == nil {
		err = m.store.Install(certPEM, keyPEM)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	c, lerr := m.loadConfig(ctx)
	if lerr != nil {
		return lerr
	}
	now := m.Now()
	if err != nil {
		c.ACME.LastError, c.ACME.LastErrorAt = err.Error(), &now
		_ = m.saveConfig(ctx, c)
		return err
	}
	c.Mode = ModeACME
	c.ACME.LastIssued, c.ACME.LastError, c.ACME.LastErrorAt = &now, "", nil
	log.Printf("Zertifikat ausgestellt und aktiviert.")
	return m.saveConfig(ctx, c)
}

// checkIssued prüft, ob das Zertifikat alle gewünschten Domains abdeckt.
func (m *Manager) checkIssued(certPEM []byte, domains []string) error {
	var leaf *x509.Certificate
	rest := certPEM
	for {
		var blk *pem.Block
		blk, rest = pem.Decode(rest)
		if blk == nil {
			break
		}
		if blk.Type == "CERTIFICATE" {
			c, err := x509.ParseCertificate(blk.Bytes)
			if err != nil {
				return err
			}
			leaf = c
			break
		}
	}
	if leaf == nil {
		return errors.New("Let's Encrypt hat kein Zertifikat geliefert")
	}
	for _, d := range domains {
		if err := leaf.VerifyHostname(d); err != nil {
			return fmt.Errorf("das ausgestellte Zertifikat gilt nicht für %s", d)
		}
	}
	return nil
}

func (m *Manager) renewACME(ctx context.Context, c Config) error {
	if c.ACME.Provider == "" || len(c.ACME.Domains) == 0 {
		return nil
	}
	leaf := m.store.Leaf()
	covers := leaf != nil
	if covers {
		for _, d := range c.ACME.Domains {
			if leaf.VerifyHostname(d) != nil {
				covers = false
			}
		}
	}
	remaining := time.Duration(0)
	if leaf != nil {
		remaining = leaf.NotAfter.Sub(m.Now())
	}
	if covers && remaining > renewBefore {
		return nil
	}
	err := m.IssueACME(ctx, slogLogger{})
	if err != nil && !errors.Is(err, ErrBusy) {
		m.notifyFailure(ctx, remaining, err)
	}
	return err
}

type slogLogger struct{}

func (slogLogger) Printf(format string, a ...any) {
	slog.Info(fmt.Sprintf(format, a...), "quelle", "tls")
}

func (m *Manager) notifyFailure(ctx context.Context, remaining time.Duration, err error) {
	if m.notifier == nil || remaining > 14*24*time.Hour || m.Now().Sub(m.lastNotify) < 24*time.Hour {
		return
	}
	m.lastNotify = m.Now()
	lvl := notify.Level("warn")
	if remaining <= 3*24*time.Hour {
		lvl = notify.Level("crit")
	}
	m.notifier.Send(ctx, notify.Message{
		Level: lvl,
		Title: "HTTPS-Zertifikat konnte nicht erneuert werden",
		Body:  fmt.Sprintf("Das Let's-Encrypt-Zertifikat läuft in %d Tagen ab. Fehler: %v", int(remaining.Hours()/24), err),
	})
}

// ---- Erneuerung ----

// Check führt eine Erneuerungsprüfung durch (lokale CA: Adressen/Ablauf, Let's Encrypt: Ablauf).
func (m *Manager) Check(ctx context.Context) {
	m.mu.Lock()
	c, err := m.loadConfig(ctx)
	mode := m.effectiveMode(c)
	m.mu.Unlock()
	if err != nil {
		slog.Warn("zertifikat: konfiguration", "err", err)
		return
	}
	switch mode {
	case ModeLocalCA:
		m.mu.Lock()
		err = m.renewLocal(ctx, c)
		m.mu.Unlock()
	case ModeACME:
		err = m.renewACME(ctx, c)
	}
	if err != nil {
		slog.Warn("zertifikat: erneuern", "modus", mode, "err", err)
	}
}

// Run prüft kurz nach dem Start und danach alle 15 Minuten, ob ein Zertifikat erneuert werden muss.
func (m *Manager) Run(ctx context.Context) {
	t := time.NewTimer(30 * time.Second)
	defer t.Stop()
	acmeTick := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		// Let's Encrypt nur alle 6 Stunden (Wiederholungen bei Fehlern, aber ohne Limit-Gefahr).
		m.mu.Lock()
		c, _ := m.loadConfig(ctx)
		mode := m.effectiveMode(c)
		m.mu.Unlock()
		if mode == ModeACME && acmeTick%24 != 0 {
			// ausgelassen
		} else {
			m.Check(ctx)
		}
		acmeTick++
		t.Reset(15 * time.Minute)
	}
}

// ---- Status ----

// ProviderInfo ist ein DNS-Anbieter für die GUI.
type ProviderInfo = Provider

// LocalCAStatus beschreibt die lokale CA.
type LocalCAStatus struct {
	Exists      bool      `json:"exists"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	NotAfter    time.Time `json:"notAfter,omitempty"`
	Names       []string  `json:"names"`
}

// ACMEStatus beschreibt die Let's-Encrypt-Einstellungen ohne Geheimnisse.
type ACMEStatus struct {
	Configured     bool            `json:"configured"`
	Provider       string          `json:"provider"`
	Domains        []string        `json:"domains"`
	Email          string          `json:"email"`
	Staging        bool            `json:"staging"`
	AgreedTerms    bool            `json:"agreedTerms"`
	CredentialsSet map[string]bool `json:"credentialsSet"`
	LastIssued     *time.Time      `json:"lastIssued,omitempty"`
	LastError      string          `json:"lastError,omitempty"`
	Busy           bool            `json:"busy"`
}

// Status ist der Gesamtzustand für die GUI.
type Status struct {
	Mode      Mode           `json:"mode"`
	LocalCA   LocalCAStatus  `json:"localCA"`
	ACME      ACMEStatus     `json:"acme"`
	Providers []ProviderInfo `json:"providers"`
}

// Status liefert die Betriebsart und die Einstellungen (ohne Geheimnisse).
func (m *Manager) Status(ctx context.Context) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := m.loadConfig(ctx)
	if err != nil {
		return Status{}, err
	}
	st := Status{Mode: m.effectiveMode(c), Providers: providers}
	st.LocalCA.Names = c.Local.Names
	if st.LocalCA.Names == nil {
		st.LocalCA.Names = []string{}
	}
	if ca, err := LoadLocalCA(m.dir); err == nil {
		st.LocalCA.Exists, st.LocalCA.Fingerprint, st.LocalCA.NotAfter = true, Fingerprint(ca.Cert), ca.Cert.NotAfter
	}
	a := c.ACME
	set := map[string]bool{}
	for k, v := range a.Credentials {
		set[k] = v != ""
	}
	doms := a.Domains
	if doms == nil {
		doms = []string{}
	}
	sort.Strings(doms)
	st.ACME = ACMEStatus{
		Configured: a.Provider != "", Provider: a.Provider, Domains: doms, Email: a.Email, Staging: a.Staging,
		AgreedTerms: a.AgreedTerms, CredentialsSet: set, LastIssued: a.LastIssued, LastError: a.LastError, Busy: m.acmeBusy,
	}
	return st, nil
}
