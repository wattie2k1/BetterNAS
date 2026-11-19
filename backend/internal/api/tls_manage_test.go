package api

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/system"
	"github.com/betternas/betternas/internal/tlsmgr"
)

type fakeACME struct {
	ca  *tlsmgr.LocalCA
	err error
}

func (f fakeACME) Obtain(_ context.Context, cfg tlsmgr.ACMEConfig, _ string, _ tlsmgr.Logger) ([]byte, []byte, error) {
	if f.err != nil {
		return nil, nil, f.err
	}
	return f.ca.Issue(cfg.Domains, nil)
}

func newTLSAPI(t *testing.T, iss tlsmgr.Issuer) (*storageAPI, *CertStore) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	jm := jobs.NewManager(d, hub)
	store := auth.NewStore(d, time.Hour)
	token, csrf, _ := store.Create(context.Background(), "anna")
	dir := t.TempDir()
	cs, err := NewCertStore(dir, "nas")
	if err != nil {
		t.Fatal(err)
	}
	tm := tlsmgr.New(d, dir, cs, nil)
	tm.Hostname = func() string { return "nas" }
	tm.LocalIPs = func() []net.IP { return []net.IP{net.ParseIP("192.168.1.65")} }
	if iss != nil {
		tm.SetIssuer(iss)
	}
	h := NewServer(Config{WebDir: t.TempDir(), Dev: true}, Deps{
		DB: d, Jobs: jm, Hub: hub,
		Auth:           auth.NewHandlers(d, store, nil, nil, false),
		SystemSettings: system.NewSettings(d, &sysAgentStub{}), Certs: cs, TLS: tm,
	})
	return &storageAPI{h: h, cookie: &http.Cookie{Name: auth.CookieName, Value: token}, csrf: csrf, jobs: jm, db: d}, cs
}

func TestTLSRoutesLocalCA(t *testing.T) {
	s, cs := newTLSAPI(t, nil)
	// Vorher: selbstsigniert, keine CA.
	w := s.do("GET", "/api/v1/system/certificate", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"mode":"selfsigned"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/system/tls/ca.crt", ""); w.Code != 404 {
		t.Fatalf("ohne CA erwartet 404, war %d", w.Code)
	}
	// Ungültiger Name wird abgelehnt.
	if w := s.do("POST", "/api/v1/system/tls/localca", `{"names":["kein name!"]}`); w.Code != 400 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/system/tls/localca", `{"names":["nas.fritz.box"]}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var st tlsmgr.Status
	_ = json.Unmarshal(s.do("GET", "/api/v1/system/tls", "").Body.Bytes(), &st)
	if st.Mode != tlsmgr.ModeLocalCA || !st.LocalCA.Exists || len(st.Providers) < 5 {
		t.Fatalf("%+v", st)
	}
	// Download: gültiges CA-Zertifikat, das das aktive Zertifikat beglaubigt.
	dl := s.do("GET", "/api/v1/system/tls/ca.crt", "")
	if dl.Code != 200 || !strings.Contains(dl.Header().Get("Content-Disposition"), "betternas-ca.crt") {
		t.Fatalf("%d %v", dl.Code, dl.Header())
	}
	blk, _ := pem.Decode(dl.Body.Bytes())
	ca, err := x509.ParseCertificate(blk.Bytes)
	if err != nil || !ca.IsCA {
		t.Fatalf("%v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	for _, n := range []string{"nas", "nas.fritz.box", "192.168.1.65"} {
		if _, err := cs.Leaf().Verify(x509.VerifyOptions{Roots: pool, DNSName: n}); err != nil {
			t.Fatalf("%s: %v", n, err)
		}
	}
	// Zertifikatsansicht zeigt die Betriebsart; Neuerzeugen schaltet zurück auf selbstsigniert.
	if w := s.do("GET", "/api/v1/system/certificate", ""); !strings.Contains(w.Body.String(), `"mode":"localca"`) {
		t.Fatal(w.Body.String())
	}
	if w := s.do("POST", "/api/v1/system/certificate/regenerate", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"mode":"selfsigned"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	// CA zurücksetzen erzeugt eine andere CA.
	_ = s.do("POST", "/api/v1/system/tls/localca", `{}`)
	var before, after tlsmgr.Status
	_ = json.Unmarshal(s.do("GET", "/api/v1/system/tls", "").Body.Bytes(), &before)
	_ = s.do("POST", "/api/v1/system/tls/localca", `{"resetCA":true}`)
	_ = json.Unmarshal(s.do("GET", "/api/v1/system/tls", "").Body.Bytes(), &after)
	if before.LocalCA.Fingerprint == after.LocalCA.Fingerprint || after.LocalCA.Fingerprint == "" {
		t.Fatal("CA wurde nicht zurückgesetzt")
	}
}

func TestTLSRoutesACME(t *testing.T) {
	other, err := tlsmgr.CreateLocalCA(t.TempDir(), "le")
	if err != nil {
		t.Fatal(err)
	}
	s, cs := newTLSAPI(t, fakeACME{ca: other})
	// Anfordern ohne Einrichtung.
	if w := s.do("POST", "/api/v1/system/tls/acme/issue", ""); w.Code != 400 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	// Ungültige Eingaben.
	for _, body := range []string{
		`{"provider":"duckdns","domains":["192.168.1.65"],"agreedTerms":true,"credentials":{"token":"t"}}`,
		`{"provider":"duckdns","domains":["nas.duckdns.org"],"agreedTerms":false,"credentials":{"token":"t"}}`,
		`{"provider":"duckdns","domains":["nas.duckdns.org"],"agreedTerms":true,"credentials":{}}`,
	} {
		if w := s.do("PUT", "/api/v1/system/tls/acme", body); w.Code != 400 {
			t.Fatalf("%s → %d", body, w.Code)
		}
	}
	w := s.do("PUT", "/api/v1/system/tls/acme", `{"provider":"duckdns","domains":["nas.duckdns.org"],"email":"a@b.de","agreedTerms":true,"credentials":{"token":"streng-geheim"}}`)
	if w.Code != 200 || strings.Contains(w.Body.String(), "streng-geheim") || !strings.Contains(w.Body.String(), `"token":true`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if g := s.do("GET", "/api/v1/system/tls", ""); strings.Contains(g.Body.String(), "streng-geheim") {
		t.Fatal("Zugangsdaten dürfen nie ausgeliefert werden")
	}
	// Anfordern als Job.
	iw := s.do("POST", "/api/v1/system/tls/acme/issue", "")
	if iw.Code != 202 {
		t.Fatalf("%d %s", iw.Code, iw.Body)
	}
	s.waitJob(t, iw.Body.Bytes())
	if cs.Leaf().VerifyHostname("nas.duckdns.org") != nil {
		t.Fatal("Zertifikat wurde nicht aktiviert")
	}
	if g := s.do("GET", "/api/v1/system/certificate", ""); !strings.Contains(g.Body.String(), `"mode":"acme"`) {
		t.Fatal(g.Body.String())
	}
	// Löschen entfernt die Einstellungen.
	if dw := s.do("DELETE", "/api/v1/system/tls/acme", ""); dw.Code != 200 || !strings.Contains(dw.Body.String(), `"configured":false`) {
		t.Fatalf("%d %s", dw.Code, dw.Body)
	}
}

func TestTLSRoutesACMEFailureKeepsCertificate(t *testing.T) {
	other, _ := tlsmgr.CreateLocalCA(t.TempDir(), "le")
	s, cs := newTLSAPI(t, fakeACME{ca: other, err: context.DeadlineExceeded})
	before := cs.Info().Fingerprint
	_ = s.do("PUT", "/api/v1/system/tls/acme", `{"provider":"desec","domains":["nas.dedyn.io"],"agreedTerms":true,"credentials":{"token":"t"}}`)
	iw := s.do("POST", "/api/v1/system/tls/acme/issue", "")
	var r struct{ JobID string }
	_ = json.Unmarshal(iw.Body.Bytes(), &r)
	for i := 0; i < 300; i++ {
		if j, _ := s.jobs.Get(context.Background(), r.JobID); j.Status == jobs.StatusFailed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if j, _ := s.jobs.Get(context.Background(), r.JobID); j.Status != jobs.StatusFailed {
		t.Fatalf("Job sollte fehlschlagen: %s", j.Status)
	}
	if cs.Info().Fingerprint != before {
		t.Fatal("bei Fehlern bleibt das alte Zertifikat aktiv")
	}
	var st tlsmgr.Status
	_ = json.Unmarshal(s.do("GET", "/api/v1/system/tls", "").Body.Bytes(), &st)
	if st.ACME.LastError == "" {
		t.Fatal("Fehler wird nicht angezeigt")
	}
}
