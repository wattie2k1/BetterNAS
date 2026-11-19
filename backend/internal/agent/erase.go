package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/storage"
)

// EraseState ist der Zustand eines laufenden oder abgeschlossenen Löschvorgangs.
type EraseState struct {
	Known        bool   `json:"known"` // false: für dieses Gerät gibt es keinen Lauf (z. B. nach Neustart des Agents)
	Running      bool   `json:"running"`
	Method       string `json:"method,omitempty"`
	WrittenBytes uint64 `json:"writtenBytes"`
	TotalBytes   uint64 `json:"totalBytes"`
	Result       string `json:"result,omitempty"` // done | failed | aborted
	Error        string `json:"error,omitempty"`
}

const (
	eraseZero    = "zero"    // alle Bytes mit Nullen überschreiben (jede Platte)
	eraseDiscard = "discard" // SSD/NVMe: alle Blöcke verwerfen (blkdiscard)
	eraseChunk   = 4 << 20
	eraseSync    = 128 << 20
)

type eraseRun struct {
	device  string
	written atomic.Uint64
	cancel  context.CancelFunc
	state   EraseState // unter erases.Mutex (ohne written)
}

var erases = struct {
	sync.Mutex
	run *eraseRun
}{}

// eraseTarget ist das Ziel des Überschreibens; in Tests eine gewöhnliche Datei.
type eraseTarget interface {
	io.Writer
	Sync() error
	Close() error
}

var eraseOpen = func(path string) (eraseTarget, error) { return os.OpenFile(path, os.O_WRONLY, 0) }

// EraseStart startet das Löschen einer Platte im Hintergrund. Der Agent prüft selbst noch einmal, dass die Platte weder
// Systemplatte noch eingehängt ist und die erwartete Größe hat; die Bestätigung des Nutzers prüft der Server.
func EraseStart(ctx context.Context, d Deps, device, method string, expectSize uint64) error {
	if method != eraseZero && method != eraseDiscard {
		return apierr.New(apierr.InvalidParams, "Unbekannte Löschmethode")
	}
	disks, err := ListDisks(ctx, d)
	if err != nil {
		return err
	}
	disk, ok := findDisk(disks, func(x storage.Disk) bool { return x.Device == device })
	switch {
	case !ok:
		return apierr.New(apierr.NotFound, "Unbekannter Datenträger")
	case disk.System:
		return apierr.New(apierr.SystemDisk, "Die Systemplatte kann nicht gelöscht werden")
	case len(disk.Mountpoints()) > 0:
		return apierr.New(apierr.Conflict, "Der Datenträger ist eingehängt")
	case expectSize != 0 && disk.SizeBytes != expectSize:
		return apierr.New(apierr.Conflict, "Der Datenträger hat sich seit der Bestätigung verändert")
	case method == eraseDiscard && disk.Rotational:
		return apierr.New(apierr.InvalidParams, "Verwerfen (TRIM) gibt es nur für SSDs; für Festplatten bitte Überschreiben wählen.")
	}
	erases.Lock()
	if erases.run != nil && erases.run.state.Running {
		erases.Unlock()
		return apierr.New(apierr.Conflict, "Es läuft bereits ein Löschvorgang.")
	}
	rctx, cancel := context.WithCancel(context.Background())
	run := &eraseRun{device: device, cancel: cancel, state: EraseState{Known: true, Running: true, Method: method, TotalBytes: disk.SizeBytes}}
	erases.run = run
	erases.Unlock()

	go func() {
		defer cancel()
		var err error
		if method == eraseZero {
			err = eraseWrite(rctx, run, disk.SizeBytes)
		} else {
			err = eraseDiscardRun(rctx, d, device)
		}
		if err == nil {
			// Reste von Partitionstabellen und Signaturen entfernen, dann dem Kernel die leere Platte melden.
			_, _ = d.Runner.Run(context.Background(), nil, "wipefs", "-a", "-f", device)
			_, _ = d.Runner.Run(context.Background(), nil, "blockdev", "--rereadpt", device)
		}
		erases.Lock()
		defer erases.Unlock()
		run.state.Running = false
		switch {
		case err == nil:
			run.state.Result = "done"
		case errors.Is(err, context.Canceled):
			run.state.Result = "aborted"
		default:
			run.state.Result, run.state.Error = "failed", err.Error()
		}
	}()
	return nil
}

func eraseWrite(ctx context.Context, run *eraseRun, total uint64) error {
	f, err := eraseOpen(run.device)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, eraseChunk)
	var sinceSync uint64
	for done := uint64(0); done < total; {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := uint64(len(buf))
		if rest := total - done; rest < n {
			n = rest
		}
		w, err := f.Write(buf[:n])
		done += uint64(w)
		run.written.Store(done)
		sinceSync += uint64(w)
		if err != nil {
			return err
		}
		if sinceSync >= eraseSync {
			if err := f.Sync(); err != nil {
				return err
			}
			sinceSync = 0
		}
	}
	return f.Sync()
}

func eraseDiscardRun(ctx context.Context, d Deps, device string) error {
	res, err := d.Runner.Run(ctx, nil, "blkdiscard", "-s", "-f", device)
	if err == nil && res.ExitCode != 0 {
		// Sicheres Verwerfen nicht unterstützt: einfaches Verwerfen versuchen.
		res, err = d.Runner.Run(ctx, nil, "blkdiscard", "-f", device)
	}
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return apierr.New(apierr.Conflict, "Die Platte unterstützt das Verwerfen nicht: "+tail(string(res.Stderr), 300))
	}
	return nil
}

// EraseStatus liefert den Zustand des letzten Laufs für ein Gerät.
func EraseStatus(device string) EraseState {
	erases.Lock()
	defer erases.Unlock()
	r := erases.run
	if r == nil || r.device != device {
		return EraseState{}
	}
	st := r.state
	st.WrittenBytes = r.written.Load()
	if st.Result == "done" {
		st.WrittenBytes = st.TotalBytes
	}
	return st
}

// EraseAbort bricht einen laufenden Löschvorgang ab; die Platte bleibt dann teilweise überschrieben.
func EraseAbort(device string) {
	erases.Lock()
	r := erases.run
	erases.Unlock()
	if r != nil && r.device == device {
		r.cancel()
	}
}

func registerErase(s *rpc.Server, d Deps) {
	type req struct {
		Device    string `json:"device"`
		Method    string `json:"method"`
		SizeBytes uint64 `json:"sizeBytes"`
	}
	s.Handle("disk.erase.start", func(ctx context.Context, p json.RawMessage) (any, error) {
		var r req
		if err := decode(p, &r); err != nil {
			return nil, err
		}
		mutate.Lock()
		defer mutate.Unlock()
		if err := EraseStart(ctx, d, r.Device, r.Method, r.SizeBytes); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	})
	s.Handle("disk.erase.status", func(_ context.Context, p json.RawMessage) (any, error) {
		var r req
		if err := decode(p, &r); err != nil {
			return nil, err
		}
		return EraseStatus(r.Device), nil
	})
	s.Handle("disk.erase.abort", func(_ context.Context, p json.RawMessage) (any, error) {
		var r req
		if err := decode(p, &r); err != nil {
			return nil, err
		}
		EraseAbort(r.Device)
		return map[string]bool{"ok": true}, nil
	})
}
