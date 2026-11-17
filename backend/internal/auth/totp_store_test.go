package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/db"
)

type totpEnv struct {
	s      *TOTPStore
	ctx    context.Context
	now    time.Time
	secret string
	codes  []string
}

// enabledFor richtet 2FA für user vollständig ein und liefert Secret und Wiederherstellungscodes.
func newTOTPEnv(t *testing.T) *totpEnv {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &totpEnv{s: NewTOTPStore(d), ctx: context.Background(), now: time.Unix(1_800_000_000, 0)}
}

func (e *totpEnv) enable(t *testing.T, user string) {
	t.Helper()
	secret, _ := NewSecret()
	if err := e.s.BeginSetup(e.ctx, user, secret); err != nil {
		t.Fatal(err)
	}
	codes, _ := NewRecoveryCodes(10)
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = HashRecoveryCode(c)
	}
	if err := e.s.Enable(e.ctx, user, TOTPStep(e.now)-5, hashes); err != nil {
		t.Fatal(err)
	}
	e.secret, e.codes = secret, codes
}

func (e *totpEnv) code(t *testing.T, offset int64) string {
	t.Helper()
	c, err := TOTPCode(e.secret, TOTPStep(e.now)+offset)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSetupEnableVerifyFlow(t *testing.T) {
	e := newTOTPEnv(t)
	if on, err := e.s.Enabled(e.ctx, "anna"); err != nil || on {
		t.Fatalf("%v %v", on, err)
	}
	secret, _ := NewSecret()
	if err := e.s.BeginSetup(e.ctx, "anna", secret); err != nil {
		t.Fatal(err)
	}
	if on, _ := e.s.Enabled(e.ctx, "anna"); on {
		t.Fatal("vorläufig darf nicht aktiv sein")
	}
	got, ok, err := e.s.Pending(e.ctx, "anna")
	if err != nil || !ok || got != secret {
		t.Fatalf("%q %v %v", got, ok, err)
	}
	c, _ := TOTPCode(secret, TOTPStep(e.now))
	if ok, _ := e.s.Verify(e.ctx, "anna", c, e.now); ok {
		t.Fatal("Verify vor Enable muss false sein")
	}
	e.secret = secret
	e.enable(t, "anna2")
	if on, _ := e.s.Enabled(e.ctx, "anna2"); !on {
		t.Fatal("nach Enable aktiv erwartet")
	}
	if _, ok, _ := e.s.Pending(e.ctx, "anna2"); ok {
		t.Fatal("aktives Geheimnis ist nicht „vorläufig“")
	}
}

func TestVerifyReplayRejected(t *testing.T) {
	e := newTOTPEnv(t)
	e.enable(t, "anna")
	c := e.code(t, 0)
	if ok, err := e.s.Verify(e.ctx, "anna", c, e.now); err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if ok, _ := e.s.Verify(e.ctx, "anna", c, e.now); ok {
		t.Fatal("zweite Verwendung akzeptiert")
	}
	if ok, _ := e.s.Verify(e.ctx, "anna", e.code(t, -1), e.now); ok {
		t.Fatal("älterer Schritt nach neuerem akzeptiert")
	}
	if ok, _ := e.s.Verify(e.ctx, "anna", e.code(t, 1), e.now); !ok {
		t.Fatal("neuerer Schritt muss gelten")
	}
}

func TestRecoveryCodeSingleUse(t *testing.T) {
	e := newTOTPEnv(t)
	e.enable(t, "anna")
	if ok, _ := e.s.Verify(e.ctx, "anna", e.codes[0], e.now); !ok {
		t.Fatal("Wiederherstellungscode abgelehnt")
	}
	if ok, _ := e.s.Verify(e.ctx, "anna", e.codes[0], e.now); ok {
		t.Fatal("Code zweimal akzeptiert")
	}
	if ok, _ := e.s.Verify(e.ctx, "anna", e.codes[1], e.now); !ok {
		t.Fatal("andere Codes müssen gültig bleiben")
	}
	// Eingabe ohne Bindestrich und in Kleinbuchstaben
	alt := e.codes[2][:5] + e.codes[2][6:]
	if ok, _ := e.s.Verify(e.ctx, "anna", lower(alt), e.now); !ok {
		t.Fatal("normalisierte Eingabe abgelehnt")
	}
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func TestBeginSetupRefusedWhenActive(t *testing.T) {
	e := newTOTPEnv(t)
	e.enable(t, "anna")
	other, _ := NewSecret()
	if err := e.s.BeginSetup(e.ctx, "anna", other); apierr.From(err).Code != apierr.Conflict {
		t.Fatalf("%v", err)
	}
	if ok, _ := e.s.Verify(e.ctx, "anna", e.code(t, 0), e.now); !ok {
		t.Fatal("aktives Geheimnis wurde überschrieben")
	}
}

func TestBeginSetupReplacesPending(t *testing.T) {
	e := newTOTPEnv(t)
	a, _ := NewSecret()
	b, _ := NewSecret()
	e.s.BeginSetup(e.ctx, "anna", a)
	if err := e.s.BeginSetup(e.ctx, "anna", b); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := e.s.Pending(e.ctx, "anna"); got != b {
		t.Fatalf("%q", got)
	}
}

func TestDisableInvalidatesEverything(t *testing.T) {
	e := newTOTPEnv(t)
	e.enable(t, "anna")
	c, rec := e.code(t, 0), e.codes[0]
	if err := e.s.Disable(e.ctx, "anna"); err != nil {
		t.Fatal(err)
	}
	if err := e.s.Disable(e.ctx, "anna"); err != nil {
		t.Fatalf("Disable muss idempotent sein: %v", err)
	}
	if on, _ := e.s.Enabled(e.ctx, "anna"); on {
		t.Fatal("noch aktiv")
	}
	for _, code := range []string{c, rec} {
		if ok, _ := e.s.Verify(e.ctx, "anna", code, e.now); ok {
			t.Fatalf("%q nach Disable akzeptiert", code)
		}
	}
}

func TestUsersIsolated(t *testing.T) {
	e := newTOTPEnv(t)
	e.enable(t, "anna")
	if ok, _ := e.s.Verify(e.ctx, "ben", e.code(t, 0), e.now); ok {
		t.Fatal("Code von anna gilt für ben")
	}
}
