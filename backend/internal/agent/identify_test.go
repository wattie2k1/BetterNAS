package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/sysexec"
)

func TestIdentifyPulseReadsOnlyAndAcceptsTimeout(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses["timeout 1 dd if=/dev/sda"] = sysexec.Result{ExitCode: 124}
	if err := IdentifyPulse(context.Background(), d, "/dev/sda"); err != nil {
		t.Fatal(err)
	}
	var call string
	for _, c := range f.Calls {
		if strings.HasPrefix(c, "timeout") {
			call = c
		}
	}
	if !strings.Contains(call, "of=/dev/null") || !strings.Contains(call, "iflag=direct") || strings.Contains(call, "of=/dev/sda") {
		t.Fatalf("nur lesen erlaubt: %s", call)
	}
}

func TestIdentifyPulseErrors(t *testing.T) {
	d, f, _ := storageEnv(t)
	if code(IdentifyPulse(context.Background(), d, "/dev/zzz")) != "not_found" {
		t.Error("unbekannte Platte")
	}
	f.Responses["timeout 1 dd if=/dev/sda"] = sysexec.Result{ExitCode: 1}
	if code(IdentifyPulse(context.Background(), d, "/dev/sda")) != "conflict" {
		t.Error("Lesefehler muss als conflict ankommen")
	}
}
