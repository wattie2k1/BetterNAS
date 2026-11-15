package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/storage"
	"github.com/betternas/betternas/internal/sysexec"
)

// run führt einen Befehl aus; ExitCode != 0 wird zu einem internen Fehler mit Stderr.
func run(ctx context.Context, r sysexec.Runner, stdin []byte, name string, args ...string) ([]byte, error) {
	res, err := r.Run(ctx, stdin, name, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if res.ExitCode != 0 {
		return res.Stdout, fmt.Errorf("%s (Exit %d): %s", sysexec.CommandLine(name, args...), res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	return res.Stdout, nil
}

// ListDisks liefert alle erkannten Datenträger.
func ListDisks(ctx context.Context, d Deps) ([]storage.Disk, error) {
	out, err := run(ctx, d.Runner, nil, "lsblk", "--json", "--bytes", "--tree", "-o", storage.LsblkColumns)
	if err != nil {
		return nil, err
	}
	disks, err := storage.ParseLsblk(out, storage.IDOptions{AllowLoop: d.AllowLoop})
	if disks == nil {
		disks = []storage.Disk{}
	}
	return disks, err
}

func findDisk(disks []storage.Disk, pred func(storage.Disk) bool) (storage.Disk, bool) {
	for _, d := range disks {
		if pred(d) {
			return d, true
		}
	}
	return storage.Disk{}, false
}

// SmartInfo ist eine Zusammenfassung der SMART-Werte.
type SmartInfo struct {
	Passed       bool `json:"passed"`
	TemperatureC int  `json:"temperatureC"`
	PowerOnHours int  `json:"powerOnHours"`
}

// Smart liest SMART-Werte eines erkannten Datenträgers.
func Smart(ctx context.Context, d Deps, device string) (SmartInfo, error) {
	disks, err := ListDisks(ctx, d)
	if err != nil {
		return SmartInfo{}, err
	}
	if _, ok := findDisk(disks, func(x storage.Disk) bool { return x.Device == device }); !ok {
		return SmartInfo{}, apierr.New(apierr.NotFound, "Unbekannter Datenträger")
	}
	res, err := d.Runner.Run(ctx, nil, "smartctl", "--json", "-H", "-A", device)
	if err != nil {
		return SmartInfo{}, err
	}
	// Bits 0 und 1 bedeuten: Aufruf- bzw. Öffnungsfehler. Höhere Bits sind Befunde, die Daten sind gültig.
	if res.ExitCode&3 != 0 {
		return SmartInfo{}, fmt.Errorf("smartctl %s: Exit %d", device, res.ExitCode)
	}
	var raw struct {
		SmartStatus struct {
			Passed bool `json:"passed"`
		} `json:"smart_status"`
		Temperature struct {
			Current int `json:"current"`
		} `json:"temperature"`
		PowerOnTime struct {
			Hours int `json:"hours"`
		} `json:"power_on_time"`
	}
	if err := json.Unmarshal(res.Stdout, &raw); err != nil {
		return SmartInfo{}, fmt.Errorf("smartctl-ausgabe ungültig: %w", err)
	}
	return SmartInfo{Passed: raw.SmartStatus.Passed, TemperatureC: raw.Temperature.Current, PowerOnHours: raw.PowerOnTime.Hours}, nil
}
