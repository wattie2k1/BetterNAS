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
	ttl time.Duration
	now func() time.Time
}

// NewStore erzeugt einen Store mit gleitender Gültigkeit ttl.
func NewStore(d *sql.DB, ttl time.Duration) *Store { return &Store{db: d, ttl: ttl, now: time.Now} }

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

func (s *Store) expiry() string { return s.now().Add(s.ttl).UTC().Format(time.RFC3339Nano) }

// Create legt eine Sitzung an und liefert Token und CSRF-Token.
func (s *Store) Create(ctx context.Context, username string) (token, csrf string, err error) {
	if token, err = randomToken(); err != nil {
		return "", "", err
	}
	if csrf, err = randomToken(); err != nil {
		return "", "", err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO sessions(token_hash, username, csrf, expires_at) VALUES (?,?,?,?)`,
		hashToken(token), username, csrf, s.expiry())
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
