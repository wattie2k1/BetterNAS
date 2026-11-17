package agent

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/storage"
)

// Spindown setzt die Ruhezeit (hdparm -S) eines erkannten Datenträgers. value: 0..251.
func Spindown(ctx context.Context, d Deps, device string, value int) error {
	if value < 0 || value > 251 {
		return apierr.New(apierr.InvalidParams, "Ungültiger Wert für den Ruhezustand.")
	}
	disks, err := ListDisks(ctx, d)
	if err != nil {
		return err
	}
	if _, ok := findDisk(disks, func(x storage.Disk) bool { return x.Device == device }); !ok {
		return apierr.New(apierr.NotFound, "Unbekannter Datenträger")
	}
	res, err := d.Runner.Run(ctx, nil, "hdparm", "-S", strconv.Itoa(value), device)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return apierr.New(apierr.Conflict, "Diese Platte unterstützt keinen Ruhezustand (häufig bei USB-Gehäusen).")
	}
	return nil
}

func registerSpindown(s *rpc.Server, d Deps) {
	s.Handle("disk.spindown", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Device string `json:"device"`
			Value  int    `json:"value"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		if err := Spindown(ctx, d, req.Device, req.Value); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	})
}
