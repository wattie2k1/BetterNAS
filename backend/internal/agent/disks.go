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
	Passed             bool `json:"passed"`
	TemperatureC       int  `json:"temperatureC"`
	PowerOnHours       int  `json:"powerOnHours"`
	Standby            bool `json:"standby"`
	ReallocatedSectors int  `json:"reallocatedSectors"`
	PendingSectors     int  `json:"pendingSectors"`
	// SelfTest beschreibt laufenden und letzten SMART-Selbsttest (nil, wenn die Platte keinen meldet).
	SelfTest *SelfTest `json:"selfTest,omitempty"`
	// Attributes sind die SMART-Attribute (ATA) bzw. Gesundheitswerte (NVMe) im Rohformat der Platte.
	Attributes []SmartAttr `json:"attributes,omitempty"`
	// Wear beschreibt den Verschleiß einer SSD (nil bei HDDs und wenn die SSD ihn nicht meldet).
	Wear *Wear `json:"wear,omitempty"`
}

// SelfTest ist der Zustand der SMART-Selbsttests einer Platte.
type SelfTest struct {
	Running          bool            `json:"running"`
	RemainingPercent int             `json:"remainingPercent,omitempty"`
	Last             *SelfTestResult `json:"last,omitempty"`
}

// SelfTestResult ist ein abgeschlossener Selbsttest.
type SelfTestResult struct {
	Type         string `json:"type"`   // short | long | other
	Result       string `json:"result"` // passed | aborted | failed
	Status       string `json:"status"` // Text der Platte
	PowerOnHours int    `json:"powerOnHours,omitempty"`
}

// Smart liest SMART-Werte eines erkannten Datenträgers. Schlafende Platten werden nicht geweckt (-n standby).
func Smart(ctx context.Context, d Deps, device string) (SmartInfo, error) {
	disks, err := ListDisks(ctx, d)
	if err != nil {
		return SmartInfo{}, err
	}
	if _, ok := findDisk(disks, func(x storage.Disk) bool { return x.Device == device }); !ok {
		return SmartInfo{}, apierr.New(apierr.NotFound, "Unbekannter Datenträger")
	}
	base := []string{"--json", "-H", "-A", "-c", "-l", "selftest", "-n", "standby"}
	res, err := d.Runner.Run(ctx, nil, "smartctl", append(base, device)...)
	if err != nil {
		return SmartInfo{}, err
	}
	if res.ExitCode&2 != 0 && strings.Contains(strings.ToUpper(string(res.Stdout)+string(res.Stderr)), "STANDBY") {
		return SmartInfo{Standby: true}, nil
	}
	// Bits 0 und 1 bedeuten: Aufruf- bzw. Öffnungsfehler. Höhere Bits sind Befunde, die Daten sind gültig.
	if res.ExitCode&3 != 0 {
		// USB-SATA-Gehäuse brauchen oft -d sat.
		res, err = d.Runner.Run(ctx, nil, "smartctl", append(append(base, "-d", "sat"), device)...)
		if err != nil {
			return SmartInfo{}, err
		}
		if res.ExitCode&2 != 0 && strings.Contains(strings.ToUpper(string(res.Stdout)+string(res.Stderr)), "STANDBY") {
			return SmartInfo{Standby: true}, nil
		}
		if res.ExitCode&3 != 0 {
			return SmartInfo{}, fmt.Errorf("smartctl %s: Exit %d", device, res.ExitCode)
		}
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
		ATA struct {
			Table []ataAttr `json:"table"`
		} `json:"ata_smart_attributes"`
		BlockSize  int         `json:"logical_block_size"`
		NVMeHealth *nvmeHealth `json:"nvme_smart_health_information_log"`
		ATAData    struct {
			SelfTest struct {
				Status struct {
					Value            int  `json:"value"`
					RemainingPercent *int `json:"remaining_percent"`
				} `json:"status"`
			} `json:"self_test"`
		} `json:"ata_smart_data"`
		ATALog struct {
			Standard struct {
				Table []ataTest `json:"table"`
			} `json:"standard"`
		} `json:"ata_smart_self_test_log"`
		NVMeLog struct {
			Operation struct {
				Value int `json:"value"`
			} `json:"current_self_test_operation"`
			Completion *int       `json:"current_self_test_completion_percent"`
			Table      []nvmeTest `json:"table"`
		} `json:"nvme_self_test_log"`
	}
	if err := json.Unmarshal(res.Stdout, &raw); err != nil {
		return SmartInfo{}, fmt.Errorf("smartctl-ausgabe ungültig: %w", err)
	}
	info := SmartInfo{Passed: raw.SmartStatus.Passed, TemperatureC: raw.Temperature.Current, PowerOnHours: raw.PowerOnTime.Hours}
	for _, a := range raw.ATA.Table {
		switch a.ID {
		case 5:
			info.ReallocatedSectors = a.Raw.Value
		case 197:
			info.PendingSectors = a.Raw.Value
		}
	}
	info.Attributes, info.Wear = smartExtras(raw.ATA.Table, raw.NVMeHealth, raw.BlockSize)
	info.SelfTest = ataSelfTest(raw.ATAData.SelfTest.Status.Value, raw.ATAData.SelfTest.Status.RemainingPercent, raw.ATALog.Standard.Table)
	if info.SelfTest == nil {
		info.SelfTest = nvmeSelfTest(raw.NVMeLog.Operation.Value, raw.NVMeLog.Completion, raw.NVMeLog.Table)
	}
	return info, nil
}
