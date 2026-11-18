package vm

import (
	"encoding/json"
	"errors"
	"path"
	"strings"
)

// Disk ist eine Festplatte einer VM mit ihrer Größe.
type Disk struct {
	Path        string `json:"path"`
	SizeGiB     int    `json:"sizeGiB"`     // virtuelle Größe, aufgerundet
	UsedBytes   int64  `json:"usedBytes"`   // tatsächlich belegter Platz auf dem Host
	VirtualSize int64  `json:"virtualSize"` // virtuelle Größe in Bytes
}

// DiskResize ist die Anforderung, eine Festplatte zu vergrößern.
type DiskResize struct {
	Path            string `json:"path"`
	SizeGiB         int    `json:"sizeGiB"`
	DeleteSnapshots bool   `json:"deleteSnapshots"` // vorhandene Snapshots dafür löschen (qcow2 erlaubt sonst kein Vergrößern)
}

const GiB = int64(1) << 30

// Validate prüft Pfad und Zielgröße.
func (r DiskResize) Validate() error {
	switch {
	case path.Clean(r.Path) != r.Path || !strings.HasPrefix(r.Path, DiskDir+"/") || strings.Contains(r.Path, "'"):
		return invalid("Ungültiger Festplattenpfad")
	case r.SizeGiB < MinDiskGiB || r.SizeGiB > MaxDiskGiB:
		return invalid("Festplatte: 1 bis 65536 GiB")
	}
	return nil
}

// ParseImageInfo liest `qemu-img info --output=json` (virtuelle Größe und belegter Platz in Bytes).
func ParseImageInfo(out []byte) (virtual, actual int64, err error) {
	var i struct {
		Virtual int64 `json:"virtual-size"`
		Actual  int64 `json:"actual-size"`
	}
	if err = json.Unmarshal(out, &i); err != nil {
		return 0, 0, err
	}
	if i.Virtual <= 0 {
		return 0, 0, errors.New("keine Größe im qemu-img-Ergebnis")
	}
	return i.Virtual, i.Actual, nil
}
