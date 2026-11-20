package agent

import (
	"context"
	"log/slog"
	"path"
	"slices"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/storage"
)

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
		disk, err := resolveTarget(disks, want)
		if err != nil {
			return storage.BtrfsCreateResult{}, err
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
	args := append([]string{"-f", "-L", label, "-d", req.Profile, "-m", storage.BtrfsMetadataProfile(req.Profile, len(parts))}, parts...)
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

// BtrfsStatusRequest nennt den Pool, dessen Zustand gelesen werden soll.
type BtrfsStatusRequest struct {
	Name   string `json:"name"`
	FSUUID string `json:"fsUuid"`
}

// BtrfsScrubRequest startet oder bricht eine Prüfung ab.
type BtrfsScrubRequest struct {
	Name   string `json:"name"`
	Action string `json:"action"` // start | cancel
}

const scrubUnitPrefix = "betternas-btrfs-scrub@"

func scrubService(name string) string { return scrubUnitPrefix + name + ".service" }
func scrubTimer(name string) string   { return scrubUnitPrefix + name + ".timer" }

// output führt einen Befehl aus und liefert die Ausgabe; bei Fehlern (z. B. Pool nicht eingehängt) leer.
func output(ctx context.Context, d Deps, name string, args ...string) string {
	res, err := d.Runner.Run(ctx, nil, name, args...)
	if err != nil || res.ExitCode != 0 {
		return ""
	}
	return string(res.Stdout)
}

func isMounted(ctx context.Context, d Deps, where string) bool {
	res, err := d.Runner.Run(ctx, nil, "findmnt", "--mountpoint", where)
	return err == nil && res.ExitCode == 0
}

// BtrfsStatus liest Zustand, Belegung, Fehlerzähler und Prüfung eines btrfs-Pools.
// Fehlende Platten erkennt es auch bei nicht eingehängtem Pool (btrfs filesystem show).
func BtrfsStatus(ctx context.Context, d Deps, req BtrfsStatusRequest) (storage.BtrfsStatus, error) {
	if !storage.PoolNameRe.MatchString(req.Name) {
		return storage.BtrfsStatus{}, apierr.New(apierr.InvalidParams, "Ungültiger Poolname")
	}
	if !storage.UUIDValid(req.FSUUID) {
		return storage.BtrfsStatus{}, apierr.New(apierr.InvalidParams, "Ungültige Dateisystem-UUID")
	}
	where := storage.PoolPath(req.Name)
	devs, missing := storage.ParseBtrfsShow(output(ctx, d, "btrfs", "filesystem", "show", "--raw", req.FSUUID))
	mounted := isMounted(ctx, d, where)
	var stats map[string]storage.BtrfsErrors
	var usage storage.BtrfsUsage
	var scrub *storage.BtrfsScrub
	var op *storage.BtrfsOp
	var rep *storage.BtrfsReplace
	if mounted {
		if o, err := BtrfsOpStatus(ctx, d, req.Name); err == nil && o.Op != "" {
			op = &o
		}
		if r := storage.ParseBtrfsReplace(output(ctx, d, btrfsBin, "replace", "status", "-1", where)); r.State != "never" {
			rep = &r
		}
		stats = storage.ParseBtrfsStats(output(ctx, d, "btrfs", "device", "stats", where))
		usage = storage.ParseBtrfsUsage(output(ctx, d, "btrfs", "filesystem", "usage", "-b", where))
		scrub = storage.ParseBtrfsScrub(output(ctx, d, "btrfs", "scrub", "status", "-R", where))
	}
	st := storage.AssembleBtrfsStatus(devs, missing, stats, usage, mounted, scrub)
	st.Op, st.Replace = op, rep
	return st, nil
}

// BtrfsScrub startet eine Prüfung (über die Dienst-Unit, die auch der Timer nutzt) oder bricht sie ab.
func BtrfsScrub(ctx context.Context, d Deps, req BtrfsScrubRequest) error {
	if !storage.PoolNameRe.MatchString(req.Name) {
		return apierr.New(apierr.InvalidParams, "Ungültiger Poolname")
	}
	where := storage.PoolPath(req.Name)
	if !isMounted(ctx, d, where) {
		return apierr.New(apierr.Conflict, "Der Pool ist nicht eingehängt.")
	}
	running := storage.ParseBtrfsScrub(output(ctx, d, "btrfs", "scrub", "status", "-R", where))
	isRunning := running != nil && running.Running
	switch req.Action {
	case "start":
		if isRunning {
			return apierr.New(apierr.Conflict, "Die Prüfung läuft bereits.")
		}
		_, err := run(ctx, d.Runner, nil, "systemctl", "start", "--no-block", scrubService(req.Name))
		return err
	case "cancel":
		if !isRunning {
			return apierr.New(apierr.Conflict, "Es läuft keine Prüfung.")
		}
		_, err := run(ctx, d.Runner, nil, "btrfs", "scrub", "cancel", where)
		return err
	}
	return apierr.New(apierr.InvalidParams, "Aktion muss start oder cancel sein")
}

const scrubDropIn = "/etc/systemd/system/betternas-btrfs-scrub@.timer.d/10-betternas.conf"

// syncScrubTimers stellt die Prüf-Timer der btrfs-Pools ein: schedule "" = Standard (monatlich), "off" = aus,
// sonst ein OnCalendar-Ausdruck (als Drop-in der Timer-Vorlage). Für entfernte Pools (stale) schaltet es den Timer aus.
func syncScrubTimers(ctx context.Context, d Deps, pools []storage.Pool, stale []string, schedule string) {
	changed := syncScrubDropIn(ctx, d, schedule, pools)
	for _, p := range pools {
		if !p.IsBtrfs() {
			continue
		}
		if schedule == "off" {
			_, _ = d.Runner.Run(ctx, nil, "systemctl", "disable", "--now", scrubTimer(p.Name))
			continue
		}
		if res, err := d.Runner.Run(ctx, nil, "systemctl", "enable", "--now", scrubTimer(p.Name)); err != nil || res.ExitCode != 0 {
			slog.Warn("scrub-timer konnte nicht aktiviert werden", "pool", p.Name)
		}
		if changed {
			_, _ = d.Runner.Run(ctx, nil, "systemctl", "restart", scrubTimer(p.Name))
		}
	}
	for _, name := range stale {
		_, _ = d.Runner.Run(ctx, nil, "systemctl", "disable", "--now", scrubTimer(name))
	}
}

// syncScrubDropIn schreibt oder entfernt das Drop-in mit dem Zeitplan. Es meldet, ob sich etwas geändert hat.
func syncScrubDropIn(ctx context.Context, d Deps, schedule string, pools []storage.Pool) bool {
	hasBtrfs := slices.ContainsFunc(pools, func(p storage.Pool) bool { return p.IsBtrfs() })
	want := ""
	if schedule != "" && schedule != "off" {
		want = storage.ManagedHeader + "\n[Timer]\nOnCalendar=\nOnCalendar=" + schedule + "\n"
	}
	cur, err := d.FS.ReadFile(scrubDropIn)
	exists := err == nil
	switch {
	case want == "" && exists:
		_ = d.FS.Remove(scrubDropIn)
	case want != "" && hasBtrfs && (!exists || string(cur) != want):
		if err := d.FS.MkdirAll(path.Dir(scrubDropIn), 0o755); err != nil {
			slog.Warn("scrub-zeitplan konnte nicht geschrieben werden", "err", err)
			return false
		}
		if err := d.FS.WriteFile(scrubDropIn, []byte(want), 0o644); err != nil {
			slog.Warn("scrub-zeitplan konnte nicht geschrieben werden", "err", err)
			return false
		}
	default:
		return false
	}
	_, _ = d.Runner.Run(ctx, nil, "systemctl", "daemon-reload")
	return true
}

// resolveTarget prüft eine vom Nutzer bestätigte Platte: vorhanden, verwaltbar, keine Systemplatte,
// Gerät und Größe unverändert, nicht eingehängt.
func resolveTarget(disks []storage.Disk, want storage.BtrfsDisk) (storage.Disk, error) {
	disk, ok := findDisk(disks, func(x storage.Disk) bool { return x.ID == want.DiskID })
	switch {
	case !ok:
		return storage.Disk{}, apierr.New(apierr.NotFound, "Datenträger nicht gefunden: "+want.DiskID)
	case disk.System:
		return storage.Disk{}, apierr.New(apierr.SystemDisk, "Die Systemplatte kann nicht verwendet werden")
	case !disk.Manageable:
		return storage.Disk{}, apierr.New(apierr.Conflict, "Datenträger kann nicht verwaltet werden: "+disk.Reason)
	case disk.Device != want.Device || disk.SizeBytes != want.SizeBytes:
		return storage.Disk{}, apierr.New(apierr.Conflict, "Ein Datenträger hat sich seit der Vorschau verändert. Bitte erneut prüfen.")
	case len(disk.Mountpoints()) > 0:
		return storage.Disk{}, apierr.New(apierr.Conflict, "Der Datenträger ist eingehängt: "+strings.Join(disk.Mountpoints(), ", "))
	}
	return disk, nil
}
