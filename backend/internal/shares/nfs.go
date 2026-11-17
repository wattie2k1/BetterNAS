package shares

import (
	"hash/crc32"
	"net"
	"regexp"
	"strings"
)

const maxNFSClients = 32

// Squash-Modi: root = nur root wird abgebildet (Standard), all = alle Zugriffe laufen als anonymer Benutzer.
const (
	SquashRoot = "root"
	SquashAll  = "all"
)

var (
	clientCharsRe = regexp.MustCompile(`^[A-Za-z0-9.:/_-]{1,253}$`)
	hostLabelRe   = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
	numericRe     = regexp.MustCompile(`^[0-9]+$`)
)

// NFS beschreibt den optionalen NFS-Export einer Freigabe.
type NFS struct {
	Enabled  bool     `json:"enabled"`
	Clients  []string `json:"clients"`
	ReadOnly bool     `json:"readOnly"`
	Squash   string   `json:"squash"`
}

// ValidClient prüft einen Client-Eintrag: IP-Adresse, CIDR oder Hostname. Kein „*“, keine Exportoptionen.
func ValidClient(c string) bool {
	if !clientCharsRe.MatchString(c) {
		return false
	}
	if strings.Contains(c, "/") {
		_, _, err := net.ParseCIDR(c)
		return err == nil
	}
	if net.ParseIP(c) != nil {
		return true
	}
	if strings.Contains(c, ":") || strings.Contains(c, "_") {
		return false
	}
	labels := strings.Split(c, ".")
	allNumeric := true
	for _, l := range labels {
		if !hostLabelRe.MatchString(l) {
			return false
		}
		allNumeric = allNumeric && numericRe.MatchString(l)
	}
	return !allNumeric // „999.1.1.1“ ist weder Adresse noch Hostname
}

// Validate prüft die Einstellungen; ein deaktivierter Export wird nicht geprüft.
func (n NFS) Validate() error {
	if !n.Enabled {
		return nil
	}
	if len(n.Clients) == 0 {
		return invalid("nfs", "Mindestens ein Client ist nötig (IP-Adresse, Netz wie 192.168.1.0/24 oder Rechnername).")
	}
	if len(n.Clients) > maxNFSClients {
		return invalid("nfs", "Höchstens 32 Clients.")
	}
	for _, c := range n.Clients {
		if !ValidClient(c) {
			return invalid("nfs", "Ungültiger Client: "+c+" (erlaubt sind IP-Adressen, Netze und Rechnernamen, kein * und keine Optionen).")
		}
	}
	if n.Squash != "" && n.Squash != SquashRoot && n.Squash != SquashAll {
		return invalid("nfs", "Unbekannter Abbildungsmodus.")
	}
	return nil
}

// FSID liefert die stabile NFS-fsid einer Freigabe (mergerfs/FUSE hat keine eigene).
func FSID(shareName string) int {
	return 1000 + int(crc32.ChecksumIEEE([]byte(strings.ToLower(shareName)))%900000)
}
