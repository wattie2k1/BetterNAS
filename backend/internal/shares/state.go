// Package shares verwaltet Samba-Freigaben: Datenmodell, Prüfung und Konfigurationsrenderer.
package shares

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/storage"
	"github.com/betternas/betternas/internal/users"
)

// ConfDir enthält die von BetterNAS erzeugten Samba-Dateien.
const ConfDir = "/etc/samba/betternas.d"

// Zugriffsstufen und Prinzipal-Typen.
const (
	AccessNone = "none"
	AccessRO   = "ro"
	AccessRW   = "rw"
	TypeUser   = "user"
	TypeGroup  = "group"
)

var (
	nameRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$`)
	subdirRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,63}$`)
	// Von Samba selbst belegte Freigabenamen (ohne Beachtung der Schreibweise).
	reservedNames = []string{"global", "homes", "printers", "print$", "ipc$"}
)

// ACL ist ein Rechte-Eintrag einer Freigabe für einen Benutzer oder eine Gruppe.
type ACL struct {
	Type   string `json:"type"`   // user | group
	Name   string `json:"name"`   // Benutzer- oder Gruppenname
	Access string `json:"access"` // none | ro | rw
}

// Share ist eine Samba-Freigabe auf einem Ordner eines Pools.
type Share struct {
	Name      string `json:"name"`
	Pool      string `json:"pool"`
	Subdir    string `json:"subdir"`
	Comment   string `json:"comment"`
	Browsable bool   `json:"browsable"`
	Guest     bool   `json:"guest"`
	// GuestWrite erlaubt Gästen (und allen ohne Listeneintrag) auch Schreiben; nur mit Guest sinnvoll.
	GuestWrite bool  `json:"guestWrite"`
	Recycle    bool  `json:"recycle"`
	Fruit      bool  `json:"fruit"`
	ACL        []ACL `json:"acl"`
	// NFS ist der optionale zusätzliche Export per NFS (Zugriff nur nach Client-Liste, nicht nach ACL).
	NFS NFS `json:"nfs"`
}

// Path ist der absolute Ordner der Freigabe.
func (s Share) Path() string { return storage.PoolPath(s.Pool) + "/" + s.Subdir }

// FileName ist der Dateiname der Samba-Konfiguration dieser Freigabe.
func (s Share) FileName() string { return strings.ToLower(s.Name) + ".conf" }

// State ist der gewünschte Zustand aller Freigaben.
type State struct {
	Shares []Share `json:"shares"`
}

func invalid(field, msg string) error {
	return apierr.New(apierr.InvalidParams, msg).With("field", field)
}

// Validate prüft den gesamten Zustand.
func (st State) Validate() error {
	seen := map[string]bool{}
	fsids := map[int]string{}
	for _, s := range st.Shares {
		if err := s.Validate(); err != nil {
			return err
		}
		key := strings.ToLower(s.Name)
		if seen[key] {
			return invalid("name", "Es gibt bereits eine Freigabe mit diesem Namen.")
		}
		seen[key] = true
		if s.NFS.Enabled {
			id := FSID(s.Name)
			if other, dup := fsids[id]; dup {
				return invalid("nfs", "Die NFS-Kennung von „"+s.Name+"“ kollidiert mit „"+other+"“. Bitte einen der Namen ändern.")
			}
			fsids[id] = s.Name
		}
	}
	return nil
}

// Validate prüft eine einzelne Freigabe.
func (s Share) Validate() error {
	switch {
	case !nameRe.MatchString(s.Name):
		return invalid("name", "Buchstaben, Ziffern, _ und -, höchstens 32 Zeichen")
	case containsFold(reservedNames, s.Name):
		return invalid("name", "Dieser Name ist für Samba reserviert")
	case !storage.PoolNameRe.MatchString(s.Pool):
		return invalid("pool", "Ungültiger Pool")
	}
	segments := strings.Split(s.Subdir, "/")
	if len(segments) > 4 {
		return invalid("subdir", "Höchstens vier Ordnerebenen")
	}
	for _, seg := range segments {
		if !subdirRe.MatchString(seg) {
			return invalid("subdir", "Ordnernamen: Buchstaben, Ziffern, Leerzeichen, _ . und -, nicht mit Punkt beginnend")
		}
	}
	if len([]rune(s.Comment)) > 128 || strings.ContainsAny(s.Comment, `\%`) || strings.IndexFunc(s.Comment, unicode.IsControl) >= 0 {
		return invalid("comment", "Höchstens 128 Zeichen, ohne Steuerzeichen, Backslash und Prozentzeichen")
	}
	if err := s.NFS.Validate(); err != nil {
		return err
	}
	granted := false
	seen := map[string]bool{}
	for _, a := range s.ACL {
		if a.Type != TypeUser && a.Type != TypeGroup {
			return invalid("acl", "Unbekannter Rechtetyp")
		}
		if a.Access != AccessNone && a.Access != AccessRO && a.Access != AccessRW {
			return invalid("acl", "Unbekannte Zugriffsstufe")
		}
		if !users.NameRe.MatchString(a.Name) {
			return invalid("acl", "Ungültiger Benutzer- oder Gruppenname")
		}
		key := a.Type + ":" + a.Name
		if seen[key] {
			return invalid("acl", "Jeder Benutzer und jede Gruppe darf nur einmal vorkommen")
		}
		seen[key] = true
		granted = granted || a.Access != AccessNone
	}
	if !granted && !s.Guest {
		return invalid("acl", "Mindestens ein Benutzer oder eine Gruppe braucht Zugriff, oder Gastzugriff muss erlaubt sein")
	}
	return nil
}

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}
