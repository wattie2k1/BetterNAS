package agent

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"strconv"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/storage"
)

// IdentifyPulse liest eine Sekunde lang ab einer zufälligen Stelle von der Platte (nur lesend, an der Zwischenablage
// des Kernels vorbei), damit ihre Aktivitäts-LED leuchtet und sie anläuft. Wiederholte Impulse mit Pausen ergeben ein Blinken.
func IdentifyPulse(ctx context.Context, d Deps, device string) error {
	disks, err := ListDisks(ctx, d)
	if err != nil {
		return err
	}
	disk, ok := findDisk(disks, func(x storage.Disk) bool { return x.Device == device })
	if !ok {
		return apierr.New(apierr.NotFound, "Unbekannter Datenträger")
	}
	skip := uint64(0)
	if mib := disk.SizeBytes >> 20; mib > 256 {
		skip = rand.Uint64N(mib - 256)
	}
	res, err := d.Runner.Run(ctx, nil, "timeout", "1", "dd", "if="+device, "of=/dev/null", "bs=1M", "iflag=direct", "status=none", "skip="+strconv.FormatUint(skip, 10))
	if err != nil {
		return err
	}
	// 124: timeout hat den Lesevorgang planmäßig nach einer Sekunde beendet.
	if res.ExitCode != 0 && res.ExitCode != 124 {
		return apierr.New(apierr.Conflict, "Die Platte lässt sich nicht lesen.")
	}
	return nil
}

func registerIdentify(s *rpc.Server, d Deps) {
	s.Handle("disk.identify", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Device string `json:"device"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		if err := IdentifyPulse(ctx, d, req.Device); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	})
}
