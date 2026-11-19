package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/storage"
)

// FsCheckState ist der Zustand einer Dateisystem-Prüfung (laufend oder abgeschlossen).
type FsCheckState struct {
	Running bool   `json:"running"`
	Known   bool   `json:"known"` // false: für dieses Gerät gibt es keinen Lauf (z. B. nach Neustart des Agents)
	Result  string `json:"result,omitempty"`
	Online  bool   `json:"online"` // Dateisystem war während der Prüfung eingehängt
	Tool    string `json:"tool,omitempty"`
	Output  string `json:"output,omitempty"`
}

const (
	fsOK     = "ok"
	fsErrors = "errors"
	fsFailed = "failed"

	fsCheckLimit  = 12 * time.Hour
	fsOutputLimit = 6000
)

type fsCheckRun struct {
	device string
	FsCheckState
}

var fsChecks = struct {
	sync.Mutex
	run *fsCheckRun // der zuletzt gestartete Lauf; es läuft immer nur einer, um die Platten nicht zu überlasten
}{}

// fsCheckCommand wählt den rein lesenden Prüfbefehl. Eingehängte Dateisysteme werden online geprüft, soweit das Werkzeug es kann.
func fsCheckCommand(fstype, device, mountpoint string) (tool string, args []string, err error) {
	online := mountpoint != ""
	switch fstype {
	case "ext4", "ext3", "ext2":
		return "e2fsck", []string{"-f", "-n", device}, nil
	case "xfs":
		if online {
			return "xfs_scrub", []string{"-n", "-v", mountpoint}, nil
		}
		return "xfs_repair", []string{"-n", device}, nil
	case "btrfs":
		if online {
			return "btrfs", []string{"scrub", "start", "-B", "-R", mountpoint}, nil
		}
		return "btrfs", []string{"check", "--readonly", device}, nil
	}
	return "", nil, apierr.New(apierr.InvalidParams, "Dieses Dateisystem lässt sich nicht prüfen (unterstützt: ext4, xfs, btrfs).")
}

// fsCheckResult wertet den Exit-Code aus: ok, errors (Dateisystem meldet Fehler) oder failed (Prüfung nicht durchführbar).
func fsCheckResult(fstype string, online bool, code int) string {
	switch {
	case code == 0:
		return fsOK
	case fstype == "xfs" && !online && code == 1: // xfs_repair -n: Inkonsistenzen gefunden
		return fsErrors
	case (fstype == "ext4" || fstype == "ext3" || fstype == "ext2") && code&4 != 0 && code&^(4|1|2) == 0:
		return fsErrors
	case fstype == "xfs" && online && code == 1: // xfs_scrub: Fehler gefunden
		return fsErrors
	case fstype == "btrfs" && code != 0 && code != 1 && code != 127:
		return fsErrors
	}
	return fsFailed
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		s = "…" + s[len(s)-n:]
	}
	return s
}

// FsCheckStart startet die Prüfung der einzigen Dateisystem-Partition einer Platte im Hintergrund.
func FsCheckStart(ctx context.Context, d Deps, device string) error {
	disks, err := ListDisks(ctx, d)
	if err != nil {
		return err
	}
	disk, ok := findDisk(disks, func(x storage.Disk) bool { return x.Device == device })
	if !ok {
		return apierr.New(apierr.NotFound, "Unbekannter Datenträger")
	}
	if disk.System {
		return apierr.New(apierr.Conflict, "Die Systemplatte lässt sich hier nicht prüfen.")
	}
	var part *storage.Partition
	for i := range disk.Partitions {
		switch disk.Partitions[i].FSType {
		case "ext4", "ext3", "ext2", "xfs", "btrfs":
			if part != nil {
				return apierr.New(apierr.Conflict, "Die Platte hat mehrere Dateisysteme; geprüft wird nur eine Platte mit genau einem.")
			}
			part = &disk.Partitions[i]
		}
	}
	if part == nil {
		return apierr.New(apierr.InvalidParams, "Auf der Platte ist kein ext4-, xfs- oder btrfs-Dateisystem.")
	}
	mp := ""
	if len(part.Mountpoints) > 0 {
		mp = part.Mountpoints[0]
	}
	tool, args, err := fsCheckCommand(part.FSType, part.Device, mp)
	if err != nil {
		return err
	}
	fsChecks.Lock()
	if fsChecks.run != nil && fsChecks.run.Running {
		fsChecks.Unlock()
		return apierr.New(apierr.Conflict, "Es läuft bereits eine Dateisystem-Prüfung.")
	}
	run := &fsCheckRun{device: device, FsCheckState: FsCheckState{Running: true, Known: true, Online: mp != "", Tool: tool}}
	fsChecks.run = run
	fsChecks.Unlock()

	go func() {
		cctx, cancel := context.WithTimeout(context.Background(), fsCheckLimit)
		defer cancel()
		res, rerr := d.Runner.Run(cctx, nil, tool, args...)
		fsChecks.Lock()
		defer fsChecks.Unlock()
		run.Running = false
		run.Output = tail(string(res.Stdout)+"\n"+string(res.Stderr), fsOutputLimit)
		switch {
		case rerr != nil:
			run.Result = fsFailed
			run.Output = tail(tool+" lässt sich nicht starten: "+rerr.Error(), fsOutputLimit)
		default:
			run.Result = fsCheckResult(part.FSType, run.Online, res.ExitCode)
		}
	}()
	return nil
}

// FsCheckStatus liefert den Zustand des letzten Laufs für ein Gerät.
func FsCheckStatus(device string) FsCheckState {
	fsChecks.Lock()
	defer fsChecks.Unlock()
	if r := fsChecks.run; r != nil && r.device == device {
		return r.FsCheckState
	}
	return FsCheckState{}
}

func registerFsCheck(s *rpc.Server, d Deps) {
	s.Handle("disk.fscheck.start", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Device string `json:"device"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		if err := FsCheckStart(ctx, d, req.Device); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	})
	s.Handle("disk.fscheck.status", func(_ context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Device string `json:"device"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return FsCheckStatus(req.Device), nil
	})
}
