package agent

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/storage"
)

// Energiezustände einer Platte.
const (
	PowerActive  = "active"
	PowerStandby = "standby"
	PowerUnknown = "unknown"
)

// powerState liest den Energiezustand (hdparm -C, weckt die Platte nicht).
func powerState(ctx context.Context, d Deps, device string) string {
	res, err := d.Runner.Run(ctx, nil, "hdparm", "-C", device)
	if err != nil || res.ExitCode != 0 {
		return PowerUnknown
	}
	out := strings.ToLower(string(res.Stdout))
	switch {
	case !strings.Contains(out, "drive state is"):
		return PowerUnknown
	case strings.Contains(out, "standby"), strings.Contains(out, "sleeping"):
		return PowerStandby
	case strings.Contains(out, "active"), strings.Contains(out, "idle"):
		return PowerActive
	}
	return PowerUnknown
}

// PowerStates liefert den Energiezustand der angegebenen (erkannten) Geräte; unbekannte Geräte fehlen.
func PowerStates(ctx context.Context, d Deps, devices []string) (map[string]string, error) {
	disks, err := ListDisks(ctx, d)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, dev := range devices {
		if _, ok := findDisk(disks, func(x storage.Disk) bool { return x.Device == dev }); ok {
			out[dev] = powerState(ctx, d, dev)
		}
	}
	return out, nil
}

// Tuning sind die aktuellen APM- und Schreibcache-Einstellungen einer Platte.
type Tuning struct {
	// Standby: die Platte schläft; sie wird für die Abfrage nicht geweckt, Werte fehlen dann.
	Standby    bool  `json:"standby,omitempty"`
	APM        *int  `json:"apm,omitempty"`        // 1–254, 255 = aus
	WriteCache *bool `json:"writeCache,omitempty"` // nil = unbekannt
}

var (
	apmRe   = regexp.MustCompile(`APM_level\s*=\s*(\S+)`)
	cacheRe = regexp.MustCompile(`write-caching\s*=\s*(\d)`)
)

func hdparmOut(ctx context.Context, d Deps, args ...string) string {
	res, err := d.Runner.Run(ctx, nil, "hdparm", args...)
	if err != nil || res.ExitCode != 0 {
		return ""
	}
	return string(res.Stdout)
}

// GetTuning liest APM und Schreibcache; eine schlafende Platte wird dafür nicht geweckt.
func GetTuning(ctx context.Context, d Deps, device string) (Tuning, error) {
	st, err := PowerStates(ctx, d, []string{device})
	if err != nil {
		return Tuning{}, err
	}
	state, ok := st[device]
	if !ok {
		return Tuning{}, apierr.New(apierr.NotFound, "Unbekannter Datenträger")
	}
	if state == PowerStandby {
		return Tuning{Standby: true}, nil
	}
	var t Tuning
	if m := apmRe.FindStringSubmatch(hdparmOut(ctx, d, "-B", device)); m != nil {
		switch {
		case m[1] == "off":
			v := 255
			t.APM = &v
		default:
			if v, err := strconv.Atoi(m[1]); err == nil {
				t.APM = &v
			}
		}
	}
	if m := cacheRe.FindStringSubmatch(hdparmOut(ctx, d, "-W", device)); m != nil {
		on := m[1] == "1"
		t.WriteCache = &on
	}
	return t, nil
}

// Tune setzt APM (apm 1–255, 0 = nicht ändern) und/oder den Schreibcache ("on" | "off" | "" = nicht ändern).
func Tune(ctx context.Context, d Deps, device string, apm int, writeCache string) error {
	if apm < 0 || apm > 255 || (writeCache != "" && writeCache != "on" && writeCache != "off") {
		return apierr.New(apierr.InvalidParams, "Ungültige Einstellung.")
	}
	disks, err := ListDisks(ctx, d)
	if err != nil {
		return err
	}
	if _, ok := findDisk(disks, func(x storage.Disk) bool { return x.Device == device }); !ok {
		return apierr.New(apierr.NotFound, "Unbekannter Datenträger")
	}
	run := func(args ...string) error {
		res, err := d.Runner.Run(ctx, nil, "hdparm", args...)
		if err != nil {
			return err
		}
		if res.ExitCode != 0 {
			return apierr.New(apierr.Conflict, "Diese Platte unterstützt die Einstellung nicht (häufig bei USB-Gehäusen).")
		}
		return nil
	}
	if apm > 0 {
		if err := run("-B", strconv.Itoa(apm), device); err != nil {
			return err
		}
	}
	switch writeCache {
	case "on":
		return run("-W1", device)
	case "off":
		return run("-W0", device)
	}
	return nil
}

func registerPower(s *rpc.Server, d Deps) {
	s.Handle("disk.powerstate", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Devices []string `json:"devices"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return PowerStates(ctx, d, req.Devices)
	})
	s.Handle("disk.tuning", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Device string `json:"device"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return GetTuning(ctx, d, req.Device)
	})
	s.Handle("disk.tune", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Device     string `json:"device"`
			APM        int    `json:"apm"`
			WriteCache string `json:"writeCache"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		if err := Tune(ctx, d, req.Device, req.APM, req.WriteCache); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	})
}
