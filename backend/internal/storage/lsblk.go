// Package storage enthält die reine Logik für Datenträger, Pools und deren Systemkonfiguration.
package storage

import (
	"encoding/json"
	"fmt"
	"strings"
)

// LsblkColumns sind die Spalten für `lsblk --json --bytes --tree -o <LsblkColumns>`.
const LsblkColumns = "NAME,KNAME,PATH,TYPE,SIZE,MODEL,SERIAL,WWN,TRAN,ROTA,FSTYPE,UUID,LABEL,MOUNTPOINTS"

// Partition ist eine Partition oder ein Dateisystem auf einer Disk.
type Partition struct {
	Device      string   `json:"device"`
	FSType      string   `json:"fsType"`
	UUID        string   `json:"uuid"`
	Label       string   `json:"label"`
	SizeBytes   uint64   `json:"sizeBytes"`
	Mountpoints []string `json:"mountpoints"`
}

// Disk ist ein physischer (oder virtueller) Datenträger.
type Disk struct {
	ID         string      `json:"id"`
	Device     string      `json:"device"`
	Model      string      `json:"model"`
	Serial     string      `json:"serial"`
	WWN        string      `json:"wwn"`
	Transport  string      `json:"transport"`
	SizeBytes  uint64      `json:"sizeBytes"`
	Rotational bool        `json:"rotational"`
	System     bool        `json:"system"`
	Manageable bool        `json:"manageable"`
	Reason     string      `json:"reason,omitempty"`
	Partitions []Partition `json:"partitions"`
}

// Mountpoints liefert alle Mountpoints der Disk und ihrer Partitionen.
func (d Disk) Mountpoints() []string {
	var out []string
	for _, p := range d.Partitions {
		out = append(out, p.Mountpoints...)
	}
	return out
}

type lsblkDev struct {
	Name        string     `json:"name"`
	KName       string     `json:"kname"`
	Path        string     `json:"path"`
	Type        string     `json:"type"`
	Size        uint64     `json:"size"`
	Model       *string    `json:"model"`
	Serial      *string    `json:"serial"`
	WWN         *string    `json:"wwn"`
	Tran        *string    `json:"tran"`
	Rota        flexBool   `json:"rota"`
	FSType      *string    `json:"fstype"`
	UUID        *string    `json:"uuid"`
	Label       *string    `json:"label"`
	Mountpoints []*string  `json:"mountpoints"`
	Children    []lsblkDev `json:"children"`
}

// flexBool akzeptiert true/false sowie "1"/"0" (ältere lsblk-Versionen).
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	*b = s == "true" || s == "1"
	return nil
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

var systemMounts = map[string]bool{"/": true, "/boot": true, "/boot/efi": true, "[SWAP]": true}

// ParseLsblk wertet die Ausgabe von `lsblk --json --bytes --tree -o LsblkColumns` aus.
func ParseLsblk(data []byte, opts IDOptions) ([]Disk, error) {
	var doc struct {
		Blockdevices []lsblkDev `json:"blockdevices"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("lsblk-ausgabe ungültig: %w", err)
	}
	var disks []Disk
	for _, d := range doc.Blockdevices {
		if !includeDevice(d, opts) {
			continue
		}
		disk := Disk{
			Device:     d.Path,
			Model:      str(d.Model),
			Serial:     str(d.Serial),
			WWN:        str(d.WWN),
			Transport:  str(d.Tran),
			SizeBytes:  d.Size,
			Rotational: bool(d.Rota),
			Partitions: []Partition{},
		}
		// Dateisystem direkt auf der Disk (ohne Partitionstabelle)
		if str(d.FSType) != "" {
			disk.Partitions = append(disk.Partitions, toPartition(d))
		}
		collectChildren(d.Children, &disk)
		for _, m := range disk.Mountpoints() {
			if systemMounts[m] {
				disk.System = true
			}
		}
		id, ok := DiskID(disk.Model, disk.Serial, disk.WWN, d.KName, opts)
		disk.ID = id
		switch {
		case disk.System:
			disk.Reason = "Systemplatte"
		case !ok:
			disk.Reason = "Keine eindeutige Kennung"
		default:
			disk.Manageable = true
		}
		disks = append(disks, disk)
	}
	markDuplicates(disks)
	return disks, nil
}

func includeDevice(d lsblkDev, opts IDOptions) bool {
	if d.Size == 0 {
		return false
	}
	for _, p := range []string{"zram", "ram", "sr"} {
		if strings.HasPrefix(d.KName, p) {
			return false
		}
	}
	switch d.Type {
	case "disk":
		return true
	case "loop":
		return opts.AllowLoop
	}
	return false
}

func toPartition(d lsblkDev) Partition {
	p := Partition{Device: d.Path, FSType: str(d.FSType), UUID: str(d.UUID), Label: str(d.Label), SizeBytes: d.Size, Mountpoints: []string{}}
	for _, m := range d.Mountpoints {
		if m != nil && *m != "" {
			p.Mountpoints = append(p.Mountpoints, *m)
		}
	}
	return p
}

// collectChildren übernimmt Partitionen und deren Nachfahren (z. B. LUKS, LVM) als Partitionen.
func collectChildren(children []lsblkDev, disk *Disk) {
	for _, c := range children {
		disk.Partitions = append(disk.Partitions, toPartition(c))
		collectChildren(c.Children, disk)
	}
}

func markDuplicates(disks []Disk) {
	count := map[string]int{}
	for _, d := range disks {
		if d.ID != "" {
			count[d.ID]++
		}
	}
	for i := range disks {
		if disks[i].ID != "" && count[disks[i].ID] > 1 {
			disks[i].Manageable = false
			disks[i].Reason = "Doppelte Kennung (z. B. USB-Gehäuse)"
		}
	}
}
