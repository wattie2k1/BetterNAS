package storage

import (
	"testing"
	"time"
)

func spinning(e *env) {
	for i := range e.agent.disks {
		e.agent.disks[i].Rotational = e.agent.disks[i].ID == "d1" || e.agent.disks[i].ID == "d2"
	}
}

func TestSamplePowerCountsMinutesAndSpinups(t *testing.T) {
	e := newEnv(t)
	spinning(e)
	at := time.Date(2026, 11, 19, 10, 5, 0, 0, time.Local)
	e.m.now = func() time.Time { return at }
	step := func(d1, d2 string) {
		e.agent.powerStates = map[string]string{"/dev/sda": d1, "/dev/sdb": d2}
		if err := e.m.SamplePower(e.ctx); err != nil {
			t.Fatal(err)
		}
		at = at.Add(time.Minute)
	}
	step("active", "standby")
	step("standby", "standby")
	step("standby", "unknown") // unbekannt zählt nicht
	step("active", "standby")  // d1: Anlauf
	st, err := e.m.PowerStats(e.ctx, "d1", 30)
	if err != nil || len(st) != 1 {
		t.Fatalf("%+v %v", st, err)
	}
	if st[0].Day != "2026-11-19" || st[0].ActiveMin != 2 || st[0].StandbyMin != 2 || st[0].Spinups != 1 {
		t.Fatalf("d1: %+v", st[0])
	}
	st2, _ := e.m.PowerStats(e.ctx, "d2", 30)
	if len(st2) != 1 || st2[0].StandbyMin != 3 || st2[0].ActiveMin != 0 || st2[0].Spinups != 0 {
		t.Fatalf("d2: %+v", st2)
	}
	if st3, _ := e.m.PowerStats(e.ctx, "sys", 30); len(st3) != 0 {
		t.Fatalf("SSD wird nicht gemessen: %+v", st3)
	}
}

func TestPowerStatsRangeAndPrune(t *testing.T) {
	e := newEnv(t)
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.Local) // volle Stunde: räumt auf
	e.m.now = func() time.Time { return now }
	ins := func(day string) {
		if _, err := e.m.db.Exec(`INSERT INTO power_stats VALUES ('d1', ?, 10, 20, 1)`, day); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{"2026-11-19", "2026-11-12", "2026-10-01", "2025-01-01"} {
		ins(d)
	}
	spinning(e)
	e.agent.powerStates = map[string]string{"/dev/sda": "active"}
	if err := e.m.SamplePower(e.ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = e.m.db.QueryRow(`SELECT COUNT(*) FROM power_stats WHERE day = '2025-01-01'`).Scan(&n)
	if n != 0 {
		t.Fatal("zu alte Tage müssen entfallen")
	}
	if st, _ := e.m.PowerStats(e.ctx, "d1", 7); len(st) != 1 || st[0].Day != "2026-11-19" {
		t.Fatalf("7 Tage (heute zählt mit): %+v", st)
	}
	if st, _ := e.m.PowerStats(e.ctx, "d1", 8); len(st) != 2 || st[0].Day != "2026-11-12" {
		t.Fatalf("8 Tage: %+v", st)
	}
	if st, _ := e.m.PowerStats(e.ctx, "d1", 90); len(st) != 3 {
		t.Fatalf("90 Tage: %+v", st)
	}
	if _, err := e.m.PowerStats(e.ctx, "d1", 500); codeOf(err) != "invalid_params" {
		t.Fatal("Zeitraum prüfen")
	}
}

func TestSetTuningAppliesSavesAndReconciles(t *testing.T) {
	e := newEnv(t)
	e.twoDataDisks(t)
	if err := e.m.SetTuning(e.ctx, "admin", "d1", TuningSettings{APM: 128, WriteCache: "off"}); err != nil {
		t.Fatal(err)
	}
	if len(e.agent.tuneReqs) != 1 || e.agent.tuneReqs[0]["device"] != "/dev/sda" || e.agent.tuneReqs[0]["apm"] != float64(128) || e.agent.tuneReqs[0]["writeCache"] != "off" {
		t.Fatalf("%v", e.agent.tuneReqs)
	}
	st, err := e.m.Tuning(e.ctx, "d1")
	if err != nil || st.Saved.APM != 128 || st.Saved.WriteCache != "off" {
		t.Fatalf("%+v %v", st, err)
	}
	if st2, _ := e.m.Tuning(e.ctx, "d2"); st2.Saved != (TuningSettings{}) {
		t.Fatalf("d2 unberührt: %+v", st2)
	}
	e.agent.tuneReqs = nil
	if err := e.m.ReconcileTuning(e.ctx); err != nil || len(e.agent.tuneReqs) != 1 || e.agent.tuneReqs[0]["apm"] != float64(128) {
		t.Fatalf("%v %v", e.agent.tuneReqs, err)
	}
	e.agent.tuneReqs = nil
	if err := e.m.SetTuning(e.ctx, "admin", "d1", TuningSettings{}); err != nil || len(e.agent.tuneReqs) != 0 {
		t.Fatalf("Zurücksetzen fasst die Platte nicht an: %v %v", e.agent.tuneReqs, err)
	}
	if st, _ := e.m.Tuning(e.ctx, "d1"); st.Saved != (TuningSettings{}) {
		t.Fatalf("%+v", st)
	}
	if err := e.m.ReconcileTuning(e.ctx); err != nil || len(e.agent.tuneReqs) != 0 {
		t.Fatal("nichts mehr anzuwenden")
	}
}

func TestSetTuningRejections(t *testing.T) {
	e := newEnv(t)
	e.twoDataDisks(t)
	for _, in := range []TuningSettings{{APM: 300}, {APM: -1}, {WriteCache: "vielleicht"}} {
		if codeOf(e.m.SetTuning(e.ctx, "admin", "d1", in)) != "invalid_params" {
			t.Errorf("%+v muss abgelehnt werden", in)
		}
	}
	if codeOf(e.m.SetTuning(e.ctx, "admin", "d3", TuningSettings{APM: 128})) != "conflict" {
		t.Error("nicht zugewiesene Platte")
	}
	if codeOf(e.m.SetTuning(e.ctx, "admin", "sys", TuningSettings{APM: 128})) != "conflict" {
		t.Error("Systemplatte")
	}
	e.agent.tuneFail = true
	if codeOf(e.m.SetTuning(e.ctx, "admin", "d1", TuningSettings{APM: 128})) != "conflict" {
		t.Error("Platte lehnt ab")
	}
	if st, _ := e.m.Tuning(e.ctx, "d1"); st.Saved.APM != 0 {
		t.Fatalf("bei Ablehnung nicht speichern: %+v", st)
	}
}
