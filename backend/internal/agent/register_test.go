package agent

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/storage"
)

func rpcClient(t *testing.T, d Deps) *rpc.Client {
	t.Helper()
	dir, _ := os.MkdirTemp("", "ag")
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "a.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	s := rpc.NewServer(nil)
	Register(s, d)
	go s.Serve(l)
	return rpc.NewClient(sock)
}

func TestRegisteredStorageMethods(t *testing.T) {
	d, _, _ := storageEnv(t)
	c := rpcClient(t, d)
	ctx := context.Background()

	var disks []storage.Disk
	if err := c.Call(ctx, "disk.list", nil, &disks); err != nil || len(disks) != 3 {
		t.Fatalf("disk.list: %v %d", err, len(disks))
	}
	var fr FormatResult
	if err := c.Call(ctx, "disk.format", sdaRequest(), &fr); err != nil || fr.FSUUID != "aaaa-bbbb" {
		t.Fatalf("disk.format: %v %+v", err, fr)
	}
	var ar ApplyResult
	if err := c.Call(ctx, "storage.apply", applyState(), &ar); err != nil || len(ar.Changed) != 3 {
		t.Fatalf("storage.apply: %v %+v", err, ar)
	}
	if err := c.Call(ctx, "disk.smart", map[string]string{"device": "/dev/nope"}, nil); apierr.From(err).Code != apierr.NotFound {
		t.Fatalf("disk.smart: %v", err)
	}
	if err := c.Call(ctx, "storage.apply", map[string]any{"pools": "kaputt"}, nil); apierr.From(err).Code != apierr.InvalidParams {
		t.Fatalf("invalid params: %v", err)
	}
}
