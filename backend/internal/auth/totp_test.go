package auth

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// RFC 6238, Anhang B (SHA-1): Secret ASCII "12345678901234567890".
const rfcSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestTOTPRFC6238Vectors(t *testing.T) {
	cases := map[int64]string{59: "287082", 1111111109: "081804", 1111111111: "050471", 2000000000: "279037"}
	for unix, want := range cases {
		got, err := TOTPCode(rfcSecret, TOTPStep(time.Unix(unix, 0)))
		if err != nil || got != want {
			t.Errorf("t=%d: %q (%v) statt %q", unix, got, err, want)
		}
	}
	if _, err := TOTPCode("kein base32!", 1); err == nil {
		t.Error("ungültiges Base32 akzeptiert")
	}
}

func TestVerifyTOTPWindowAndReplay(t *testing.T) {
	now := time.Unix(1111111109, 0)
	step := TOTPStep(now)
	code := func(s int64) string { c, _ := TOTPCode(rfcSecret, s); return c }
	for _, off := range []int64{-1, 0, 1} {
		got, ok := VerifyTOTP(rfcSecret, code(step+off), now, 0)
		if !ok || got != step+off {
			t.Errorf("Schritt %+d: ok=%v step=%d", off, ok, got)
		}
	}
	for _, off := range []int64{-2, 2} {
		if _, ok := VerifyTOTP(rfcSecret, code(step+off), now, 0); ok {
			t.Errorf("Schritt %+d akzeptiert", off)
		}
	}
	// Wiederverwendung: Schritt gilt nur, wenn er neuer als der zuletzt akzeptierte ist
	if _, ok := VerifyTOTP(rfcSecret, code(step), now, step); ok {
		t.Error("gleicher Schritt zweimal akzeptiert")
	}
	if _, ok := VerifyTOTP(rfcSecret, code(step-1), now, step); ok {
		t.Error("älterer Schritt nach neuerem akzeptiert")
	}
	for _, bad := range []string{"12345", "abcdef", "", "1234567", " 287082"} {
		if _, ok := VerifyTOTP(rfcSecret, bad, now, 0); ok {
			t.Errorf("%q akzeptiert", bad)
		}
	}
}

func TestNewSecretAndURI(t *testing.T) {
	a, err := NewSecret()
	b, _ := NewSecret()
	if err != nil || len(a) != 32 || a == b || !regexp.MustCompile(`^[A-Z2-7]{32}$`).MatchString(a) {
		t.Fatalf("%q %q %v", a, b, err)
	}
	u := OTPAuthURI("BetterNAS", "anna müller@nas", a)
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme != "otpauth" || parsed.Host != "totp" {
		t.Fatalf("%q %v", u, err)
	}
	q := parsed.Query()
	if q.Get("secret") != a || q.Get("issuer") != "BetterNAS" || q.Get("digits") != "6" || q.Get("period") != "30" || q.Get("algorithm") != "SHA1" {
		t.Fatalf("%v", q)
	}
	if !strings.Contains(parsed.Path, "anna müller@nas") {
		t.Fatalf("Konto im Pfad: %q", parsed.Path)
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, err := NewRecoveryCodes(10)
	if err != nil || len(codes) != 10 {
		t.Fatalf("%v %v", codes, err)
	}
	re := regexp.MustCompile(`^[A-Z2-7]{5}-[A-Z2-7]{5}$`)
	seen := map[string]bool{}
	for _, c := range codes {
		if !re.MatchString(c) || seen[c] {
			t.Fatalf("%q", c)
		}
		seen[c] = true
	}
	if HashRecoveryCode("abcde-fghij") != HashRecoveryCode("ABCDEFGHIJ") || HashRecoveryCode("abcde-fghij") == HashRecoveryCode("abcde-fghik") {
		t.Fatal("Normalisierung/Hash")
	}
}
