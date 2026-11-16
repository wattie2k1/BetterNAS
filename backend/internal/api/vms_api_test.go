package api

import (
	"context"
	"encoding/json"
	"net/http"
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
	h := NewServer(Config{WebDir: t.TempDir(), Dev: true}, Deps{
		DB: d, Jobs: jm, Hub: hub,
		Auth: auth.NewHandlers(d, store, nil, nil, false),
		VMs:  vm.NewManager(d, stub, hub),
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
