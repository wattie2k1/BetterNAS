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
	snapErr  map[string]error
}

func (f *fakeSource) ListDisks(context.Context) ([]storage.DiskView, error) {
	return f.disks, f.disksErr
}
func (f *fakeSource) ListPools(context.Context) ([]storage.PoolView, error) {
	return f.pools, f.poolsErr
}
func (f *fakeSource) SnapraidStatus(_ context.Context, pool string) (snapraid.Status, error) {
	return f.snap[pool], f.snapErr[pool]
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

func recoveryPool(msg string) *fakeSource {
	return &fakeSource{
		disks: []storage.DiskView{healthyDisk("d1")},
		pools: []storage.PoolView{{PoolInput: storage.PoolInput{Name: "media"}, Mounted: true}},
		snap: map[string]snapraid.Status{"media": {
			Enabled:  true,
			Recovery: &snapraid.Recovery{Disk: "old1", State: "open"},
			LastFix:  &snapraid.RunState{Result: snapraid.ResultErrors, Message: msg},
		}},
	}
}

func TestFindingRecoveryOpen(t *testing.T) {
	m, _, _ := newMon(t, recoveryPool("2 Dateien sind nicht wiederherstellbar."))
	fs, err := m.Findings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Key != "snapraid-recovery:media" || fs[0].Level != "crit" {
		t.Fatalf("%+v", fs)
	}
	if !strings.Contains(fs[0].Title, "media") || !strings.Contains(fs[0].Body, "old1") || !strings.Contains(fs[0].Body, "2 Dateien sind nicht wiederherstellbar") {
		t.Fatalf("%+v", fs[0])
	}
	// ohne LastFix-Meldung steht trotzdem ein Text da
	src := recoveryPool("")
	src.snap["media"] = snapraid.Status{Enabled: true, Recovery: &snapraid.Recovery{Disk: "old1", State: "open"}}
	m2, _, _ := newMon(t, src)
	if fs, _ := m2.Findings(context.Background()); len(fs) != 1 || !strings.Contains(fs[0].Body, "old1") {
		t.Fatalf("%+v", fs)
	}
}

func TestRecoveryClearedRecovers(t *testing.T) {
	src := recoveryPool("x")
	m, n, _ := newMon(t, src)
	ctx := context.Background()
	_ = m.Check(ctx)
	_ = m.Check(ctx)
	if n.count() != 1 {
		t.Fatalf("%d", n.count())
	}
	st := src.snap["media"]
	st.Recovery = nil
	src.snap["media"] = st
	_ = m.Check(ctx)
	_ = m.Check(ctx)
	if n.count() != 2 || !strings.HasPrefix(n.sent[1].Title, "Behoben:") {
		t.Fatalf("%d %+v", n.count(), n.sent)
	}
}

func TestRecoveryStatusErrorKeepsAlert(t *testing.T) {
	src := recoveryPool("x")
	m, n, _ := newMon(t, src)
	ctx := context.Background()
	_ = m.Check(ctx)
	src.snapErr = map[string]error{"media": errors.New("agent weg")}
	if err := m.Check(ctx); err == nil {
		t.Fatal("Fehler erwartet")
	}
	if a, _ := m.Active(ctx); len(a) != 1 || a[0].Key != "snapraid-recovery:media" {
		t.Fatalf("%+v", a)
	}
	if n.count() != 1 {
		t.Fatalf("keine 'Behoben'-Meldung bei Quellfehler erlaubt: %d", n.count())
	}
}

type fakeTrends struct {
	growth map[string]int
	err    error
}

func (f fakeTrends) SectorGrowth(context.Context, int) (map[string]int, error) {
	return f.growth, f.err
}

func TestSelfTestFailureAndGrowthFindings(t *testing.T) {
	failed := healthyDisk("d1")
	failed.Smart.SelfTest = &storage.SelfTest{Last: &storage.SelfTestResult{Type: "long", Result: "failed", Status: "Read failure"}}
	aborted := healthyDisk("d2")
	aborted.Smart.SelfTest = &storage.SelfTest{Last: &storage.SelfTestResult{Type: "short", Result: "aborted"}}
	growing := healthyDisk("d3")
	slow := healthyDisk("d4")
	m, _, _ := newMon(t, &fakeSource{disks: []storage.DiskView{failed, aborted, growing, slow}})
	m.SetTrends(fakeTrends{growth: map[string]int{"d3": 8, "d4": 7}})
	fs, err := m.Findings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := keys(fs)
	if len(got) != 2 || got["selftest:d1"] != "crit" || got["smart-growth:d3"] != "crit" {
		t.Fatalf("%v", got)
	}
}

func TestTrendErrorKeepsGrowthAlerts(t *testing.T) {
	d := healthyDisk("d1")
	m, _, _ := newMon(t, &fakeSource{disks: []storage.DiskView{d}})
	ctx := context.Background()
	m.SetTrends(fakeTrends{growth: map[string]int{"d1": 20}})
	if err := m.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if a, _ := m.Active(ctx); len(a) != 1 || a[0].Key != "smart-growth:d1" {
		t.Fatalf("%v", a)
	}
	m.SetTrends(fakeTrends{err: errors.New("db weg")})
	if err := m.Check(ctx); err == nil {
		t.Fatal("Fehler erwartet")
	}
	if a, _ := m.Active(ctx); len(a) != 1 {
		t.Fatalf("Alarm bei Quellfehler nicht stehen lassen: %v", a)
	}
}

func TestSSDWearFinding(t *testing.T) {
	pct := func(n int) *storage.Wear { return &storage.Wear{PercentUsed: &n} }
	worn, fine, unknown := healthyDisk("w"), healthyDisk("f"), healthyDisk("u")
	worn.Smart.Wear, fine.Smart.Wear, unknown.Smart.Wear = pct(92), pct(89), &storage.Wear{BytesWritten: 5}
	m, _, _ := newMon(t, &fakeSource{disks: []storage.DiskView{worn, fine, unknown}})
	fs, err := m.Findings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := keys(fs); len(got) != 1 || got["ssd-wear:w"] != "warn" {
		t.Fatalf("%v", got)
	}
}

func TestNewDiskFindingResolvesQuietly(t *testing.T) {
	d := healthyDisk("n1")
	d.Role = "unassigned"
	d.New = true
	d.Device, d.SizeBytes = "/dev/sdz", 8e12
	src := &fakeSource{disks: []storage.DiskView{d}}
	m, n, _ := newMon(t, src)
	ctx := context.Background()
	if err := m.Check(ctx); err != nil {
		t.Fatal(err)
	}
	a, _ := m.Active(ctx)
	if len(a) != 1 || a[0].Key != "disk-new:n1" || a[0].Level != "info" || !strings.Contains(a[0].Body, "8,0 TB") {
		t.Fatalf("%+v", a)
	}
	sent := n.count()
	if sent != 1 {
		t.Fatalf("eine Meldung erwartet, %d gesendet", sent)
	}
	src.disks[0].New = false // eingerichtet oder ignoriert
	if err := m.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if a, _ := m.Active(ctx); len(a) != 0 {
		t.Fatalf("Befund muss verschwinden: %+v", a)
	}
	if n.count() != sent {
		t.Fatal("kein „Behoben“ für eine neue Platte")
	}
}

func TestPerDiskTempLimit(t *testing.T) {
	// Globale Schwelle löst bei 56 °C aus; eine höhere Grenze an der Platte unterdrückt die Warnung,
	// eine niedrigere löst sie früher aus.
	warm := healthyDisk("w")
	warm.Smart.TemperatureC = 56
	warm.Note = &storage.DiskNote{TempWarnC: 60}
	cool := healthyDisk("c")
	cool.Smart.TemperatureC = 41
	cool.Note = &storage.DiskNote{TempWarnC: 40}
	src := &fakeSource{disks: []storage.DiskView{warm, cool}, snap: map[string]snapraid.Status{}}
	m, _, _ := newMon(t, src)
	fs, err := m.Findings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := keys(fs)
	if _, ok := got["temp:w"]; ok {
		t.Errorf("Grenze 60 °C darf bei 56 °C nicht warnen: %v", got)
	}
	if got["temp:c"] != "warn" {
		t.Errorf("Grenze 40 °C muss bei 41 °C warnen: %v", got)
	}
}

func TestFsCheckErrorsRaiseAlert(t *testing.T) {
	bad := healthyDisk("b")
	bad.FsCheck = &storage.FsCheck{Result: "errors"}
	running := healthyDisk("r")
	running.FsCheck = &storage.FsCheck{Running: true, Result: "errors"}
	good := healthyDisk("g")
	good.FsCheck = &storage.FsCheck{Result: "ok"}
	failed := healthyDisk("f")
	failed.FsCheck = &storage.FsCheck{Result: "failed"}
	src := &fakeSource{disks: []storage.DiskView{bad, running, good, failed}, snap: map[string]snapraid.Status{}}
	m, _, _ := newMon(t, src)
	fs, err := m.Findings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := keys(fs)
	if got["fscheck:b"] != "crit" || len(got) != 1 {
		t.Fatalf("%v", got)
	}
}
