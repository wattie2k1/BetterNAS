package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// generateSelfSigned erzeugt ein selbstsigniertes Zertifikat (ECDSA P-256, 10 Jahre) als PEM.
func generateSelfSigned(hostname string) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: hostname, Organization: []string{"BetterNAS"}},
		DNSNames:              []string{hostname, hostname + ".local", "localhost"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// EnsureSelfSignedCert legt bei Bedarf ein selbstsigniertes Zertifikat an. Vorhandene Dateien bleiben unverändert.
func EnsureSelfSignedCert(dir, hostname string) (certFile, keyFile string, err error) {
	certFile, keyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if _, e1 := os.Stat(certFile); e1 == nil {
		if _, e2 := os.Stat(keyFile); e2 == nil {
			return certFile, keyFile, nil
		}
	}
	c, k, err := generateSelfSigned(hostname)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", "", err
	}
	if err := writeAtomic(keyFile, k, 0o600); err != nil {
		return "", "", err
	}
	if err := writeAtomic(certFile, c, 0o644); err != nil {
		return "", "", err
	}
	return certFile, keyFile, nil
}

// CertInfo beschreibt das aktive Zertifikat.
type CertInfo struct {
	Subject     string    `json:"subject"`
	Issuer      string    `json:"issuer"`
	DNSNames    []string  `json:"dnsNames"`
	IPAddresses []string  `json:"ipAddresses"`
	NotBefore   time.Time `json:"notBefore"`
	NotAfter    time.Time `json:"notAfter"`
	Fingerprint string    `json:"fingerprint"` // SHA-256, hex mit Doppelpunkten
	SelfSigned  bool      `json:"selfSigned"`
	Expired     bool      `json:"expired"`
}

// CertStore hält das aktive Zertifikat und lädt es ohne Neustart nach.
type CertStore struct {
	dir string
	mu  sync.RWMutex
	cur *tls.Certificate
}

// NewCertStore legt bei Bedarf ein selbstsigniertes Zertifikat an und lädt es.
func NewCertStore(dir, hostname string) (*CertStore, error) {
	cf, kf, err := EnsureSelfSignedCert(dir, hostname)
	if err != nil {
		return nil, err
	}
	s := &CertStore{dir: dir}
	return s, s.load(cf, kf)
}

func (s *CertStore) load(certFile, keyFile string) error {
	c, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return err
	}
	if c.Leaf == nil {
		if c.Leaf, err = x509.ParseCertificate(c.Certificate[0]); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.cur = &c
	s.mu.Unlock()
	return nil
}

// GetCertificate ist der tls.Config-Callback.
func (s *CertStore) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur, nil
}

// Info beschreibt das aktive Zertifikat.
func (s *CertStore) Info() CertInfo {
	s.mu.RLock()
	l := s.cur.Leaf
	s.mu.RUnlock()
	sum := sha256.Sum256(l.Raw)
	hexs := strings.ToUpper(hex.EncodeToString(sum[:]))
	parts := make([]string, 0, 32)
	for i := 0; i < len(hexs); i += 2 {
		parts = append(parts, hexs[i:i+2])
	}
	ips := []string{}
	for _, ip := range l.IPAddresses {
		ips = append(ips, ip.String())
	}
	names := l.DNSNames
	if names == nil {
		names = []string{}
	}
	return CertInfo{
		Subject: l.Subject.String(), Issuer: l.Issuer.String(), DNSNames: names, IPAddresses: ips,
		NotBefore: l.NotBefore, NotAfter: l.NotAfter, Fingerprint: strings.Join(parts, ":"),
		SelfSigned: l.Subject.String() == l.Issuer.String(), Expired: time.Now().After(l.NotAfter),
	}
}

// Install prüft ein hochgeladenes Zertifikat samt Schlüssel (PEM), speichert es und aktiviert es.
func (s *CertStore) Install(certPEM, keyPEM []byte) error {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("Zertifikat und Schlüssel passen nicht zusammen oder sind nicht lesbar (der Schlüssel darf nicht passwortgeschützt sein): %w", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return err
	}
	if time.Now().After(leaf.NotAfter) {
		return fmt.Errorf("Das Zertifikat ist abgelaufen")
	}
	if time.Now().Before(leaf.NotBefore.Add(-24 * time.Hour)) {
		return fmt.Errorf("Das Zertifikat ist noch nicht gültig")
	}
	return s.replace(certPEM, keyPEM)
}

// Regenerate ersetzt das Zertifikat durch ein neues selbstsigniertes für den Hostnamen.
func (s *CertStore) Regenerate(hostname string) error {
	c, k, err := generateSelfSigned(hostname)
	if err != nil {
		return err
	}
	return s.replace(c, k)
}

func (s *CertStore) replace(certPEM, keyPEM []byte) error {
	cf, kf := filepath.Join(s.dir, "cert.pem"), filepath.Join(s.dir, "key.pem")
	// Vorher sichern, damit ein Fehler beim Laden den alten Stand nicht zerstört.
	oldC, _ := os.ReadFile(cf)
	oldK, _ := os.ReadFile(kf)
	if err := writeAtomic(kf, keyPEM, 0o600); err != nil {
		return err
	}
	if err := writeAtomic(cf, certPEM, 0o644); err != nil {
		return err
	}
	if err := s.load(cf, kf); err != nil {
		_ = writeAtomic(kf, oldK, 0o600)
		_ = writeAtomic(cf, oldC, 0o644)
		return err
	}
	return nil
}
