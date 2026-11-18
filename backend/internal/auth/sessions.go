// Package auth enthält Anmeldung, Sitzungen und Zugriffsschutz der Web-API.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sync/atomic"
	"time"
)

// Session ist eine angemeldete Sitzung.
type Session struct {
	Username string
	CSRF     string
}

// ErrNoSession bedeutet: Sitzung unbekannt oder abgelaufen.
var ErrNoSession = errors.New("keine gültige sitzung")

// Store speichert Sitzungen in SQLite; gespeichert wird nur sha256(token).
type Store struct {
	db  *sql.DB
	ttl atomic.Int64 // time.Duration
	now func() time.Time
}

// NewStore erzeugt einen Store mit gleitender Gültigkeit ttl.
func NewStore(d *sql.DB, ttl time.Duration) *Store {
	s := &Store{db: d, now: time.Now}
	s.ttl.Store(int64(ttl))
	return s
}

// SetTTL ändert die Gültigkeit; sie gilt ab dem nächsten Zugriff auch für bestehende Sitzungen.
func (s *Store) SetTTL(ttl time.Duration) { s.ttl.Store(int64(ttl)) }

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(t string) string {
	s := sha256.Sum256([]byte(t))
	return hex.EncodeToString(s[:])
}

func (s *Store) expiry() string {
	return s.now().Add(time.Duration(s.ttl.Load())).UTC().Format(time.RFC3339Nano)
}

// Create legt eine Sitzung an und liefert Token und CSRF-Token.
func (s *Store) Create(ctx context.Context, username string) (token, csrf string, err error) {
	return s.CreateFrom(ctx, username, "")
}

// CreateFrom wie Create, merkt sich zusätzlich die Client-Adresse.
func (s *Store) CreateFrom(ctx context.Context, username, ip string) (token, csrf string, err error) {
	if token, err = randomToken(); err != nil {
		return "", "", err
	}
	if csrf, err = randomToken(); err != nil {
		return "", "", err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO sessions(token_hash, username, csrf, expires_at, created_at, ip) VALUES (?,?,?,?,?,?)`,
		hashToken(token), username, csrf, s.expiry(), s.now().UTC().Format(time.RFC3339), ip)
	return token, csrf, err
}

// Lookup liefert die Sitzung und verlängert ihre Gültigkeit.
func (s *Store) Lookup(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, ErrNoSession
	}
	h := hashToken(token)
	var sess Session
	var exp string
	err := s.db.QueryRowContext(ctx, `SELECT username, csrf, expires_at FROM sessions WHERE token_hash = ?`, h).
		Scan(&sess.Username, &sess.CSRF, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNoSession
	}
	if err != nil {
		return Session{}, err
	}
	t, err := time.Parse(time.RFC3339Nano, exp)
	if err != nil || !s.now().Before(t) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, h)
		return Session{}, ErrNoSession
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET expires_at = ? WHERE token_hash = ?`, s.expiry(), h)
	return sess, nil
}

// Delete beendet eine Sitzung.
func (s *Store) Delete(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

// Purge löscht abgelaufene Sitzungen (sonst nur beim jeweiligen Zugriff) und liefert die Anzahl.
func (s *Store) Purge(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, s.now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SessionInfo beschreibt eine aktive Sitzung (ohne Geheimnisse).
type SessionInfo struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	CreatedAt string `json:"createdAt"`
	ExpiresAt string `json:"expiresAt"`
	IP        string `json:"ip"`
	Current   bool   `json:"current"`
}

// SessionID ist die öffentliche Kennung einer Sitzung (Präfix des Token-Hashes).
func SessionID(token string) string { return hashToken(token)[:16] }

// List liefert alle nicht abgelaufenen Sitzungen, neueste zuerst; current ist die Kennung der eigenen Sitzung.
func (s *Store) List(ctx context.Context, current string) ([]SessionInfo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT token_hash, username, created_at, expires_at, ip FROM sessions WHERE expires_at > ? ORDER BY created_at DESC, rowid DESC`,
		s.now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionInfo{}
	for rows.Next() {
		var h string
		var i SessionInfo
		if err := rows.Scan(&h, &i.Username, &i.CreatedAt, &i.ExpiresAt, &i.IP); err != nil {
			return nil, err
		}
		i.ID = h[:16]
		i.Current = i.ID == current
		out = append(out, i)
	}
	return out, rows.Err()
}

// DeleteByID beendet die Sitzung mit der Kennung id und meldet, ob es sie gab.
func (s *Store) DeleteByID(ctx context.Context, id string) (bool, error) {
	if len(id) != 16 {
		return false, nil
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE substr(token_hash,1,16) = ?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteOthers beendet alle Sitzungen außer der mit der Kennung keep und liefert die Anzahl.
func (s *Store) DeleteOthers(ctx context.Context, keep string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE substr(token_hash,1,16) <> ?`, keep)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
