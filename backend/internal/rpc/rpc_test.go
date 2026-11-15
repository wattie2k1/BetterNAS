package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/apierr"
)

// startServer startet einen Server auf einem Socket im Temp-Verzeichnis.
func startServer(t *testing.T, s *Server) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rpc") // kurzer Pfad: Unix-Sockets sind auf ~104 Zeichen begrenzt
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "s.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go s.Serve(l)
	return path
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestCallRoundTrip(t *testing.T) {
	s := NewServer(nil)
	s.Handle("echo", func(_ context.Context, p json.RawMessage) (any, error) { return p, nil })
	c := NewClient(startServer(t, s))
	var out map[string]int
	if err := c.Call(ctx(t), "echo", map[string]int{"a": 1}, &out); err != nil {
		t.Fatal(err)
	}
	if out["a"] != 1 {
		t.Fatalf("%v", out)
	}
}

func TestUnknownMethod(t *testing.T) {
	c := NewClient(startServer(t, NewServer(nil)))
	err := c.Call(ctx(t), "gibtsnicht", nil, nil)
	if apierr.From(err).Code != apierr.NotFound {
		t.Fatalf("%v", err)
	}
}

func TestHandlerErrorPropagates(t *testing.T) {
	s := NewServer(nil)
	s.Handle("busy", func(context.Context, json.RawMessage) (any, error) {
		return nil, apierr.New(apierr.PoolBusy, "Pool wird verwendet").With("unit", "u")
	})
	s.Handle("boom", func(context.Context, json.RawMessage) (any, error) {
		return nil, errors.New("geheimes Detail /etc/shadow")
	})
	c := NewClient(startServer(t, s))
	e := apierr.From(c.Call(ctx(t), "busy", nil, nil))
	if e.Code != apierr.PoolBusy || e.Details["unit"] != "u" {
		t.Fatalf("%+v", e)
	}
	e = apierr.From(c.Call(ctx(t), "boom", nil, nil))
	if e.Code != apierr.Internal || e.Message == "geheimes Detail /etc/shadow" {
		t.Fatalf("%+v", e)
	}
}

func TestSubscribeReceivesEvents(t *testing.T) {
	s := NewServer(nil)
	s.HandleStream("events", func(c context.Context, emit func(Event) error) error {
		for i := 0; i < 2; i++ {
			if err := emit(Event{Event: "disk.changed"}); err != nil {
				return err
			}
		}
		<-c.Done()
		return nil
	})
	cl := NewClient(startServer(t, s))
	c, cancel := context.WithCancel(context.Background())
	ch, err := cl.Subscribe(c, "events")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case ev := <-ch:
			if ev.Event != "disk.changed" {
				t.Fatalf("%+v", ev)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timeout")
		}
	}
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("channel not closed after cancel")
	}
}
