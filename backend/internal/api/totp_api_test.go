package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/auth"
)

func currentCode(t *testing.T, secret string) string {
	t.Helper()
	c, err := auth.TOTPCode(secret, auth.TOTPStep(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func setupTOTP(t *testing.T, s *storageAPI) string {
	t.Helper()
	w := s.do("POST", "/api/v1/auth/totp/setup", "")
	if w.Code != 200 {
		t.Fatalf("setup %d %s", w.Code, w.Body)
	}
	var r struct{ Secret, URI string }
	json.Unmarshal(w.Body.Bytes(), &r)
	if r.Secret == "" || !strings.HasPrefix(r.URI, "otpauth://totp/") || !strings.Contains(r.URI, "anna") {
		t.Fatalf("%+v", r)
	}
	return r.Secret
}

func enableTOTP(t *testing.T, s *storageAPI) (secret string, codes []string) {
	t.Helper()
	secret = setupTOTP(t, s)
	w := s.do("POST", "/api/v1/auth/totp/enable", `{"code":"`+currentCode(t, secret)+`"}`)
	if w.Code != 200 {
		t.Fatalf("enable %d %s", w.Code, w.Body)
	}
	var r struct{ RecoveryCodes []string }
	json.Unmarshal(w.Body.Bytes(), &r)
	return secret, r.RecoveryCodes
}

func TestTotpSetupEnableFlow(t *testing.T) {
	s := newStorageAPI(t)
	if w := s.do("GET", "/api/v1/auth/totp", ""); !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatalf("%s", w.Body)
	}
	_, codes := enableTOTP(t, s)
	if len(codes) != 10 {
		t.Fatalf("%d Codes", len(codes))
	}
	if w := s.do("GET", "/api/v1/auth/totp", ""); !strings.Contains(w.Body.String(), `"enabled":true`) {
		t.Fatalf("%s", w.Body)
	}
	if w := s.do("POST", "/api/v1/auth/totp/setup", ""); w.Code != 409 {
		t.Fatalf("zweites setup bei aktivem 2FA: %d", w.Code)
	}
	list, _ := audit.List(context.Background(), s.db, 20)
	var acts []string
	for _, a := range list {
		acts = append(acts, a.Action+":"+a.Result)
	}
	for _, want := range []string{"auth.totp.setup:ok", "auth.totp.enable:ok"} {
		if !strings.Contains(strings.Join(acts, " "), want) {
			t.Errorf("Audit %s fehlt (%v)", want, acts)
		}
	}
}

func TestEnableWrongCodeStaysOff(t *testing.T) {
	s := newStorageAPI(t)
	setupTOTP(t, s)
	if w := s.do("POST", "/api/v1/auth/totp/enable", `{"code":"000000"}`); w.Code != 400 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/auth/totp", ""); !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatalf("%s", w.Body)
	}
}

func TestEnableWithoutSetupConflicts(t *testing.T) {
	s := newStorageAPI(t)
	if w := s.do("POST", "/api/v1/auth/totp/enable", `{"code":"123456"}`); w.Code != 409 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestDisableNeedsValidCode(t *testing.T) {
	s := newStorageAPI(t)
	secret, codes := enableTOTP(t, s)
	if w := s.do("POST", "/api/v1/auth/totp/disable", `{"code":"000000"}`); w.Code != 403 {
		t.Fatalf("falscher Code: %d", w.Code)
	}
	if w := s.do("GET", "/api/v1/auth/totp", ""); !strings.Contains(w.Body.String(), `"enabled":true`) {
		t.Fatal("trotz falschem Code abgeschaltet")
	}
	_ = secret
	if w := s.do("POST", "/api/v1/auth/totp/disable", `{"code":"`+codes[0]+`"}`); w.Code != 204 {
		t.Fatalf("Wiederherstellungscode: %d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/auth/totp", ""); !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatal("nicht abgeschaltet")
	}
}

func TestDisableIsRateLimited(t *testing.T) {
	s := newStorageAPI(t)
	_, codes := enableTOTP(t, s)
	for i := 0; i < 5; i++ {
		if w := s.do("POST", "/api/v1/auth/totp/disable", `{"code":"000000"}`); w.Code != 403 {
			t.Fatalf("Versuch %d: %d", i, w.Code)
		}
	}
	// Auch der richtige Code wird nach zu vielen Fehlversuchen nicht mehr angenommen.
	if w := s.do("POST", "/api/v1/auth/totp/disable", `{"code":"`+codes[0]+`"}`); w.Code != 429 {
		t.Fatalf("erwartet 429: %d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/auth/totp", ""); !strings.Contains(w.Body.String(), `"enabled":true`) {
		t.Fatal("trotz Sperre abgeschaltet")
	}
}

func TestAdminResetOtherUser(t *testing.T) {
	s := newStorageAPI(t)
	store := auth.NewTOTPStore(s.db)
	secret, _ := auth.NewSecret()
	store.BeginSetup(context.Background(), "ben", secret)
	store.Enable(context.Background(), "ben", 1, nil)
	if on, _ := store.Enabled(context.Background(), "ben"); !on {
		t.Fatal("Vorbereitung")
	}
	if w := s.do("DELETE", "/api/v1/users/ben/totp", ""); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if on, _ := store.Enabled(context.Background(), "ben"); on {
		t.Fatal("2FA von ben noch aktiv")
	}
	if w := s.do("DELETE", "/api/v1/users/anna/totp", ""); w.Code != 409 {
		t.Fatalf("eigener Name: %d", w.Code)
	}
	if w := s.do("DELETE", "/api/v1/users/unbekannt/totp", ""); w.Code != 204 {
		t.Fatalf("idempotent: %d", w.Code)
	}
	list, _ := audit.List(context.Background(), s.db, 20)
	found := false
	for _, a := range list {
		if a.Action == "auth.totp.reset" && a.Target == "ben" && a.Actor == "anna" {
			found = true
		}
	}
	if !found {
		t.Fatal("kein Audit-Eintrag")
	}
}

func TestMeShowsTotpFlag(t *testing.T) {
	s := newStorageAPI(t)
	if w := s.do("GET", "/api/v1/auth/me", ""); !strings.Contains(w.Body.String(), `"totp":false`) {
		t.Fatalf("%s", w.Body)
	}
	enableTOTP(t, s)
	if w := s.do("GET", "/api/v1/auth/me", ""); !strings.Contains(w.Body.String(), `"totp":true`) {
		t.Fatalf("%s", w.Body)
	}
}

func TestTotpRoutesNeedSession(t *testing.T) {
	s := newStorageAPI(t)
	for _, c := range [][2]string{{"GET", "/api/v1/auth/totp"}, {"POST", "/api/v1/auth/totp/setup"}, {"POST", "/api/v1/auth/totp/enable"}, {"POST", "/api/v1/auth/totp/disable"}, {"DELETE", "/api/v1/users/ben/totp"}} {
		r := httptest.NewRequest(c[0], c[1], strings.NewReader(`{}`))
		r.RemoteAddr = "192.168.1.10:1"
		w := httptest.NewRecorder()
		s.h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Errorf("%v ohne Sitzung: %d", c, w.Code)
		}
	}
}
