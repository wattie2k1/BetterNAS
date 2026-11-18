package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
)

const settingPolicy = "auth.policy"

// Policy sind die Anmelde-Einstellungen.
type Policy struct {
	SessionHours   int  `json:"sessionHours"`
	MaxFailures    int  `json:"maxFailures"`
	LockoutMinutes int  `json:"lockoutMinutes"`
	RequireTOTP    bool `json:"requireTotp"`
}

// DefaultPolicy entspricht dem bisherigen festen Verhalten.
func DefaultPolicy() Policy {
	return Policy{SessionHours: 12, MaxFailures: 5, LockoutMinutes: 5}
}

// Validate prüft die Wertebereiche.
func (p Policy) Validate() error {
	switch {
	case p.SessionHours < 1 || p.SessionHours > 720:
		return apierr.New(apierr.InvalidParams, "Die Sitzungsdauer muss zwischen 1 und 720 Stunden liegen")
	case p.MaxFailures < 3 || p.MaxFailures > 20:
		return apierr.New(apierr.InvalidParams, "Die Zahl der Fehlversuche muss zwischen 3 und 20 liegen")
	case p.LockoutMinutes < 1 || p.LockoutMinutes > 60:
		return apierr.New(apierr.InvalidParams, "Die Sperrzeit muss zwischen 1 und 60 Minuten liegen")
	}
	return nil
}

type policyHolder struct {
	mu sync.RWMutex
	p  Policy
	ok bool
}

// Policy liefert die gültigen Einstellungen (Standard, solange keine geladen/gespeichert wurden).
func (h *Handlers) Policy() Policy {
	h.policy.mu.RLock()
	defer h.policy.mu.RUnlock()
	if !h.policy.ok {
		return DefaultPolicy()
	}
	return h.policy.p
}

func (h *Handlers) applyPolicy(p Policy) {
	h.policy.mu.Lock()
	h.policy.p, h.policy.ok = p, true
	h.policy.mu.Unlock()
	h.store.SetTTL(time.Duration(p.SessionHours) * time.Hour)
	h.limiter.SetLimits(p.MaxFailures, time.Duration(p.LockoutMinutes)*time.Minute)
}

// LoadPolicy liest die gespeicherten Einstellungen (beim Start); ungültige oder fehlende werden durch den Standard ersetzt.
func (h *Handlers) LoadPolicy(ctx context.Context) error {
	var raw string
	err := h.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, settingPolicy).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		h.applyPolicy(DefaultPolicy())
		return nil
	}
	if err != nil {
		return err
	}
	p := DefaultPolicy()
	if json.Unmarshal([]byte(raw), &p) != nil || p.Validate() != nil {
		p = DefaultPolicy()
	}
	h.applyPolicy(p)
	return nil
}

// SetPolicy prüft, speichert und aktiviert die Einstellungen sofort.
func (h *Handlers) SetPolicy(ctx context.Context, actor string, p Policy) error {
	if err := p.Validate(); err != nil {
		return err
	}
	b, _ := json.Marshal(p)
	_, err := h.db.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, settingPolicy, string(b))
	res := "ok"
	if err != nil {
		res = "error"
	} else {
		h.applyPolicy(p)
	}
	_ = audit.Record(ctx, h.db, audit.Entry{Actor: actor, Action: "auth.policy.set", Target: "auth", Result: res})
	return err
}

func (h *Handlers) currentSessionID(r *http.Request) string {
	if c, err := r.Cookie(CookieName); err == nil {
		return SessionID(c.Value)
	}
	return ""
}

// RegisterSecurity hängt die Routen für Anmelde-Einstellungen und Sitzungen an mux (hinter RequireSession).
// Sie liegen bewusst nicht unter /api/v1/auth/, damit die 2FA-Pflicht sie sperrt.
func (h *Handlers) RegisterSecurity(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/security/policy", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, h.Policy())
	})
	mux.HandleFunc("PUT /api/v1/security/policy", func(w http.ResponseWriter, r *http.Request) {
		var p Policy
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&p); err != nil {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
			return
		}
		if err := h.SetPolicy(r.Context(), SessionFrom(r.Context()).Username, p); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/security/sessions", func(w http.ResponseWriter, r *http.Request) {
		list, err := h.store.List(r.Context(), h.currentSessionID(r))
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, list)
	})
	mux.HandleFunc("DELETE /api/v1/security/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == h.currentSessionID(r) {
			apierr.Write(w, apierr.New(apierr.Conflict, "Die eigene Sitzung beendest du über „Abmelden“."))
			return
		}
		ok, err := h.store.DeleteByID(r.Context(), id)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		if !ok {
			apierr.Write(w, apierr.New(apierr.NotFound, "Sitzung nicht gefunden"))
			return
		}
		_ = audit.Record(r.Context(), h.db, audit.Entry{Actor: SessionFrom(r.Context()).Username, Action: "auth.session.revoke", Target: id, Result: "ok"})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/security/sessions/revoke-others", func(w http.ResponseWriter, r *http.Request) {
		n, err := h.store.DeleteOthers(r.Context(), h.currentSessionID(r))
		if err != nil {
			apierr.Write(w, err)
			return
		}
		_ = audit.Record(r.Context(), h.db, audit.Entry{Actor: SessionFrom(r.Context()).Username, Action: "auth.session.revoke-others", Target: "all", Result: "ok"})
		writeJSON(w, http.StatusOK, map[string]int64{"revoked": n})
	})
}
