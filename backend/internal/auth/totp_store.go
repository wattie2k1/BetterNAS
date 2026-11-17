package auth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/betternas/betternas/internal/apierr"
)

// TOTPStore speichert die Zwei-Faktor-Einstellungen je Benutzer.
type TOTPStore struct{ db *sql.DB }

// NewTOTPStore erzeugt den Speicher.
func NewTOTPStore(d *sql.DB) *TOTPStore { return &TOTPStore{db: d} }

// Enabled meldet, ob für den Benutzer 2FA aktiv ist (ein vorläufiges Geheimnis zählt nicht).
func (s *TOTPStore) Enabled(ctx context.Context, user string) (bool, error) {
	var on int
	err := s.db.QueryRowContext(ctx, `SELECT enabled FROM totp WHERE username = ?`, user).Scan(&on)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return on == 1, err
}

// BeginSetup legt ein vorläufiges Geheimnis an oder ersetzt ein vorläufiges; ein aktives wird nie überschrieben.
func (s *TOTPStore) BeginSetup(ctx context.Context, user, secret string) error {
	on, err := s.Enabled(ctx, user)
	if err != nil {
		return err
	}
	if on {
		return apierr.New(apierr.Conflict, "Die Zwei-Faktor-Anmeldung ist bereits aktiv.")
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO totp(username, secret, enabled, last_step, recovery_json, created_at) VALUES (?,?,0,0,'[]',?)
		 ON CONFLICT(username) DO UPDATE SET secret = excluded.secret, last_step = 0, recovery_json = '[]', created_at = excluded.created_at WHERE enabled = 0`,
		user, secret, time.Now().UTC().Format(time.RFC3339))
	return err
}

// Pending liefert das vorläufige (noch nicht bestätigte) Geheimnis.
func (s *TOTPStore) Pending(ctx context.Context, user string) (string, bool, error) {
	var secret string
	err := s.db.QueryRowContext(ctx, `SELECT secret FROM totp WHERE username = ? AND enabled = 0`, user).Scan(&secret)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return secret, err == nil, err
}

// Enable schaltet 2FA ein, merkt den bestätigten Zeitschritt und speichert die Hashes der Wiederherstellungscodes.
func (s *TOTPStore) Enable(ctx context.Context, user string, step int64, recoveryHashes []string) error {
	b, err := json.Marshal(recoveryHashes)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE totp SET enabled = 1, last_step = ?, recovery_json = ? WHERE username = ? AND enabled = 0`, step, string(b), user)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apierr.New(apierr.Conflict, "Es ist keine Einrichtung offen.")
	}
	return nil
}

// Verify prüft einen TOTP- oder Wiederherstellungscode. Beide gelten nur einmal; nur bei aktivem 2FA.
func (s *TOTPStore) Verify(ctx context.Context, user, code string, now time.Time) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var secret, rj string
	var on int
	var last int64
	err = tx.QueryRowContext(ctx, `SELECT secret, enabled, last_step, recovery_json FROM totp WHERE username = ?`, user).Scan(&secret, &on, &last, &rj)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && on != 1) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if step, ok := VerifyTOTP(secret, code, now, last); ok {
		if _, err := tx.ExecContext(ctx, `UPDATE totp SET last_step = ? WHERE username = ?`, step, user); err != nil {
			return false, err
		}
		return true, tx.Commit()
	}
	if !recoverRe.MatchString(normalizeRecovery(code)) {
		return false, nil
	}
	var hashes []string
	_ = json.Unmarshal([]byte(rj), &hashes)
	want := HashRecoveryCode(code)
	for i, h := range hashes {
		if subtle.ConstantTimeCompare([]byte(h), []byte(want)) == 1 {
			rest := append(append([]string{}, hashes[:i]...), hashes[i+1:]...)
			b, _ := json.Marshal(rest)
			if _, err := tx.ExecContext(ctx, `UPDATE totp SET recovery_json = ? WHERE username = ?`, string(b), user); err != nil {
				return false, err
			}
			return true, tx.Commit()
		}
	}
	return false, nil
}

// Disable entfernt die 2FA-Einstellungen (idempotent); alle Codes werden damit ungültig.
func (s *TOTPStore) Disable(ctx context.Context, user string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM totp WHERE username = ?`, user)
	return err
}
