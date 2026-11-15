package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// IDOptions steuert die Bildung von Disk-IDs.
type IDOptions struct {
	// AllowLoop lässt Loop-Geräte zu (nur für Tests).
	AllowLoop bool
}

// DiskID bildet eine stabile, dateisystem- und mergerfs-sichere Kennung.
// Reihenfolge: WWN → Modell+Seriennummer → loop-<kname> (falls erlaubt).
func DiskID(model, serial, wwn, kname string, opts IDOptions) (string, bool) {
	model, serial, wwn = strings.TrimSpace(model), strings.TrimSpace(serial), strings.TrimSpace(wwn)
	switch {
	case wwn != "":
		return sanitize("wwn-" + wwn), true
	case serial != "":
		if model == "" {
			return sanitize(serial), true
		}
		return sanitize(model + "_" + serial), true
	case opts.AllowLoop && strings.HasPrefix(kname, "loop"):
		return sanitize("loop-" + kname), true
	}
	return "", false
}

func sanitize(s string) string {
	b := []byte(s)
	for i, c := range b {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
		if !ok {
			b[i] = '_'
		}
	}
	if len(b) > 0 && (b[0] == '.' || b[0] == '-') {
		b[0] = '_'
	}
	return string(b)
}

// ShortID liefert die ersten 7 Hex-Zeichen von sha256(diskID).
func ShortID(diskID string) string {
	sum := sha256.Sum256([]byte(diskID))
	return hex.EncodeToString(sum[:])[:7]
}

// FSLabel liefert das Dateisystem-Label für eine Datenplatte (12 Zeichen, passt für ext4 und XFS).
func FSLabel(diskID string) string { return "bnas-" + ShortID(diskID) }
