package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/vm"
)

type vmAgentStub struct{ calls map[string]any }

func (a *vmAgentStub) Call(_ context.Context, method string, params, result any) error {
	if a.calls == nil {
		a.calls = map[string]any{}
	}
	// Parameter wie über RPC als JSON-Objekt festhalten.
	b, _ := json.Marshal(params)
	var p any
	_ = json.Unmarshal(b, &p)
	a.calls[method] = p
	var out any
	switch method {
	case "vm.status":
		out = vm.Status{Installed: true, Active: true, KVM: true}
	case "vm.list":
		out = []vm.VM{{Name: "debian", State: "running", VCPUs: 2, MemoryMiB: 4096, Disks: []string{}}}
	case "vm.networks":
		out = []string{"default"}
	case "vm.isos":
		out = []string{"debian.iso"}
	case "vm.snapshots":
		out = []vm.Snapshot{{Name: "s1", Created: "2026-11-17 10:00:00 +0100", State: "shutoff"}}
	case "vm.interfaces":
		out = []string{"eno1"}
	case "vm.display":
		out = map[string]int{"port": 5901}
	case "vm.disk.add":
		out = map[string]string{"path": "/srv/vms/disks/debian-2.qcow2"}
	case "vm.usb":
		out = []vm.USBDevice{{Vendor: "1a86", Product: "7523", Name: "QinHeng USB Serial"}}
	case "vm.disks":
		out = []vm.Disk{{Path: "/srv/vms/disks/debian.qcow2", SizeGiB: 40, VirtualSize: 40 * vm.GiB, UsedBytes: 1 << 30}}
	case "vm.config":
		out = vm.Config{Name: "debian", State: "shutoff", VCPUs: 2, MemoryMiB: 4096, Firmware: "uefi", Network: "default", Disks: []string{}}
	}
	if result != nil && out != nil {
		b, _ := json.Marshal(out)
		return json.Unmarshal(b, result)
	}
	return nil
}

func newVMAPI(t *testing.T) (*storageAPI, *vmAgentStub) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	jm := jobs.NewManager(d, hub)
	store := auth.NewStore(d, time.Hour)
	token, csrf, _ := store.Create(context.Background(), "anna")
	stub := &vmAgentStub{}
	mgr := vm.NewManager(d, stub, hub)
	mgr.SetISODir(t.TempDir())
	h := NewServer(Config{WebDir: t.TempDir(), Dev: true}, Deps{
		DB: d, Jobs: jm, Hub: hub,
		Auth: auth.NewHandlers(d, store, nil, nil, false),
		VMs:  mgr,
	})
	return &storageAPI{h: h, cookie: &http.Cookie{Name: auth.CookieName, Value: token}, csrf: csrf, jobs: jm}, stub
}

func TestVMRoutes(t *testing.T) {
	s, stub := newVMAPI(t)
	for path, want := range map[string]string{
		"/api/v1/vms/status": `"kvm":true`, "/api/v1/vms": `"name":"debian"`,
		"/api/v1/vms/networks": `"default"`, "/api/v1/vms/isos": `"debian.iso"`,
	} {
		if w := s.do("GET", path, ""); w.Code != 200 || !strings.Contains(w.Body.String(), want) {
			t.Fatalf("%s %d %s", path, w.Code, w.Body)
		}
	}
	if w := s.do("POST", "/api/v1/vms/debian/start", ""); w.Code != 204 {
		t.Fatalf("start %d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/vms/debian/undefine", ""); w.Code != 404 && w.Code != 405 {
		t.Fatalf("unbekannte Aktion %d", w.Code)
	}
	if w := s.do("DELETE", "/api/v1/vms/debian?disks=1", ""); w.Code != 204 {
		t.Fatalf("delete %d %s", w.Code, w.Body)
	}
	if p, _ := stub.calls["vm.delete"].(map[string]any); p["deleteDisks"] != true {
		t.Fatalf("disks-Flag: %v", stub.calls["vm.delete"])
	}
	if w := s.do("DELETE", "/api/v1/vms/debian", ""); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if p, _ := stub.calls["vm.delete"].(map[string]any); p["deleteDisks"] != false {
		t.Fatalf("ohne disks: %v", stub.calls["vm.delete"])
	}
}

func TestCreateRoute(t *testing.T) {
	s, stub := newVMAPI(t)
	body := `{"name":"debian","os":"linux","firmware":"uefi","network":"default","vcpus":2,"memoryMiB":4096,"diskGiB":40,"tpm":false,"start":true}`
	if w := s.do("POST", "/api/v1/vms", body); w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	p, _ := stub.calls["vm.create"].(map[string]any)
	if p["name"] != "debian" || p["memoryMiB"] != float64(4096) || p["start"] != true {
		t.Fatalf("%v", p)
	}
	if w := s.do("POST", "/api/v1/vms", `{"name":"../x"}`); w.Code != 400 {
		t.Fatalf("ungültig %d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/vms", `kaputt`); w.Code != 400 {
		t.Fatalf("json %d", w.Code)
	}
}

func TestVMRoutesRequireAuth(t *testing.T) {
	s, _ := newVMAPI(t)
	s.cookie = &http.Cookie{Name: auth.CookieName, Value: "ungueltig"}
	if w := s.do("GET", "/api/v1/vms", ""); w.Code != 401 {
		t.Fatalf("%d", w.Code)
	}
}

func TestSnapshotAndCloneRoutes(t *testing.T) {
	s, stub := newVMAPI(t)
	if w := s.do("GET", "/api/v1/vms/debian/snapshots", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"s1"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/vms/debian/snapshots", `{"name":"vor-update"}`); w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if p, _ := stub.calls["vm.snapshot.create"].(map[string]any); p["snapshot"] != "vor-update" || p["name"] != "debian" {
		t.Fatalf("%v", stub.calls["vm.snapshot.create"])
	}
	if w := s.do("POST", "/api/v1/vms/debian/snapshots", `{"name":"--all"}`); w.Code != 400 {
		t.Fatalf("ungültiger Name %d", w.Code)
	}
	if w := s.do("POST", "/api/v1/vms/debian/snapshots/s1/revert", ""); w.Code != 204 {
		t.Fatalf("%d", w.Code)
	}
	if w := s.do("DELETE", "/api/v1/vms/debian/snapshots/s1", ""); w.Code != 204 {
		t.Fatalf("%d", w.Code)
	}
	if w := s.do("POST", "/api/v1/vms/debian/clone", `{"name":"debian2"}`); w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if p, _ := stub.calls["vm.clone"].(map[string]any); p["newName"] != "debian2" {
		t.Fatalf("%v", stub.calls["vm.clone"])
	}
	if w := s.do("POST", "/api/v1/vms/debian/clone", `{"name":"../x"}`); w.Code != 400 {
		t.Fatalf("%d", w.Code)
	}
	if w := s.do("GET", "/api/v1/vms/interfaces", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "eno1") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestISOUploadRoute(t *testing.T) {
	s, stub := newVMAPI(t)
	if w := s.do("PUT", "/api/v1/vms/isos/neu.iso", "inhalt"); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := s.do("PUT", "/api/v1/vms/isos/neu.iso", "zweite"); w.Code != 409 {
		t.Fatalf("keine Überschreibung: %d", w.Code)
	}
	for _, name := range []string{"x.txt", ".iso", "a..b.exe"} {
		if w := s.do("PUT", "/api/v1/vms/isos/"+name, "x"); w.Code != 400 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if w := s.do("DELETE", "/api/v1/vms/isos/neu.iso", ""); w.Code != 204 {
		t.Fatalf("%d", w.Code)
	}
	if p, _ := stub.calls["vm.iso.delete"].(map[string]any); p["file"] != "neu.iso" {
		t.Fatalf("%v", stub.calls["vm.iso.delete"])
	}
}

func TestProxyVNC(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { // Mini-VNC-Server: Banner senden, danach alles zurückspiegeln
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = c.Write([]byte("RFB 003.008\n"))
		_, _ = io.Copy(c, c)
	}()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxyVNC(w, r, ln.Addr().String()) }))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{Subprotocols: []string{"binary"}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if c.Subprotocol() != "binary" {
		t.Fatalf("Subprotokoll %q", c.Subprotocol())
	}
	typ, b, err := c.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || string(b) != "RFB 003.008\n" {
		t.Fatalf("%v %v %q", typ, err, b)
	}
	if err := c.Write(ctx, websocket.MessageBinary, []byte("hallo")); err != nil {
		t.Fatal(err)
	}
	if _, b, err = c.Read(ctx); err != nil || !bytes.Equal(b, []byte("hallo")) {
		t.Fatalf("%v %q", err, b)
	}
}

func TestConsoleRouteNeedsAuthAndVM(t *testing.T) {
	s, _ := newVMAPI(t)
	if w := s.do("GET", "/api/v1/vms/-bad/console", ""); w.Code != 400 {
		t.Fatalf("%d", w.Code)
	}
	s.cookie = &http.Cookie{Name: auth.CookieName, Value: "ungueltig"}
	if w := s.do("GET", "/api/v1/vms/debian/console", ""); w.Code != 401 {
		t.Fatalf("%d", w.Code)
	}
}

func TestConfigRoutes(t *testing.T) {
	s, stub := newVMAPI(t)
	if w := s.do("GET", "/api/v1/vms/debian/config", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"firmware":"uefi"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	body := `{"vcpus":4,"memoryMiB":2048,"secureBoot":false,"network":"direct:eno1","media":"a.iso"}`
	if w := s.do("POST", "/api/v1/vms/debian/config", body); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if p, _ := stub.calls["vm.configure"].(map[string]any); p["name"] != "debian" || p["vcpus"] != float64(4) || p["network"] != "direct:eno1" || p["media"] != "a.iso" {
		t.Fatalf("%v", stub.calls["vm.configure"])
	}
	for _, bad := range []string{`{"vcpus":0,"memoryMiB":2048,"network":"default"}`, `{"vcpus":2,"memoryMiB":2048,"network":"default","media":"../x.iso"}`, `kaputt`} {
		if w := s.do("POST", "/api/v1/vms/debian/config", bad); w.Code != 400 {
			t.Fatalf("%q: %d", bad, w.Code)
		}
	}
	if w := s.do("GET", "/api/v1/vms/-bad/config", ""); w.Code != 400 {
		t.Fatalf("%d", w.Code)
	}
}

func TestDiskRoutes(t *testing.T) {
	s, stub := newVMAPI(t)
	if w := s.do("GET", "/api/v1/vms/debian/disks", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"sizeGiB":40`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	body := `{"path":"/srv/vms/disks/debian.qcow2","sizeGiB":80,"deleteSnapshots":true}`
	if w := s.do("POST", "/api/v1/vms/debian/disks/resize", body); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if p, _ := stub.calls["vm.disk.resize"].(map[string]any); p["name"] != "debian" || p["sizeGiB"] != float64(80) || p["deleteSnapshots"] != true {
		t.Fatalf("%v", stub.calls["vm.disk.resize"])
	}
	for _, bad := range []string{`{"path":"/etc/passwd","sizeGiB":80}`, `{"path":"/srv/vms/disks/../x.qcow2","sizeGiB":80}`, `{"path":"/srv/vms/disks/a.qcow2","sizeGiB":0}`, `kaputt`} {
		if w := s.do("POST", "/api/v1/vms/debian/disks/resize", bad); w.Code != 400 {
			t.Fatalf("%q: %d", bad, w.Code)
		}
	}
}

func TestUSBRoute(t *testing.T) {
	s, stub := newVMAPI(t)
	if w := s.do("GET", "/api/v1/vms/usb", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"vendor":"1a86"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	body := `{"vcpus":2,"memoryMiB":2048,"network":"default","bootFirst":"hd","mac":"52:54:00:aa:bb:cc","usb":[{"vendor":"1a86","product":"7523"}]}`
	if w := s.do("POST", "/api/v1/vms/debian/config", body); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	p, _ := stub.calls["vm.configure"].(map[string]any)
	if p["bootFirst"] != "hd" || p["mac"] != "52:54:00:aa:bb:cc" || len(p["usb"].([]any)) != 1 {
		t.Fatalf("%v", p)
	}
	if w := s.do("POST", "/api/v1/vms/debian/config", `{"vcpus":2,"memoryMiB":2048,"network":"default","mac":"x"}`); w.Code != 400 {
		t.Fatalf("%d", w.Code)
	}
}

func TestTuningRoute(t *testing.T) {
	s, stub := newVMAPI(t)
	body := `{"vcpus":4,"memoryMiB":2048,"network":"default","cpuMode":"host-model","topology":{"sockets":1,"threads":2},"hyperv":true,"clock":"localtime","extraNics":[{"network":"default","model":"virtio"}],"diskOpts":[{"path":"/srv/vms/disks/d.qcow2","cache":"none","discard":"unmap"}]}`
	if w := s.do("POST", "/api/v1/vms/debian/config", body); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	p, _ := stub.calls["vm.configure"].(map[string]any)
	if p["cpuMode"] != "host-model" || p["hyperv"] != true || p["clock"] != "localtime" || p["topology"].(map[string]any)["threads"] != float64(2) || len(p["extraNics"].([]any)) != 1 || len(p["diskOpts"].([]any)) != 1 {
		t.Fatalf("%v", p)
	}
	for _, bad := range []string{
		`{"vcpus":5,"memoryMiB":2048,"network":"default","topology":{"sockets":1,"threads":2}}`,
		`{"vcpus":2,"memoryMiB":2048,"network":"default","clock":"gmt"}`,
		`{"vcpus":2,"memoryMiB":2048,"network":"default","diskOpts":[{"path":"/x","cache":"unsafe","discard":"unmap"}]}`,
	} {
		if w := s.do("POST", "/api/v1/vms/debian/config", bad); w.Code != 400 {
			t.Fatalf("%q: %d", bad, w.Code)
		}
	}
}

func TestDiskAddRemoveRoutes(t *testing.T) {
	s, stub := newVMAPI(t)
	w := s.do("POST", "/api/v1/vms/debian/disks", `{"sizeGiB":50,"deleteSnapshots":true}`)
	if w.Code != 201 || !strings.Contains(w.Body.String(), "debian-2.qcow2") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if p, _ := stub.calls["vm.disk.add"].(map[string]any); p["name"] != "debian" || p["sizeGiB"] != float64(50) || p["deleteSnapshots"] != true {
		t.Fatalf("%v", stub.calls["vm.disk.add"])
	}
	if w := s.do("POST", "/api/v1/vms/debian/disks/remove", `{"path":"/srv/vms/disks/debian-2.qcow2","deleteFile":true}`); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if p, _ := stub.calls["vm.disk.remove"].(map[string]any); p["path"] != "/srv/vms/disks/debian-2.qcow2" || p["deleteFile"] != true {
		t.Fatalf("%v", stub.calls["vm.disk.remove"])
	}
	for _, bad := range []struct{ path, body string }{
		{"/api/v1/vms/debian/disks", `{"sizeGiB":0}`},
		{"/api/v1/vms/debian/disks", `{"sizeGiB":70000}`},
		{"/api/v1/vms/debian/disks", `kaputt`},
		{"/api/v1/vms/debian/disks/remove", `{"path":"/etc/passwd"}`},
		{"/api/v1/vms/debian/disks/remove", `{"path":"/srv/vms/disks/../x.qcow2"}`},
		{"/api/v1/vms/-bad/disks", `{"sizeGiB":5}`},
	} {
		if w := s.do("POST", bad.path, bad.body); w.Code != 400 {
			t.Fatalf("%s %q: %d", bad.path, bad.body, w.Code)
		}
	}
}

func TestRenameRoute(t *testing.T) {
	s, stub := newVMAPI(t)
	if w := s.do("POST", "/api/v1/vms/debian/rename", `{"newName":"neu","renameFiles":true,"deleteSnapshots":true}`); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if p, _ := stub.calls["vm.rename"].(map[string]any); p["name"] != "debian" || p["newName"] != "neu" || p["renameFiles"] != true || p["deleteSnapshots"] != true {
		t.Fatalf("%v", stub.calls["vm.rename"])
	}
	for _, bad := range []string{`{"newName":"Falsch"}`, `{"newName":""}`, `kaputt`} {
		if w := s.do("POST", "/api/v1/vms/debian/rename", bad); w.Code != 400 {
			t.Fatalf("%q: %d", bad, w.Code)
		}
	}
	if w := s.do("POST", "/api/v1/vms/-bad/rename", `{"newName":"x"}`); w.Code != 400 {
		t.Fatalf("%d", w.Code)
	}
}

func TestLiveRoute(t *testing.T) {
	s, stub := newVMAPI(t)
	body := `{"memoryMiB":6144,"vcpus":3,"usb":[{"vendor":"1a86","product":"7523"}],"media":"a.iso"}`
	if w := s.do("POST", "/api/v1/vms/debian/live", body); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	p, _ := stub.calls["vm.live"].(map[string]any)
	if p["name"] != "debian" || p["memoryMiB"] != float64(6144) || p["vcpus"] != float64(3) || p["media"] != "a.iso" || len(p["usb"].([]any)) != 1 {
		t.Fatalf("%v", p)
	}
	for _, bad := range []string{`{"memoryMiB":10}`, `{"vcpus":100}`, `{"media":"../x.iso"}`, `{"usb":[{"vendor":"x","product":"y"}]}`, `kaputt`} {
		if w := s.do("POST", "/api/v1/vms/debian/live", bad); w.Code != 400 {
			t.Fatalf("%q: %d", bad, w.Code)
		}
	}
}

func TestSharedDirsRoute(t *testing.T) {
	s, stub := newVMAPI(t)
	body := `{"vcpus":2,"memoryMiB":2048,"network":"default","sharedDirs":[{"path":"/srv/pools/test/Medien","tag":"medien","readOnly":true}]}`
	if w := s.do("POST", "/api/v1/vms/debian/config", body); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	p, _ := stub.calls["vm.configure"].(map[string]any)
	l, _ := p["sharedDirs"].([]any)
	if len(l) != 1 || l[0].(map[string]any)["tag"] != "medien" || l[0].(map[string]any)["readOnly"] != true {
		t.Fatalf("%v", p)
	}
	for _, bad := range []string{
		`{"vcpus":2,"memoryMiB":2048,"network":"default","sharedDirs":[{"path":"/etc","tag":"x"}]}`,
		`{"vcpus":2,"memoryMiB":2048,"network":"default","sharedDirs":[{"path":"/srv/pools/t/a","tag":"bad tag"}]}`,
	} {
		if w := s.do("POST", "/api/v1/vms/debian/config", bad); w.Code != 400 {
			t.Fatalf("%q: %d", bad, w.Code)
		}
	}
}
