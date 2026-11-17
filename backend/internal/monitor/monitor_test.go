package monitor

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/notify"
	"github.com/betternas/betternas/internal/snapraid"
	"github.com/betternas/betternas/internal/storage"
)

type fakeSource struct {
	disks    []storage.DiskView
	pools    []storage.PoolView
	snap     map[string]snapraid.Status
	disksErr error
	poolsErr error
}

func (f *fakeSource) ListDisks(context.Context) ([]storage.DiskView, error) {
	return f.disks, f.disksErr
}
func (f *fakeSource) ListPools(context.Context) ([]storage.PoolView, error) {
	return f.pools, f.poolsErr
}
func (f *fakeSource) SnapraidStatus(_ context.Context, pool string) (snapraid.Status, error) {
	return f.snap[pool], nil
}

type fakeNotifier struct {
	mu   sync.Mutex
	sent []notify.Message
}

func (f *fakeNotifier) Send(_ context.Context, m notify.Message) []notify.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	return nil
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func healthyDisk(id string) storage.DiskView {
	return storage.DiskView{
		Disk:  storage.Disk{ID: id, Model: "HDD-" + id, Serial: "SER-" + id},
		State: "online", Role: "data",
		Smart: &storage.SmartInfo{Passed: true, TemperatureC: 38, PowerOnHours: 1000},
	}
}

func newMon(t *testing.T, src Source) (*Monitor, *fakeNotifier, *events.Hub) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	n := &fakeNotifier{}
	return New(d, src, n, hub), n, hub
}

func keys(fs []Finding) map[string]notify.Level {
	out := map[string]notify.Level{}
	for _, f := range fs {
		out[f.Key] = f.Level
	}
	return out
}

func TestFindingsFromDisksPoolsSnapraid(t *testing.T) {
	bad := healthyDisk("d1")
	bad.Smart.Passed = false
	sect := healthyDisk("d2")
	sect.Smart.PendingSectors = 3
	hot := healthyDisk("d3")
	hot.Smart.TemperatureC = 56
	missing := storage.DiskView{Disk: storage.Disk{ID: "d4", Model: "X"}, State: "missing"}
	src := &fakeSource{
		disks: []storage.DiskView{bad, sect, hot, missing, healthyDisk("ok")},
		pools: []storage.PoolView{
			{PoolInput: storage.PoolInput{Name: "tank"}, Mounted: false},
			{PoolInput: storage.PoolInput{Name: "fine"}, Mounted: true},
		},
		snap: map[string]snapraid.Status{
			"tank": {Enabled: true, LastSync: &snapraid.RunState{Result: snapraid.ResultFailed, Message: "kaputt"}, LastScrub: &snapraid.RunState{Result: snapraid.ResultErrors}},
			"fine": {Enabled: true, LastSync: &snapraid.RunState{Result: snapraid.ResultOK}},
		},
	}
	m, _, _ := newMon(t, src)
	fs, err := m.Findings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := keys(fs)
	want := map[string]notify.Level{
		"smart:d1": "crit", "smart-sectors:d2": "warn", "temp:d3": "warn", "disk-missing:d4": "crit",
		"pool-unmounted:tank": "crit", "snapraid-sync:tank": "crit", "snapraid-scrub:tank": "crit",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, l := range want {
		if got[k] != l {
			t.Errorf("%s: %q statt %q (alle: %v)", k, got[k], l, got)
		}
	}
	// gesund → leer
	src2 := &fakeSource{disks: []storage.DiskView{healthyDisk("a")}, pools: []storage.PoolView{{PoolInput: storage.PoolInput{Name: "p"}, Mounted: true}}, snap: map[string]snapraid.Status{}}
	m2, _, _ := newMon(t, src2)
	if fs, _ := m2.Findings(context.Background()); len(fs) != 0 {
		t.Fatalf("%v", fs)
	}
}

func TestTemperatureZeroNeverAlerts(t *testing.T) {
	d := healthyDisk("usb")
	d.Smart.TemperatureC = 0
	m, _, _ := newMon(t, &fakeSource{disks: []storage.DiskView{d}})
	if fs, _ := m.Findings(context.Background()); len(fs) != 0 {
		t.Fatalf("%v", fs)
	}
}

func TestStandbyWithoutValuesNoSmartAlert(t *testing.T) {
	d := healthyDisk("s")
	d.Smart = &storage.SmartInfo{Standby: true}
	m, _, _ := newMon(t, &fakeSource{disks: []storage.DiskView{d}})
	if fs, _ := m.Findings(context.Background()); len(fs) != 0 {
		t.Fatalf("%v", fs)
	}
}

func TestCheckSendsOnceAndRecoversOnce(t *testing.T) {
	bad := healthyDisk("d1")
	bad.Smart.Passed = false
	src := &fakeSource{disks: []storage.DiskView{bad}}
	m, n, _ := newMon(t, src)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := m.Check(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if n.count() != 1 {
		t.Fatalf("%d Meldungen statt 1", n.count())
	}
	if a, _ := m.Active(ctx); len(a) != 1 || a[0].Key != "smart:d1" {
		t.Fatalf("%+v", a)
	}
	src.disks = []storage.DiskView{healthyDisk("d1")}
	_ = m.Check(ctx)
	_ = m.Check(ctx)
	if n.count() != 2 {
		t.Fatalf("%d Meldungen statt 2", n.count())
	}
	last := n.sent[1]
	if !strings.HasPrefix(last.Title, "Behoben:") || last.Level != "info" {
		t.Fatalf("%+v", last)
	}
	if a, _ := m.Active(ctx); len(a) != 0 {
		t.Fatalf("%+v", a)
	}
}

func TestCheckPersistsAcrossRestart(t *testing.T) {
	bad := healthyDisk("d1")
	bad.Smart.Passed = false
	src := &fakeSource{disks: []storage.DiskView{bad}}
	m, n, _ := newMon(t, src)
	_ = m.Check(context.Background())
	m2 := New(m.db, src, n, nil)
	_ = m2.Check(context.Background())
	if n.count() != 1 {
		t.Fatalf("%d", n.count())
	}
}

func TestCheckSourceErrorKeepsAlerts(t *testing.T) {
	bad := healthyDisk("d1")
	bad.Smart.Passed = false
	src := &fakeSource{disks: []storage.DiskView{bad}}
	m, n, _ := newMon(t, src)
	ctx := context.Background()
	_ = m.Check(ctx)
	src.disksErr = errors.New("agent weg")
	if err := m.Check(ctx); err == nil {
		t.Fatal("Fehler erwartet")
	}
	if a, _ := m.Active(ctx); len(a) != 1 {
		t.Fatalf("Alert verloren: %+v", a)
	}
	if n.count() != 1 {
		t.Fatalf("keine 'Behoben'-Meldung bei Quellfehler erlaubt, %d gesendet", n.count())
	}
}

func TestCheckPublishesEvent(t *testing.T) {
	bad := healthyDisk("d1")
	bad.Smart.Passed = false
	m, _, hub := newMon(t, &fakeSource{disks: []storage.DiskView{bad}})
	ch, cancel := hub.Subscribe()
	defer cancel()
	_ = m.Check(context.Background())
	select {
	case ev := <-ch:
		if ev.Type != "alerts.changed" {
			t.Fatal(ev)
		}
	default:
		t.Fatal("kein Ereignis")
	}
}
