package storage

import (
	"testing"
	"time"
)

func (f *fakeAgent) pulses() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.identifyDevs)
}

func identifying(t *testing.T, e *env, id string) bool {
	t.Helper()
	ds, err := e.m.ListDisks(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ds {
		if d.ID == id {
			return d.Identifying
		}
	}
	t.Fatalf("%s fehlt", id)
	return false
}

func TestIdentifyPulsesThenStopsByItself(t *testing.T) {
	e := newEnv(t)
	e.m.identifyFor, e.m.identifyGap = 80*time.Millisecond, 5*time.Millisecond
	if err := e.m.StartIdentify(e.ctx, "admin", "d1"); err != nil {
		t.Fatal(err)
	}
	if !identifying(t, e, "d1") || identifying(t, e, "d2") {
		t.Fatal("nur d1 soll blinken")
	}
	if err := e.m.StartIdentify(e.ctx, "admin", "d1"); err != nil { // zweiter Aufruf: kein neuer Lauf
		t.Fatal(err)
	}
	waitFor(t, func() bool { return !identifying(t, e, "d1") })
	if n := e.agent.pulses(); n < 3 {
		t.Fatalf("nur %d Impulse", n)
	}
	for _, d := range e.agent.identifyDevs {
		if d != "/dev/sda" {
			t.Fatalf("falsches Gerät %s", d)
		}
	}
}

func TestIdentifyStopAndErrors(t *testing.T) {
	e := newEnv(t)
	e.m.identifyFor, e.m.identifyGap = time.Minute, 5*time.Millisecond
	if err := e.m.StartIdentify(e.ctx, "admin", "d2"); err != nil {
		t.Fatal(err)
	}
	e.m.StopIdentify("d2")
	waitFor(t, func() bool { return !identifying(t, e, "d2") })
	n := e.agent.pulses()
	time.Sleep(30 * time.Millisecond)
	if e.agent.pulses() > n+1 {
		t.Fatal("nach dem Stoppen darf nicht weiter gelesen werden")
	}
	if codeOf(e.m.StartIdentify(e.ctx, "admin", "nope")) != "not_found" {
		t.Error("unbekannte Platte")
	}
	e.agent.identifyFail = true
	if codeOf(e.m.StartIdentify(e.ctx, "admin", "d1")) != "conflict" {
		t.Error("Fehler des ersten Impulses muss ankommen")
	}
	if identifying(t, e, "d1") {
		t.Error("nach Fehler darf nichts blinken")
	}
	e.m.StopIdentify("unbekannt") // darf nicht abstürzen
}
