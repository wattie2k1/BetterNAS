package vm

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// DiskAdd ist die Anforderung, eine leere Daten-Festplatte hinzuzufügen.
type DiskAdd struct {
	SizeGiB         int  `json:"sizeGiB"`
	DeleteSnapshots bool `json:"deleteSnapshots"` // vorhandene Snapshots dafür löschen
}

// Validate prüft die Größe.
func (a DiskAdd) Validate() error {
	if a.SizeGiB < MinDiskGiB || a.SizeGiB > MaxDiskGiB {
		return invalid("Festplatte: 1 bis 65536 GiB")
	}
	return nil
}

// DiskRemove ist die Anforderung, eine Festplatte von der VM zu trennen.
type DiskRemove struct {
	Path            string `json:"path"`
	DeleteFile      bool   `json:"deleteFile"`      // Datei endgültig löschen
	DeleteSnapshots bool   `json:"deleteSnapshots"` // vorhandene Snapshots dafür löschen
}

// Validate prüft den Pfad.
func (r DiskRemove) Validate() error {
	if path.Clean(r.Path) != r.Path || !strings.HasPrefix(r.Path, DiskDir+"/") || strings.Contains(r.Path, "'") {
		return invalid("Ungültiger Festplattenpfad")
	}
	return nil
}

// ExtraDiskPath ist die Datei der n-ten Festplatte (n ≥ 2) einer VM. Der Name muss vorher geprüft sein.
func ExtraDiskPath(name string, n int) string {
	return path.Join(DiskDir, name+"-"+strconv.Itoa(n)+".qcow2")
}

var (
	reDiskAny    = regexp.MustCompile(`(?s)[ \t]*<disk\b[^>]*>.*?</disk>\n?`)
	reDiskTarget = regexp.MustCompile(`<target dev='([a-z0-9]+)'(?: bus='([a-z]+)')?`)
	reDiskFile   = regexp.MustCompile(`<source file='([^']*)'`)
)

// NextDiskTarget liefert Gerätename und Bus für eine neue Festplatte: Bus der Systemfestplatte,
// nächster freier Buchstabe (vdb, vdc … bzw. sdb …), auch an CD-Laufwerken vorbei.
func NextDiskTarget(xmlStr string) (dev, bus string, err error) {
	used := map[string]bool{}
	for _, blk := range reDiskAny.FindAllString(xmlStr, -1) {
		m := reDiskTarget.FindStringSubmatch(blk)
		if m == nil {
			continue
		}
		used[m[1]] = true
		if bus == "" && strings.Contains(blk, "device='disk'") {
			bus = m[2]
		}
	}
	if bus == "" {
		return "", "", invalid("Die VM hat keine Systemfestplatte")
	}
	prefix := "sd"
	switch bus {
	case "virtio":
		prefix = "vd"
	case "ide":
		prefix = "hd"
	}
	for c := 'b'; c <= 'z'; c++ {
		if d := prefix + string(c); !used[d] {
			return d, bus, nil
		}
	}
	return "", "", invalid("Kein freier Laufwerksname mehr")
}

// AddDiskXML hängt eine qcow2-Festplatte an die Domain-XML an.
func AddDiskXML(xmlStr, file, dev, bus string) (string, error) {
	if strings.Contains(xmlStr, "'"+esc(file)+"'") {
		return "", invalid("Die Festplatte ist der VM bereits zugewiesen")
	}
	if !strings.Contains(xmlStr, "  </devices>") {
		return "", invalid("Die VM hat keinen Geräte-Abschnitt")
	}
	blk := fmt.Sprintf("    <disk type='file' device='disk'>\n      <driver name='qemu' type='qcow2'/>\n      <source file='%s'/>\n      <target dev='%s' bus='%s'/>\n    </disk>\n", esc(file), dev, bus)
	return strings.Replace(xmlStr, "  </devices>", blk+"  </devices>", 1), nil
}

// RemoveDiskXML entfernt die Festplatte mit dieser Datei aus der Domain-XML. Die erste Festplatte
// (Systemfestplatte) bleibt unantastbar.
func RemoveDiskXML(xmlStr, file string) (string, error) {
	first := true
	found := false
	out := reDiskAny.ReplaceAllStringFunc(xmlStr, func(blk string) string {
		if !strings.Contains(blk, "device='disk'") {
			return blk
		}
		isFirst := first
		first = false
		m := reDiskFile.FindStringSubmatch(blk)
		if m == nil || m[1] != file {
			return blk
		}
		found = true
		if isFirst {
			return blk
		}
		return ""
	})
	switch {
	case !found:
		return "", invalid("Die Festplatte gehört nicht zu dieser VM")
	case out == xmlStr:
		return "", invalid("Die Systemfestplatte (erste Festplatte) lässt sich nicht entfernen")
	}
	return out, nil
}
