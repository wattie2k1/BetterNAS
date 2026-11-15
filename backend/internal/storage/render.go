package storage

import (
	"fmt"
	"strings"
)

// ManagedHeader steht in der ersten Zeile jeder von BetterNAS erzeugten Datei.
const ManagedHeader = "# Verwaltet von BetterNAS – nicht manuell bearbeiten"

// RenderUnits erzeugt alle Mount-Units (Dateiname → Inhalt).
func RenderUnits(s State) (map[string][]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, d := range s.Disks {
		where := DiskPath(d.DiskID)
		// WantedBy auf die Geräte-Unit: Kehrt die Platte zurück, hängt systemd sie von selbst ein.
		device := EscapePath("/dev/disk/by-uuid/"+d.FSUUID) + ".device"
		out[UnitName(where)] = []byte(fmt.Sprintf(`%s
[Unit]
Description=BetterNAS Datenplatte %s

[Mount]
What=/dev/disk/by-uuid/%s
Where=%s
Type=%s
Options=defaults,noatime,nofail

[Install]
WantedBy=multi-user.target %s
`, ManagedHeader, d.DiskID, d.FSUUID, where, d.FSType, device))
	}
	for _, p := range s.Pools {
		var paths, units []string
		for _, b := range p.Branches {
			paths = append(paths, DiskPath(b))
			units = append(units, UnitName(DiskPath(b)))
		}
		where := PoolPath(p.Name)
		// Wants statt Requires: Fehlt eine Platte, hängt der Pool mit den übrigen ein.
		// WantedBy auf die Platten-Units: Wird eine Platte (wieder) eingehängt, startet auch der Pool.
		out[UnitName(where)] = []byte(fmt.Sprintf(`%s
[Unit]
Description=BetterNAS Pool %s
Wants=%s
After=%s

[Mount]
What=%s
Where=%s
Type=fuse.mergerfs
Options=category.create=%s,minfreespace=%s,moveonenospc=true,cache.files=off,dropcacheonclose=true,fsname=bnas-%s,nofail

[Install]
WantedBy=multi-user.target %s
`, ManagedHeader, p.Name, strings.Join(units, " "), strings.Join(units, " "),
			strings.Join(paths, ":"), where, p.CreatePolicy, p.MinFree, p.Name, strings.Join(units, " ")))
	}
	return out, nil
}
