package storage

import (
	"testing"
	"time"
)

func TestRecordSmartAndHistory(t *testing.T) {
	e := newEnv(t)
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	e.m.now = func() time.Time { return base }
	e.agent.smartBy = map[string]SmartInfo{
		"/dev/sda": {Passed: true, TemperatureC: 38, PowerOnHours: 100, ReallocatedSectors: 2},
		"/dev/sdb": {Standby: true}, // schläft: kein Messpunkt
	}
	if err := e.m.RecordSmart(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.m.now = func() time.Time { return base.Add(time.Hour) }
	e.m.invalidateSmart("/dev/sda")
	e.agent.smartBy["/dev/sda"] = SmartInfo{Passed: true, TemperatureC: 41, PowerOnHours: 101, ReallocatedSectors: 10, PendingSectors: 1}
	if err := e.m.RecordSmart(e.ctx); err != nil {
		t.Fatal(err)
	}
	h, err := e.m.History(e.ctx, "d1", 30)
	if err != nil || len(h) != 2 {
		t.Fatalf("%+v %v", h, err)
	}
	if h[0].TemperatureC != 38 || h[1].TemperatureC != 41 || h[1].Reallocated != 10 || h[1].Pending != 1 || h[0].TS >= h[1].TS {
		t.Fatalf("%+v", h)
	}
	if h2, _ := e.m.History(e.ctx, "d2", 30); len(h2) != 0 {
		t.Fatalf("schlafende Platte: %+v", h2)
	}
	g, err := e.m.SectorGrowth(e.ctx, 7)
	if err != nil || g["d1"] != 9 {
		t.Fatalf("%v %v", g, err)
	}
	if _, ok := g["d2"]; ok {
		t.Fatal("ohne Messpunkte kein Wachstum")
	}
	if _, err := e.m.History(e.ctx, "d1", 9999); codeOf(err) != "invalid_params" {
		t.Fatalf("Zeitraum prüfen: %v", err)
	}
}

func TestHistoryDropsOldAndDownsamples(t *testing.T) {
	e := newEnv(t)
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	e.m.now = func() time.Time { return now }
	ins := func(ts time.Time, temp int) {
		if _, err := e.m.db.Exec(`INSERT INTO smart_history VALUES ('d1', ?, ?, 0, 0, 0)`, ts.Unix(), temp); err != nil {
			t.Fatal(err)
		}
	}
	ins(now.AddDate(0, 0, -400), 99) // älter als ein Jahr
	for i := 0; i < 1000; i++ {
		ins(now.Add(-time.Duration(i)*time.Hour), 30+i%7)
	}
	e.agent.smartBy = map[string]SmartInfo{}
	if err := e.m.RecordSmart(e.ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := e.m.db.QueryRow(`SELECT COUNT(*) FROM smart_history WHERE temp_c = 99`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("alter Messpunkt blieb: %d %v", n, err)
	}
	h, err := e.m.History(e.ctx, "d1", 365)
	if err != nil || len(h) == 0 || len(h) > historyMaxPoints {
		t.Fatalf("%d Punkte %v", len(h), err)
	}
	for i := 1; i < len(h); i++ {
		if h[i].TS <= h[i-1].TS {
			t.Fatal("nicht aufsteigend")
		}
	}
	for _, p := range h {
		if p.TemperatureC < 30 || p.TemperatureC > 36 {
			t.Fatalf("%+v", p)
		}
	}
}

func TestDiskUsage(t *testing.T) {
	e := newEnv(t)
	e.twoDataDisks(t)
	paths := []string{}
	e.m.statfs = func(p string) (uint64, uint64, error) { paths = append(paths, p); return 4e12, 1e12, nil }
	e.m.mounted = func(p string) bool { return p != DiskPath("d2") }
	ds, err := e.m.ListDisks(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*Usage{}
	for _, d := range ds {
		got[d.ID] = d.Usage
	}
	if u := got["d1"]; u == nil || u.SizeBytes != 4e12 || u.FreeBytes != 1e12 || u.UsedBytes != 3e12 {
		t.Fatalf("d1: %+v", u)
	}
	if got["d2"] != nil {
		t.Fatalf("nicht eingehängt: %+v", got["d2"])
	}
	if got["d3"] != nil {
		t.Fatalf("nicht zugewiesen: %+v", got["d3"])
	}
	if got["sys"] == nil {
		t.Fatal("System: Wurzeldateisystem erwartet")
	}
}
