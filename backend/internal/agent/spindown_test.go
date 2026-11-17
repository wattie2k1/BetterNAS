package agent

import (
	"context"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/sysexec"
)

func TestSpindownRunsHdparm(t *testing.T) {
	d, f, _ := storageEnv(t)
	if err := Spindown(context.Background(), d, "/dev/sda", 242); err != nil {
		t.Fatal(err)
	}
	if !f.Called("hdparm -S 242 /dev/sda") {
		t.Fatalf("%v", f.Calls)
	}
}

func TestSpindownUnknownDeviceOrValue(t *testing.T) {
	d, f, _ := storageEnv(t)
	if err := Spindown(context.Background(), d, "/dev/sda1; rm -rf /", 60); code(err) != apierr.NotFound || f.Called("hdparm") {
		t.Fatal(err)
	}
	if err := Spindown(context.Background(), d, "/dev/sda", 252); code(err) != apierr.InvalidParams || f.Called("hdparm") {
		t.Fatal(err)
	}
}

func TestSpindownUnsupportedMapsToConflict(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses["hdparm -S 242 /dev/sda"] = sysexec.Result{ExitCode: 25, Stderr: []byte("HDIO_DRIVE_CMD(setidle) failed")}
	err := Spindown(context.Background(), d, "/dev/sda", 242)
	if code(err) != apierr.Conflict {
		t.Fatalf("%v", err)
	}
}
