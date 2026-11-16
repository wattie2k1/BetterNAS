package sysexec

import (
	"context"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestStreamStartAndClose(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	s := NewStreamer()
	r, err := s.Start(context.Background(), "sh", "-c", "echo hallo; echo fehler 1>&2")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	r.Close()
	if !strings.Contains(string(b), "hallo") || !strings.Contains(string(b), "fehler") {
		t.Fatalf("%q", b)
	}

	// Close beendet einen endlosen Prozess.
	r, err = s.Start(context.Background(), "sh", "-c", "while true; do echo x; sleep 0.05; done")
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2)
	if _, err := r.Read(buf); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { r.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close hängt")
	}
}

func TestStartPTYEchoAndResize(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	s := NewStreamer()
	p, err := s.StartPTY(context.Background(), 24, 80, "cat")
	if err != nil {
		t.Skipf("kein PTY verfügbar: %v", err)
	}
	defer p.Close()
	if err := p.Resize(40, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Write([]byte("hi\n")); err != nil {
		t.Fatal(err)
	}
	var got string
	buf := make([]byte, 64)
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(got, "hi") && time.Now().Before(deadline) {
		n, err := p.Read(buf)
		if err != nil {
			break
		}
		got += string(buf[:n])
	}
	if !strings.Contains(got, "hi") {
		t.Fatalf("%q", got)
	}
}

func TestFakeStreamer(t *testing.T) {
	f := &FakeStreamer{Output: "abc"}
	r, _ := f.Start(context.Background(), "docker", "logs", "x")
	b, _ := io.ReadAll(r)
	r.Close()
	if string(b) != "abc" || f.Last() != "docker logs x" || f.Closed != 1 {
		t.Fatalf("%q %q %d", b, f.Last(), f.Closed)
	}
}
