package installer

import (
	"net/netip"
	"regexp"
	"slices"
	"strings"
)

// FieldError beschreibt eine ungültige Eingabe.
type FieldError struct {
	Field   string
	Message string
}

func (e FieldError) Error() string { return e.Field + ": " + e.Message }

var (
	hostnameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	usernameRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	sshKeyRe   = regexp.MustCompile(`^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521)|sk-ssh-ed25519@openssh\.com) [A-Za-z0-9+/=]+( .*)?$`)
)

// reservedUsers enthält Systemkonten aus Debian (base-passwd und Paketkonten) sowie BetterNAS-Konten.
var reservedUsers = []string{
	"root", "daemon", "bin", "sys", "sync", "games", "man", "lp", "mail", "news", "uucp",
	"proxy", "www-data", "backup", "list", "irc", "gnats", "_apt", "nobody", "messagebus",
	"sshd", "avahi", "polkitd", "systemd-network", "systemd-resolve", "systemd-timesync",
	"systemd-coredump", "tss", "betternas", "betternas-admin", "sudo", "admin",
}

// MinRootSizeGiB ist die minimale Größe der Root-Partition.
const MinRootSizeGiB = 16

// Validate prüft alle Antworten. knownTimezones prüft, ob eine Zeitzone existiert.
func (a Answers) Validate(knownTimezones func(string) bool) []FieldError {
	var errs []FieldError
	add := func(f, m string) { errs = append(errs, FieldError{f, m}) }

	if !slices.Contains(Languages, a.Language) {
		add("language", "Unbekannte Sprache")
	}
	if !slices.Contains(Keymaps, a.Keymap) {
		add("keymap", "Unbekanntes Tastaturlayout")
	}
	if knownTimezones != nil && !knownTimezones(a.Timezone) {
		add("timezone", "Unbekannte Zeitzone")
	}
	if a.RootSizeGiB < MinRootSizeGiB {
		add("rootSizeGiB", "Die Systempartition muss mindestens 16 GiB groß sein")
	}
	if !hostnameRe.MatchString(a.Hostname) {
		add("hostname", "Nur Kleinbuchstaben, Ziffern und Bindestriche (nicht am Anfang oder Ende), max. 63 Zeichen")
	}
	errs = append(errs, a.validateNet()...)
	errs = append(errs, a.validateAdmin()...)
	return errs
}

// ValidateFields liefert nur die Fehler für die genannten Felder (Präfixvergleich, z. B. "net").
func (a Answers) ValidateFields(knownTimezones func(string) bool, fields ...string) []FieldError {
	var out []FieldError
	for _, e := range a.Validate(knownTimezones) {
		for _, f := range fields {
			if e.Field == f || len(e.Field) > len(f) && e.Field[:len(f)+1] == f+"." {
				out = append(out, e)
			}
		}
	}
	return out
}

func (a Answers) validateAdmin() []FieldError {
	var errs []FieldError
	switch {
	case !usernameRe.MatchString(a.AdminUser):
		errs = append(errs, FieldError{"adminUser", "Kleinbuchstaben, Ziffern, _ und -, beginnend mit Buchstabe oder _, max. 32 Zeichen"})
	case slices.Contains(reservedUsers, a.AdminUser):
		errs = append(errs, FieldError{"adminUser", "Dieser Name ist für das System reserviert"})
	}
	switch {
	case len([]rune(a.AdminPassword)) < 8:
		errs = append(errs, FieldError{"adminPassword", "Das Passwort muss mindestens 8 Zeichen lang sein"})
	case a.AdminPassword == a.AdminUser:
		errs = append(errs, FieldError{"adminPassword", "Das Passwort darf nicht dem Benutzernamen entsprechen"})
	}
	if k := a.AdminSSHKey; k != "" && (!sshKeyRe.MatchString(k) || strings.ContainsAny(k, "\r\n")) {
		errs = append(errs, FieldError{"adminSSHKey", "Ungültiger öffentlicher SSH-Schlüssel"})
	}
	return errs
}

func (a Answers) validateNet() []FieldError {
	if a.Net.DHCP {
		return nil
	}
	var errs []FieldError
	prefix, err := netip.ParsePrefix(a.Net.Address)
	if err != nil || !prefix.Addr().Is4() && !prefix.Addr().Is6() {
		errs = append(errs, FieldError{"net.address", "Adresse mit Präfix angeben, z. B. 192.168.1.20/24"})
	}
	gw, gerr := netip.ParseAddr(a.Net.Gateway)
	switch {
	case gerr != nil:
		errs = append(errs, FieldError{"net.gateway", "Ungültiges Gateway"})
	case err == nil && !prefix.Masked().Contains(gw):
		errs = append(errs, FieldError{"net.gateway", "Das Gateway liegt nicht im Netz der Adresse"})
	}
	if len(a.Net.DNS) == 0 {
		errs = append(errs, FieldError{"net.dns", "Mindestens einen DNS-Server angeben"})
	}
	for _, d := range a.Net.DNS {
		if _, err := netip.ParseAddr(d); err != nil {
			errs = append(errs, FieldError{"net.dns", "Ungültiger DNS-Server: " + d})
			break
		}
	}
	return errs
}
