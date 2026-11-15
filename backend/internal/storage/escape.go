package storage

import (
	"fmt"
	"strings"
)

// EscapePath entspricht `systemd-escape --path`.
func EscapePath(p string) string {
	p = strings.Trim(p, "/")
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '/':
			b.WriteByte('-')
		case c == '.' && i == 0:
			fmt.Fprintf(&b, `\x%02x`, c)
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == ':', c == '_', c == '.':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, `\x%02x`, c)
		}
	}
	return b.String()
}

// UnitName liefert den Namen der Mount-Unit für einen Pfad.
func UnitName(path string) string { return EscapePath(path) + ".mount" }
