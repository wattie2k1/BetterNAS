package storage

import (
	"slices"
	"testing"
	"time"
)

func smartOK() SmartInfo { return SmartInfo{Passed: true, TemperatureC: 35, PowerOnHours: 10} }

func TestSelfTestScheduleDefaultsAndValidation(t *testing.T) {
	e := newEnv(t)
	s, err := e.m.SelfTestSchedule(e.ctx)
	if err != nil || s.Short != "off" || s.Long != "off" || s.Hour != 2 {
		t.Fatalf("%+v %v", s, err)
	}
	if err := e.m.SetSelfTestSchedule(e.ctx, "admin", SelfTestSchedule{Short: "daily", Long: "off", Hour: 2}); codeOf(err) != "invalid_params" {
		t.Fatalf("unbekannte Häufigkeit muss scheitern: %v", err)
	}
	if err := e.m.SetSelfTestSchedule(e.ctx, "admin", SelfTestSchedule{Short: "weekly", Long: "monthly", Hour: 24}); codeOf(err) != "invalid_params" {
		t.Fatalf("Stunde 24 muss scheitern: %v", err)
	}
	want := SelfTestSchedule{Short: "weekly", Long: "quarterly", Hour: 4}
	if err := e.m.SetSelfTestSchedule(e.ctx, "admin", want); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.m.SelfTestSchedule(e.ctx); got != want {
		t.Fatalf("%+v", got)
	}
}

func TestStartAbortSelfTest(t *testing.T) {
	e := newEnv(t)
	if err := e.m.StartSelfTest(e.ctx, "admin", "d1", "long"); err != nil {
		t.Fatal(err)
	}
	if err := e.m.AbortSelfTest(e.ctx, "admin", "d1"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(e.agent.selfTests, []string{"start:/dev/sda:long", "abort:/dev/sda"}) {
		t.Fatalf("%v", e.agent.selfTests)
	}
	if codeOf(e.m.StartSelfTest(e.ctx, "admin", "d1", "weird")) != "invalid_params" {
		t.Error("Testart muss geprüft werden")
	}
	if codeOf(e.m.StartSelfTest(e.ctx, "admin", "nope", "short")) != "not_found" {
		t.Error("unbekannte Platte")
	}
	e.agent.selfTestFail = true
	if codeOf(e.m.StartSelfTest(e.ctx, "admin", "d1", "short")) != "conflict" {
		t.Error("Fehler des Agents muss durchgereicht werden")
	}
}

func TestSelfTestTickRunsDueTestsInOrder(t *testing.T) {
	e := newEnv(t)
	e.m.selfTestPoll = time.Millisecond
	e.agent.smartBy = map[string]SmartInfo{"/dev/sda": smartOK(), "/dev/sdb": smartOK(), "/dev/nvme0n1": smartOK()} // sdc ohne SMART
	if err := e.m.SetSelfTestSchedule(e.ctx, "admin", SelfTestSchedule{Short: "weekly", Long: "off", Hour: 3}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 20, 3, 5, 0, 0, time.Local)
	e.m.now = func() time.Time { return at }
	e.m.selfTestTick(e.ctx)
	waitFor(t, func() bool { return !e.m.stRunning.Load() })
	want := []string{"start:/dev/nvme0n1:short", "start:/dev/sda:short", "start:/dev/sdb:short"}
	if !slices.Equal(e.agent.selfTests, want) {
		t.Fatalf("%v", e.agent.selfTests)
	}
	// Zweiter Tick in derselben Woche: nichts mehr.
	e.m.now = func() time.Time { return at.Add(24 * time.Hour) }
	e.m.selfTestTick(e.ctx)
	waitFor(t, func() bool { return !e.m.stRunning.Load() })
	if len(e.agent.selfTests) != 3 {
		t.Fatalf("zu früh erneut gestartet: %v", e.agent.selfTests)
	}
	// Eine Woche später wieder.
	e.m.now = func() time.Time { return at.Add(7 * 24 * time.Hour) }
	e.m.selfTestTick(e.ctx)
	waitFor(t, func() bool { return !e.m.stRunning.Load() })
	if len(e.agent.selfTests) != 6 {
		t.Fatalf("%v", e.agent.selfTests)
	}
}

func TestSelfTestTickOnlyAtConfiguredHourAndLongWins(t *testing.T) {
	e := newEnv(t)
	e.m.selfTestPoll = time.Millisecond
	e.agent.smartBy = map[string]SmartInfo{"/dev/sda": smartOK()}
	_ = e.m.SetSelfTestSchedule(e.ctx, "admin", SelfTestSchedule{Short: "weekly", Long: "monthly", Hour: 3})
	at := time.Date(2026, 11, 20, 4, 0, 0, 0, time.Local)
	e.m.now = func() time.Time { return at }
	e.m.selfTestTick(e.ctx)
	waitFor(t, func() bool { return !e.m.stRunning.Load() })
	if len(e.agent.selfTests) != 0 {
		t.Fatalf("außerhalb der Stunde gestartet: %v", e.agent.selfTests)
	}
	at = at.Add(-time.Hour)
	e.m.selfTestTick(e.ctx)
	waitFor(t, func() bool { return !e.m.stRunning.Load() })
	if !slices.Equal(e.agent.selfTests, []string{"start:/dev/sda:long"}) {
		t.Fatalf("der lange Test soll den kurzen ersetzen: %v", e.agent.selfTests)
	}
}

func TestSelfTestOffDoesNothing(t *testing.T) {
	e := newEnv(t)
	e.agent.smartBy = map[string]SmartInfo{"/dev/sda": smartOK()}
	e.m.now = func() time.Time { return time.Date(2026, 11, 20, 2, 0, 0, 0, time.Local) }
	e.m.selfTestTick(e.ctx)
	if len(e.agent.selfTests) != 0 || e.m.stRunning.Load() {
		t.Fatal("bei Zeitplan „aus“ darf nichts laufen")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("Bedingung nicht eingetreten")
}
