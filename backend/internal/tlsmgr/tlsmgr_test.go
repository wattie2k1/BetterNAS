package tlsmgr

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/notify"
)

type fakeStore struct {
	leaf     *x509.Certificate
	installs int
}

func (s *fakeStore) Install(certPEM, keyPEM []byte) error {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return err
	}
	l, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return err
	}
	s.leaf = l
	s.installs++
	return nil
}
func (s *fakeStore) Leaf() *x509.Certificate { return s.leaf }

type fakeIssuer struct {
	calls int
	err   error
	ca    *LocalCA // stellt „Let's Encrypt“-Zertifikate nach
	dir   string
}

func (f *fakeIssuer) Obtain(_ context.Context, cfg ACMEConfig, _ string, log Logger) ([]byte, []byte, error) {
	f.calls++
	if f.err != nil {
		return nil, nil, f.err
	}
	return f.ca.Issue(cfg.Domains, nil)
}

type fakeNotifier struct{ msgs []notify.Message }

func (n *fakeNotifier) Send(_ context.Context, m notify.Message) []notify.Result {
	n.msgs = append(n.msgs, m)
	return nil
}

type discard struct{}

func (discard) Printf(string, ...any) {}

func newMgr(t *testing.T) (*Manager, *fakeStore, *fakeIssuer, *fakeNotifier) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	st, n := &fakeStore{}, &fakeNotifier{}
	m := New(d, t.TempDir(), st, n)
	m.Hostname = func() string { return "nas" }
	m.LocalIPs = func() []net.IP { return []net.IP{net.ParseIP("192.168.1.65")} }
	// Gegenstelle für „Let's Encrypt“: eine eigene CA, die nichts mit der lokalen CA zu tun hat.
	other, err := CreateLocalCA(t.TempDir(), "le")
	if err != nil {
		t.Fatal(err)
	}
	iss := &fakeIssuer{ca: other}
	m.issuer = iss
	return m, st, iss, n
}

func TestLocalCAIssueChainAndNames(t *testing.T) {
	dir := t.TempDir()
	ca, err := CreateLocalCA(dir, "nas")
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(filepath.Join(dir, "ca", "ca-key.pem")); st.Mode().Perm() != 0o600 {
		t.Fatal("CA-Schlüssel nur für den Besitzer lesbar")
	}
	if !ca.Cert.IsCA || ca.Cert.MaxPathLen != 0 {
		t.Fatalf("keine Wurzel-CA: %+v", ca.Cert)
	}
	certPEM, keyPEM, err := ca.Issue([]string{"nas", "nas.local"}, []net.IP{net.ParseIP("192.168.1.65")})
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(pair.Certificate[0])
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	for _, name := range []string{"nas", "nas.local", "192.168.1.65"} {
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: name}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// Eine fremde CA darf das Zertifikat nicht beglaubigen.
	other, _ := CreateLocalCA(t.TempDir(), "x")
	otherPool := x509.NewCertPool()
	otherPool.AddCert(other.Cert)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: otherPool, DNSName: "nas"}); err == nil {
		t.Fatal("fremde CA darf nicht vertrauenswürdig sein")
	}
	if leaf.NotAfter.Sub(time.Now()) > 366*24*time.Hour {
		t.Fatal("Zertifikat zu lange gültig")
	}
	// Die CA lässt sich neu laden und stellt weiter aus.
	again, err := LoadLocalCA(dir)
	if err != nil || Fingerprint(again.Cert) != Fingerprint(ca.Cert) {
		t.Fatal(err)
	}
	if _, err := LoadLocalCA(t.TempDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fehlende CA: %v", err)
	}
}

func TestEnableLocalCAAndRenewal(t *testing.T) {
	m, st, _, _ := newMgr(t)
	ctx := context.Background()
	if _, err := m.CACert(); err == nil {
		t.Fatal("ohne CA gibt es nichts herunterzuladen")
	}
	if err := m.EnableLocalCA(ctx, []string{"Nas.Fritz.Box", "10.0.0.7"}, false); err != nil {
		t.Fatal(err)
	}
	ca, _ := LoadLocalCA(m.dir)
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	for _, n := range []string{"nas", "nas.local", "localhost", "nas.fritz.box", "192.168.1.65", "10.0.0.7", "127.0.0.1"} {
		if _, err := st.leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: n}); err != nil {
			t.Fatalf("%s: %v", n, err)
		}
	}
	s, _ := m.Status(ctx)
	if s.Mode != ModeLocalCA || !s.LocalCA.Exists || len(s.LocalCA.Names) != 2 {
		t.Fatalf("%+v", s)
	}
	if pem, err := m.CACert(); err != nil || !strings.Contains(string(pem), "BEGIN CERTIFICATE") {
		t.Fatal(err)
	}

	// Nichts zu tun: keine Neuausstellung.
	before := st.installs
	m.Check(ctx)
	if st.installs != before {
		t.Fatal("unnötig neu ausgestellt")
	}
	// Neue LAN-Adresse (DHCP) → Zertifikat wird automatisch neu ausgestellt, CA bleibt.
	m.LocalIPs = func() []net.IP { return []net.IP{net.ParseIP("192.168.1.99")} }
	m.Check(ctx)
	if st.installs != before+1 {
		t.Fatal("neue Adresse wurde nicht übernommen")
	}
	if _, err := st.leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: "192.168.1.99"}); err != nil {
		t.Fatal(err)
	}
	// Bald ablaufendes Zertifikat wird erneuert.
	m.Now = func() time.Time { return time.Now().Add(340 * 24 * time.Hour) }
	m.Check(ctx)
	if st.installs != before+2 {
		t.Fatal("ablaufendes Zertifikat wurde nicht erneuert")
	}
	// CA zurücksetzen: neue CA, altes Root passt nicht mehr.
	if err := m.EnableLocalCA(ctx, nil, true); err != nil {
		t.Fatal(err)
	}
	ca2, _ := LoadLocalCA(m.dir)
	if Fingerprint(ca2.Cert) == Fingerprint(ca.Cert) {
		t.Fatal("CA wurde nicht ersetzt")
	}
	if st.leaf.CheckSignatureFrom(ca.Cert) == nil {
		t.Fatal("neues Zertifikat darf nicht von der alten CA stammen")
	}
	if err := m.EnableLocalCA(ctx, []string{"kein gültiger name!"}, false); err == nil {
		t.Fatal("ungültiger Name muss abgelehnt werden")
	}
}

func TestSaveACMEValidationAndSecrets(t *testing.T) {
	m, _, _, _ := newMgr(t)
	ctx := context.Background()
	ok := ACMEInput{Provider: "duckdns", Domains: []string{"MeinNAS.duckdns.org"}, AgreedTerms: true, Credentials: map[string]string{"token": "geheim-123"}}
	bad := []struct {
		name string
		mut  func(*ACMEInput)
	}{
		{"ohne Anbieter", func(i *ACMEInput) { i.Provider = "" }},
		{"unbekannter Anbieter", func(i *ACMEInput) { i.Provider = "evil" }},
		{"ohne Domain", func(i *ACMEInput) { i.Domains = nil }},
		{"IP statt Domain", func(i *ACMEInput) { i.Domains = []string{"192.168.1.65"} }},
		{".local", func(i *ACMEInput) { i.Domains = []string{"nas.local"} }},
		{"einzelnes Label", func(i *ACMEInput) { i.Domains = []string{"nas"} }},
		{"Platzhalter", func(i *ACMEInput) { i.Domains = []string{"*.a.duckdns.org"} }},
		{"zu viele", func(i *ACMEInput) { i.Domains = []string{"a.de", "b.de", "c.de", "d.de", "e.de", "f.de"} }},
		{"ohne Zustimmung", func(i *ACMEInput) { i.AgreedTerms = false }},
		{"ohne Token", func(i *ACMEInput) { i.Credentials = nil }},
		{"kaputte Mail", func(i *ACMEInput) { i.Email = "kein-mail" }},
	}
	for _, c := range bad {
		in := ok
		c.mut(&in)
		if err := m.SaveACME(ctx, in); err == nil {
			t.Errorf("%s muss abgelehnt werden", c.name)
		}
	}
	if err := m.SaveACME(ctx, ok); err != nil {
		t.Fatal(err)
	}
	s, _ := m.Status(ctx)
	if !s.ACME.Configured || s.ACME.Domains[0] != "meinnas.duckdns.org" || !s.ACME.CredentialsSet["token"] {
		t.Fatalf("%+v", s.ACME)
	}
	// Status enthält nie das Geheimnis.
	if b, _ := jsonString(s); strings.Contains(b, "geheim-123") {
		t.Fatal("Geheimnis im Status")
	}
	// Leeres Feld behält das gespeicherte Geheimnis.
	ok.Credentials = map[string]string{"token": ""}
	ok.Staging = true
	if err := m.SaveACME(ctx, ok); err != nil {
		t.Fatal(err)
	}
	c, _ := m.loadConfig(ctx)
	if c.ACME.Credentials["token"] != "geheim-123" || !c.ACME.Staging {
		t.Fatalf("%+v", c.ACME)
	}
	// Anbieterwechsel übernimmt keine fremden Geheimnisse.
	if err := m.SaveACME(ctx, ACMEInput{Provider: "desec", Domains: []string{"a.dedyn.io"}, AgreedTerms: true}); err == nil {
		t.Fatal("Token vom anderen Anbieter darf nicht übernommen werden")
	}
}

func TestIssueACMEAndRenewal(t *testing.T) {
	m, st, iss, n := newMgr(t)
	ctx := context.Background()
	if err := m.IssueACME(ctx, discard{}); err == nil {
		t.Fatal("ohne Einrichtung nichts anfordern")
	}
	if err := m.SaveACME(ctx, ACMEInput{Provider: "duckdns", Domains: []string{"nas.duckdns.org"}, AgreedTerms: true, Credentials: map[string]string{"token": "t"}}); err != nil {
		t.Fatal(err)
	}
	// Fehler: altes Zertifikat bleibt, Fehler wird gespeichert.
	iss.err = errors.New("DNS-Eintrag nicht gefunden")
	if err := m.IssueACME(ctx, discard{}); err == nil || st.installs != 0 {
		t.Fatalf("%v %d", err, st.installs)
	}
	s, _ := m.Status(ctx)
	if s.ACME.LastError == "" || s.Mode == ModeACME {
		t.Fatalf("%+v", s)
	}
	// Erfolg.
	iss.err = nil
	if err := m.IssueACME(ctx, discard{}); err != nil {
		t.Fatal(err)
	}
	s, _ = m.Status(ctx)
	if s.Mode != ModeACME || s.ACME.LastError != "" || s.ACME.LastIssued == nil || st.leaf.VerifyHostname("nas.duckdns.org") != nil {
		t.Fatalf("%+v", s)
	}
	// Noch lange gültig: keine neue Anforderung.
	calls := iss.calls
	m.Check(ctx)
	if iss.calls != calls {
		t.Fatal("unnötig angefordert")
	}
	// Kurz vor Ablauf: Erneuerung.
	m.Now = func() time.Time { return time.Now().Add(340 * 24 * time.Hour) }
	m.Check(ctx)
	if iss.calls != calls+1 {
		t.Fatal("Erneuerung blieb aus")
	}
	// Erneuerung scheitert kurz vor Ablauf: Meldung, aber nur einmal pro Tag.
	iss.err = errors.New("kaputt")
	m.Now = func() time.Time { return time.Now().Add(355 * 24 * time.Hour) }
	m.Check(ctx)
	m.Check(ctx)
	if len(n.msgs) != 1 || n.msgs[0].Level != "warn" && n.msgs[0].Level != "crit" {
		t.Fatalf("%+v", n.msgs)
	}
	// Einstellungen löschen: Zertifikat bleibt, wird aber nicht mehr als automatisch geführt.
	if err := m.RemoveACME(ctx); err != nil {
		t.Fatal(err)
	}
	s, _ = m.Status(ctx)
	if s.ACME.Configured || s.Mode != ModeCustom {
		t.Fatalf("%+v", s)
	}
}

func TestIssueACMERejectsWrongDomainCertAndBusy(t *testing.T) {
	m, st, iss, _ := newMgr(t)
	ctx := context.Background()
	_ = m.SaveACME(ctx, ACMEInput{Provider: "duckdns", Domains: []string{"nas.duckdns.org"}, AgreedTerms: true, Credentials: map[string]string{"token": "t"}})
	// Issuer liefert ein Zertifikat für eine andere Domain → abgelehnt, nichts installiert.
	m.issuer = issuerFunc(func(cfg ACMEConfig) ([]byte, []byte, error) { return iss.ca.Issue([]string{"andere.example.org"}, nil) })
	if err := m.IssueACME(ctx, discard{}); err == nil || st.installs != 0 {
		t.Fatalf("%v", err)
	}
	// Zweite Anforderung während einer laufenden wird abgewiesen.
	started, release := make(chan struct{}), make(chan struct{})
	m.issuer = issuerFunc(func(cfg ACMEConfig) ([]byte, []byte, error) {
		close(started)
		<-release
		return iss.ca.Issue(cfg.Domains, nil)
	})
	done := make(chan error, 1)
	go func() { done <- m.IssueACME(ctx, discard{}) }()
	<-started
	if err := m.IssueACME(ctx, discard{}); !errors.Is(err, ErrBusy) {
		t.Fatalf("erwartet ErrBusy, war %v", err)
	}
	if s, _ := m.Status(ctx); !s.ACME.Busy {
		t.Fatal("Status zeigt die laufende Anforderung nicht")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

type issuerFunc func(cfg ACMEConfig) ([]byte, []byte, error)

func (f issuerFunc) Obtain(_ context.Context, cfg ACMEConfig, _ string, _ Logger) ([]byte, []byte, error) {
	return f(cfg)
}

func TestSetModeAndInference(t *testing.T) {
	m, st, _, _ := newMgr(t)
	ctx := context.Background()
	if err := m.SetMode(ctx, ModeACME); err == nil {
		t.Fatal("acme darf nicht direkt gesetzt werden")
	}
	// Ohne gespeicherte Betriebsart wird sie aus dem Zertifikat abgeleitet.
	ca, _ := CreateLocalCA(t.TempDir(), "x")
	c, k, _ := ca.Issue([]string{"a.example.org"}, nil)
	_ = st.Install(c, k)
	if s, _ := m.Status(ctx); s.Mode != ModeCustom {
		t.Fatalf("%s", s.Mode)
	}
	if err := m.SetMode(ctx, ModeSelfSigned); err != nil {
		t.Fatal(err)
	}
	if s, _ := m.Status(ctx); s.Mode != ModeSelfSigned {
		t.Fatalf("%s", s.Mode)
	}
}

func TestProvidersBuildAndAreConsistent(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Providers() {
		if seen[p.ID] || p.Label == "" || len(p.Fields) == 0 || p.build == nil {
			t.Fatalf("Anbieter %q unvollständig", p.ID)
		}
		seen[p.ID] = true
		creds := map[string]string{}
		for _, f := range p.Fields {
			creds[f.Key] = "x"
		}
		// Mit Platzhalter-Zugangsdaten muss sich der Anbieter bauen lassen (kein Netzwerkzugriff).
		if _, err := p.build(creds); err != nil {
			// INWX meldet bei der Einrichtung evtl. Verbindungsfehler; alle anderen dürfen nicht scheitern.
			if p.ID != "inwx" {
				t.Errorf("%s: %v", p.ID, err)
			}
		}
	}
}

func TestNeedsReissueDetectsMissingAddress(t *testing.T) {
	ca, _ := CreateLocalCA(t.TempDir(), "x")
	c, k, _ := ca.Issue([]string{"nas"}, []net.IP{net.ParseIP("10.0.0.1")})
	st := &fakeStore{}
	_ = st.Install(c, k)
	if need, _ := needsLocalReissue(st.leaf, ca, []string{"nas"}, []net.IP{net.ParseIP("10.0.0.1")}, time.Now()); need {
		t.Fatal("kein Grund zur Neuausstellung")
	}
	if need, _ := needsLocalReissue(st.leaf, ca, []string{"nas"}, []net.IP{net.ParseIP("10.0.0.2")}, time.Now()); !need {
		t.Fatal("neue IP muss erkannt werden")
	}
	if need, _ := needsLocalReissue(nil, ca, nil, nil, time.Now()); !need {
		t.Fatal("ohne Zertifikat neu ausstellen")
	}
}

func jsonString(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}
