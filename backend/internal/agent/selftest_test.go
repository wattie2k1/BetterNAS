package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/sysexec"
)

func smartWith(extra string) []byte {
	return []byte(`{"smart_status":{"passed":true},"temperature":{"current":34},"power_on_time":{"hours":10},` + extra + `}`)
}

func TestSmartSelfTestATARunning(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses[smartCall] = sysexec.Result{Stdout: smartWith(`"ata_smart_data":{"self_test":{"status":{"value":249,"string":"in progress","remaining_percent":90}}},
	 "ata_smart_self_test_log":{"standard":{"table":[{"type":{"string":"Extended offline"},"status":{"value":0,"string":"Completed without error"},"lifetime_hours":9}]}}`)}
	s, err := Smart(context.Background(), d, "/dev/sda")
	if err != nil || s.SelfTest == nil || !s.SelfTest.Running || s.SelfTest.RemainingPercent != 90 {
		t.Fatalf("%+v %v", s.SelfTest, err)
	}
	if l := s.SelfTest.Last; l == nil || l.Type != "long" || l.Result != "passed" || l.PowerOnHours != 9 {
		t.Fatalf("%+v", l)
	}
}

func TestSmartSelfTestATAResults(t *testing.T) {
	for _, c := range []struct {
		value int
		want  string
	}{{0, "passed"}, {25, "aborted"}, {16, "aborted"}, {113, "failed"}, {48, "failed"}} {
		if got := ataSelfTest(0, nil, []ataTest{{Status: struct {
			Value  int    `json:"value"`
			String string `json:"string"`
		}{Value: c.value}}}); got.Last.Result != c.want {
			t.Errorf("Wert %d: %s statt %s", c.value, got.Last.Result, c.want)
		}
	}
	if ataSelfTest(0, nil, nil) != nil {
		t.Error("ohne Angaben muss nil herauskommen")
	}
}

func TestSmartSelfTestNVMe(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses[smartCall] = sysexec.Result{Stdout: smartWith(`"nvme_self_test_log":{"current_self_test_operation":{"value":1},"current_self_test_completion_percent":30,
	 "table":[{"self_test_code":{"string":"Short"},"self_test_result":{"value":7,"string":"Failed in a segment"},"power_on_hours":5}]}`)}
	s, err := Smart(context.Background(), d, "/dev/sda")
	if err != nil || s.SelfTest == nil || !s.SelfTest.Running || s.SelfTest.RemainingPercent != 70 {
		t.Fatalf("%+v %v", s.SelfTest, err)
	}
	if l := s.SelfTest.Last; l == nil || l.Type != "short" || l.Result != "failed" {
		t.Fatalf("%+v", l)
	}
}

func TestSmartWithoutSelfTestInfo(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses[smartCall] = sysexec.Result{Stdout: []byte(smartJSON)}
	if s, err := Smart(context.Background(), d, "/dev/sda"); err != nil || s.SelfTest != nil {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestSelfTestStartAbort(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses["smartctl -t short /dev/sda"] = sysexec.Result{}
	if err := SelfTestStart(context.Background(), d, "/dev/sda", "short"); err != nil {
		t.Fatal(err)
	}
	f.Responses["smartctl -X /dev/sda"] = sysexec.Result{}
	if err := SelfTestAbort(context.Background(), d, "/dev/sda"); err != nil {
		t.Fatal(err)
	}
	if code(SelfTestStart(context.Background(), d, "/dev/sda", "conveyance")) != "invalid_params" {
		t.Error("unbekannte Testart muss abgelehnt werden")
	}
	if code(SelfTestStart(context.Background(), d, "/dev/zzz", "long")) != "not_found" {
		t.Error("unbekannte Platte muss abgelehnt werden")
	}
	f.Responses["smartctl -t long /dev/sda"] = sysexec.Result{ExitCode: 2}
	f.Responses["smartctl -t long -d sat /dev/sda"] = sysexec.Result{ExitCode: 2}
	if code(SelfTestStart(context.Background(), d, "/dev/sda", "long")) != "conflict" {
		t.Error("Fehlschlag muss als conflict ankommen")
	}
	if !strings.Contains(strings.Join(f.Calls, "\n"), "smartctl -t long -d sat /dev/sda") {
		t.Error("Fallback -d sat fehlt")
	}
}
