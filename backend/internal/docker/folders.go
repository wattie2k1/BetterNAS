package docker

import (
	"path"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
)

// Folder ist ein Unterordner in der Ordnerauswahl.
type Folder struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// FolderList ist der Inhalt eines Ordners. Parent ist leer, wenn der Ordner eine Wurzel ist (dann geht es zur Übersicht).
// Path ist leer in der Übersicht der Wurzeln.
type FolderList struct {
	Path   string   `json:"path"`
	Parent string   `json:"parent"`
	Dirs   []Folder `json:"dirs"`
}

// CleanFolder bereinigt einen Pfad der Ordnerauswahl. Leer steht für die Übersicht der Wurzeln.
// Geprüft wird nur die Syntax (absolut, ohne „..“); welche Wurzeln erlaubt sind, entscheidet der Agent.
func CleanFolder(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	bad := apierr.New(apierr.InvalidParams, "Ungültiger Ordnerpfad")
	if !strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) || len(p) > 4096 {
		return "", bad
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", bad
		}
	}
	return path.Clean(p), nil
}

// HostDevice ist ein Gerät des Hosts, das an Container durchgereicht werden kann.
type HostDevice struct {
	Kind  string `json:"kind"`  // gpu | serial
	Path  string `json:"path"`  // Pfad auf dem Host, so wie er in die compose.yaml gehört
	Name  string `json:"name"`  // Anzeigename
	GID   int    `json:"gid"`   // numerische Gruppe der Gerätedatei (für group_add)
	Group string `json:"group"` // Name dieser Gruppe
	Note  string `json:"note,omitempty"`
}
