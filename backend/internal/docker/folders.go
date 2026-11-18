package docker

import (
	"path"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
)

// FolderRoots sind die Wurzeln, unterhalb derer Ordner für Container ausgewählt werden dürfen:
// die Speicherpools und die App-Daten.
var FolderRoots = []string{"/srv/pools", "/srv/appdata"}

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

// IsFolderRoot sagt, ob p eine der Wurzeln ist.
func IsFolderRoot(p string) bool {
	for _, r := range FolderRoots {
		if p == r {
			return true
		}
	}
	return false
}

// UnderFolderRoot sagt, ob p eine Wurzel oder ein Pfad darunter ist (p muss bereits bereinigt sein).
func UnderFolderRoot(p string) bool {
	for _, r := range FolderRoots {
		if p == r || strings.HasPrefix(p, r+"/") {
			return true
		}
	}
	return false
}

// CleanFolder bereinigt einen Pfad der Ordnerauswahl. Leer steht für die Übersicht der Wurzeln.
// Erlaubt sind nur absolute Pfade unterhalb von FolderRoots ohne „..“.
func CleanFolder(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	bad := apierr.New(apierr.InvalidParams, "Ungültiger Ordnerpfad (erlaubt: Ordner unter /srv/pools und /srv/appdata)")
	if !strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) || len(p) > 4096 {
		return "", bad
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", bad
		}
	}
	c := path.Clean(p)
	if !UnderFolderRoot(c) {
		return "", bad
	}
	return c, nil
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
