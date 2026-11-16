// Package users beschreibt Benutzer und Gruppen des Systems und prüft Eingaben dafür.
package users

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/betternas/betternas/internal/apierr"
)

// AdminGroup ist die Gruppe, deren Mitglieder die GUI bedienen dürfen.
const AdminGroup = "betternas-admin"

// Bereich der IDs, die BetterNAS selbst vergibt und verwaltet.
const (
	MinID = 10000
	MaxID = 59999
	// MinHumanUID ist die kleinste UID, die in der Benutzerliste erscheint (Installer-Admin hat 1000).
	MinHumanUID = 1000
)

// NameRe beschreibt gültige Benutzer- und Gruppennamen.
var NameRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// reserved enthält Systemkonten aus Debian (base-passwd und Paketkonten) sowie BetterNAS-Konten.
var reserved = []string{
	"root", "daemon", "bin", "sys", "sync", "games", "man", "lp", "mail", "news", "uucp",
	"proxy", "www-data", "backup", "list", "irc", "gnats", "_apt", "nobody", "messagebus",
	"sshd", "avahi", "polkitd", "systemd-network", "systemd-resolve", "systemd-timesync",
	"systemd-coredump", "tss", "betternas", "betternas-admin", "sudo", "admin",
}

// IsReserved meldet, ob ein Name für das System reserviert ist.
func IsReserved(name string) bool { return slices.Contains(reserved, name) }

// MinPasswordLen ist die Mindestlänge eines Passworts in Zeichen.
const MinPasswordLen = 8

func invalid(field, msg string) error {
	return apierr.New(apierr.InvalidParams, msg).With("field", field)
}

// ValidateName prüft einen Benutzer- oder Gruppennamen.
func ValidateName(name string) error {
	switch {
	case !NameRe.MatchString(name):
		return invalid("name", "Kleinbuchstaben, Ziffern, _ und -, beginnend mit Buchstabe oder _, max. 32 Zeichen")
	case IsReserved(name):
		return invalid("name", "Dieser Name ist für das System reserviert")
	}
	return nil
}

// ValidatePassword prüft ein Passwort für einen Benutzer.
func ValidatePassword(user, pw string) error {
	switch {
	case len([]rune(pw)) < MinPasswordLen:
		return invalid("password", "Das Passwort muss mindestens 8 Zeichen lang sein")
	case pw == user:
		return invalid("password", "Das Passwort darf nicht dem Benutzernamen entsprechen")
	case strings.ContainsAny(pw, "\n\r\x00"):
		return invalid("password", "Das Passwort enthält ungültige Zeichen")
	}
	return nil
}

// ValidateFullName prüft den vollständigen Namen (GECOS-Feld).
func ValidateFullName(s string) error {
	if len([]rune(s)) > 64 {
		return invalid("fullName", "Maximal 64 Zeichen")
	}
	if strings.ContainsAny(s, ":,") || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return invalid("fullName", "Keine Doppelpunkte, Kommas oder Steuerzeichen")
	}
	return nil
}

// User ist ein Benutzerkonto, wie die GUI es zeigt.
type User struct {
	Name     string   `json:"name"`
	FullName string   `json:"fullName"`
	UID      int      `json:"uid"`
	Groups   []string `json:"groups"` // nur BetterNAS-Gruppen
	Admin    bool     `json:"admin"`
	System   bool     `json:"system"` // UID < 10000, z. B. der Admin aus dem Installer
}

// Group ist eine von BetterNAS verwaltete Gruppe.
type Group struct {
	Name    string   `json:"name"`
	GID     int      `json:"gid"`
	Members []string `json:"members"`
}

// Accounts fasst Benutzer und Gruppen zusammen.
type Accounts struct {
	Users  []User  `json:"users"`
	Groups []Group `json:"groups"`
}
