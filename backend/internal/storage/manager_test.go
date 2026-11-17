package storage

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/snapraid"
)

// fakeAgent simuliert den Agent auf Ebene der RPC-Methoden.
type fakeAgent struct {
	mu       sync.Mutex
	disks    []Disk
	calls    []string
	applied  []State
	applyErr error

	snapApplied []snapraid.State
	snapErr     error
	statuses    []snapraid.Status // Antworten von snapraid.status in Reihenfolge; die letzte wiederholt sich
	started     []map[string]any

	spindownReqs []map[string]any // Anfragen an disk.spindown
	spindownFail map[string]bool  // Gerät → disk.spindown schlägt fehl

	smartBy map[string]SmartInfo // Antworten von disk.smart je Gerät; fehlt ein Gerät, schlägt der Aufruf fehl
}

func (f *fakeAgent) Call(_ context.Context, method string, params, result any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)
	var out any
	switch method {
	case "disk.list":
		out = f.disks
	case "disk.smart":
		b, _ := json.Marshal(params)
		var req map[string]string
		json.Unmarshal(b, &req)
		si, ok := f.smartBy[req["device"]]
		if !ok {
			return apierr.New(apierr.Internal, "kein SMART")
		}
		out = si
	case "disk.spindown":
		b, _ := json.Marshal(params)
		var req map[string]any
		json.Unmarshal(b, &req)
		f.spindownReqs = append(f.spindownReqs, req)
		if f.spindownFail[req["device"].(string)] {
			return apierr.New(apierr.Conflict, "Diese Platte unterstützt keinen Ruhezustand")
		}
		out = map[string]bool{"ok": true}
	case "disk.format":
		out = map[string]string{"partition": "/dev/sdb1", "fsUuid": "new-uuid", "fsType": "ext4", "label": "bnas-x"}
	case "snapraid.apply":
		b, _ := json.Marshal(params)
		var st snapraid.State
		json.Unmarshal(b, &st)
		f.snapApplied = append(f.snapApplied, st)
		if f.snapErr != nil {
			return f.snapErr
		}
		out = map[string]any{"changed": []string{}}
	case "snapraid.start":
		b, _ := json.Marshal(params)
		var req map[string]any
		json.Unmarshal(b, &req)
		f.started = append(f.started, req)
		out = map[string]string{"unit": "betternas-snapraid-x"}
	case "snapraid.status":
		var st snapraid.Status
		if len(f.statuses) > 0 {
			st = f.statuses[0]
			if len(f.statuses) > 1 {
				f.statuses = f.statuses[1:]
			}
		}
		out = st
	case "storage.apply":
		b, _ := json.Marshal(params)
		var st State
		json.Unmarshal(b, &st)
		f.applied = append(f.applied, st)
		if f.applyErr != nil {
			return f.applyErr
		}
		out = map[string]any{"changed": []string{}}
	default:
		return errors.New("unbekannt: " + method)
	}
	if result != nil {
		b, _ := json.Marshal(out)
		return json.Unmarshal(b, result)
	}
	return nil
}

func (f *fakeAgent) called(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == method {
			n++
		}
	}
	return n
}

func (f *fakeAgent) lastApplied() State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.applied[len(f.applied)-1]
}

func liveDisks() []Disk {
	return []Disk{
		{ID: "sys", Device: "/dev/nvme0n1", System: true, Reason: "Systemplatte", SizeBytes: 500e9, Partitions: []Partition{}},
		{ID: "d1", Device: "/dev/sda", Serial: "SER1", Model: "HDD1", SizeBytes: 4e12, Manageable: true, Partitions: []Partition{}},
		{ID: "d2", Device: "/dev/sdb", Serial: "SER2", Model: "HDD2", SizeBytes: 4e12, Manageable: true, Partitions: []Partition{}},
		{ID: "d3", Device: "/dev/sdc", Serial: "SER3", Model: "HDD3", SizeBytes: 2e12, Manageable: true,
			Partitions: []Partition{{Device: "/dev/sdc1", FSType: "ext4", UUID: "uuid-3", Label: "alt", Mountpoints: []string{}}}},
	}
}

type env struct {
	m     *Manager
	agent *fakeAgent
	jobs  *jobs.Manager
	ctx   context.Context
	deps  []string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	e := &env{agent: &fakeAgent{disks: liveDisks()}, jobs: jobs.NewManager(d, hub), ctx: context.Background()}
	e.m = NewManager(d, e.agent, e.jobs, hub, func(context.Context, string) ([]string, error) { return e.deps, nil })
	e.m.statfs = func(string) (uint64, uint64, error) { return 8e12, 6e12, nil }
	e.m.mounted = func(string) bool { return true }
	e.m.pollEvery = 5 * time.Millisecond
	return e
}

func (e *env) wait(t *testing.T, id string) jobs.Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		j, err := e.jobs.Get(e.ctx, id)
		if err == nil && (j.Status == jobs.StatusSucceeded || j.Status == jobs.StatusFailed) {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return jobs.Job{}
}

func (e *env) mustSucceed(t *testing.T, id string, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if j := e.wait(t, id); j.Status != jobs.StatusSucceeded {
		t.Fatalf("job failed: %s", j.Log)
	}
}

// formatDisk formatiert eine Platte über Vorschau + Bestätigung.
func (e *env) formatDisk(t *testing.T, id, serial string) {
	t.Helper()
	p, err := e.m.PreviewFormat(e.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	jid, err := e.m.Format(e.ctx, "anna", id, FormatRequest{FSType: "ext4", ConfirmSerial: serial, ConfirmToken: p.ConfirmToken})
	e.mustSucceed(t, jid, err)
}

func codeOf(err error) string { return apierr.From(err).Code }

func TestListDisksMergesRolesAndMissing(t *testing.T) {
	e := newEnv(t)
	e.formatDisk(t, "d1", "SER1")
	e.agent.disks = liveDisks()[:1] // d1 wurde abgezogen
	views, err := e.m.ListDisks(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]DiskView{}
	for _, v := range views {
		byID[v.ID] = v
	}
	if byID["sys"].Role != "system" || byID["d1"].State != "missing" || byID["d1"].Role != "data" || byID["d1"].Serial != "SER1" {
		t.Fatalf("%+v", byID)
	}
}

func TestFormatRequiresValidTokenAndSerial(t *testing.T) {
	e := newEnv(t)
	p, err := e.m.PreviewFormat(e.ctx, "d2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Format(e.ctx, "anna", "d2", FormatRequest{FSType: "ext4", ConfirmSerial: "falsch", ConfirmToken: p.ConfirmToken}); codeOf(err) != apierr.InvalidParams {
		t.Fatalf("serial: %v", err)
	}
	if _, err := e.m.Format(e.ctx, "anna", "d2", FormatRequest{FSType: "ext4", ConfirmSerial: "SER2", ConfirmToken: "unbekannt"}); codeOf(err) != apierr.Conflict {
		t.Fatalf("token: %v", err)
	}
	jid, err := e.m.Format(e.ctx, "anna", "d2", FormatRequest{FSType: "ext4", ConfirmSerial: "SER2", ConfirmToken: p.ConfirmToken})
	e.mustSucceed(t, jid, err)
	if _, err := e.m.Format(e.ctx, "anna", "d2", FormatRequest{FSType: "ext4", ConfirmSerial: "SER2", ConfirmToken: p.ConfirmToken}); codeOf(err) != apierr.Conflict {
		t.Fatalf("token reuse: %v", err)
	}
	st := e.agent.lastApplied()
	if len(st.Disks) != 1 || st.Disks[0].DiskID != "d2" || st.Disks[0].FSUUID != "new-uuid" {
		t.Fatalf("%+v", st)
	}
	views, _ := e.m.ListDisks(e.ctx)
	for _, v := range views {
		if v.ID == "d2" && v.Role != "data" {
			t.Fatalf("%+v", v)
		}
	}
}

func TestFormatTokenExpires(t *testing.T) {
	e := newEnv(t)
	p, _ := e.m.PreviewFormat(e.ctx, "d2")
	e.m.now = func() time.Time { return time.Now().Add(6 * time.Minute) }
	if _, err := e.m.Format(e.ctx, "anna", "d2", FormatRequest{FSType: "ext4", ConfirmSerial: "SER2", ConfirmToken: p.ConfirmToken}); codeOf(err) != apierr.Conflict {
		t.Fatal(err)
	}
}

func TestFormatTokenBoundToDevice(t *testing.T) {
	e := newEnv(t)
	p, _ := e.m.PreviewFormat(e.ctx, "d2")
	e.agent.disks[2].Device = "/dev/sdx" // andere Platte steckt jetzt an diesem Platz
	if _, err := e.m.Format(e.ctx, "anna", "d2", FormatRequest{FSType: "ext4", ConfirmSerial: "SER2", ConfirmToken: p.ConfirmToken}); codeOf(err) != apierr.Conflict {
		t.Fatal(err)
	}
	if e.agent.called("disk.format") != 0 {
		t.Fatal("formatted despite changed device")
	}
}

func TestPreviewRejectsSystemAndManaged(t *testing.T) {
	e := newEnv(t)
	if _, err := e.m.PreviewFormat(e.ctx, "sys"); codeOf(err) != apierr.SystemDisk {
		t.Fatal(err)
	}
	e.formatDisk(t, "d1", "SER1")
	if _, err := e.m.PreviewFormat(e.ctx, "d1"); codeOf(err) != apierr.Conflict {
		t.Fatal(err)
	}
	if _, err := e.m.PreviewFormat(e.ctx, "nope"); codeOf(err) != apierr.NotFound {
		t.Fatal(err)
	}
}

func TestImportExistingFilesystem(t *testing.T) {
	e := newEnv(t)
	jid, err := e.m.Import(e.ctx, "anna", "d3", "")
	e.mustSucceed(t, jid, err)
	st := e.agent.lastApplied()
	if len(st.Disks) != 1 || st.Disks[0].FSUUID != "uuid-3" || st.Disks[0].FSType != "ext4" {
		t.Fatalf("%+v", st)
	}
	if _, err := e.m.Import(e.ctx, "anna", "d2", ""); codeOf(err) != apierr.InvalidParams {
		t.Fatal(err)
	}
}

func (e *env) twoDataDisks(t *testing.T) {
	e.formatDisk(t, "d1", "SER1")
	e.formatDisk(t, "d2", "SER2")
}

func TestCreatePoolValidation(t *testing.T) {
	e := newEnv(t)
	e.twoDataDisks(t)
	jid, err := e.m.CreatePool(e.ctx, "anna", PoolInput{Name: "media", Branches: []string{"d1"}})
	e.mustSucceed(t, jid, err)
	applies := e.agent.called("storage.apply")
	cases := map[string]struct {
		in   PoolInput
		code string
	}{
		"traversal":       {PoolInput{Name: "../x", Branches: []string{"d2"}}, apierr.InvalidParams},
		"komma":           {PoolInput{Name: "a,b", Branches: []string{"d2"}}, apierr.InvalidParams},
		"unassigned":      {PoolInput{Name: "neu", Branches: []string{"d3"}}, apierr.InvalidParams},
		"in anderem pool": {PoolInput{Name: "neu", Branches: []string{"d1"}}, apierr.Conflict},
		"leer":            {PoolInput{Name: "neu"}, apierr.InvalidParams},
		"name vergeben":   {PoolInput{Name: "media", Branches: []string{"d2"}}, apierr.Conflict},
		"minfree":         {PoolInput{Name: "neu", Branches: []string{"d2"}, MinFree: "1G,allow_other"}, apierr.InvalidParams},
	}
	for name, c := range cases {
		if _, err := e.m.CreatePool(e.ctx, "anna", c.in); codeOf(err) != c.code {
			t.Errorf("%s: want %s got %v", name, c.code, err)
		}
	}
	if e.agent.called("storage.apply") != applies {
		t.Fatal("agent called for invalid input")
	}
}

func TestCreatePoolDefaults(t *testing.T) {
	e := newEnv(t)
	e.twoDataDisks(t)
	jid, err := e.m.CreatePool(e.ctx, "anna", PoolInput{Name: "media", Branches: []string{"d1", "d2"}})
	e.mustSucceed(t, jid, err)
	p := e.agent.lastApplied().Pools[0]
	if p.CreatePolicy != "epmfs" || p.MinFree != "20G" || strings.Join(p.Branches, ",") != "d1,d2" {
		t.Fatalf("%+v", p)
	}
	pools, _ := e.m.ListPools(e.ctx)
	if len(pools) != 1 || pools[0].Path != "/srv/pools/media" || pools[0].FreeBytes != 6e12 || !pools[0].Mounted {
		t.Fatalf("%+v", pools)
	}
	list, _ := audit.List(e.ctx, e.m.db, 1)
	if list[0].Action != "pool.create" || list[0].Result != "ok" {
		t.Fatalf("%+v", list)
	}
}

func TestUpdatePoolBusyKeepsDB(t *testing.T) {
	e := newEnv(t)
	e.twoDataDisks(t)
	jid, err := e.m.CreatePool(e.ctx, "anna", PoolInput{Name: "media", Branches: []string{"d1"}})
	e.mustSucceed(t, jid, err)
	e.agent.applyErr = apierr.New(apierr.PoolBusy, "Der Pool wird gerade verwendet.")
	jid, err = e.m.UpdatePool(e.ctx, "anna", "media", PoolInput{Branches: []string{"d1", "d2"}})
	if err != nil {
		t.Fatal(err)
	}
	j := e.wait(t, jid)
	if j.Status != jobs.StatusFailed || !strings.Contains(j.Log, "verwendet") {
		t.Fatalf("%+v", j)
	}
	pools, _ := e.m.ListPools(e.ctx)
	if strings.Join(pools[0].Branches, ",") != "d1" {
		t.Fatalf("db changed: %+v", pools[0])
	}
}

func TestUpdatePoolNotFound(t *testing.T) {
	e := newEnv(t)
	if _, err := e.m.UpdatePool(e.ctx, "anna", "nope", PoolInput{Branches: []string{"d1"}}); codeOf(err) != apierr.NotFound {
		t.Fatal(err)
	}
}

func TestDeletePoolWithDependents(t *testing.T) {
	e := newEnv(t)
	e.twoDataDisks(t)
	jid, err := e.m.CreatePool(e.ctx, "anna", PoolInput{Name: "media", Branches: []string{"d1"}})
	e.mustSucceed(t, jid, err)
	e.deps = []string{"share:filme"}
	_, err = e.m.DeletePool(e.ctx, "anna", "media")
	if ae := apierr.From(err); ae.Code != apierr.Conflict || ae.Details["dependents"] == nil {
		t.Fatal(err)
	}
	e.deps = nil
	jid, err = e.m.DeletePool(e.ctx, "anna", "media")
	e.mustSucceed(t, jid, err)
	if pools, _ := e.m.ListPools(e.ctx); len(pools) != 0 {
		t.Fatal(pools)
	}
}

func TestReleaseDiskInPool(t *testing.T) {
	e := newEnv(t)
	e.twoDataDisks(t)
	jid, err := e.m.CreatePool(e.ctx, "anna", PoolInput{Name: "media", Branches: []string{"d1"}})
	e.mustSucceed(t, jid, err)
	_, err = e.m.Release(e.ctx, "anna", "d1")
	if ae := apierr.From(err); ae.Code != apierr.DiskInUse || ae.Details["pool"] != "media" {
		t.Fatal(err)
	}
	jid, err = e.m.Release(e.ctx, "anna", "d2")
	e.mustSucceed(t, jid, err)
	if st := e.agent.lastApplied(); len(st.Disks) != 1 || st.Disks[0].DiskID != "d1" {
		t.Fatalf("%+v", st)
	}
}

// Beim Start gleicht die API die Mount-Units mit der Datenbank ab, damit eine neue
// Version geänderte Units (z. B. nach einem Update) sofort schreibt.
func TestReconcileAppliesStoredState(t *testing.T) {
	e := newEnv(t)
	e.twoDataDisks(t)
	jid, err := e.m.CreatePool(e.ctx, "anna", PoolInput{Name: "media", Branches: []string{"d1", "d2"}})
	e.mustSucceed(t, jid, err)
	before := e.agent.called("storage.apply")
	if err := e.m.Reconcile(e.ctx); err != nil {
		t.Fatal(err)
	}
	if e.agent.called("storage.apply") != before+1 {
		t.Fatal("Reconcile hat storage.apply nicht aufgerufen")
	}
	st := e.agent.lastApplied()
	if len(st.Disks) != 2 || len(st.Pools) != 1 || st.Pools[0].Name != "media" {
		t.Fatalf("falscher Zustand: %+v", st)
	}
}

func TestFormatAsParityStoresRole(t *testing.T) {
	e := newEnv(t)
	p, err := e.m.PreviewFormat(e.ctx, "d2")
	if err != nil {
		t.Fatal(err)
	}
	jid, err := e.m.Format(e.ctx, "anna", "d2", FormatRequest{FSType: "ext4", Role: "parity", ConfirmSerial: "SER2", ConfirmToken: p.ConfirmToken})
	e.mustSucceed(t, jid, err)
	st := e.agent.lastApplied()
	if len(st.Disks) != 1 || st.Disks[0].Role != "parity" {
		t.Fatalf("%+v", st)
	}
	views, _ := e.m.ListDisks(e.ctx)
	for _, v := range views {
		if v.ID == "d2" && v.Role != "parity" {
			t.Fatalf("%+v", v)
		}
	}
	// Eine Paritätsplatte kann nicht in einen Pool.
	if _, err := e.m.CreatePool(e.ctx, "anna", PoolInput{Name: "x", Branches: []string{"d2"}}); codeOf(err) != apierr.InvalidParams {
		t.Fatalf("pool with parity disk: %v", err)
	}
	p2, _ := e.m.PreviewFormat(e.ctx, "d1")
	if _, err := e.m.Format(e.ctx, "anna", "d1", FormatRequest{FSType: "ext4", Role: "boss", ConfirmSerial: "SER1", ConfirmToken: p2.ConfirmToken}); codeOf(err) != apierr.InvalidParams {
		t.Fatalf("bad role: %v", err)
	}
}

func TestImportWithRole(t *testing.T) {
	e := newEnv(t)
	jid, err := e.m.Import(e.ctx, "anna", "d3", "parity")
	e.mustSucceed(t, jid, err)
	if st := e.agent.lastApplied(); len(st.Disks) != 1 || st.Disks[0].Role != "parity" {
		t.Fatalf("%+v", st)
	}
	if _, err := e.m.Import(e.ctx, "anna", "d3", "boss"); codeOf(err) != apierr.InvalidParams {
		t.Fatal(err)
	}
}
