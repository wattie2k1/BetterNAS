package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/db"
)

type mfaEnv struct {
	*env
	totp   *TOTPStore
	clock  time.Time
	secret string
	codes  []string
}

// newMFAEnv: alice hat 2FA aktiv, bob (kein Admin) nicht; die Uhr steht fest.
func newMFAEnv(t *testing.T) *mfaEnv {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	m := &mfaEnv{clock: time.Unix(1_800_000_000, 0), totp: NewTOTPStore(d)}
	h := NewHandlers(d, NewStore(d, time.Hour), fakeVerifier{"alice": "pw-alice", "carol": "pw-carol"}, admins("alice", "carol"), false).WithTOTP(m.totp)
	h.now = func() time.Time { return m.clock }
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/login", h.Login)
	mux.HandleFunc("POST /api/v1/auth/login/totp", h.LoginTOTP)
	mux.Handle("GET /api/v1/auth/me", h.RequireSession(http.HandlerFunc(h.Me)))
	m.env = &env{db: d, h: h, mx: mux}
	m.secret, _ = NewSecret()
	ctx := context.Background()
	if err := m.totp.BeginSetup(ctx, "alice", m.secret); err != nil {
		t.Fatal(err)
	}
	m.codes, _ = NewRecoveryCodes(3)
	var hashes []string
	for _, c := range m.codes {
		hashes = append(hashes, HashRecoveryCode(c))
	}
	if err := m.totp.Enable(ctx, "alice", TOTPStep(m.clock)-5, hashes); err != nil {
		t.Fatal(err)
	}
	return m
}

func (m *mfaEnv) code(t *testing.T, offset int64) string {
	t.Helper()
	c, err := TOTPCode(m.secret, TOTPStep(m.clock)+offset)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// step1 meldet alice mit Passwort an und liefert den mfaToken.
func (m *mfaEnv) step1(t *testing.T) string {
	t.Helper()
	w := m.do("POST", "/api/v1/auth/login", `{"username":"alice","password":"pw-alice"}`, "192.168.1.10", nil, "")
	var body struct {
		MfaRequired bool   `json:"mfaRequired"`
		MfaToken    string `json:"mfaToken"`
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != 200 || !body.MfaRequired || body.MfaToken == "" {
		t.Fatalf("schritt 1: %d %s", w.Code, w.Body)
	}
	return body.MfaToken
}

func (m *mfaEnv) step2(token, code, ip string) *httptest.ResponseRecorder {
	return m.do("POST", "/api/v1/auth/login/totp", `{"mfaToken":"`+token+`","code":"`+code+`"}`, ip, nil, "")
}

func sessionCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == CookieName {
			return c
		}
	}
	return nil
}

func TestLoginWithoutTOTPUnchanged(t *testing.T) {
	m := newMFAEnv(t)
	w := m.do("POST", "/api/v1/auth/login", `{"username":"carol","password":"pw-carol"}`, "192.168.1.10", nil, "")
	if w.Code != 200 || sessionCookie(w) == nil || strings.Contains(w.Body.String(), "mfaRequired") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestLoginRequiresSecondStep(t *testing.T) {
	m := newMFAEnv(t)
	w := m.do("POST", "/api/v1/auth/login", `{"username":"alice","password":"pw-alice"}`, "192.168.1.10", nil, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"mfaRequired":true`) || sessionCookie(w) != nil || len(w.Result().Cookies()) != 0 {
		t.Fatalf("%d %s cookies=%v", w.Code, w.Body, w.Result().Cookies())
	}
	list, _ := audit.List(context.Background(), m.db, 5)
	if len(list) != 1 || list[0].Action != "auth.login" || list[0].Result != "mfa" {
		t.Fatalf("%+v", list)
	}
	// falsches Passwort verrät nichts über 2FA
	if w := m.do("POST", "/api/v1/auth/login", `{"username":"alice","password":"falsch"}`, "192.168.1.10", nil, ""); w.Code != 401 || strings.Contains(w.Body.String(), "mfa") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestLoginTOTPSuccess(t *testing.T) {
	m := newMFAEnv(t)
	w := m.step2(m.step1(t), m.code(t, 0), "192.168.1.10")
	c := sessionCookie(w)
	if w.Code != 200 || c == nil || !c.HttpOnly || !strings.Contains(w.Body.String(), `"alice"`) || !strings.Contains(w.Body.String(), "csrfToken") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if me := m.do("GET", "/api/v1/auth/me", "", "192.168.1.10", c, ""); me.Code != 200 {
		t.Fatalf("me %d", me.Code)
	}
	list, _ := audit.List(context.Background(), m.db, 5)
	if list[0].Action != "auth.login.totp" || list[0].Result != "ok" {
		t.Fatalf("%+v", list[0])
	}
}

func TestLoginTOTPWrongCode401AndCounts(t *testing.T) {
	m := newMFAEnv(t)
	tok := m.step1(t)
	w := m.step2(tok, "000000", "192.168.1.10")
	if w.Code != 401 || sessionCookie(w) != nil || !strings.Contains(w.Body.String(), "Code ungültig") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	list, _ := audit.List(context.Background(), m.db, 5)
	if list[0].Action != "auth.login.totp" || list[0].Result != "error" {
		t.Fatalf("%+v", list[0])
	}
	// derselbe Token funktioniert danach noch mit dem richtigen Code
	if w := m.step2(tok, m.code(t, 0), "192.168.1.10"); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestMfaTokenSingleUse(t *testing.T) {
	m := newMFAEnv(t)
	tok := m.step1(t)
	if w := m.step2(tok, m.code(t, 0), "192.168.1.10"); w.Code != 200 {
		t.Fatalf("%d", w.Code)
	}
	if w := m.step2(tok, m.code(t, 1), "192.168.1.10"); w.Code != 401 {
		t.Fatalf("zweiter Tausch: %d", w.Code)
	}
}

func TestMfaTokenLockedAfterFiveFailures(t *testing.T) {
	m := newMFAEnv(t)
	tok := m.step1(t)
	for i := 0; i < 5; i++ { // verschiedene IPs, damit nur die Token-Sperre greift
		if w := m.step2(tok, "000000", "192.168.1.3"+string(rune('0'+i))); w.Code != 401 {
			t.Fatalf("Versuch %d: %d", i, w.Code)
		}
	}
	w := m.step2(tok, m.code(t, 0), "192.168.1.40")
	if w.Code != 401 || sessionCookie(w) != nil {
		t.Fatalf("gesperrter Token akzeptiert: %d %s", w.Code, w.Body)
	}
}

func TestMfaTokenExpires(t *testing.T) {
	m := newMFAEnv(t)
	tok := m.step1(t)
	m.clock = m.clock.Add(6 * time.Minute)
	if w := m.step2(tok, m.code(t, 0), "192.168.1.10"); w.Code != 401 {
		t.Fatalf("%d", w.Code)
	}
}

func TestMfaTokenIsNotASession(t *testing.T) {
	m := newMFAEnv(t)
	tok := m.step1(t)
	c := &http.Cookie{Name: CookieName, Value: tok}
	if w := m.do("GET", "/api/v1/auth/me", "", "192.168.1.10", c, ""); w.Code != 401 {
		t.Fatalf("%d", w.Code)
	}
}

func TestReplayedCodeRejected(t *testing.T) {
	m := newMFAEnv(t)
	code := m.code(t, 0)
	if w := m.step2(m.step1(t), code, "192.168.1.10"); w.Code != 200 {
		t.Fatalf("%d", w.Code)
	}
	if w := m.step2(m.step1(t), code, "192.168.1.11"); w.Code != 401 {
		t.Fatalf("Wiederverwendung: %d", w.Code)
	}
}

func TestRecoveryCodeLogsIn(t *testing.T) {
	m := newMFAEnv(t)
	if w := m.step2(m.step1(t), m.codes[0], "192.168.1.10"); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := m.step2(m.step1(t), m.codes[0], "192.168.1.11"); w.Code != 401 {
		t.Fatalf("zweite Verwendung: %d", w.Code)
	}
}

func TestIPRateLimitAppliesToTOTP(t *testing.T) {
	m := newMFAEnv(t)
	a, b, c := m.step1(t), m.step1(t), m.step1(t)
	for i := 0; i < 3; i++ {
		m.step2(a, "000000", "192.168.1.10")
	}
	for i := 0; i < 2; i++ {
		m.step2(b, "000000", "192.168.1.10")
	}
	if w := m.step2(c, m.code(t, 0), "192.168.1.10"); w.Code != 429 {
		t.Fatalf("erwartet 429, bekam %d", w.Code)
	}
}

func TestLoginTOTPBadInput(t *testing.T) {
	m := newMFAEnv(t)
	if w := m.do("POST", "/api/v1/auth/login/totp", `kaputt`, "192.168.1.10", nil, ""); w.Code != 400 {
		t.Fatalf("%d", w.Code)
	}
	if w := m.step2("unbekannt", "123456", "192.168.1.10"); w.Code != 401 {
		t.Fatalf("%d", w.Code)
	}
}
