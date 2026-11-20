package docker

import (
	"path"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
)

// Paths sind die beiden einstellbaren Wurzeln der Docker-Daten.
// Appdata enthält Einstellungen der Apps sowie Stacks, Reverse-Proxy und Sicherungen von BetterNAS,
// Dockerdata alle übrigen App-Ordner und den Docker-data-root.
type Paths struct {
	Appdata    string `json:"appdata"`
	Dockerdata string `json:"dockerdata"`
}

// LegacyPaths ist der Zustand bestehender Installationen, bei denen alles unter /srv/appdata liegt.
var LegacyPaths = Paths{Appdata: "/srv/appdata", Dockerdata: "/srv/appdata"}

const legacyRoot = "/srv/appdata"

// Unterordner, die BetterNAS in den Wurzeln selbst verwaltet.
var (
	appdataReserved    = map[string]bool{"stacks": true, "reverse-proxy": true, "backups": true}
	dockerdataReserved = map[string]bool{"docker": true}
)

// StacksDir enthält je Stack ein Verzeichnis mit compose.yaml und .env.
func (p Paths) StacksDir() string { return p.Appdata + "/stacks" }

// ProxyDir enthält Caddyfile (conf), Zertifikate und Zustand (data, config) des Reverse-Proxys.
func (p Paths) ProxyDir() string { return p.Appdata + "/reverse-proxy" }

// BackupBase enthält die Datensicherungen vor Updates.
func (p Paths) BackupBase() string { return p.Appdata + "/backups/stacks" }

// DataRoot ist der data-root des Docker-Dienstes.
func (p Paths) DataRoot() string { return p.Dockerdata + "/docker" }

// StackDir ist das Verzeichnis eines Stacks. Der Name muss vorher mit StackNameRe geprüft sein.
func (p Paths) StackDir(name string) string { return p.StacksDir() + "/" + name }

// Reserved sagt, ob first (erster Namensteil unterhalb von root) BetterNAS selbst gehört.
func (p Paths) Reserved(root, first string) bool {
	if root == p.Appdata && appdataReserved[first] {
		return true
	}
	return root == p.Dockerdata && dockerdataReserved[first]
}

// CleanRoot bereinigt einen Wurzelpfad und prüft ihn rein syntaktisch: absolut, ohne „..“, unterhalb von
// /srv/pools oder /srv/disks (bei allowLegacy auch /srv/appdata), nie diese Bereiche selbst.
func CleanRoot(p string, allowLegacy bool) (string, error) {
	bad := apierr.New(apierr.InvalidParams, "Ungültiger Speicherort (erlaubt: ein Ordner unterhalb von /srv/pools oder /srv/disks)")
	if !strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) || len(p) > 4096 {
		return "", bad
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", bad
		}
	}
	c := path.Clean(p)
	if allowLegacy && (c == legacyRoot || strings.HasPrefix(c, legacyRoot+"/")) {
		return c, nil
	}
	for _, r := range []string{"/srv/pools", "/srv/disks"} {
		if strings.HasPrefix(c, r+"/") {
			return c, nil
		}
	}
	return "", bad
}

// Validate prüft beide Wurzeln. Sie dürfen weder gleich noch ineinander verschachtelt sein
// (Ausnahme: der Zustand LegacyPaths bestehender Installationen).
func (p Paths) Validate(allowLegacy bool) error {
	a, err := CleanRoot(p.Appdata, allowLegacy)
	if err != nil {
		return err
	}
	d, err := CleanRoot(p.Dockerdata, allowLegacy)
	if err != nil {
		return err
	}
	if allowLegacy && a == LegacyPaths.Appdata && d == LegacyPaths.Dockerdata {
		return nil
	}
	if a == d || strings.HasPrefix(a, d+"/") || strings.HasPrefix(d, a+"/") {
		return apierr.New(apierr.InvalidParams, "Die beiden Speicherorte dürfen nicht gleich sein und nicht ineinander liegen.")
	}
	return nil
}

// PathsInfo ist der Zustand der Docker-Speicherorte für die Oberfläche.
type PathsInfo struct {
	Configured bool     `json:"configured"`
	Appdata    string   `json:"appdata"`
	Dockerdata string   `json:"dockerdata"`
	Warnings   []string `json:"warnings"`
	// StaleStacks zählt Stacks, deren compose.yaml Pfade außerhalb der eingerichteten Speicherorte und der Pools enthält.
	StaleStacks int `json:"staleStacks"`
}
