package agent

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/sysexec"
)

type countTarget struct {
	n       atomic.Uint64
	failAt  uint64
	onWrite func()
	synced  int
	closed  bool
}

func (c *countTarget) Write(p []byte) (int, error) {
	if c.onWrite != nil {
		c.onWrite()
	}
	if c.failAt > 0 && c.n.Load()+uint64(len(p)) > c.failAt {
		return 0, errors.New("E/A-Fehler")
	}
	c.n.Add(uint64(len(p)))
	return len(p), nil
}
func (c *countTarget) Sync() error  { c.synced++; return nil }
func (c *countTarget) Close() error { c.closed = true; return nil }

func waitErase(t *testing.T, dev string) EraseState {
	t.Helper()
	for i := 0; i < 500; i++ {
		if s := EraseStatus(dev); s.Known && !s.Running {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Löschen wurde nicht fertig")
	return EraseState{}
}

func TestEraseZeroWritesWholeDiskThenWipes(t *testing.T) {
	d, f, _ := storageEnv(t)
	tgt := &countTarget{}
	old := eraseOpen
	eraseOpen = func(string) (eraseTarget, error) { return tgt, nil }
	defer func() { eraseOpen = old }()
	if err := EraseStart(context.Background(), d, "/dev/sda", eraseZero, 4000787030016); err != nil {
		t.Fatal(err)
	}
	s := waitErase(t, "/dev/sda")
	if s.Result != "done" || tgt.n.Load() != 4000787030016 || !tgt.closed || tgt.synced == 0 || s.WrittenBytes != s.TotalBytes {
		t.Fatalf("%+v n=%d", s, tgt.n.Load())
	}
	joined := strings.Join(f.Calls, "\n")
	if !strings.Contains(joined, "wipefs -a -f /dev/sda") {
		t.Fatalf("wipefs fehlt:\n%s", joined)
	}
}

func TestEraseFailureAndAbort(t *testing.T) {
	d, _, _ := storageEnv(t)
	old := eraseOpen
	defer func() { eraseOpen = old }()

	eraseOpen = func(string) (eraseTarget, error) { return &countTarget{failAt: 10 << 20}, nil }
	if err := EraseStart(context.Background(), d, "/dev/sda", eraseZero, 0); err != nil {
		t.Fatal(err)
	}
	if s := waitErase(t, "/dev/sda"); s.Result != "failed" || !strings.Contains(s.Error, "E/A-Fehler") {
		t.Fatalf("%+v", s)
	}

	var started atomic.Bool
	eraseOpen = func(string) (eraseTarget, error) {
		return &countTarget{onWrite: func() { started.Store(true); time.Sleep(time.Millisecond) }}, nil
	}
	if err := EraseStart(context.Background(), d, "/dev/sda", eraseZero, 0); err != nil {
		t.Fatal(err)
	}
	if code(EraseStart(context.Background(), d, "/dev/sda", eraseZero, 0)) != "conflict" {
		t.Error("zweiter Lauf gleichzeitig")
	}
	for !started.Load() {
		time.Sleep(time.Millisecond)
	}
	EraseAbort("/dev/sda")
	if s := waitErase(t, "/dev/sda"); s.Result != "aborted" {
		t.Fatalf("%+v", s)
	}
}

func TestEraseGuards(t *testing.T) {
	d, f, _ := storageEnv(t)
	ctx := context.Background()
	if code(EraseStart(ctx, d, "/dev/zzz", eraseZero, 0)) != "not_found" {
		t.Error("unbekannt")
	}
	if code(EraseStart(ctx, d, "/dev/sda", "shred", 0)) != "invalid_params" {
		t.Error("Methode")
	}
	if code(EraseStart(ctx, d, "/dev/sda", eraseZero, 123)) != "conflict" {
		t.Error("Größe stimmt nicht")
	}
	// Verwerfen nur für SSDs: sda der Vorlage ist eine Festplatte.
	if code(EraseStart(ctx, d, "/dev/sda", eraseDiscard, 0)) != "invalid_params" {
		t.Error("TRIM auf HDD")
	}
	_ = f
	_ = sysexec.Result{}
}
