package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/notify"
)

const cfgBody = `{"mail":{"enabled":false,"port":587,"security":"starttls","password":"geheim","to":[]},` +
	`"ntfy":{"enabled":true,"server":"%s","topic":"nas","token":"tok-xyz"},"tempWarnC":55}`

func putConfig(s *storageAPI, server string) *httptest.ResponseRecorder {
	return s.do("PUT", "/api/v1/notifications/config", strings.Replace(cfgBody, "%s", server, 1))
}

func TestNotificationsConfigNeverLeaksSecrets(t *testing.T) {
	s := newStorageAPI(t)
	w := putConfig(s, "http://127.0.0.1:9")
	if w.Code != 200 {
		t.Fatalf("put %d %s", w.Code, w.Body)
	}
	for _, body := range []string{w.Body.String(), s.do("GET", "/api/v1/notifications/config", "").Body.String()} {
		if strings.Contains(body, "geheim") || strings.Contains(body, "tok-xyz") {
			t.Fatalf("Geheimnis in Antwort: %s", body)
		}
		if !strings.Contains(body, `"passwordSet":true`) || !strings.Contains(body, `"tokenSet":true`) {
			t.Fatalf("passwordSet/tokenSet fehlt: %s", body)
		}
	}
	// Audit
	list, _ := audit.List(context.Background(), s.db, 20)
	found := false
	for _, a := range list {
		if a.Action == "notifications.config" && a.Actor == "anna" {
			found = true
		}
	}
	if !found {
		t.Fatal("kein Audit-Eintrag")
	}
}

func TestNotificationsPutKeepsSecretWhenEmpty(t *testing.T) {
	s := newStorageAPI(t)
	putConfig(s, "http://127.0.0.1:9")
	body := `{"mail":{"enabled":false,"port":587,"security":"starttls","password":"","to":[]},` +
		`"ntfy":{"enabled":true,"server":"http://127.0.0.1:9","topic":"nas","token":""},"tempWarnC":60}`
	if w := s.do("PUT", "/api/v1/notifications/config", body); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	c, err := notify.LoadConfig(context.Background(), s.db)
	if err != nil || c.Mail.Password != "geheim" || c.Ntfy.Token != "tok-xyz" || c.TempWarnC != 60 {
		t.Fatalf("%+v %v", c, err)
	}
	if w := s.do("PUT", "/api/v1/notifications/config", `{"tempWarnC":5}`); w.Code != 400 {
		t.Fatalf("ungültig: %d", w.Code)
	}
}

func TestNotificationsTestRoute(t *testing.T) {
	got := make(chan string, 2)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- string(b)
	}))
	defer ts.Close()
	s := newStorageAPI(t)
	putConfig(s, ts.URL)
	if w := s.do("POST", "/api/v1/notifications/test", `{"channel":"ntfy"}`); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	<-got
	if w := s.do("POST", "/api/v1/notifications/test", `{"channel":"mail"}`); w.Code != 409 || !strings.Contains(w.Body.String(), "nicht aktiviert") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/notifications/test", `{"channel":"sms"}`); w.Code != 400 {
		t.Fatalf("%d", w.Code)
	}
}

func TestHistoryAndAlertsRoutes(t *testing.T) {
	s := newStorageAPI(t)
	ns := notify.NewService(s.db, nil)
	for _, title := range []string{"eins", "zwei"} {
		ns.Send(context.Background(), notify.Message{Level: "info", Title: title, Body: "b"})
	}
	w := s.do("GET", "/api/v1/notifications/history?limit=1", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "zwei") || strings.Contains(w.Body.String(), "eins") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/notifications/history?limit=9999", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "eins") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/alerts", ""); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("leer erwartet: %d %s", w.Code, w.Body)
	}
	_, _ = s.db.Exec(`INSERT INTO alerts(key, level, title, body, since) VALUES ('smart:d1','crit','SMART-Fehler','b','2026-11-17T10:00:00Z')`)
	if w := s.do("GET", "/api/v1/alerts", ""); !strings.Contains(w.Body.String(), `"key":"smart:d1"`) {
		t.Fatalf("%s", w.Body)
	}
}

func TestNotificationRoutesNeedSession(t *testing.T) {
	s := newStorageAPI(t)
	r := httptest.NewRequest("GET", "/api/v1/alerts", nil)
	r.RemoteAddr = "192.168.1.10:1"
	w := httptest.NewRecorder()
	s.h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("%d", w.Code)
	}
}

func TestSpindownRoute(t *testing.T) {
	s := newStorageAPI(t)
	s.formatAs(t, "d1", "S1", "data")
	if w := s.do("PUT", "/api/v1/disks/d1/spindown", `{"minutes":60}`); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/disks", ""); !strings.Contains(w.Body.String(), `"spindown":60`) {
		t.Fatalf("%s", w.Body)
	}
	if w := s.do("PUT", "/api/v1/disks/d1/spindown", `{"minutes":7}`); w.Code != 400 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("PUT", "/api/v1/disks/d2/spindown", `{"minutes":60}`); w.Code != 409 {
		t.Fatalf("unzugeordnet: %d %s", w.Code, w.Body)
	}
	list, _ := audit.List(context.Background(), s.db, 50)
	found := false
	for _, a := range list {
		if a.Action == "disk.spindown" && a.Target == "d1" && a.Result == "ok" {
			found = true
		}
	}
	if !found {
		t.Fatal("kein Audit-Eintrag")
	}
}

func TestSelfTestAndHistoryRoutes(t *testing.T) {
	s := newStorageAPI(t)
	if w := s.do("POST", "/api/v1/disks/d1/selftest", `{"kind":"short"}`); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/disks/d1/selftest", `{"kind":"x"}`); w.Code != 400 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/disks/nope/selftest", `{"kind":"long"}`); w.Code != 404 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("DELETE", "/api/v1/disks/d1/selftest", ""); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/disks/selftest/schedule", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"short":"off"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("PUT", "/api/v1/disks/selftest/schedule", `{"short":"weekly","long":"monthly","hour":3}`); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/disks/selftest/schedule", ""); !strings.Contains(w.Body.String(), `"long":"monthly"`) || !strings.Contains(w.Body.String(), `"hour":3`) {
		t.Fatalf("%s", w.Body)
	}
	if w := s.do("PUT", "/api/v1/disks/selftest/schedule", `{"short":"hourly","long":"off","hour":3}`); w.Code != 400 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/disks/d1/history?days=7", ""); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/disks/d1/history?days=abc", ""); w.Code != 400 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/disks/d1/history?days=0", ""); w.Code != 200 {
		t.Fatalf("Standardzeitraum: %d %s", w.Code, w.Body)
	}
}

func TestIdentifyRoutes(t *testing.T) {
	s := newStorageAPI(t)
	if w := s.do("POST", "/api/v1/disks/d1/identify", ""); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/disks", ""); !strings.Contains(w.Body.String(), `"identifying":true`) {
		t.Fatalf("%s", w.Body)
	}
	if w := s.do("DELETE", "/api/v1/disks/d1/identify", ""); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/disks/nope/identify", ""); w.Code != 404 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestNoteRoute(t *testing.T) {
	s := newStorageAPI(t)
	if w := s.do("PUT", "/api/v1/disks/d1/note", `{"name":"Schacht 1","notes":"","purchased":"2024-01-02","warrantyUntil":""}`); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/disks", ""); !strings.Contains(w.Body.String(), `"note":{"name":"Schacht 1"`) {
		t.Fatalf("%s", w.Body)
	}
	if w := s.do("PUT", "/api/v1/disks/d1/note", `{"purchased":"gestern"}`); w.Code != 400 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("PUT", "/api/v1/disks/nope/note", `{"name":"x"}`); w.Code != 404 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestPowerAndTuningRoutes(t *testing.T) {
	s := newStorageAPI(t)
	s.formatAs(t, "d1", "S1", "data")
	if w := s.do("GET", "/api/v1/disks/d1/power?days=7", ""); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/disks/d1/power?days=x", ""); w.Code != 400 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("PUT", "/api/v1/disks/d1/tuning", `{"apm":128,"writeCache":"on"}`); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/disks/d1/tuning", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"saved":{"apm":128,"writeCache":"on"}`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("PUT", "/api/v1/disks/d1/tuning", `{"apm":999}`); w.Code != 400 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("PUT", "/api/v1/disks/d2/tuning", `{"apm":128}`); w.Code != 409 {
		t.Fatalf("unzugeordnet: %d %s", w.Code, w.Body)
	}
}

func TestDismissRoute(t *testing.T) {
	s := newStorageAPI(t)
	if w := s.do("POST", "/api/v1/disks/d1/dismiss", ""); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/disks/nope/dismiss", ""); w.Code != 404 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestFsCheckRoute(t *testing.T) {
	s := newStorageAPI(t)
	if w := s.do("POST", "/api/v1/disks/d1/fscheck", ""); w.Code != 202 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/disks/nope/fscheck", ""); w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}
}

func TestBenchRoute(t *testing.T) {
	s := newStorageAPI(t)
	if w := s.do("POST", "/api/v1/disks/d1/bench", ""); w.Code != 202 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/disks/nope/bench", ""); w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}
}

func TestDiskIORoute(t *testing.T) {
	s := newStorageAPI(t)
	w := s.do("GET", "/api/v1/disks/io", "")
	if w.Code != 200 && w.Code != 500 { // 500 nur ohne /proc/diskstats (nicht Linux)
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w.Code == 200 && !strings.Contains(w.Body.String(), `"devices"`) {
		t.Fatalf("%s", w.Body)
	}
}

func TestSpareRoute(t *testing.T) {
	s := newStorageAPI(t)
	if w := s.do("PUT", "/api/v1/disks/d1/spare", `{"spare":true}`); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/disks", ""); !strings.Contains(w.Body.String(), `"spare":true`) {
		t.Fatalf("%s", w.Body)
	}
	if w := s.do("PUT", "/api/v1/disks/nope/spare", `{"spare":true}`); w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}
}

func TestEraseRoutesGuard(t *testing.T) {
	s := newStorageAPI(t)
	if w := s.do("POST", "/api/v1/disks/nope/erase/preview", ""); w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}
	if w := s.do("POST", "/api/v1/disks/d1/erase", `{"method":"zero","confirmSerial":"x","confirmToken":"kaputt"}`); w.Code != 409 {
		t.Fatalf("ohne gültige Bestätigung muss abgelehnt werden: %d %s", w.Code, w.Body)
	}
	if w := s.do("DELETE", "/api/v1/disks/d1/erase", ""); w.Code != 409 {
		t.Fatalf("%d", w.Code)
	}
}
