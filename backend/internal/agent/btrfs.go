package agent

import (
	"context"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/storage"
)

// btrfsMetadata wählt das Metadaten-Profil: Bei „single“ über mehrere Platten liegen die Metadaten
// gespiegelt, damit der Ausfall einer Platte die übrigen Dateien nicht mitreißt.
func btrfsMetadata(profile string, disks int) string {
	if profile != "single" {
		return profile
	}
	if disks > 1 {
		return "raid1"
	}
	return "dup"
}

// BtrfsCreate löscht die bestätigten Platten und legt ein gemeinsames btrfs-Dateisystem an.
// Jede Platte wird vorher gegen die Bestätigung des Nutzers geprüft (Gerät, Größe, nicht eingehängt).
func BtrfsCreate(ctx context.Context, d Deps, req storage.BtrfsCreateRequest) (storage.BtrfsCreateResult, error) {
	if !storage.PoolNameRe.MatchString(req.Name) {
		return storage.BtrfsCreateResult{}, apierr.New(apierr.InvalidParams, "Ungültiger Poolname")
	}
	min := storage.BtrfsMinDisks(req.Profile)
	if min == 0 {
		return storage.BtrfsCreateResult{}, apierr.New(apierr.InvalidParams, "Unbekanntes btrfs-Profil")
	}
	if len(req.Disks) < min {
		return storage.BtrfsCreateResult{}, apierr.New(apierr.InvalidParams, "Zu wenige Datenträger für dieses Profil")
	}
	disks, err := ListDisks(ctx, d)
	if err != nil {
		return storage.BtrfsCreateResult{}, err
	}
	seen := map[string]bool{}
	var devices []string
	for _, want := range req.Disks {
		if seen[want.DiskID] {
			return storage.BtrfsCreateResult{}, apierr.New(apierr.InvalidParams, "Datenträger doppelt: "+want.DiskID)
		}
		seen[want.DiskID] = true
		disk, ok := findDisk(disks, func(x storage.Disk) bool { return x.ID == want.DiskID })
		switch {
		case !ok:
			return storage.BtrfsCreateResult{}, apierr.New(apierr.NotFound, "Datenträger nicht gefunden: "+want.DiskID)
		case disk.System:
			return storage.BtrfsCreateResult{}, apierr.New(apierr.SystemDisk, "Die Systemplatte kann nicht verwendet werden")
		case !disk.Manageable:
			return storage.BtrfsCreateResult{}, apierr.New(apierr.Conflict, "Datenträger kann nicht verwaltet werden: "+disk.Reason)
		case disk.Device != want.Device || disk.SizeBytes != want.SizeBytes:
			return storage.BtrfsCreateResult{}, apierr.New(apierr.Conflict, "Ein Datenträger hat sich seit der Vorschau verändert. Bitte erneut prüfen.")
		case len(disk.Mountpoints()) > 0:
			return storage.BtrfsCreateResult{}, apierr.New(apierr.Conflict, "Der Datenträger ist eingehängt: "+strings.Join(disk.Mountpoints(), ", "))
		}
		devices = append(devices, disk.Device)
	}
	var parts []string
	for _, dev := range devices {
		part, err := partitionDisk(ctx, d, dev)
		if err != nil {
			return storage.BtrfsCreateResult{}, err
		}
		parts = append(parts, part)
	}
	label := storage.BtrfsLabel(req.Name)
	args := append([]string{"-f", "-L", label, "-d", req.Profile, "-m", btrfsMetadata(req.Profile, len(parts))}, parts...)
	if _, err := run(ctx, d.Runner, nil, "mkfs.btrfs", args...); err != nil {
		return storage.BtrfsCreateResult{}, err
	}
	if _, err := run(ctx, d.Runner, nil, "udevadm", "settle"); err != nil {
		return storage.BtrfsCreateResult{}, err
	}
	uuid, err := run(ctx, d.Runner, nil, "blkid", "-s", "UUID", "-o", "value", parts[0])
	if err != nil {
		return storage.BtrfsCreateResult{}, err
	}
	return storage.BtrfsCreateResult{FSUUID: strings.TrimSpace(string(uuid)), Label: label}, nil
}
