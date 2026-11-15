package installer

import (
	"fmt"
	"strconv"
)

// PartitionSpec beschreibt eine anzulegende Partition. SizeMiB 0 bedeutet „Rest der Platte“.
type PartitionSpec struct {
	Number   int
	Name     string
	TypeCode string
	FSType   string
	Label    string
	Mount    string
	SizeMiB  uint64
}

// Layout ist der Partitionsplan für die Systemplatte.
type Layout struct {
	Device     string
	Partitions []PartitionSpec
}

const (
	biosBootMiB     = 1
	efiMiB          = 512
	minAppdataMiB   = 8 * 1024
	alignReserveMiB = 2 // GPT-Header vorne und hinten
)

// PlanLayout berechnet den Partitionsplan.
func PlanLayout(t Target, a Answers) (Layout, error) {
	diskMiB := t.SizeBytes >> 20
	rootMiB := uint64(a.RootSizeGiB) * 1024
	fixed := uint64(biosBootMiB + efiMiB + alignReserveMiB)
	if rootMiB+fixed > diskMiB {
		return Layout{}, fmt.Errorf("die Systempartition (%d GiB) passt nicht auf %s", a.RootSizeGiB, t.Device)
	}
	l := Layout{Device: t.Device, Partitions: []PartitionSpec{
		{Number: 1, Name: "BIOS", TypeCode: "EF02", SizeMiB: biosBootMiB},
		{Number: 2, Name: "EFI", TypeCode: "EF00", FSType: "vfat", Label: "EFI", Mount: "/boot/efi", SizeMiB: efiMiB},
		{Number: 3, Name: "bnas-root", TypeCode: "8300", FSType: "ext4", Label: "bnas-root", Mount: "/", SizeMiB: rootMiB},
	}}
	rest := diskMiB - fixed - rootMiB
	if a.CreateAppdata && rest >= minAppdataMiB {
		l.Partitions = append(l.Partitions, PartitionSpec{Number: 4, Name: "bnas-appdata", TypeCode: "8300", FSType: "ext4", Label: "bnas-appdata", Mount: "/srv/appdata"})
	} else {
		l.Partitions[2].SizeMiB = 0
	}
	return l, nil
}

// HasAppdata meldet, ob eine Appdata-Partition angelegt wird.
func (l Layout) HasAppdata() bool { return len(l.Partitions) == 4 }

// PartitionDevice liefert den Gerätenamen der n-ten Partition.
func PartitionDevice(disk string, n int) string {
	if disk == "" {
		return ""
	}
	last := disk[len(disk)-1]
	if last >= '0' && last <= '9' {
		return disk + "p" + strconv.Itoa(n)
	}
	return disk + strconv.Itoa(n)
}
