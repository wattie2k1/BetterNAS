package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// policyEnv: Routen für Sicherheit + Echo hinter RequireSession; alice ist Admin ohne 2FA.
func policyEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t, time.Hour)
	e.h.WithTOTP(NewTOTPStore(e.db))
	if err := e.h.LoadPolicy(context.Background()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/login", e.h.Login)
	mux.HandleFunc("GET /api/v1/auth/me", e.h.Me)
	sec := http.NewServeMux()
	e.h.RegisterSecurity(sec)
	sec.HandleFunc("GET /api/v1/auth/me", e.h.Me)
	sec.HandleFunc("GET /api/v1/echo", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.Handle("/api/", e.h.RequireSession(sec))
	e.mx = mux
	return e
}

func TestPolicyDefaultsAndValidation(t *testing.T) {
	e := policyEnv(t)
	c, csrf := e.login(t, "alice", "pw-alice")
	w := e.do("GET", "/api/v1/security/policy", "", "192.168.1.10", c, "")
	var p Policy
	json.Unmarshal(w.Body.Bytes(), &p)
	if p != DefaultPolicy() {
		t.Fatalf("%+v", p)
	}
	for _, bad := range []string{`{"sessionHours":0,"maxFailures":5,"lockoutMinutes":5}`, `{"sessionHours":12,"maxFailures":2,"lockoutMinutes":5}`, `{"sessionHours":12,"maxFailures":5,"lockoutMinutes":99}`} {
		if w := e.do("PUT", "/api/v1/security/policy", bad, "192.168.1.10", c, csrf); w.Code != 400 {
			t.Fatalf("%s → %d", bad, w.Code)
		}
	}
	if w := e.do("PUT", "/api/v1/security/policy", `{"sessionHours":24,"maxFailures":3,"lockoutMinutes":10,"requireTotp":false}`, "192.168.1.10", c, csrf); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	// Persistiert: neuer Handler lädt dieselben Werte.
	e.h.policy = policyHolder{}
	if err := e.h.LoadPolicy(context.Background()); err != nil || e.h.Policy().SessionHours != 24 {
		t.Fatalf("%v %+v", err, e.h.Policy())
	}
}

func TestPolicyLockoutAppliesImmediately(t *testing.T) {
	e := policyEnv(t)
	if err := e.h.SetPolicy(context.Background(), "alice", Policy{SessionHours: 12, MaxFailures: 3, LockoutMinutes: 5}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		e.do("POST", "/api/v1/auth/login", `{"username":"alice","password":"x"}`, "192.168.1.20", nil, "")
	}
	if w := e.do("POST", "/api/v1/auth/login", `{"username":"alice","password":"pw-alice"}`, "192.168.1.20", nil, ""); w.Code != 429 {
		t.Fatalf("nach 3 Fehlversuchen gesperrt erwartet, %d", w.Code)
	}
}

func TestSessionsListAndRevoke(t *testing.T) {
	e := policyEnv(t)
	c1, csrf := e.login(t, "alice", "pw-alice")
	c2, _ := e.login(t, "alice", "pw-alice")
	w := e.do("GET", "/api/v1/security/sessions", "", "192.168.1.10", c1, "")
	var list []SessionInfo
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 2 || list[0].IP != "192.168.1.10" {
		t.Fatalf("%+v", list)
	}
	cur, other := "", ""
	for _, s := range list {
		if s.Current {
			cur = s.ID
		} else {
			other = s.ID
		}
	}
	if cur != SessionID(c1.Value) || other != SessionID(c2.Value) {
		t.Fatalf("%+v", list)
	}
	if w := e.do("DELETE", "/api/v1/security/sessions/"+cur, "", "192.168.1.10", c1, csrf); w.Code != 409 {
		t.Fatalf("eigene Sitzung %d", w.Code)
	}
	if w := e.do("DELETE", "/api/v1/security/sessions/"+other, "", "192.168.1.10", c1, csrf); w.Code != 204 {
		t.Fatalf("%d", w.Code)
	}
	if w := e.do("GET", "/api/v1/echo", "", "192.168.1.10", c2, ""); w.Code != 401 {
		t.Fatalf("beendete Sitzung muss ungültig sein, %d", w.Code)
	}
	if w := e.do("DELETE", "/api/v1/security/sessions/"+other, "", "192.168.1.10", c1, csrf); w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}
	c3, _ := e.login(t, "alice", "pw-alice")
	w = e.do("POST", "/api/v1/security/sessions/revoke-others", "", "192.168.1.10", c1, csrf)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"revoked":1`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := e.do("GET", "/api/v1/echo", "", "192.168.1.10", c3, ""); w.Code != 401 {
		t.Fatal("c3 muss beendet sein")
	}
	if w := e.do("GET", "/api/v1/echo", "", "192.168.1.10", c1, ""); w.Code != 200 {
		t.Fatal("eigene Sitzung bleibt")
	}
}

func TestRequireTOTPBlocksUntilSetup(t *testing.T) {
	e := policyEnv(t)
	c, csrf := e.login(t, "alice", "pw-alice")
	if err := e.h.SetPolicy(context.Background(), "alice", Policy{SessionHours: 12, MaxFailures: 5, LockoutMinutes: 5, RequireTOTP: true}); err != nil {
		t.Fatal(err)
	}
	w := e.do("GET", "/api/v1/echo", "", "192.168.1.10", c, "")
	if w.Code != 403 || !strings.Contains(w.Body.String(), "mfa_required") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := e.do("PUT", "/api/v1/security/policy", `{"sessionHours":12,"maxFailures":5,"lockoutMinutes":5}`, "192.168.1.10", c, csrf); w.Code != 403 {
		t.Fatal("ohne 2FA darf die Pflicht nicht abschaltbar sein")
	}
	me := e.do("GET", "/api/v1/auth/me", "", "192.168.1.10", c, "")
	if me.Code != 200 || !strings.Contains(me.Body.String(), `"totpRequired":true`) {
		t.Fatalf("%d %s", me.Code, me.Body)
	}
	// Nach Einrichtung von 2FA ist alles wieder erreichbar.
	secret, _ := NewSecret()
	tot := e.h.totp
	if err := tot.BeginSetup(context.Background(), "alice", secret); err != nil {
		t.Fatal(err)
	}
	if err := tot.Enable(context.Background(), "alice", 1, []string{HashRecoveryCode("AAAA")}); err != nil {
		t.Fatal(err)
	}
	if w := e.do("GET", "/api/v1/echo", "", "192.168.1.10", c, ""); w.Code != 200 {
		t.Fatalf("%d", w.Code)
	}
}
