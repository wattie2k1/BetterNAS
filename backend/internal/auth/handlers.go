package auth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"os/user"
	"strings"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
)

const (
	// CookieName ist der Name des Sitzungs-Cookies.
	CookieName = "bnas_session"
	// AdminGroup ist die Gruppe, deren Mitglieder die GUI bedienen dürfen.
	AdminGroup = "betternas-admin"
)

// Verifier prüft Benutzername und Passwort.
type Verifier interface {
	Verify(ctx context.Context, username, password string) (bool, error)
}

// GroupChecker meldet, ob ein Benutzer Mitglied einer Gruppe ist.
type GroupChecker func(username, group string) (bool, error)

// SystemGroupChecker prüft über /etc/passwd und /etc/group (os/user ohne cgo).
func SystemGroupChecker(username, group string) (bool, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return false, nil
	}
	g, err := user.LookupGroup(group)
	if err != nil {
		return false, nil
	}
	ids, err := u.GroupIds()
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if id == g.Gid {
			return true, nil
		}
	}
	return false, nil
}

// Caller ist der Teil des Agent-Clients, den die Anmeldung braucht.
type Caller interface {
	Call(ctx context.Context, method string, params, result any) error
}

// AgentVerifier prüft Passwörter über die Agent-Methode auth.verify.
type AgentVerifier struct{ Agent Caller }

func (v AgentVerifier) Verify(ctx context.Context, username, password string) (bool, error) {
	var res struct {
		OK bool `json:"ok"`
	}
	err := v.Agent.Call(ctx, "auth.verify", map[string]string{"username": username, "password": password}, &res)
	return res.OK, err
}

// Handlers stellt Login, Logout, Me und die Sitzungs-Middleware bereit.
type Handlers struct {
	db       *sql.DB
	store    *Store
	verifier Verifier
	inGroup  GroupChecker
	limiter  *RateLimiter
	secure   bool
	totp     *TOTPStore
	policy   policyHolder
	mfa      *mfaTokens
	now      func() time.Time
}

// NewHandlers erzeugt die Handler. secure setzt das Secure-Flag am Cookie (aus nur im Dev-Modus).
func NewHandlers(d *sql.DB, store *Store, v Verifier, g GroupChecker, secure bool) *Handlers {
	h := &Handlers{db: d, store: store, verifier: v, inGroup: g, limiter: NewRateLimiter(5, 5*time.Minute), secure: secure, now: time.Now}
	h.mfa = newMFATokens(func() time.Time { return h.now() })
	return h
}

type ctxKey struct{}

// SessionFrom liefert die Sitzung aus dem Request-Kontext.
func SessionFrom(ctx context.Context) Session {
	s, _ := ctx.Value(ctxKey{}).(Session)
	return s
}

// WithSession hängt eine Sitzung an einen Kontext (für Tests anderer Pakete).
func WithSession(ctx context.Context, s Session) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

var errBadLogin = apierr.New(apierr.Unauthorized, "Benutzername oder Passwort falsch")

// Login prüft die Anmeldedaten und setzt das Sitzungs-Cookie.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	ip := ClientIP(r)
	if h.limiter.Blocked(ip) {
		apierr.Write(w, apierr.New(apierr.RateLimited, "Zu viele Fehlversuche. Bitte in einigen Minuten erneut versuchen."))
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
		return
	}
	ok, err := h.verifier.Verify(r.Context(), req.Username, req.Password)
	if err != nil {
		slog.Error("login: passwortprüfung fehlgeschlagen", "err", err)
		apierr.Write(w, err)
		return
	}
	if ok {
		ok, err = h.inGroup(req.Username, AdminGroup)
		if err != nil {
			apierr.Write(w, err)
			return
		}
	}
	if !ok {
		h.limiter.Fail(ip)
		_ = audit.Record(r.Context(), h.db, audit.Entry{Actor: req.Username, Action: "auth.login", Target: ip, Result: "error"})
		apierr.Write(w, errBadLogin)
		return
	}
	if h.totp != nil {
		on, err := h.totp.Enabled(r.Context(), req.Username)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		if on {
			_ = audit.Record(r.Context(), h.db, audit.Entry{Actor: req.Username, Action: "auth.login", Target: ip, Result: "mfa"})
			writeJSON(w, http.StatusOK, map[string]any{"mfaRequired": true, "mfaToken": h.mfa.New(req.Username)})
			return
		}
	}
	h.startSession(w, r, req.Username, "auth.login")
}

// startSession legt die Sitzung an, setzt das Cookie und protokolliert die Anmeldung.
func (h *Handlers) startSession(w http.ResponseWriter, r *http.Request, username, action string) {
	token, csrf, err := h.store.CreateFrom(r.Context(), username, ClientIP(r))
	if err != nil {
		apierr.Write(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: token, Path: "/", HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteStrictMode})
	_ = audit.Record(r.Context(), h.db, audit.Entry{Actor: username, Action: action, Target: ClientIP(r), Result: "ok"})
	writeJSON(w, http.StatusOK, map[string]string{"username": username, "csrfToken": csrf})
}

// Logout beendet die Sitzung.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		_ = h.store.Delete(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

// Me liefert den angemeldeten Benutzer und das CSRF-Token.
func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	s := SessionFrom(r.Context())
	totp := false
	if h.totp != nil {
		totp, _ = h.totp.Enabled(r.Context(), s.Username)
	}
	required := h.Policy().RequireTOTP && h.totp != nil && !totp
	writeJSON(w, http.StatusOK, map[string]any{"username": s.Username, "csrfToken": s.CSRF, "totp": totp, "totpRequired": required})
}

// RequireSession verlangt eine gültige Sitzung und bei ändernden Anfragen ein passendes CSRF-Token.
func (h *Handlers) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		if err != nil {
			apierr.Write(w, apierr.New(apierr.Unauthorized, "Bitte anmelden"))
			return
		}
		s, err := h.store.Lookup(r.Context(), c.Value)
		if err != nil {
			apierr.Write(w, apierr.New(apierr.Unauthorized, "Sitzung abgelaufen, bitte erneut anmelden"))
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.CSRF)) != 1 {
				apierr.Write(w, apierr.New(apierr.Forbidden, "Ungültiges CSRF-Token"))
				return
			}
		}
		// 2FA-Pflicht: bis zur Einrichtung bleiben nur Anmelde-/Profilrouten erreichbar.
		if h.Policy().RequireTOTP && h.totp != nil && !strings.HasPrefix(r.URL.Path, "/api/v1/auth/") {
			if on, err := h.totp.Enabled(r.Context(), s.Username); err == nil && !on {
				apierr.Write(w, apierr.New(apierr.Forbidden, "Die Zwei-Faktor-Anmeldung ist Pflicht. Bitte zuerst im Profil einrichten.").With("reason", "mfa_required"))
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(WithSession(r.Context(), s)))
	})
}

func (h *Handlers) sweep(ctx context.Context) {
	_, _ = h.store.Purge(ctx)
	h.limiter.Sweep()
}

// Maintain räumt abgelaufene Sitzungen und alte Fehlversuche regelmäßig auf, bis ctx endet.
func (h *Handlers) Maintain(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.sweep(ctx)
		}
	}
}
