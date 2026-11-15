package installer

import (
	"strings"

	"github.com/betternas/betternas/internal/storage"
)

const (
	// MinDiskBytes ist die Mindestgröße einer Systemplatte (24 GiB).
	MinDiskBytes = 24 << 30
	// LiveMediumMount ist der Mountpoint des Boot-Mediums im Live-System.
	LiveMediumMount = "/run/live/medium"
)

// Target ist ein möglicher Systemdatenträger.
type Target struct {
	Device     string
	Model      string
	Serial     string
	SizeBytes  uint64
	Rotational bool
	HasData    bool
	Warning    string
}

// Targets liefert alle Datenträger, auf die installiert werden darf.
func Targets(disks []storage.Disk, liveMediumDevice string) []Target {
	var out []Target
	for _, d := range disks {
		if d.Device == liveMediumDevice || d.SizeBytes < MinDiskBytes {
			continue
		}
		t := Target{Device: d.Device, Model: d.Model, Serial: d.Serial, SizeBytes: d.SizeBytes, Rotational: d.Rotational, HasData: len(d.Partitions) > 0}
		if d.Rotational {
			t.Warning = "HDD als Systemplatte: langsamer, Platte kann nicht schlafen"
		}
		out = append(out, t)
	}
	return out
}

// LiveMediumDevice ermittelt die Disk, von der das Live-System gestartet wurde.
func LiveMediumDevice(mountinfo []byte, disks []storage.Disk) string {
	src := ""
	for _, line := range strings.Split(string(mountinfo), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 || fields[4] != LiveMediumMount {
			continue
		}
		for i, f := range fields {
			if f == "-" && i+2 < len(fields) {
				src = fields[i+2]
			}
		}
	}
	if src == "" {
		return ""
	}
	for _, d := range disks {
		if d.Device == src {
			return d.Device
		}
		for _, p := range d.Partitions {
			if p.Device == src {
				return d.Device
			}
		}
	}
	return ""
}
