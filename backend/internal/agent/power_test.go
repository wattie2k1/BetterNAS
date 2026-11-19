package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/sysexec"
)

func TestPowerStates(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses["hdparm -C /dev/sda"] = sysexec.Result{Stdout: []byte("\n/dev/sda:\n drive state is:  standby\n")}
	got, err := PowerStates(context.Background(), d, []string{"/dev/sda", "/dev/zzz"})
	if err != nil || got["/dev/sda"] != PowerStandby || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
	for out, want := range map[string]string{" drive state is:  active/idle": PowerActive, " drive state is:  sleeping": PowerStandby, " drive state is:  unknown": PowerUnknown, "nonsense": PowerUnknown} {
		f.Responses["hdparm -C /dev/sda"] = sysexec.Result{Stdout: []byte(out)}
		if g, _ := PowerStates(context.Background(), d, []string{"/dev/sda"}); g["/dev/sda"] != want {
			t.Errorf("%q: %s statt %s", out, g["/dev/sda"], want)
		}
	}
	f.Responses["hdparm -C /dev/sda"] = sysexec.Result{ExitCode: 25}
	if g, _ := PowerStates(context.Background(), d, []string{"/dev/sda"}); g["/dev/sda"] != PowerUnknown {
		t.Error("Fehler = unknown")
	}
}

func TestGetTuning(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses["hdparm -C /dev/sda"] = sysexec.Result{Stdout: []byte(" drive state is:  active/idle")}
	f.Responses["hdparm -B /dev/sda"] = sysexec.Result{Stdout: []byte("\n/dev/sda:\n APM_level\t= 128\n")}
	f.Responses["hdparm -W /dev/sda"] = sysexec.Result{Stdout: []byte("\n/dev/sda:\n write-caching =  1 (on)\n")}
	tu, err := GetTuning(context.Background(), d, "/dev/sda")
	if err != nil || tu.APM == nil || *tu.APM != 128 || tu.WriteCache == nil || !*tu.WriteCache {
		t.Fatalf("%+v %v", tu, err)
	}
	f.Responses["hdparm -B /dev/sda"] = sysexec.Result{Stdout: []byte(" APM_level\t= off")}
	f.Responses["hdparm -W /dev/sda"] = sysexec.Result{Stdout: []byte(" write-caching =  0 (off)")}
	if tu, _ := GetTuning(context.Background(), d, "/dev/sda"); *tu.APM != 255 || *tu.WriteCache {
		t.Fatalf("%+v", tu)
	}
	f.Responses["hdparm -B /dev/sda"] = sysexec.Result{ExitCode: 25, Stdout: []byte(" APM_level\t= not supported")}
	if tu, _ := GetTuning(context.Background(), d, "/dev/sda"); tu.APM != nil {
		t.Fatalf("nicht unterstützt = nil: %+v", tu)
	}
	if _, err := GetTuning(context.Background(), d, "/dev/zzz"); code(err) != "not_found" {
		t.Fatal("unbekannte Platte")
	}
}

func TestGetTuningDoesNotWakeSleepingDisk(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses["hdparm -C /dev/sda"] = sysexec.Result{Stdout: []byte(" drive state is:  standby")}
	tu, err := GetTuning(context.Background(), d, "/dev/sda")
	if err != nil || !tu.Standby || tu.APM != nil {
		t.Fatalf("%+v %v", tu, err)
	}
	for _, c := range f.Calls {
		if strings.HasPrefix(c, "hdparm -B") || strings.HasPrefix(c, "hdparm -W") {
			t.Fatalf("schlafende Platte angesprochen: %s", c)
		}
	}
}

func TestTune(t *testing.T) {
	d, f, _ := storageEnv(t)
	if err := Tune(context.Background(), d, "/dev/sda", 192, "off"); err != nil {
		t.Fatal(err)
	}
	if !f.Called("hdparm -B 192 /dev/sda") || !f.Called("hdparm -W0 /dev/sda") {
		t.Fatalf("%v", f.Calls)
	}
	f.Calls = nil
	if err := Tune(context.Background(), d, "/dev/sda", 0, "on"); err != nil || f.Called("hdparm -B") || !f.Called("hdparm -W1 /dev/sda") {
		t.Fatalf("nur Cache: %v %v", f.Calls, err)
	}
	for _, c := range []struct {
		apm int
		wc  string
	}{{256, ""}, {-1, ""}, {0, "maybe"}} {
		if code(Tune(context.Background(), d, "/dev/sda", c.apm, c.wc)) != "invalid_params" {
			t.Errorf("%+v muss abgelehnt werden", c)
		}
	}
	if code(Tune(context.Background(), d, "/dev/zzz", 128, "")) != "not_found" {
		t.Error("unbekannte Platte")
	}
	f.Responses["hdparm -B 64 /dev/sda"] = sysexec.Result{ExitCode: 25}
	if code(Tune(context.Background(), d, "/dev/sda", 64, "")) != "conflict" {
		t.Error("nicht unterstützt = conflict")
	}
}
