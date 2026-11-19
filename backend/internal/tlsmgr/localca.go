// Package tlsmgr verwaltet die Zertifikatsquellen der Weboberfläche: selbstsigniert, eigene
// lokale Zertifizierungsstelle (CA) und Let's Encrypt (DNS-01).
package tlsmgr

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	caValidity   = 10 * 365 * 24 * time.Hour
	leafValidity = 365 * 24 * time.Hour
	// renewBefore: so früh vor Ablauf wird ein Zertifikat erneuert.
	renewBefore = 30 * 24 * time.Hour
)

// LocalCA ist die lokale Zertifizierungsstelle. Der Schlüssel verlässt das NAS nie.
type LocalCA struct {
	Cert    *x509.Certificate
	CertPEM []byte
	key     *ecdsa.PrivateKey
}

func caFiles(dir string) (certFile, keyFile string) {
	return filepath.Join(dir, "ca", "ca.pem"), filepath.Join(dir, "ca", "ca-key.pem")
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadLocalCA lädt die CA; fehlt sie, ist der Fehler os.ErrNotExist.
func LoadLocalCA(dir string) (*LocalCA, error) {
	cf, kf := caFiles(dir)
	certPEM, err := os.ReadFile(cf)
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(kf)
	if err != nil {
		return nil, err
	}
	cb, _ := pem.Decode(certPEM)
	kb, _ := pem.Decode(keyPEM)
	if cb == nil || kb == nil {
		return nil, errors.New("CA-Dateien sind nicht lesbar")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, err
	}
	key, err := x509.ParseECPrivateKey(kb.Bytes)
	if err != nil {
		return nil, err
	}
	return &LocalCA{Cert: cert, CertPEM: certPEM, key: key}, nil
}

// CreateLocalCA erzeugt eine neue CA und speichert sie (ersetzt eine vorhandene).
func CreateLocalCA(dir, hostname string) (*LocalCA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := newSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "BetterNAS Lokale CA (" + hostname + ")", Organization: []string{"BetterNAS"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cf, kf := caFiles(dir)
	if err := os.MkdirAll(filepath.Dir(cf), 0o750); err != nil {
		return nil, err
	}
	// Erst der Schlüssel (0600), dann das Zertifikat – so gibt es nie ein Zertifikat ohne Schlüssel.
	if err := writeAtomic(kf, keyPEM, 0o600); err != nil {
		return nil, err
	}
	if err := writeAtomic(cf, certPEM, 0o644); err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &LocalCA{Cert: cert, CertPEM: certPEM, key: key}, nil
}

func newSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
}

// Issue stellt ein Server-Zertifikat (ECDSA P-256, höchstens 1 Jahr) für Namen und IPs aus.
func (ca *LocalCA) Issue(dnsNames []string, ips []net.IP) (certPEM, keyPEM []byte, err error) {
	if len(dnsNames) == 0 && len(ips) == 0 {
		return nil, nil, errors.New("keine Namen für das Zertifikat")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := newSerial()
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	notAfter := now.Add(leafValidity)
	if notAfter.After(ca.Cert.NotAfter) {
		notAfter = ca.Cert.NotAfter
	}
	cn := "BetterNAS"
	if len(dnsNames) > 0 {
		cn = dnsNames[0]
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn, Organization: []string{"BetterNAS"}},
		DNSNames:              dnsNames,
		IPAddresses:           ips,
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), nil
}

// Fingerprint ist der SHA-256-Fingerabdruck als Hex mit Doppelpunkten.
func Fingerprint(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	h := strings.ToUpper(hex.EncodeToString(sum[:]))
	parts := make([]string, 0, 32)
	for i := 0; i < len(h); i += 2 {
		parts = append(parts, h[i:i+2])
	}
	return strings.Join(parts, ":")
}

// needsLocalReissue sagt, ob das aktuelle Zertifikat neu ausgestellt werden muss:
// nicht von dieser CA, bald abgelaufen oder es fehlt ein gewünschter Name/eine IP.
func needsLocalReissue(leaf *x509.Certificate, ca *LocalCA, dnsNames []string, ips []net.IP, now time.Time) (bool, string) {
	if leaf == nil {
		return true, "kein Zertifikat"
	}
	if leaf.CheckSignatureFrom(ca.Cert) != nil {
		return true, "nicht von der lokalen CA ausgestellt"
	}
	if leaf.NotAfter.Sub(now) < renewBefore && leaf.NotAfter.Before(ca.Cert.NotAfter) {
		return true, "läuft bald ab"
	}
	for _, n := range dnsNames {
		if leaf.VerifyHostname(n) != nil {
			return true, fmt.Sprintf("Name %s fehlt", n)
		}
	}
	for _, ip := range ips {
		if leaf.VerifyHostname(ip.String()) != nil {
			return true, fmt.Sprintf("Adresse %s fehlt", ip)
		}
	}
	return false, ""
}
