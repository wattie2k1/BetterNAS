package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/system"
)

func TestCertStoreRegenerateAndInstall(t *testing.T) {
	dir := t.TempDir()
	cs, err := NewCertStore(dir, "nas")
	if err != nil {
		t.Fatal(err)
	}
	first := cs.Info()
	if !first.SelfSigned || first.Expired || len(first.DNSNames) == 0 || len(strings.Split(first.Fingerprint, ":")) != 32 {
		t.Fatalf("%+v", first)
	}
	if err := cs.Regenerate("neuer-name"); err != nil {
		t.Fatal(err)
	}
	second := cs.Info()
	if second.Fingerprint == first.Fingerprint || second.DNSNames[0] != "neuer-name" {
		t.Fatalf("%+v", second)
	}
	// GetCertificate liefert ohne Neustart das neue Zertifikat.
	if c, _ := cs.GetCertificate(&tls.ClientHelloInfo{}); c == nil || c.Leaf.Subject.CommonName != "neuer-name" {
		t.Fatal("neues Zertifikat wird nicht ausgeliefert")
	}
	// Hochladen: passendes Paar ok, falscher Schlüssel und Müll abgelehnt, alter Stand bleibt.
	certPEM, keyPEM, _ := generateSelfSigned("hochgeladen")
	_, otherKey, _ := generateSelfSigned("anders")
	before := cs.Info().Fingerprint
	if err := cs.Install(certPEM, otherKey); err == nil {
		t.Fatal("falscher Schlüssel muss abgelehnt werden")
	}
	if err := cs.Install([]byte("müll"), []byte("müll")); err == nil {
		t.Fatal("Müll muss abgelehnt werden")
	}
	if cs.Info().Fingerprint != before {
		t.Fatal("bei Fehlern bleibt das alte Zertifikat aktiv")
	}
	if err := cs.Install(certPEM, keyPEM); err != nil || cs.Info().DNSNames[0] != "hochgeladen" {
		t.Fatal(err)
	}
	if st, _ := os.Stat(filepath.Join(dir, "key.pem")); st.Mode().Perm() != 0o600 {
		t.Fatal("Schlüssel nur für den Besitzer lesbar")
	}
	// Neustart des Dienstes lädt das gespeicherte Zertifikat.
	cs2, err := NewCertStore(dir, "nas")
	if err != nil || cs2.Info().Fingerprint != cs.Info().Fingerprint {
		t.Fatal(err)
	}
}

type sysAgentStub struct{ calls map[string]any }

func (a *sysAgentStub) Call(_ context.Context, method string, params, result any) error {
	if a.calls == nil {
		a.calls = map[string]any{}
	}
	b, _ := json.Marshal(params)
	var p any
	_ = json.Unmarshal(b, &p)
	a.calls[method] = p
	if method == "system.settings.get" {
		return json.Unmarshal([]byte(`{"hostname":"nas","timezone":"Europe/Berlin","ntp":true}`), result)
	}
	if method == "service.list" {
		return json.Unmarshal([]byte(`[]`), result)
	}
	if method == "system.timezones" {
		return json.Unmarshal([]byte(`["Europe/Berlin","UTC"]`), result)
	}
	return nil
}

func TestSystemSettingsAndWebAccessRoutes(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	store := auth.NewStore(d, time.Hour)
	token, csrf, _ := store.Create(context.Background(), "anna")
	stub := &sysAgentStub{}
	cs, err := NewCertStore(t.TempDir(), "nas")
	if err != nil {
		t.Fatal(err)
	}
	h := NewServer(Config{WebDir: t.TempDir(), Dev: true}, Deps{
		DB: d, Jobs: jobs.NewManager(d, hub), Hub: hub,
		Auth:           auth.NewHandlers(d, store, nil, nil, false),
		SystemSettings: system.NewSettings(d, stub), Certs: cs,
	})
	s := &storageAPI{h: h, cookie: &http.Cookie{Name: auth.CookieName, Value: token}, csrf: csrf}

	if w := s.do("GET", "/api/v1/system/settings", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"timezone":"Europe/Berlin"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/system/timezones", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "UTC") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	// Nur bekannte Felder werden weitergereicht.
	if w := s.do("PUT", "/api/v1/system/settings", `{"hostname":"x","evil":"y","ntp":false}`); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if p, _ := stub.calls["system.settings.set"].(map[string]any); p["hostname"] != "x" || p["ntp"] != false || p["evil"] != nil {
		t.Fatalf("%v", stub.calls)
	}
	// Energie nur mit richtigem Bestätigungswort.
	if w := s.do("POST", "/api/v1/system/power", `{"action":"reboot","confirm":"nein"}`); w.Code != 400 || stub.calls["system.power"] != nil {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/system/power", `{"action":"reboot","confirm":"NEUSTARTEN"}`); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if p, _ := stub.calls["system.power"].(map[string]any); p["action"] != "reboot" {
		t.Fatalf("%v", stub.calls)
	}
	if w := s.do("POST", "/api/v1/system/power", `{"action":"format","confirm":"x"}`); w.Code != 400 {
		t.Fatalf("%d", w.Code)
	}
	// Dienste und Einstellungen werden durchgereicht
	if w := s.do("POST", "/api/v1/services/docker/restart", ""); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if p, _ := stub.calls["service.action"].(map[string]any); p["unit"] != "docker" || p["action"] != "restart" {
		t.Fatalf("%v", stub.calls)
	}
	if w := s.do("PUT", "/api/v1/settings/smb", `{"workgroup":"HEIM","serverName":"x","minProtocol":"SMB3_11"}`); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if p, _ := stub.calls["smb.global.set"].(map[string]any); p["workgroup"] != "HEIM" {
		t.Fatalf("%v", stub.calls)
	}
	if w := s.do("PUT", "/api/v1/settings/docker", `{"logMaxSize":"10m","logMaxFile":3}`); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/services", ""); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	// Zertifikat
	if w := s.do("GET", "/api/v1/system/certificate", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"selfSigned":true`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/system/certificate/regenerate", ""); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("PUT", "/api/v1/system/certificate", `{"cert":"x","key":"y"}`); w.Code != 400 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
