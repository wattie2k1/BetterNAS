//go:build linux

package agent

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestKernelUeventsReal braucht root und echte Blockgeräte: BNAS_UEVENT_TEST=1 sudo ./agent.test
func TestKernelUeventsReal(t *testing.T) {
	if os.Getenv("BNAS_UEVENT_TEST") != "1" {
		t.Skip("nur mit BNAS_UEVENT_TEST=1 (root)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, err := KernelUevents{}.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("udevadm", "trigger", "--subsystem-match=block", "--action=change").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	select {
	case <-ch:
	case <-ctx.Done():
		t.Fatal("kein Block-Uevent empfangen")
	}
}
