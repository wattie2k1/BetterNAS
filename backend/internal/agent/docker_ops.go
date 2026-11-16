package agent

import (
	"context"
	"encoding/json"
	"io"
	"strconv"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/docker"
	"github.com/betternas/betternas/internal/rpc"
)

const (
	defaultLogTail = 200
	maxLogTail     = 5000
	maxTermSize    = 500
	// shellStarter ist ein fester String ohne Nutzereingaben: bash, wenn vorhanden, sonst sh.
	shellStarter = "command -v bash >/dev/null 2>&1 && exec bash || exec sh"
)

// ContainerStatsList liefert CPU und Speicher aller laufenden Container.
func ContainerStatsList(ctx context.Context, d Deps) ([]docker.ContainerStats, error) {
	out, err := run(ctx, d.Runner, nil, "docker", "stats", "--no-stream", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, apierr.New(apierr.Internal, "Docker ist nicht verfügbar").With("detail", err.Error())
	}
	return docker.ParseStats(out)
}

func data(b []byte) rpc.Event {
	raw, _ := json.Marshal(b) // []byte wird als Base64-String übertragen
	return rpc.Event{Event: "data", Data: raw}
}

func fail(emit func(rpc.Event) error, err error) error {
	raw, _ := json.Marshal(apierr.From(err).Message)
	_ = emit(rpc.Event{Event: "error", Data: raw})
	return err
}

// pump liest r und sendet jeden Block als „data“; am Ende schließt es done.
func pump(r io.Reader, emit func(rpc.Event) error) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 16<<10)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				if emit(data(append([]byte(nil), buf[:n]...))) != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return done
}

// ContainerLogs streamt `docker logs -f` eines Containers, bis der Client die Sitzung beendet oder der Container endet.
func ContainerLogs(ctx context.Context, d Deps, params json.RawMessage, in <-chan rpc.Event, emit func(rpc.Event) error) error {
	var req struct {
		ID   string `json:"id"`
		Tail *int   `json:"tail"`
	}
	if err := decode(params, &req); err != nil {
		return fail(emit, err)
	}
	if !docker.ContainerIDRe.MatchString(req.ID) {
		return fail(emit, apierr.New(apierr.InvalidParams, "Ungültige Container-ID"))
	}
	tail := defaultLogTail
	if req.Tail != nil {
		tail = *req.Tail
	}
	if tail < 0 || tail > maxLogTail {
		return fail(emit, apierr.New(apierr.InvalidParams, "Ungültige Zeilenzahl (0 bis 5000)"))
	}
	if d.Stream == nil {
		return fail(emit, apierr.New(apierr.Internal, "Streaming ist nicht verfügbar"))
	}
	r, err := d.Stream.Start(ctx, "docker", "logs", "-f", "--tail", strconv.Itoa(tail), req.ID)
	if err != nil {
		return fail(emit, apierr.New(apierr.Internal, "Docker ist nicht verfügbar"))
	}
	defer r.Close()
	done := pump(r, emit)
	for {
		select {
		case <-done:
			return emit(rpc.Event{Event: "exit"})
		case <-ctx.Done():
			return nil
		case _, ok := <-in:
			if !ok {
				return nil
			}
		}
	}
}

// ContainerShell öffnet eine interaktive Shell im Container (PTY). Eingabe kommt als „input“ (Base64),
// Größenänderungen als „resize“.
func ContainerShell(ctx context.Context, d Deps, params json.RawMessage, in <-chan rpc.Event, emit func(rpc.Event) error) error {
	var req struct {
		ID   string `json:"id"`
		Rows uint16 `json:"rows"`
		Cols uint16 `json:"cols"`
	}
	if err := decode(params, &req); err != nil {
		return fail(emit, err)
	}
	if !docker.ContainerIDRe.MatchString(req.ID) {
		return fail(emit, apierr.New(apierr.InvalidParams, "Ungültige Container-ID"))
	}
	if req.Rows == 0 {
		req.Rows = 24
	}
	if req.Cols == 0 {
		req.Cols = 80
	}
	if req.Rows > maxTermSize || req.Cols > maxTermSize {
		return fail(emit, apierr.New(apierr.InvalidParams, "Ungültige Terminalgröße"))
	}
	if d.Stream == nil {
		return fail(emit, apierr.New(apierr.Internal, "Streaming ist nicht verfügbar"))
	}
	p, err := d.Stream.StartPTY(ctx, req.Rows, req.Cols, "docker", "exec", "-it", "-e", "TERM=xterm-256color", req.ID, "sh", "-c", shellStarter)
	if err != nil {
		return fail(emit, apierr.New(apierr.Internal, "Die Shell konnte nicht gestartet werden"))
	}
	defer p.Close()
	done := pump(p, emit)
	for {
		select {
		case <-done:
			return emit(rpc.Event{Event: "exit"})
		case <-ctx.Done():
			return nil
		case ev, ok := <-in:
			if !ok {
				return nil
			}
			switch ev.Event {
			case "input":
				var b []byte
				if json.Unmarshal(ev.Data, &b) == nil && len(b) > 0 && len(b) <= 64<<10 {
					_, _ = p.Write(b)
				}
			case "resize":
				var sz struct{ Rows, Cols uint16 }
				if json.Unmarshal(ev.Data, &sz) == nil && sz.Rows > 0 && sz.Cols > 0 && sz.Rows <= maxTermSize && sz.Cols <= maxTermSize {
					_ = p.Resize(sz.Rows, sz.Cols)
				}
			}
		}
	}
}

func registerDockerOps(s *rpc.Server, d Deps) {
	s.Handle("container.stats", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return ContainerStatsList(ctx, d)
	})
	s.HandleSession("container.logs", func(ctx context.Context, p json.RawMessage, in <-chan rpc.Event, emit func(rpc.Event) error) error {
		return ContainerLogs(ctx, d, p, in, emit)
	})
	s.HandleSession("container.shell", func(ctx context.Context, p json.RawMessage, in <-chan rpc.Event, emit func(rpc.Event) error) error {
		return ContainerShell(ctx, d, p, in, emit)
	})
}
