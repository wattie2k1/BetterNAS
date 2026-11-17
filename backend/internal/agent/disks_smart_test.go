package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/sysexec"
)

const smartCall = "smartctl --json -H -A -n standby /dev/sda"

func TestSmartStandbyDoesNotFail(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses[smartCall] = sysexec.Result{ExitCode: 2, Stdout: []byte(`{"smartctl":{"messages":[{"string":"Device is in STANDBY (OS) mode, exit(2)"}]}}`)}
	s, err := Smart(context.Background(), d, "/dev/sda")
	if err != nil || !s.Standby {
		t.Fatalf("%+v %v", s, err)
	}
	n := 0
	for _, c := range f.Calls {
		if strings.HasPrefix(c, "smartctl") {
			n++
			if !strings.Contains(c, "-n standby") {
				t.Fatalf("ohne -n standby: %s", c)
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d smartctl-Aufrufe statt 1", n)
	}
}

func TestSmartFallsBackToSat(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses[smartCall] = sysexec.Result{ExitCode: 1}
	f.Responses["smartctl --json -H -A -n standby -d sat /dev/sda"] = sysexec.Result{Stdout: []byte(smartJSON)}
	s, err := Smart(context.Background(), d, "/dev/sda")
	if err != nil || !s.Passed || s.TemperatureC != 34 {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestSmartParsesSectors(t *testing.T) {
	d, f, _ := storageEnv(t)
	js := `{"smart_status":{"passed":true},"temperature":{"current":40},"power_on_time":{"hours":1},
	 "ata_smart_attributes":{"table":[{"id":1,"raw":{"value":0}},{"id":5,"raw":{"value":8}},{"id":197,"raw":{"value":2}}]}}`
	f.Responses[smartCall] = sysexec.Result{ExitCode: 8, Stdout: []byte(js)}
	s, err := Smart(context.Background(), d, "/dev/sda")
	if err != nil || s.ReallocatedSectors != 8 || s.PendingSectors != 2 {
		t.Fatalf("%+v %v", s, err)
	}
}
