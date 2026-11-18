package vm

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// SharedRoot ist das Verzeichnis, unterhalb dessen Ordner an VMs freigegeben werden dürfen (die Pools).
const SharedRoot = "/srv/pools/"

// Virtiofsd ist der Dienst, den libvirt für geteilte Ordner startet.
const Virtiofsd = "/usr/libexec/virtiofsd"

const MaxSharedDirs = 8

// SharedDir ist ein Host-Ordner, der der VM per virtiofs unter einem Namen („Tag“) bereitsteht.
type SharedDir struct {
	Path     string `json:"path"`     // Ordner auf dem NAS, unterhalb von /srv/pools/
	Tag      string `json:"tag"`      // Name, unter dem der Gast den Ordner einbindet
	ReadOnly bool   `json:"readOnly"` // der Gast darf nur lesen
}

var (
	tagRe        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,35}$`)
	sharedDirRe  = regexp.MustCompile(`^/srv/pools/[A-Za-z0-9_-]+(/[\p{L}\p{N} ._-]+)*$`)
	reFilesystem = regexp.MustCompile(`(?s)[ \t]*<filesystem\b[^>]*>.*?</filesystem>\n?`)
	reMemBacking = regexp.MustCompile(`(?s)[ \t]*<memoryBacking>.*?</memoryBacking>\n?`)
	reOurMemBack = regexp.MustCompile(`(?s)[ \t]*<memoryBacking>\s*<source type='memfd'/>\s*<access mode='shared'/>\s*</memoryBacking>\n?`)
	reVCPUOpen   = regexp.MustCompile(`[ \t]*<vcpu\b`)
)

// MemoryBackingXML ist der gemeinsam nutzbare Speicher, den virtiofs braucht.
const MemoryBackingXML = "  <memoryBacking>\n    <source type='memfd'/>\n    <access mode='shared'/>\n  </memoryBacking>\n"

func validSharedDirs(list []SharedDir) bool {
	if len(list) > MaxSharedDirs {
		return false
	}
	tags := map[string]bool{}
	for _, d := range list {
		if !tagRe.MatchString(d.Tag) || tags[d.Tag] || !ValidSharedPath(d.Path) {
			return false
		}
		tags[d.Tag] = true
	}
	return true
}

// ValidSharedPath prüft, dass ein Ordner direkt in einem Pool liegt (kein „..“, keine Sonderzeichen).
func ValidSharedPath(p string) bool {
	if path.Clean(p) != p || !strings.HasPrefix(p, SharedRoot) || !sharedDirRe.MatchString(p) {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

func sameSharedDirs(a, b []SharedDir) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[SharedDir]bool{}
	for _, d := range a {
		set[d] = true
	}
	for _, d := range b {
		if !set[d] {
			return false
		}
	}
	return true
}

func sharedDirXML(d SharedDir) string {
	ro := ""
	if d.ReadOnly {
		ro = "      <readonly/>\n"
	}
	return fmt.Sprintf("    <filesystem type='mount' accessmode='passthrough'>\n      <driver type='virtiofs'/>\n      <source dir='%s'/>\n      <target dir='%s'/>\n%s    </filesystem>\n", esc(d.Path), esc(d.Tag), ro)
}

// applySharedDirs ersetzt die virtiofs-Ordner in der Domain-XML und sorgt für den gemeinsamen Speicher.
// Andere <filesystem>-Einträge (kein virtiofs) bleiben unberührt.
func applySharedDirs(out string, want []SharedDir) (string, error) {
	out = reFilesystem.ReplaceAllStringFunc(out, func(blk string) string {
		if strings.Contains(blk, "type='virtiofs'") {
			return ""
		}
		return blk
	})
	if len(want) == 0 {
		return reOurMemBack.ReplaceAllString(out, ""), nil
	}
	if mb := reMemBacking.FindString(out); mb != "" {
		if !strings.Contains(mb, "access mode='shared'") || !(strings.Contains(mb, "type='memfd'") || strings.Contains(mb, "type='file'")) {
			return "", invalid("Diese VM hat eine eigene Speicher-Konfiguration; geteilte Ordner brauchen gemeinsam genutzten Speicher")
		}
	} else {
		loc := reVCPUOpen.FindStringIndex(out)
		if loc == nil {
			return "", invalid("Die VM hat keine vCPU-Angabe")
		}
		out = out[:loc[0]] + MemoryBackingXML + out[loc[0]:]
	}
	if !strings.Contains(out, "  </devices>") {
		return "", invalid("Die VM hat keinen Geräte-Abschnitt")
	}
	var b strings.Builder
	for _, d := range want {
		b.WriteString(sharedDirXML(d))
	}
	return strings.Replace(out, "  </devices>", b.String()+"  </devices>", 1), nil
}
