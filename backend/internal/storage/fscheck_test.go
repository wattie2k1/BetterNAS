package storage

import (
	"testing"
	"time"
)

func fsCheckOf(t *testing.T, e *env, id string) *FsCheck {
	t.Helper()
	ds, err := e.m.ListDisks(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ds {
		if d.ID == id {
			return d.FsCheck
		}
	}
	t.Fatalf("%s fehlt", id)
	return nil
}

func TestFsCheckRunsAndStoresResult(t *testing.T) {
	e := newEnv(t)
	e.m.fsPoll = 5 * time.Millisecond
	e.agent.fsResult = "errors"
	if fsCheckOf(t, e, "d1") != nil {
		t.Fatal("vorher keine Prüfung")
	}
	if err := e.m.StartFsCheck(e.ctx, "admin", "d1"); err != nil {
		t.Fatal(err)
	}
	if c := fsCheckOf(t, e, "d1"); c == nil || !c.Running {
		t.Fatalf("muss laufen: %+v", c)
	}
	// Zweite Prüfung gleichzeitig: abgelehnt.
	if codeOf(e.m.StartFsCheck(e.ctx, "admin", "d2")) != "conflict" {
		t.Error("nur eine Prüfung gleichzeitig")
	}
	waitFor(t, func() bool { c := fsCheckOf(t, e, "d1"); return c != nil && !c.Running })
	c := fsCheckOf(t, e, "d1")
	if c.Result != "errors" || c.Tool != "e2fsck" || c.Output != "Ausgabe" || c.FinishedAt == nil {
		t.Fatalf("%+v", c)
	}
	if len(e.agent.fsChecks) != 1 || e.agent.fsChecks[0] != "/dev/sda" {
		t.Fatalf("%v", e.agent.fsChecks)
	}
}

func TestFsCheckUnknownDisk(t *testing.T) {
	e := newEnv(t)
	if codeOf(e.m.StartFsCheck(e.ctx, "admin", "nope")) != "not_found" {
		t.Error("unbekannte Platte")
	}
}
