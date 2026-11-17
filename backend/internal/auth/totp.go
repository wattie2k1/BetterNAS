package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // TOTP (RFC 6238) verwendet HMAC-SHA1
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Parameter der Zwei-Faktor-Codes.
const (
	TOTPDigits = 6
	TOTPPeriod = 30
)

var (
	b32       = base32.StdEncoding.WithPadding(base32.NoPadding)
	codeRe    = regexp.MustCompile(`^\d{6}$`)
	recoverRe = regexp.MustCompile(`^[A-Z2-7]{10}$`)
)

// NewSecret erzeugt ein zufälliges TOTP-Geheimnis (160 Bit, Base32).
func NewSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return b32.EncodeToString(b), nil
}

// TOTPStep liefert den Zeitschritt zu t.
func TOTPStep(t time.Time) int64 { return t.Unix() / TOTPPeriod }

// TOTPCode berechnet den Code eines Zeitschritts (RFC 4226/6238, HMAC-SHA1).
func TOTPCode(secret string, step int64) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", TOTPDigits, v%1000000), nil
}

// VerifyTOTP prüft code gegen die Schritte now-1 … now+1. Nur Schritte, die neuer als lastStep sind,
// gelten (Schutz gegen Wiederverwendung). Bei Erfolg wird der verbrauchte Schritt geliefert.
func VerifyTOTP(secret, code string, now time.Time, lastStep int64) (int64, bool) {
	if !codeRe.MatchString(code) {
		return 0, false
	}
	cur := TOTPStep(now)
	for _, s := range []int64{cur - 1, cur, cur + 1} {
		if s <= lastStep {
			continue
		}
		want, err := TOTPCode(secret, s)
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return s, true
		}
	}
	return 0, false
}

// OTPAuthURI baut die otpauth://-Adresse für Authenticator-Apps.
func OTPAuthURI(issuer, account, secret string) string {
	label := (&url.URL{Path: issuer + ":" + account}).EscapedPath()
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(TOTPDigits))
	q.Set("period", fmt.Sprint(TOTPPeriod))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// NewRecoveryCodes erzeugt n Wiederherstellungscodes der Form XXXXX-XXXXX.
func NewRecoveryCodes(n int) ([]string, error) {
	out := make([]string, 0, n)
	seen := map[string]bool{}
	for len(out) < n {
		b := make([]byte, 6) // 48 Bit → 10 Base32-Zeichen (Rest abgeschnitten)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		s := b32.EncodeToString(b)[:10]
		c := s[:5] + "-" + s[5:]
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out, nil
}

// normalizeRecovery bringt einen eingegebenen Code in die Vergleichsform (Großbuchstaben, ohne Bindestrich/Leerzeichen).
func normalizeRecovery(code string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(code))
}

// HashRecoveryCode liefert den SHA-256 (hex) des normalisierten Codes.
func HashRecoveryCode(code string) string {
	h := sha256.Sum256([]byte(normalizeRecovery(code)))
	return hex.EncodeToString(h[:])
}
