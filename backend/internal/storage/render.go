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
	members := btrfsMembers(s.Pools)
	for _, d := range s.Disks {
		if members[d.DiskID] != "" {
			continue // Mitglied eines btrfs-Pools: der Pool hängt das gemeinsame Dateisystem ein
		}
		where := d.Path()
		what := "Datenplatte"
		if d.IsParity() {
			what = "Paritätsplatte"
		}
		// WantedBy auf die Geräte-Unit: Kehrt die Platte zurück, hängt systemd sie von selbst ein.
		device := EscapePath("/dev/disk/by-uuid/"+d.FSUUID) + ".device"
		out[UnitName(where)] = []byte(fmt.Sprintf(`%s
[Unit]
Description=BetterNAS %s %s

[Mount]
What=/dev/disk/by-uuid/%s
Where=%s
Type=%s
Options=defaults,noatime,nofail

[Install]
WantedBy=multi-user.target %s
`, ManagedHeader, what, d.DiskID, d.FSUUID, where, d.FSType, device))
	}
	for _, p := range s.Pools {
		if p.IsBtrfs() {
			out[UnitName(PoolPath(p.Name))] = renderBtrfsPool(p)
			continue
		}
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
Options=category.create=%s,minfreespace=%s,moveonenospc=true,cache.files=off,dropcacheonclose=true,noforget,inodecalc=path-hash,nfsopenhack=all,fsname=bnas-%s,nofail

[Install]
WantedBy=multi-user.target %s
`, ManagedHeader, p.Name, strings.Join(units, " "), strings.Join(units, " "),
			strings.Join(paths, ":"), where, p.CreatePolicy, p.MinFree, p.Name, strings.Join(units, " ")))
	}
	return out, nil
}

// renderBtrfsPool erzeugt die Mount-Unit eines btrfs-Pools. Alle Mitglieder tragen dieselbe
// Dateisystem-UUID; die Geräte-Unit wird erst aktiv, wenn btrfs alle Mitglieder gesehen hat
// (udev-Regel „btrfs ready“). Fehlt eine Platte, hängt der Pool bewusst nicht von selbst ein.
func renderBtrfsPool(p Pool) []byte {
	device := EscapePath("/dev/disk/by-uuid/"+p.FSUUID) + ".device"
	options := BtrfsMountOptions(p.Compress, false)
	return []byte(fmt.Sprintf(`%s
[Unit]
Description=BetterNAS Pool %s (btrfs, %s)

[Mount]
What=/dev/disk/by-uuid/%s
Where=%s
Type=btrfs
Options=%s

[Install]
WantedBy=multi-user.target %s
`, ManagedHeader, p.Name, p.Profile, p.FSUUID, PoolPath(p.Name), options, device))
}

// BtrfsMountOptions liefert die Mount-Optionen eines btrfs-Pools. degraded erlaubt das Einhängen mit fehlenden Platten.
func BtrfsMountOptions(compress string, degraded bool) string {
	o := "noatime,nofail"
	if compress != "" {
		o += ",compress=" + compress
	}
	if degraded {
		o += ",degraded"
	}
	return o
}

// BtrfsMetadataProfile wählt das Metadaten-Profil zu einem Daten-Profil: Bei „single“ über mehrere Platten
// liegen die Metadaten gespiegelt (raid1), damit der Ausfall einer Platte die übrigen Dateien nicht mitreißt;
// bei einer einzelnen Platte dup. Alle anderen Profile gelten für Daten und Metadaten.
func BtrfsMetadataProfile(profile string, disks int) string {
	if profile != "single" {
		return profile
	}
	if disks > 1 {
		return "raid1"
	}
	return "dup"
}
