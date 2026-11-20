package docker

import (
	"path"
	"regexp"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
)

// Paths sind die beiden einstellbaren Wurzeln der Docker-Daten.
// Appdata enthält Einstellungen der Apps sowie Stacks, Reverse-Proxy und Sicherungen von BetterNAS,
// Dockerdata alle übrigen App-Ordner und den Docker-data-root.
type Paths struct {
	Appdata    string `json:"appdata"`
	Dockerdata string `json:"dockerdata"`
	// Previous sind frühere Wurzeln. Stacks, die vor einer Änderung angelegt wurden, behalten ihre Pfade;
	// Datensicherung und Wiederherstellung kennen deshalb auch diese Wurzeln. Wird nur vom Agenten gepflegt.
	Previous []string `json:"previous,omitempty"`
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

// Roots liefert die aktuellen Wurzeln (appdata, dockerdata) gefolgt von den früheren.
func (p Paths) Roots() []string {
	return append([]string{p.Appdata, p.Dockerdata}, p.Previous...)
}

// Reserved sagt, ob first (erster Namensteil unterhalb von root) BetterNAS selbst gehört.
// Frühere Wurzeln gelten vorsichtshalber mit beiden Namensmengen.
func (p Paths) Reserved(root, first string) bool {
	if root == p.Appdata && appdataReserved[first] {
		return true
	}
	if root == p.Dockerdata && dockerdataReserved[first] {
		return true
	}
	for _, r := range p.Previous {
		if r == root && (appdataReserved[first] || dockerdataReserved[first]) {
			return true
		}
	}
	return false
}

// rootCharsRe sind die Zeichen eines Speicherorts: dieselben wie in den Pfadfeldern der App-Vorlagen. So landet nie
// ein Zeichen mit Bedeutung für Shell, YAML, Compose oder Logs in einem Skript oder einer Konfiguration.
var rootCharsRe = regexp.MustCompile(`^[A-Za-z0-9_./+@-]+$`)

// Verzeichnisse des Systems, die nie als Speicherort dienen dürfen (auch nicht darunter).
var systemDirs = map[string]bool{
	"bin": true, "boot": true, "dev": true, "etc": true, "lib": true, "lib32": true, "lib64": true, "libx32": true,
	"proc": true, "root": true, "run": true, "sbin": true, "sys": true, "tmp": true, "usr": true, "var": true,
}

// Bereiche von BetterNAS, die selbst keine Speicherorte sein dürfen: die Pool- und Platten-Wurzeln sowie Parität und VMs.
var reservedSrv = []string{"/srv/pools", "/srv/disks", "/srv/parity", "/srv/vms"}

// CleanRoot bereinigt einen Wurzelpfad und prüft ihn rein syntaktisch: absolut, nur sichere Zeichen, ohne „..“,
// mindestens zwei Ebenen tief (z. B. /srv/dockerdata oder /mnt/ssd), nicht in Systemverzeichnissen und nicht in
// den Bereichen von BetterNAS (/srv/pools, /srv/disks, /srv/parity, /srv/vms selbst). Beliebige andere Ordner
// im Dateisystem sind erlaubt; Besitzer und Rechte prüft der Agent. allowLegacy ist für Aufrufer erhalten geblieben.
func CleanRoot(p string, allowLegacy bool) (string, error) {
	_ = allowLegacy
	bad := apierr.New(apierr.InvalidParams, "Ungültiger Speicherort (erlaubt: ein Ordner mit mindestens zwei Ebenen, z. B. /srv/dockerdata oder /mnt/ssd/docker, nicht in Systemverzeichnissen; Buchstaben ohne Umlaute, Ziffern und _ . / + @ -)")
	if !strings.HasPrefix(p, "/") || len(p) > 4096 || !rootCharsRe.MatchString(p) {
		return "", bad
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", bad
		}
	}
	c := path.Clean(p)
	segs := strings.Split(strings.TrimPrefix(c, "/"), "/")
	if len(segs) < 2 || systemDirs[segs[0]] {
		return "", bad
	}
	for _, r := range reservedSrv {
		if c == r || (r != "/srv/pools" && r != "/srv/disks" && strings.HasPrefix(c, r+"/")) {
			return "", bad
		}
	}
	return c, nil
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
	// Problem erklärt, warum paths.json nicht geladen werden konnte (Docker bleibt dann gesperrt).
	Problem string `json:"problem,omitempty"`
	// StaleStacks zählt Stacks, deren compose.yaml Pfade außerhalb der eingerichteten Speicherorte und der Pools enthält.
	StaleStacks int `json:"staleStacks"`
}
