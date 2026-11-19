package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/storage"
)

type ataTest struct {
	Type struct {
		String string `json:"string"`
	} `json:"type"`
	Status struct {
		Value  int    `json:"value"`
		String string `json:"string"`
	} `json:"status"`
	Hours int `json:"lifetime_hours"`
}

type nvmeTest struct {
	Code struct {
		String string `json:"string"`
	} `json:"self_test_code"`
	Result struct {
		Value  int    `json:"value"`
		String string `json:"string"`
	} `json:"self_test_result"`
	Hours int `json:"power_on_hours"`
}

func testKind(s string) string {
	l := strings.ToLower(s)
	switch {
	case strings.Contains(l, "short"):
		return "short"
	case strings.Contains(l, "extended"), strings.Contains(l, "long"):
		return "long"
	}
	return "other"
}

// ataSelfTest wertet Status (Wert aus ata_smart_data) und Protokoll einer ATA-Platte aus; nil, wenn beides fehlt.
// Der Statuswert trägt im oberen Halbbyte den Zustand (0xF = läuft) und im unteren den Rest in 10-%-Schritten.
func ataSelfTest(status int, remaining *int, table []ataTest) *SelfTest {
	if status == 0 && remaining == nil && len(table) == 0 {
		return nil
	}
	t := &SelfTest{}
	if status>>4 == 0xF {
		t.Running = true
		t.RemainingPercent = (status & 0xF) * 10
		if remaining != nil {
			t.RemainingPercent = *remaining
		}
	}
	if len(table) > 0 {
		e := table[0] // neuester Eintrag zuerst
		r := "failed"
		switch e.Status.Value >> 4 {
		case 0:
			r = "passed"
		case 1, 2:
			r = "aborted"
		}
		t.Last = &SelfTestResult{Type: testKind(e.Type.String), Result: r, Status: e.Status.String, PowerOnHours: e.Hours}
	}
	return t
}

// nvmeSelfTest wertet das NVMe-Selbsttest-Protokoll aus; nil, wenn die Platte keines meldet.
func nvmeSelfTest(op int, completion *int, table []nvmeTest) *SelfTest {
	if op == 0 && completion == nil && len(table) == 0 {
		return nil
	}
	t := &SelfTest{}
	if op != 0 {
		t.Running = true
		if completion != nil {
			t.RemainingPercent = 100 - *completion
		}
	}
	if len(table) > 0 {
		e := table[0]
		r := "failed"
		switch {
		case e.Result.Value == 0:
			r = "passed"
		case e.Result.Value >= 1 && e.Result.Value <= 4:
			r = "aborted"
		}
		t.Last = &SelfTestResult{Type: testKind(e.Code.String), Result: r, Status: e.Result.String, PowerOnHours: e.Hours}
	}
	return t
}

// SelfTestStart startet einen SMART-Selbsttest (short | long) im Hintergrund der Platte.
func SelfTestStart(ctx context.Context, d Deps, device, kind string) error {
	if kind != "short" && kind != "long" {
		return apierr.New(apierr.InvalidParams, "Ungültige Testart.")
	}
	return selfTestCall(ctx, d, device, []string{"-t", kind}, "Der Selbsttest konnte nicht gestartet werden – läuft schon einer, oder die Platte unterstützt ihn nicht (häufig bei USB-Gehäusen).")
}

// SelfTestAbort bricht einen laufenden Selbsttest ab.
func SelfTestAbort(ctx context.Context, d Deps, device string) error {
	return selfTestCall(ctx, d, device, []string{"-X"}, "Es läuft kein Selbsttest, der sich abbrechen ließe.")
}

func selfTestCall(ctx context.Context, d Deps, device string, args []string, failMsg string) error {
	disks, err := ListDisks(ctx, d)
	if err != nil {
		return err
	}
	if _, ok := findDisk(disks, func(x storage.Disk) bool { return x.Device == device }); !ok {
		return apierr.New(apierr.NotFound, "Unbekannter Datenträger")
	}
	res, err := d.Runner.Run(ctx, nil, "smartctl", append(append([]string{}, args...), device)...)
	if err != nil {
		return err
	}
	if res.ExitCode&3 != 0 || res.ExitCode&0x4 != 0 && strings.Contains(strings.ToLower(string(res.Stdout)), "can't start") {
		// USB-SATA-Gehäuse brauchen oft -d sat.
		res, err = d.Runner.Run(ctx, nil, "smartctl", append(append(append([]string{}, args...), "-d", "sat"), device)...)
		if err != nil {
			return err
		}
	}
	if res.ExitCode&3 != 0 {
		return apierr.New(apierr.Conflict, failMsg)
	}
	return nil
}

func registerSelfTest(s *rpc.Server, d Deps) {
	s.Handle("disk.selftest.start", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Device string `json:"device"`
			Kind   string `json:"kind"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		if err := SelfTestStart(ctx, d, req.Device, req.Kind); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	})
	s.Handle("disk.selftest.abort", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Device string `json:"device"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		if err := SelfTestAbort(ctx, d, req.Device); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	})
}
