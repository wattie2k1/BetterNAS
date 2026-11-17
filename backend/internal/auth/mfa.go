package auth

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
)

const (
	mfaTTL         = 5 * time.Minute
	mfaMaxFailures = 5
)

type mfaEntry struct {
	user    string
	expires time.Time
	fails   int
}

// mfaTokens hält die kurzlebigen Zwischen-Token zwischen Passwort und Code (nur im Speicher).
type mfaTokens struct {
	mu  sync.Mutex
	now func() time.Time
	m   map[string]*mfaEntry
}

func newMFATokens(now func() time.Time) *mfaTokens {
	return &mfaTokens{now: now, m: map[string]*mfaEntry{}}
}

func (t *mfaTokens) pruneLocked() {
	for k, e := range t.m {
		if t.now().After(e.expires) {
			delete(t.m, k)
		}
	}
}

// New erzeugt einen Token für user.
func (t *mfaTokens) New(user string) string {
	tok, _ := randomToken()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pruneLocked()
	t.m[tok] = &mfaEntry{user: user, expires: t.now().Add(mfaTTL)}
	return tok
}

// Peek liefert den Benutzer eines gültigen Tokens.
func (t *mfaTokens) Peek(token string) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pruneLocked()
	e, ok := t.m[token]
	if !ok {
		return "", false
	}
	return e.user, true
}

// Fail zählt einen Fehlversuch und liefert die verbleibenden; bei 0 wird der Token gelöscht.
func (t *mfaTokens) Fail(token string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.m[token]
	if !ok {
		return 0
	}
	e.fails++
	if e.fails >= mfaMaxFailures {
		delete(t.m, token)
		return 0
	}
	return mfaMaxFailures - e.fails
}

// Consume verbraucht den Token.
func (t *mfaTokens) Consume(token string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, token)
}

// WithTOTP schaltet die zweistufige Anmeldung für Benutzer mit aktivem 2FA ein.
func (h *Handlers) WithTOTP(s *TOTPStore) *Handlers {
	h.totp = s
	return h
}

// LoginTOTP tauscht einen mfaToken und einen Code (TOTP oder Wiederherstellungscode) gegen eine Sitzung.
func (h *Handlers) LoginTOTP(w http.ResponseWriter, r *http.Request) {
	ip := ClientIP(r)
	if h.limiter.Blocked(ip) {
		apierr.Write(w, apierr.New(apierr.RateLimited, "Zu viele Fehlversuche. Bitte in einigen Minuten erneut versuchen."))
		return
	}
	var req struct {
		MfaToken string `json:"mfaToken"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
		return
	}
	user, ok := h.mfa.Peek(req.MfaToken)
	if !ok || h.totp == nil {
		apierr.Write(w, apierr.New(apierr.Unauthorized, "Die Anmeldung ist abgelaufen. Bitte erneut anmelden."))
		return
	}
	valid, err := h.totp.Verify(r.Context(), user, req.Code, h.now())
	if err != nil {
		apierr.Write(w, err)
		return
	}
	if !valid {
		remaining := h.mfa.Fail(req.MfaToken)
		h.limiter.Fail(ip)
		_ = audit.Record(r.Context(), h.db, audit.Entry{Actor: user, Action: "auth.login.totp", Target: ip, Result: "error"})
		if remaining == 0 {
			apierr.Write(w, apierr.New(apierr.Unauthorized, "Zu viele falsche Codes. Bitte erneut anmelden."))
			return
		}
		apierr.Write(w, apierr.New(apierr.Unauthorized, "Code ungültig"))
		return
	}
	h.mfa.Consume(req.MfaToken)
	h.startSession(w, r, user, "auth.login.totp")
}
