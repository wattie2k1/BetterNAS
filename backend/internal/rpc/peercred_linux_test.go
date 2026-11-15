//go:build linux

package rpc

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestPeerUIDRejected(t *testing.T) {
	if os.Getuid() == 4242 {
		t.Skip()
	}
	s := NewServer([]uint32{4242})
	s.Handle("echo", func(_ context.Context, p json.RawMessage) (any, error) { return p, nil })
	c := NewClient(startServer(t, s))
	if err := c.Call(ctx(t), "echo", nil, nil); err == nil {
		t.Fatal("call from foreign uid must fail")
	}
}

func TestPeerUIDAllowed(t *testing.T) {
	s := NewServer([]uint32{uint32(os.Getuid())})
	s.Handle("echo", func(_ context.Context, p json.RawMessage) (any, error) { return p, nil })
	c := NewClient(startServer(t, s))
	if err := c.Call(ctx(t), "echo", nil, nil); err != nil {
		t.Fatal(err)
	}
}
