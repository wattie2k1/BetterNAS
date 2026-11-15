package shares

import (
	"errors"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
)

func testShares() []Share {
	return []Share{
		{Name: "Filme", Pool: "media", Subdir: "Filme", Comment: "Unsere Filme", Browsable: true, Recycle: true, Fruit: true,
			ACL: []ACL{{"user", "anna", "rw"}, {"group", "familie", "ro"}}},
		{Name: "Oeffentlich", Pool: "media", Subdir: "Oeffentlich/Fotos", Browsable: true, Guest: true,
			ACL: []ACL{{"user", "anna", "rw"}, {"group", "kinder", "none"}}},
		{Name: "Privat", Pool: "daten", Subdir: "Privat", Fruit: true, Recycle: false,
			ACL: []ACL{{"user", "bob", "rw"}}},
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]func(*Share){
		"subdir ..":           func(s *Share) { s.Subdir = "../etc" },
		"subdir mitte ..":     func(s *Share) { s.Subdir = "a/../b" },
		"subdir absolut":      func(s *Share) { s.Subdir = "/etc" },
		"subdir leer":         func(s *Share) { s.Subdir = "" },
		"subdir versteckt":    func(s *Share) { s.Subdir = ".papierkorb" },
		"subdir zu tief":      func(s *Share) { s.Subdir = "a/b/c/d/e" },
		"kommentar newline":   func(s *Share) { s.Comment = "a\nb" },
		"kommentar backslash": func(s *Share) { s.Comment = `a\b` },
		"kommentar prozent":   func(s *Share) { s.Comment = "100%" },
		"kommentar lang":      func(s *Share) { s.Comment = strings.Repeat("x", 129) },
		"name homes":          func(s *Share) { s.Name = "homes" },
		"name print$":         func(s *Share) { s.Name = "print$" },
		"name leer":           func(s *Share) { s.Name = "" },
		"name leerzeichen":    func(s *Share) { s.Name = "a b" },
		"name lang":           func(s *Share) { s.Name = strings.Repeat("a", 33) },
		"pool ungültig":       func(s *Share) { s.Pool = "../x" },
		"keine rechte":        func(s *Share) { s.ACL = nil },
		"nur none":            func(s *Share) { s.ACL = []ACL{{"user", "anna", "none"}} },
		"prinzipal":           func(s *Share) { s.ACL = []ACL{{"user", "a b", "rw"}} },
		"typ":                 func(s *Share) { s.ACL = []ACL{{"role", "anna", "rw"}} },
		"zugriff":             func(s *Share) { s.ACL = []ACL{{"user", "anna", "x"}} },
		"acl doppelt":         func(s *Share) { s.ACL = []ACL{{"user", "anna", "rw"}, {"user", "anna", "ro"}} },
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			sh := testShares()[0]
			mod(&sh)
			err := State{Shares: []Share{sh}}.Validate()
			var ae *apierr.Error
			if !errors.As(err, &ae) || ae.Code != apierr.InvalidParams {
				t.Fatalf("erwartet invalid_params, bekommen %v", err)
			}
		})
	}
	t.Run("doppelter name anders geschrieben", func(t *testing.T) {
		a, b := testShares()[0], testShares()[1]
		b.Name = "filme"
		if err := (State{Shares: []Share{a, b}}).Validate(); err == nil {
			t.Fatal("erwartet Fehler")
		}
	})
	t.Run("gast ohne rechte ist erlaubt", func(t *testing.T) {
		sh := testShares()[1]
		sh.ACL = nil
		if err := (State{Shares: []Share{sh}}).Validate(); err != nil {
			t.Fatal(err)
		}
	})
	if err := (State{Shares: testShares()}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPath(t *testing.T) {
	if got := testShares()[1].Path(); got != "/srv/pools/media/Oeffentlich/Fotos" {
		t.Fatal(got)
	}
}
