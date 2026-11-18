package vm

import (
	"path"
	"strings"
)

// Rename ist die Anforderung, eine ausgeschaltete VM umzubenennen.
type Rename struct {
	NewName         string `json:"newName"`
	RenameFiles     bool   `json:"renameFiles"`     // Festplatten-Dateien und UEFI-Variablen mit umbenennen
	DeleteSnapshots bool   `json:"deleteSnapshots"` // vorhandene Snapshots dafür löschen (nur nötig mit RenameFiles)
}

// Validate prüft den neuen Namen.
func (r Rename) Validate() error {
	if !NameRe.MatchString(r.NewName) {
		return invalid("Ungültiger Name (erlaubt: Kleinbuchstaben, Ziffern, - und _, bis 40 Zeichen)")
	}
	return nil
}

// FileMoves liefert, welche Dateien beim Umbenennen von old nach new mitziehen: die von BetterNAS
// angelegten Festplatten (<old>.qcow2, <old>-N.qcow2) und die UEFI-Variablen (<old>_VARS.fd).
// Andere Dateien (z. B. importierte Images) bleiben unangetastet.
func FileMoves(oldName, newName string, disks []string, nvram string) map[string]string {
	moves := map[string]string{}
	for _, f := range disks {
		f = path.Clean(f)
		switch {
		case f == DiskPath(oldName):
			moves[f] = DiskPath(newName)
		default:
			for n := 2; n < 100; n++ {
				if f == ExtraDiskPath(oldName, n) {
					moves[f] = ExtraDiskPath(newName, n)
					break
				}
			}
		}
	}
	if nvram == NvramDir+oldName+"_VARS.fd" {
		moves[nvram] = NvramDir + newName + "_VARS.fd"
	}
	return moves
}

// ApplyMoves ersetzt die Pfade in der Domain-XML (nur vollständige, in Hochkommata stehende Pfade).
func ApplyMoves(xmlStr string, moves map[string]string) string {
	for from, to := range moves {
		xmlStr = strings.ReplaceAll(xmlStr, "'"+esc(from)+"'", "'"+esc(to)+"'")
		xmlStr = strings.ReplaceAll(xmlStr, ">"+esc(from)+"<", ">"+esc(to)+"<") // <nvram>pfad</nvram>
	}
	return xmlStr
}
