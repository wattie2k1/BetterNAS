package agent

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/storage"
)

const (
	opUnitPrefix = "betternas-btrfs-op-"
	btrfsBin     = "/usr/bin/btrfs"
)

func opUnit(name string) string { return opUnitPrefix + name + ".service" }

// mountedPool prüft Poolname und Einhängezustand und liefert den Mountpoint.
func mountedPool(ctx context.Context, d Deps, name string) (string, error) {
	if !storage.PoolNameRe.MatchString(name) {
		return "", apierr.New(apierr.InvalidParams, "Ungültiger Poolname")
	}
	where := storage.PoolPath(name)
	if !isMounted(ctx, d, where) {
		return "", apierr.New(apierr.Conflict, "Der Pool ist nicht eingehängt.")
	}
	return where, nil
}

// busyCheck lehnt ab, wenn schon eine Wartung oder ein Plattenersatz läuft (btrfs erlaubt nur eine zurzeit).
func busyCheck(ctx context.Context, d Deps, name, where string) error {
	if op, err := BtrfsOpStatus(ctx, d, name); err == nil && op.Running {
		return apierr.New(apierr.Conflict, "Es läuft bereits eine Wartung ("+op.Op+").")
	}
	if r := storage.ParseBtrfsReplace(output(ctx, d, btrfsBin, "replace", "status", "-1", where)); r.State == "running" {
		return apierr.New(apierr.Conflict, "Es läuft bereits ein Plattenersatz.")
	}
	return nil
}

func prepareTarget(ctx context.Context, d Deps, want storage.BtrfsDisk) (string, error) {
	disks, err := ListDisks(ctx, d)
	if err != nil {
		return "", err
	}
	disk, err := resolveTarget(disks, want)
	if err != nil {
		return "", err
	}
	return partitionDisk(ctx, d, disk.Device)
}

// BtrfsReplaceStart ersetzt eine Platte (devId) durch eine neue; btrfs kopiert im Hintergrund.
// Die neue Platte wird dabei gelöscht und neu partitioniert.
func BtrfsReplaceStart(ctx context.Context, d Deps, req storage.BtrfsReplaceRequest) error {
	if !storage.UUIDValid(req.FSUUID) || req.DevID <= 0 {
		return apierr.New(apierr.InvalidParams, "Ungültige Anfrage")
	}
	where, err := mountedPool(ctx, d, req.Name)
	if err != nil {
		return err
	}
	if err := busyCheck(ctx, d, req.Name, where); err != nil {
		return err
	}
	part, err := prepareTarget(ctx, d, req.Target)
	if err != nil {
		return err
	}
	_, err = run(ctx, d.Runner, nil, btrfsBin, "replace", "start", "-f", strconv.Itoa(req.DevID), part, where)
	return err
}

// BtrfsReplaceStatus liefert den Stand des Plattenersatzes.
func BtrfsReplaceStatus(ctx context.Context, d Deps, name string) (storage.BtrfsReplace, error) {
	where, err := mountedPool(ctx, d, name)
	if err != nil {
		return storage.BtrfsReplace{}, err
	}
	return storage.ParseBtrfsReplace(output(ctx, d, btrfsBin, "replace", "status", "-1", where)), nil
}

// BtrfsDeviceAdd fügt dem eingehängten Pool eine neue Platte hinzu (sie wird gelöscht).
func BtrfsDeviceAdd(ctx context.Context, d Deps, req storage.BtrfsAddRequest) error {
	where, err := mountedPool(ctx, d, req.Name)
	if err != nil {
		return err
	}
	if err := busyCheck(ctx, d, req.Name, where); err != nil {
		return err
	}
	part, err := prepareTarget(ctx, d, req.Target)
	if err != nil {
		return err
	}
	_, err = run(ctx, d.Runner, nil, btrfsBin, "device", "add", "-f", part, where)
	return err
}

// BtrfsOpStart startet eine Wartung (Balance, Platte entfernen, Profilwechsel) in einer eigenen Unit.
func BtrfsOpStart(ctx context.Context, d Deps, req storage.BtrfsOpRequest) error {
	where, err := mountedPool(ctx, d, req.Name)
	if err != nil {
		return err
	}
	var args []string
	switch req.Op {
	case storage.OpBalance:
		if req.Usage < 0 || req.Usage > 100 {
			return apierr.New(apierr.InvalidParams, "Füllgrad muss zwischen 0 und 100 liegen")
		}
		args = []string{"balance", "start"}
		if req.Usage > 0 {
			args = append(args, fmt.Sprintf("-dusage=%d", req.Usage), fmt.Sprintf("-musage=%d", req.Usage))
		}
	case storage.OpConvert:
		if !slices.Contains(storage.BtrfsProfiles, req.Profile) || !(req.Metadata == "dup" || slices.Contains(storage.BtrfsProfiles, req.Metadata)) {
			return apierr.New(apierr.InvalidParams, "Unbekanntes btrfs-Profil")
		}
		args = []string{"balance", "start", "-f", "-dconvert=" + req.Profile, "-mconvert=" + req.Metadata}
	case storage.OpRemove:
		switch {
		case req.Missing:
			args = []string{"device", "remove", "missing"}
		case req.DevID > 0:
			args = []string{"device", "remove", strconv.Itoa(req.DevID)}
		default:
			return apierr.New(apierr.InvalidParams, "Platte fehlt in der Anfrage")
		}
	default:
		return apierr.New(apierr.InvalidParams, "Unbekannte Wartung")
	}
	if err := busyCheck(ctx, d, req.Name, where); err != nil {
		return err
	}
	// Ergebnis einer früheren Wartung wegräumen, damit die Unit-Kennung frei ist.
	_, _ = d.Runner.Run(ctx, nil, "systemctl", "stop", opUnit(req.Name))
	_, _ = d.Runner.Run(ctx, nil, "systemctl", "reset-failed", opUnit(req.Name))
	cmd := []string{"--unit=" + opUnit(req.Name), "--description=BetterNAS btrfs " + req.Op, "--property=RemainAfterExit=yes", btrfsBin}
	cmd = append(cmd, args...)
	cmd = append(cmd, where)
	_, err = run(ctx, d.Runner, nil, "systemd-run", cmd...)
	return err
}

// BtrfsOpStatus liefert den Stand der letzten oder laufenden Wartung (Op leer: noch nie gelaufen).
func BtrfsOpStatus(ctx context.Context, d Deps, name string) (storage.BtrfsOp, error) {
	if !storage.PoolNameRe.MatchString(name) {
		return storage.BtrfsOp{}, apierr.New(apierr.InvalidParams, "Ungültiger Poolname")
	}
	out := output(ctx, d, "systemctl", "show", "-p", "LoadState", "-p", "ActiveState", "-p", "SubState", "-p", "Result", "-p", "Description", opUnit(name))
	props := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			props[k] = strings.TrimSpace(v)
		}
	}
	if props["LoadState"] == "" || props["LoadState"] == "not-found" {
		return storage.BtrfsOp{Progress: -1}, nil
	}
	op := storage.BtrfsOp{Op: props["Description"][strings.LastIndex(props["Description"], " ")+1:], Progress: -1}
	running := props["ActiveState"] == "activating" || props["SubState"] == "running" || props["SubState"] == "start"
	failed := props["ActiveState"] == "failed" || (props["Result"] != "" && props["Result"] != "success")
	switch {
	case running:
		op.Running = true
		if op.Op == storage.OpBalance || op.Op == storage.OpConvert {
			if p, ok := storage.ParseBtrfsBalanceProgress(output(ctx, d, btrfsBin, "balance", "status", storage.PoolPath(name))); ok {
				op.Progress = p
			}
		}
	case failed:
		op.Result = "failed"
		if res, err := d.Runner.Run(ctx, nil, "journalctl", "-u", opUnit(name), "-o", "cat", "-n", "5", "--no-pager"); err == nil {
			op.Message = strings.TrimSpace(string(res.Stdout))
		}
	default:
		op.Result = "ok"
		op.Progress = 100
	}
	return op, nil
}

// BtrfsMountDegraded hängt einen unvollständigen Pool bis zum nächsten Neustart mit „degraded“ ein.
// Direkt per mount statt über die Unit: Die Unit wartet auf das Gerät, das btrfs erst als fertig meldet,
// wenn alle Platten da sind.
func BtrfsMountDegraded(ctx context.Context, d Deps, req storage.BtrfsMountRequest) error {
	if !storage.PoolNameRe.MatchString(req.Name) || !storage.UUIDValid(req.FSUUID) || !slices.Contains(storage.BtrfsCompress, req.Compress) {
		return apierr.New(apierr.InvalidParams, "Ungültige Anfrage")
	}
	where := storage.PoolPath(req.Name)
	if isMounted(ctx, d, where) {
		return apierr.New(apierr.Conflict, "Der Pool ist bereits eingehängt.")
	}
	_, err := run(ctx, d.Runner, nil, "mount", "-t", "btrfs", "-o", storage.BtrfsMountOptions(req.Compress, true), "/dev/disk/by-uuid/"+req.FSUUID, where)
	return err
}

// BtrfsResetStats setzt die Fehlerzähler aller Platten des Pools auf null.
func BtrfsResetStats(ctx context.Context, d Deps, name string) error {
	where, err := mountedPool(ctx, d, name)
	if err != nil {
		return err
	}
	_, err = run(ctx, d.Runner, nil, btrfsBin, "device", "stats", "-z", where)
	return err
}

// BtrfsResize vergrößert eine Platte des Pools auf ihre volle Größe.
func BtrfsResize(ctx context.Context, d Deps, req storage.BtrfsResizeRequest) error {
	if req.DevID <= 0 {
		return apierr.New(apierr.InvalidParams, "Ungültige Anfrage")
	}
	where, err := mountedPool(ctx, d, req.Name)
	if err != nil {
		return err
	}
	_, err = run(ctx, d.Runner, nil, btrfsBin, "filesystem", "resize", strconv.Itoa(req.DevID)+":max", where)
	return err
}
