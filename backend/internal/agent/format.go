package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/storage"
)

// FormatRequest beschreibt die zu formatierende Platte so, wie der Nutzer sie bestätigt hat.
type FormatRequest struct {
	DiskID    string `json:"diskId"`
	Device    string `json:"device"`
	SizeBytes uint64 `json:"sizeBytes"`
	FSType    string `json:"fsType"`
}

// FormatResult ist das Ergebnis einer Formatierung.
type FormatResult struct {
	Partition string `json:"partition"`
	FSUUID    string `json:"fsUuid"`
	FSType    string `json:"fsType"`
	Label     string `json:"label"`
}

// Format löscht eine Datenplatte und legt eine Partition mit Dateisystem an.
// Vorher wird geprüft, dass die Platte noch genau die bestätigte ist und nichts eingehängt ist.
func Format(ctx context.Context, d Deps, req FormatRequest) (FormatResult, error) {
	if req.FSType != "ext4" && req.FSType != "xfs" {
		return FormatResult{}, apierr.New(apierr.InvalidParams, "Dateisystem muss ext4 oder xfs sein")
	}
	disks, err := ListDisks(ctx, d)
	if err != nil {
		return FormatResult{}, err
	}
	disk, ok := findDisk(disks, func(x storage.Disk) bool { return x.ID == req.DiskID })
	switch {
	case !ok:
		return FormatResult{}, apierr.New(apierr.NotFound, "Datenträger nicht gefunden")
	case disk.System:
		return FormatResult{}, apierr.New(apierr.SystemDisk, "Die Systemplatte kann nicht formatiert werden")
	case !disk.Manageable:
		return FormatResult{}, apierr.New(apierr.Conflict, "Datenträger kann nicht verwaltet werden: "+disk.Reason)
	case disk.Device != req.Device || disk.SizeBytes != req.SizeBytes:
		return FormatResult{}, apierr.New(apierr.Conflict, "Der Datenträger hat sich seit der Vorschau verändert. Bitte erneut prüfen.")
	case len(disk.Mountpoints()) > 0:
		return FormatResult{}, apierr.New(apierr.Conflict, "Der Datenträger ist eingehängt: "+strings.Join(disk.Mountpoints(), ", "))
	}

	dev := disk.Device
	steps := [][]string{
		{"wipefs", "--all", dev},
		{"sgdisk", "--zap-all", dev},
		{"sgdisk", "--new=1:0:0", "--typecode=1:8300", "--change-name=1:bnas-data", dev},
		{"partprobe", dev},
		{"udevadm", "settle"},
	}
	for _, s := range steps {
		if _, err := run(ctx, d.Runner, nil, s[0], s[1:]...); err != nil {
			return FormatResult{}, err
		}
	}
	out, err := run(ctx, d.Runner, nil, "lsblk", "--json", "--bytes", "-o", "PATH,TYPE", dev)
	if err != nil {
		return FormatResult{}, err
	}
	part := firstPartition(out)
	if part == "" {
		return FormatResult{}, apierr.New(apierr.Internal, "Neue Partition wurde nicht gefunden")
	}
	label := storage.FSLabel(disk.ID)
	if req.FSType == "xfs" {
		_, err = run(ctx, d.Runner, nil, "mkfs.xfs", "-f", "-L", label, part)
	} else {
		_, err = run(ctx, d.Runner, nil, "mkfs.ext4", "-F", "-m", "0", "-L", label, part)
	}
	if err != nil {
		return FormatResult{}, err
	}
	uuid, err := run(ctx, d.Runner, nil, "blkid", "-s", "UUID", "-o", "value", part)
	if err != nil {
		return FormatResult{}, err
	}
	return FormatResult{Partition: part, FSUUID: strings.TrimSpace(string(uuid)), FSType: req.FSType, Label: label}, nil
}

type pathType struct {
	Path     string     `json:"path"`
	Type     string     `json:"type"`
	Children []pathType `json:"children"`
}

func firstPartition(lsblkJSON []byte) string {
	var doc struct {
		Blockdevices []pathType `json:"blockdevices"`
	}
	if json.Unmarshal(lsblkJSON, &doc) != nil {
		return ""
	}
	var walk func([]pathType) string
	walk = func(list []pathType) string {
		for _, p := range list {
			if p.Type == "part" {
				return p.Path
			}
			if s := walk(p.Children); s != "" {
				return s
			}
		}
		return ""
	}
	return walk(doc.Blockdevices)
}
