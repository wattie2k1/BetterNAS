package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/rpc"
)

type fakeSource chan struct{}

func (f fakeSource) Events(context.Context) (<-chan struct{}, error) { return f, nil }

func TestUdevDebounce(t *testing.T) {
	src := make(fakeSource, 16)
	var emitted atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go DiskEvents(ctx, src, 300*time.Millisecond, func(ev rpc.Event) error {
		if ev.Event != "disk.changed" {
			t.Errorf("event %q", ev.Event)
		}
		emitted.Add(1)
		return nil
	})
	for i := 0; i < 5; i++ {
		src <- struct{}{}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(600 * time.Millisecond)
	if n := emitted.Load(); n != 1 {
		t.Fatalf("burst: want 1 event, got %d", n)
	}
	src <- struct{}{}
	time.Sleep(600 * time.Millisecond)
	if n := emitted.Load(); n != 2 {
		t.Fatalf("second event: want 2, got %d", n)
	}
}

func TestIsBlockUevent(t *testing.T) {
	cases := map[string]bool{
		"add@/devices/pci0000:00/usb1/1-1/host6/target6:0:0/6:0:0:0/block/sdc\x00ACTION=add\x00SUBSYSTEM=block\x00DEVNAME=sdc\x00": true,
		"remove@/devices/x/block/sdc/sdc1\x00ACTION=remove\x00SUBSYSTEM=block\x00":                                                 true,
		"change@/devices/x/block/sda\x00ACTION=change\x00SUBSYSTEM=block\x00":                                                      true,
		"add@/devices/x/net/eth0\x00ACTION=add\x00SUBSYSTEM=net\x00":                                                               false,
		"bind@/devices/x/block/sda\x00ACTION=bind\x00SUBSYSTEM=block\x00":                                                          false,
		"libudev\x00garbage": false,
	}
	for msg, want := range cases {
		if got := isBlockUevent([]byte(msg)); got != want {
			t.Errorf("%q: got %v", msg, got)
		}
	}
}
