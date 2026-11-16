package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/betternas/betternas/internal/agent"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/docker"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/sysexec"
)

// streamEnv startet einen echten rpc-Server mit den Agent-Methoden (Fake-Streamer) und die API davor.
type streamEnv struct {
	srv      *httptest.Server
	cookie   *http.Cookie
	streamer *sysexec.FakeStreamer
	runner   *sysexec.FakeRunner
}

func newStreamEnv(t *testing.T) (*streamEnv, func(string) string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "ws")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "a.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	fs := &sysexec.FakeStreamer{}
	fr := &sysexec.FakeRunner{Responses: map[string]sysexec.Result{}}
	as := rpc.NewServer(nil)
	agent.Register(as, agent.Deps{Runner: fr, FS: sysexec.NewRootFS(dir), Stream: fs})
	go as.Serve(l)

	d, err := db.Open(filepath.Join(dir, "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	jm := jobs.NewManager(d, hub)
	store := auth.NewStore(d, time.Hour)
	token, _, _ := store.Create(context.Background(), "anna")
	client := rpc.NewClient(sock)
	h := NewServer(Config{WebDir: t.TempDir(), Dev: true}, Deps{
		DB: d, Jobs: jm, Hub: hub, Agent: client,
		Auth:   auth.NewHandlers(d, store, nil, nil, false),
		Docker: docker.NewManager(d, client, jm, hub),
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	audits := func(action string) string {
		var n string
		_ = d.QueryRow(`SELECT COUNT(*) FROM audit WHERE action = ?`, action).Scan(&n)
		return n
	}
	return &streamEnv{srv: srv, cookie: &http.Cookie{Name: auth.CookieName, Value: token}, streamer: fs, runner: fr}, audits
}

func (e *streamEnv) dial(t *testing.T, path string, withCookie bool) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	h := http.Header{}
	if withCookie {
		h.Set("Cookie", e.cookie.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.srv.URL, "http")+path, &websocket.DialOptions{HTTPHeader: h})
}

func TestLogsWebSocketStreamsOutput(t *testing.T) {
	e, _ := newStreamEnv(t)
	e.streamer.Output = "hallo welt\n"
	c, _, err := e.dial(t, "/api/v1/containers/0123456789ab/logs?tail=10", true)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, b, err := c.Read(ctx)
	if err != nil || typ != websocket.MessageText || string(b) != "hallo welt\n" {
		t.Fatalf("%v %q %v", typ, b, err)
	}
	if e.streamer.Last() != "docker logs -f --tail 10 0123456789ab" {
		t.Fatalf("%q", e.streamer.Last())
	}
	// Danach beendet der Container: normaler Verbindungsschluss.
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("%v", err)
	}
}

func TestShellWebSocketRoundTrip(t *testing.T) {
	e, audits := newStreamEnv(t)
	c, _, err := e.dial(t, "/api/v1/containers/0123456789ab/shell?rows=30&cols=100", true)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var p *sysexec.FakePTY
	for i := 0; i < 300 && p == nil; i++ {
		time.Sleep(10 * time.Millisecond)
		p = e.streamer.GetPTY()
	}
	if p == nil {
		t.Fatal("PTY nicht gestartet")
	}
	p.Feed("$ ")
	typ, b, err := c.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || string(b) != "$ " {
		t.Fatalf("%v %q %v", typ, b, err)
	}
	if err := c.Write(ctx, websocket.MessageBinary, []byte("ls\n")); err != nil {
		t.Fatal(err)
	}
	ctl, _ := json.Marshal(map[string]any{"type": "resize", "rows": 50, "cols": 120})
	if err := c.Write(ctx, websocket.MessageText, ctl); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 300; i++ {
		if r, cols := p.Size(); p.Written() == "ls\n" && r == 50 && cols == 120 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r, cols := p.Size(); p.Written() != "ls\n" || r != 50 || cols != 120 {
		t.Fatalf("Eingabe %q Größe %dx%d", p.Written(), r, cols)
	}
	if audits("container.shell") != "1" {
		t.Fatal("Shell-Start muss im Audit-Log stehen")
	}
	c.Close(websocket.StatusNormalClosure, "")
	for i := 0; i < 300 && !p.IsClosed(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if !p.IsClosed() {
		t.Fatal("Schließen des WebSockets muss den Prozess beenden")
	}
}

func TestStreamEndpointsRejectBadInputAndNoSession(t *testing.T) {
	e, _ := newStreamEnv(t)
	for _, path := range []string{"/api/v1/containers/0123456789ab/logs", "/api/v1/containers/0123456789ab/shell"} {
		if _, resp, err := e.dial(t, path, false); err == nil || resp == nil || resp.StatusCode != 401 {
			t.Errorf("%s ohne Sitzung: %v %v", path, resp, err)
		}
	}
	for _, path := range []string{"/api/v1/containers/zzz/logs", "/api/v1/containers/0123456789ab/logs?tail=99999", "/api/v1/containers/0123456789ab/shell?rows=0", "/api/v1/containers/0123456789ab/shell?cols=abc"} {
		if _, resp, err := e.dial(t, path, true); err == nil || resp == nil || resp.StatusCode != 400 {
			t.Errorf("%s: %v %v", path, resp, err)
		}
	}
	if len(e.streamer.Calls) != 0 {
		t.Fatalf("nichts darf gestartet werden: %v", e.streamer.Calls)
	}
}

func TestStatsEndpoint(t *testing.T) {
	e, _ := newStreamEnv(t)
	e.runner.Responses["docker stats --no-stream --no-trunc --format {{json .}}"] = sysexec.Result{Stdout: []byte(`{"ID":"aaaaaaaaaaaa","Name":"web","CPUPerc":"2.00%","MemUsage":"1MiB / 2MiB","MemPerc":"50.00%"}` + "\n")}
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/containers/stats", nil)
	req.AddCookie(e.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got []docker.ContainerStats
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil || resp.StatusCode != 200 || len(got) != 1 || got[0].Name != "web" {
		t.Fatalf("%d %+v %v", resp.StatusCode, got, err)
	}
}
