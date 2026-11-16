package sysexec

import (
	"bytes"
	"context"
	"io"
	"sync"
)

// FakeStreamer ersetzt Streamer in Tests. Aufrufe werden in Calls aufgezeichnet.
type FakeStreamer struct {
	mu    sync.Mutex
	Calls []string
	// Output ist die Ausgabe, die Start liefert (danach EOF).
	Output string
	// Err lässt Start/StartPTY fehlschlagen.
	Err error
	// LastPTY ist das zuletzt gestartete Pseudo-Terminal.
	LastPTY *FakePTY
	// Closed zählt geschlossene Ströme.
	Closed int
}

func (f *FakeStreamer) Start(_ context.Context, name string, args ...string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, CommandLine(name, args...))
	if f.Err != nil {
		return nil, f.Err
	}
	return &fakeReader{Reader: bytes.NewReader([]byte(f.Output)), f: f}, nil
}

func (f *FakeStreamer) StartPTY(_ context.Context, rows, cols uint16, name string, args ...string) (PTY, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, CommandLine(name, args...))
	if f.Err != nil {
		return nil, f.Err
	}
	pr, pw := io.Pipe()
	p := &FakePTY{out: pr, feed: pw, Rows: rows, Cols: cols, owner: f}
	f.LastPTY = p
	return p, nil
}

// Last liefert die letzte aufgezeichnete Befehlszeile.
func (f *FakeStreamer) Last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Calls) == 0 {
		return ""
	}
	return f.Calls[len(f.Calls)-1]
}

type fakeReader struct {
	io.Reader
	f    *FakeStreamer
	once sync.Once
}

func (r *fakeReader) Close() error {
	r.once.Do(func() {
		r.f.mu.Lock()
		r.f.Closed++
		r.f.mu.Unlock()
	})
	return nil
}

// FakePTY ist ein Pseudo-Terminal für Tests: Feed liefert Ausgabe des „Prozesses“, Written die Eingabe.
type FakePTY struct {
	mu         sync.Mutex
	out        *io.PipeReader
	feed       *io.PipeWriter
	written    bytes.Buffer
	Rows, Cols uint16
	closed     bool
	owner      *FakeStreamer
}

// Feed lässt den „Prozess“ etwas ausgeben.
func (p *FakePTY) Feed(s string) { _, _ = p.feed.Write([]byte(s)) }

// Exit beendet den „Prozess“ (Lesen liefert EOF).
func (p *FakePTY) Exit() { _ = p.feed.Close() }

// Written liefert alles, was in das Terminal geschrieben wurde.
func (p *FakePTY) Written() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.written.String()
}

// IsClosed meldet, ob Close aufgerufen wurde.
func (p *FakePTY) IsClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// Size liefert die zuletzt gesetzte Größe.
func (p *FakePTY) Size() (rows, cols uint16) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.Rows, p.Cols
}

func (p *FakePTY) Read(b []byte) (int, error) { return p.out.Read(b) }
func (p *FakePTY) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.written.Write(b)
}
func (p *FakePTY) Resize(rows, cols uint16) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Rows, p.Cols = rows, cols
	return nil
}
func (p *FakePTY) Close() error {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	_ = p.out.Close()
	return p.feed.Close()
}
