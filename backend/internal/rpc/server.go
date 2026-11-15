package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"slices"
	"sync"

	"github.com/betternas/betternas/internal/apierr"
)

// Handler bearbeitet einen Aufruf.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// StreamHandler liefert Ereignisse, bis ctx endet.
type StreamHandler func(ctx context.Context, emit func(Event) error) error

// Server nimmt Aufrufe entgegen.
type Server struct {
	allowed  []uint32
	mu       sync.RWMutex
	handlers map[string]Handler
	streams  map[string]StreamHandler
}

// NewServer erzeugt einen Server. allowedUIDs == nil erlaubt jeden Peer (nur für Tests/Entwicklung).
func NewServer(allowedUIDs []uint32) *Server {
	return &Server{allowed: allowedUIDs, handlers: map[string]Handler{}, streams: map[string]StreamHandler{}}
}

// Handle registriert eine Methode.
func (s *Server) Handle(method string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = h
}

// HandleStream registriert ein Abonnement.
func (s *Server) HandleStream(method string, h StreamHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streams[method] = h
}

// Serve nimmt Verbindungen an, bis der Listener geschlossen wird.
func (s *Server) Serve(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go s.serveConn(conn)
	}
}

func (s *Server) serveConn(conn net.Conn) {
	defer conn.Close()
	if s.allowed != nil {
		uid, err := peerUID(conn)
		if err != nil || !slices.Contains(s.allowed, uid) {
			slog.Warn("rpc: verbindung abgelehnt", "uid", uid, "err", err)
			return
		}
	}
	r := bufio.NewReaderSize(conn, 64*1024)
	line, err := readLine(r)
	if err != nil {
		return
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		writeJSON(conn, Response{Error: apierr.New(apierr.InvalidParams, "Ungültige Anfrage")})
		return
	}
	s.mu.RLock()
	h, okH := s.handlers[req.Method]
	sh, okS := s.streams[req.Method]
	s.mu.RUnlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	switch {
	case okS:
		// Verbindung schließt der Client → Lesen schlägt fehl → ctx endet.
		go func() {
			_, _ = r.ReadByte()
			cancel()
		}()
		var wmu sync.Mutex
		_ = sh(ctx, func(ev Event) error {
			wmu.Lock()
			defer wmu.Unlock()
			return writeJSON(conn, ev)
		})
	case okH:
		res, err := h(ctx, req.Params)
		resp := Response{ID: req.ID}
		if err != nil {
			resp.Error = apierr.From(err)
			if resp.Error.Code == apierr.Internal {
				slog.Error("rpc: interner fehler", "method", req.Method, "err", err)
			}
		} else if res != nil {
			data, merr := json.Marshal(res)
			if merr != nil {
				resp.Error = apierr.New(apierr.Internal, "Interner Fehler")
			} else {
				resp.Result = data
			}
		}
		_ = writeJSON(conn, resp)
	default:
		_ = writeJSON(conn, Response{ID: req.ID, Error: apierr.New(apierr.NotFound, "Unbekannte Methode: "+req.Method)})
	}
}

func readLine(r *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		buf = append(buf, chunk...)
		if len(buf) > MaxLine {
			return nil, errors.New("nachricht zu groß")
		}
		if !isPrefix {
			return buf, nil
		}
	}
}

func writeJSON(conn net.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = conn.Write(append(data, '\n'))
	return err
}
