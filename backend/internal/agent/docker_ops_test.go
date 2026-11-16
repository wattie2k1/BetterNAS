package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/sysexec"
)

type collector struct {
	mu     sync.Mutex
	events []rpc.Event
	ch     chan rpc.Event
}

func newCollector() *collector { return &collector{ch: make(chan rpc.Event, 64)} }
func (c *collector) emit(ev rpc.Event) error {
	c.ch <- ev
	return nil
}
func (c *collector) next(t *testing.T) rpc.Event {
	t.Helper()
	select {
	case ev := <-c.ch:
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("kein Ereignis")
		return rpc.Event{}
	}
}

func payload(t *testing.T, ev rpc.Event) []byte {
	t.Helper()
	var b []byte
	if err := json.Unmarshal(ev.Data, &b); err != nil {
		t.Fatal(err)
	}
	return b
}

func opsEnv(t *testing.T) (Deps, *sysexec.FakeRunner, *sysexec.FakeStreamer) {
	t.Helper()
	f := &sysexec.FakeRunner{Responses: map[string]sysexec.Result{}}
	s := &sysexec.FakeStreamer{}
	return Deps{Runner: f, FS: sysexec.NewRootFS(t.TempDir()), Stream: s}, f, s
}

func TestContainerLogsStreamsAndExits(t *testing.T) {
	d, _, s := opsEnv(t)
	s.Output = "zeile 1\nzeile 2\n"
	c := newCollector()
	in := make(chan rpc.Event)
	err := ContainerLogs(context.Background(), d, json.RawMessage(`{"id":"0123456789ab","tail":50}`), in, c.emit)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(payload(t, c.next(t))); got != "zeile 1\nzeile 2\n" {
		t.Fatalf("%q", got)
	}
	if ev := c.next(t); ev.Event != "exit" {
		t.Fatalf("%+v", ev)
	}
	if s.Last() != "docker logs -f --tail 50 0123456789ab" || s.Closed != 1 {
		t.Fatalf("%q %d", s.Last(), s.Closed)
	}
}

func TestContainerLogsValidation(t *testing.T) {
	d, _, s := opsEnv(t)
	for _, p := range []string{`{"id":"../x"}`, `{"id":"--all"}`, `{"id":"0123456789ab","tail":5001}`, `{"id":"0123456789ab","tail":-1}`, `kaputt`} {
		c := newCollector()
		if err := ContainerLogs(context.Background(), d, json.RawMessage(p), make(chan rpc.Event), c.emit); err == nil {
			t.Errorf("%s sollte abgelehnt werden", p)
		}
		if ev := c.next(t); ev.Event != "error" {
			t.Errorf("%s: %+v", p, ev)
		}
	}
	if len(s.Calls) != 0 {
		t.Fatalf("ungültige Eingaben dürfen nichts starten: %v", s.Calls)
	}
}

func TestContainerLogsStopsWhenClientLeaves(t *testing.T) {
	d, _, s := opsEnv(t)
	// Endlose Ausgabe: ein Reader, der nie endet.
	s.Output = ""
	in := make(chan rpc.Event)
	done := make(chan error)
	go func() {
		done <- ContainerLogs(context.Background(), d, json.RawMessage(`{"id":"0123456789ab"}`), in, newCollector().emit)
	}()
	close(in) // Client weg
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Logs-Sitzung läuft weiter")
	}
}

func TestContainerShellInputResizeAndExit(t *testing.T) {
	d, _, s := opsEnv(t)
	c := newCollector()
	in := make(chan rpc.Event, 8)
	done := make(chan error, 1)
	go func() {
		done <- ContainerShell(context.Background(), d, json.RawMessage(`{"id":"0123456789ab","rows":30,"cols":100}`), in, c.emit)
	}()
	var p *sysexec.FakePTY
	for i := 0; i < 300 && p == nil; i++ {
		time.Sleep(10 * time.Millisecond)
		p = s.GetPTY()
	}
	if p == nil {
		t.Fatal("PTY nicht gestartet")
	}
	want := "docker exec -it -e TERM=xterm-256color 0123456789ab sh -c command -v bash >/dev/null 2>&1 && exec bash || exec sh"
	if s.Last() != want {
		t.Fatalf("%q", s.Last())
	}
	if r, cols := p.Size(); r != 30 || cols != 100 {
		t.Fatalf("%d x %d", r, cols)
	}
	p.Feed("$ ")
	if got := string(payload(t, c.next(t))); got != "$ " {
		t.Fatalf("%q", got)
	}
	inp, _ := json.Marshal([]byte("ls\n"))
	in <- rpc.Event{Event: "input", Data: inp}
	in <- rpc.Event{Event: "resize", Data: json.RawMessage(`{"rows":50,"cols":120}`)}
	in <- rpc.Event{Event: "resize", Data: json.RawMessage(`{"rows":9999,"cols":120}`)} // ungültig, ignoriert
	for i := 0; i < 300 && (p.Written() == "" || func() bool { r, _ := p.Size(); return r != 50 }()); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if p.Written() != "ls\n" {
		t.Fatalf("Eingabe: %q", p.Written())
	}
	if r, cols := p.Size(); r != 50 || cols != 120 {
		t.Fatalf("Größe %d x %d", r, cols)
	}
	p.Exit()
	if ev := c.next(t); ev.Event != "exit" {
		t.Fatalf("%+v", ev)
	}
	<-done
	if !p.IsClosed() {
		t.Fatal("PTY muss geschlossen werden")
	}
}

func TestContainerShellClosesProcessWhenClientLeaves(t *testing.T) {
	d, _, s := opsEnv(t)
	in := make(chan rpc.Event)
	done := make(chan error, 1)
	go func() {
		done <- ContainerShell(context.Background(), d, json.RawMessage(`{"id":"0123456789ab"}`), in, newCollector().emit)
	}()
	for i := 0; i < 300 && s.GetPTY() == nil; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	close(in)
	<-done
	if !s.GetPTY().IsClosed() {
		t.Fatal("Prozess muss beendet werden")
	}
	if r, c := s.GetPTY().Size(); r != 24 || c != 80 {
		t.Fatalf("Standardgröße: %d x %d", r, c)
	}
}

func TestContainerShellValidation(t *testing.T) {
	d, _, s := opsEnv(t)
	for _, p := range []string{`{"id":"../x"}`, `{"id":"0123456789ab","rows":501}`, `{"id":"0123456789ab","cols":9999}`} {
		c := newCollector()
		if err := ContainerShell(context.Background(), d, json.RawMessage(p), make(chan rpc.Event), c.emit); err == nil {
			t.Errorf("%s sollte abgelehnt werden", p)
		}
	}
	if len(s.Calls) != 0 {
		t.Fatalf("%v", s.Calls)
	}
}

func TestContainerStatsList(t *testing.T) {
	d, f, _ := opsEnv(t)
	f.Responses["docker stats --no-stream --no-trunc --format {{json .}}"] = sysexec.Result{Stdout: []byte(`{"ID":"aaaaaaaaaaaa","Name":"web","CPUPerc":"2.00%","MemUsage":"1MiB / 2MiB","MemPerc":"50.00%"}` + "\n")}
	list, err := ContainerStatsList(context.Background(), d)
	if err != nil || len(list) != 1 || list[0].CPU != 2 || list[0].MemLimit != 2<<20 {
		t.Fatalf("%+v %v", list, err)
	}
}
