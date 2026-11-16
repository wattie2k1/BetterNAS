package sysexec

import (
	"context"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

// PTY ist ein Pseudo-Terminal mit laufendem Prozess. Close beendet den Prozess.
type PTY interface {
	io.ReadWriteCloser
	Resize(rows, cols uint16) error
}

// Streamer startet langlaufende Prozesse und liefert deren Ausgabe als Strom.
type Streamer interface {
	// Start startet name und liefert stdout+stderr. Close beendet den Prozess.
	Start(ctx context.Context, name string, args ...string) (io.ReadCloser, error)
	// StartPTY startet name in einem Pseudo-Terminal.
	StartPTY(ctx context.Context, rows, cols uint16, name string, args ...string) (PTY, error)
}

type execStreamer struct{}

// NewStreamer liefert einen Streamer, der echte Prozesse startet.
func NewStreamer() Streamer { return execStreamer{} }

func command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"LC_ALL=C.UTF-8", "TERM=xterm-256color", "PATH=" + defaultPath()}
	return cmd
}

type procReader struct {
	io.ReadCloser
	cmd  *exec.Cmd
	once sync.Once
}

func (p *procReader) Close() error {
	p.once.Do(func() {
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
	})
	return p.ReadCloser.Close()
}

func (execStreamer) Start(ctx context.Context, name string, args ...string) (io.ReadCloser, error) {
	cmd := command(ctx, name, args...)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		pw.Close()
		return nil, err
	}
	go func() {
		_ = cmd.Wait()
		pw.Close()
	}()
	return &procReader{ReadCloser: pr, cmd: cmd}, nil
}

type ptyProc struct {
	f    *os.File
	cmd  *exec.Cmd
	once sync.Once
}

func (p *ptyProc) Read(b []byte) (int, error)  { return p.f.Read(b) }
func (p *ptyProc) Write(b []byte) (int, error) { return p.f.Write(b) }
func (p *ptyProc) Resize(rows, cols uint16) error {
	return pty.Setsize(p.f, &pty.Winsize{Rows: rows, Cols: cols})
}
func (p *ptyProc) Close() error {
	var err error
	p.once.Do(func() {
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		err = p.f.Close()
		go func() { _ = p.cmd.Wait() }()
	})
	return err
}

func (execStreamer) StartPTY(ctx context.Context, rows, cols uint16, name string, args ...string) (PTY, error) {
	cmd := command(ctx, name, args...)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		return nil, err
	}
	return &ptyProc{f: f, cmd: cmd}, nil
}
