package api

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/rpc"
)

func TestForwardAgentEvents(t *testing.T) {
	dir, _ := os.MkdirTemp("", "ev")
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "a.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	s := rpc.NewServer(nil)
	s.HandleStream("events.subscribe", func(ctx context.Context, emit func(rpc.Event) error) error {
		_ = emit(rpc.Event{Event: "disk.changed"})
		<-ctx.Done()
		return nil
	})
	go s.Serve(l)

	hub := events.NewHub()
	ch, unsub := hub.Subscribe()
	defer unsub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ForwardAgentEvents(ctx, rpc.NewClient(sock), hub)
	select {
	case m := <-ch:
		if m.Type != "disks.changed" {
			t.Fatal(m)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no disks.changed")
	}
}
