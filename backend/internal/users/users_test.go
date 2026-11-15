package users

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
)

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"anna", "_svc", "a-b_1"} {
		if err := ValidateName(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"Anna", "1a", "a:b", "a b", strings.Repeat("a", 33), "root", "betternas-admin", ""} {
		err := ValidateName(bad)
		var ae *apierr.Error
		if !errors.As(err, &ae) || ae.Code != apierr.InvalidParams {
			t.Errorf("%q: erwartet invalid_params, bekommen %v", bad, err)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	cases := map[string]struct {
		user, pw string
		ok       bool
	}{
		"kurz":       {"anna", "kurz", false},
		"gleich":     {"anna12345", "anna12345", false},
		"zeilenumbr": {"anna", "gutes\npasswort", false},
		"nullbyte":   {"anna", "gutes\x00passwort", false},
		"ok":         {"anna", "geheim123", true},
	}
	for n, c := range cases {
		if err := ValidatePassword(c.user, c.pw); (err == nil) != c.ok {
			t.Errorf("%s: %v", n, err)
		}
	}
}

func TestValidateFullName(t *testing.T) {
	for _, bad := range []string{"a:b", "a,b", "a\nb", strings.Repeat("x", 65)} {
		if ValidateFullName(bad) == nil {
			t.Errorf("%q sollte abgelehnt werden", bad)
		}
	}
	if err := ValidateFullName("André Müller"); err != nil {
		t.Fatal(err)
	}
}

func TestParse(t *testing.T) {
	pw, _ := os.ReadFile("testdata/passwd")
	gr, _ := os.ReadFile("testdata/group")
	us, gs, all := Parse(pw, gr)
	if len(us) != 2 {
		t.Fatalf("erwartet 2 Benutzer, bekommen %+v", us)
	}
	a, w := us[0], us[1] // alphabetisch sortiert
	if w.Name != "wattie" || !w.System || !w.Admin || !slices.Equal(w.Groups, []string{"familie"}) || w.FullName != "André W." {
		t.Fatalf("wattie: %+v", w)
	}
	if a.Name != "anna" || a.System || a.Admin || a.FullName != "Anna Muster" || !slices.Equal(a.Groups, []string{"familie"}) {
		t.Fatalf("anna: %+v", a)
	}
	if len(gs) != 1 || gs[0].Name != "familie" || !slices.Equal(gs[0].Members, []string{"anna", "wattie"}) {
		t.Fatalf("gruppen: %+v", gs)
	}
	if !all["docker"] || !all["root"] || !all["anna"] || all["unbekannt"] {
		t.Fatalf("all: %v", all)
	}
	if !IsReserved("sudo") || IsReserved("anna") {
		t.Fatal("IsReserved")
	}
}
