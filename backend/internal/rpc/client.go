package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/betternas/betternas/internal/apierr"
)

// Client ruft Methoden des Agents auf. Jeder Aufruf nutzt eine eigene Verbindung.
type Client struct {
	path string
	next atomic.Uint64
}

// NewClient erzeugt einen Client für den Socket unter socketPath.
func NewClient(socketPath string) *Client { return &Client{path: socketPath} }

func (c *Client) dial(ctx context.Context, method string, params any) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.path)
	if err != nil {
		return nil, apierr.New(apierr.Internal, "Agent nicht erreichbar")
	}
	var raw json.RawMessage
	if params != nil {
		if raw, err = json.Marshal(params); err != nil {
			conn.Close()
			return nil, err
		}
	}
	if err := writeJSON(conn, Request{ID: c.next.Add(1), Method: method, Params: raw}); err != nil {
		conn.Close()
		return nil, apierr.New(apierr.Internal, "Agent nicht erreichbar")
	}
	return conn, nil
}

// Call ruft method auf und dekodiert das Ergebnis in result (darf nil sein).
// Fehler des Agents werden als *apierr.Error zurückgegeben.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	conn, err := c.dial(ctx, method, params)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	line, err := readLine(bufio.NewReaderSize(conn, 64*1024))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return apierr.New(apierr.Internal, "Agent hat die Verbindung beendet")
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return fmt.Errorf("ungültige antwort des agents: %w", err)
	}
	if resp.Error != nil {
		return resp.Error
	}
	if result != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, result)
	}
	return nil
}

// Subscribe abonniert einen Ereignisstrom. Der Kanal wird geschlossen, wenn ctx endet
// oder die Verbindung abbricht.
func (c *Client) Subscribe(ctx context.Context, method string) (<-chan Event, error) {
	conn, err := c.dial(ctx, method, nil)
	if err != nil {
		return nil, err
	}
	ch := make(chan Event, 16)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	go func() {
		defer close(ch)
		defer stop()
		defer conn.Close()
		r := bufio.NewReaderSize(conn, 64*1024)
		for {
			line, err := readLine(r)
			if err != nil {
				return
			}
			var ev Event
			if json.Unmarshal(line, &ev) != nil {
				continue
			}
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

// Session ist eine offene bidirektionale Sitzung mit dem Agent.
type Session struct {
	conn   net.Conn
	events chan Event
	wmu    sync.Mutex
	stop   func() bool
}

// OpenSession startet method mit params und liefert eine Sitzung. Der Kanal Events wird geschlossen,
// wenn der Agent die Sitzung beendet, ctx endet oder Close aufgerufen wird.
func (c *Client) OpenSession(ctx context.Context, method string, params any) (*Session, error) {
	conn, err := c.dial(ctx, method, params)
	if err != nil {
		return nil, err
	}
	s := &Session{conn: conn, events: make(chan Event, 64)}
	s.stop = context.AfterFunc(ctx, func() { conn.Close() })
	go func() {
		defer close(s.events)
		defer conn.Close()
		r := bufio.NewReaderSize(conn, 64*1024)
		for {
			line, err := readLine(r)
			if err != nil {
				return
			}
			var ev Event
			if json.Unmarshal(line, &ev) != nil {
				continue
			}
			select {
			case s.events <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return s, nil
}

// Events liefert die Ereignisse des Agents.
func (s *Session) Events() <-chan Event { return s.events }

// Send schickt ein Ereignis an den Agent.
func (s *Session) Send(ev Event) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	return writeJSON(s.conn, ev)
}

// Close beendet die Sitzung.
func (s *Session) Close() {
	s.stop()
	s.conn.Close()
}
