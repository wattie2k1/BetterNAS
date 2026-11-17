package storage

import (
	"encoding/json"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
)

func TestSpindownValue(t *testing.T) {
	for min, want := range map[int]int{0: 0, 5: 60, 10: 120, 20: 240, 30: 241, 60: 242, 120: 244, 240: 248} {
		got, err := SpindownValue(min)
		if err != nil || got != want {
			t.Errorf("%d min: %d %v, erwartet %d", min, got, err, want)
		}
	}
	for _, min := range []int{-1, 7, 15, 300} {
		if _, err := SpindownValue(min); apierr.From(err).Code != apierr.InvalidParams {
			t.Errorf("%d min: erwartet invalid_params, bekam %v", min, err)
		}
	}
}

func lastReq(e *env) map[string]any { return e.agent.spindownReqs[len(e.agent.spindownReqs)-1] }

func TestSetSpindownSavesOnlyOnSuccess(t *testing.T) {
	e := newEnv(t)
	e.formatDisk(t, "d1", "SER1")
	if err := e.m.SetSpindown(e.ctx, "anna", "d1", 60); err != nil {
		t.Fatal(err)
	}
	if r := lastReq(e); r["device"] != "/dev/sda" || r["value"].(float64) != 242 {
		t.Fatalf("%v", r)
	}
	got, _ := e.m.Spindown(e.ctx)
	if got["d1"] != 60 {
		t.Fatalf("%v", got)
	}
	// Agent lehnt ab → Einstellung bleibt bei 60
	e.agent.spindownFail = map[string]bool{"/dev/sda": true}
	if err := e.m.SetSpindown(e.ctx, "anna", "d1", 120); apierr.From(err).Code != apierr.Conflict {
		t.Fatalf("%v", err)
	}
	got, _ = e.m.Spindown(e.ctx)
	if got["d1"] != 60 {
		t.Fatalf("trotz Fehler gespeichert: %v", got)
	}
	// Anzeige in der Platten-Liste
	e.agent.spindownFail = nil
	ds, _ := e.m.ListDisks(e.ctx)
	for _, d := range ds {
		if d.ID == "d1" && d.Spindown != 60 {
			t.Fatalf("DiskView.Spindown=%d", d.Spindown)
		}
	}
	// Audit
	list, _ := audit.List(e.ctx, e.m.db, 50)
	found := false
	for _, a := range list {
		if a.Action == "disk.spindown" && a.Target == "d1" && a.Actor == "anna" {
			found = true
		}
	}
	if !found {
		t.Fatal("kein Audit-Eintrag")
	}
	// 0 entfernt den Eintrag
	if err := e.m.SetSpindown(e.ctx, "anna", "d1", 0); err != nil {
		t.Fatal(err)
	}
	if got, _ = e.m.Spindown(e.ctx); len(got) != 0 {
		t.Fatalf("%v", got)
	}
}

func TestSetSpindownRejectsSystemUnassignedAndBadMinutes(t *testing.T) {
	e := newEnv(t)
	for _, id := range []string{"sys", "d2"} { // Systemplatte, unzugeordnet
		if err := e.m.SetSpindown(e.ctx, "anna", id, 60); apierr.From(err).Code != apierr.Conflict {
			t.Errorf("%s: %v", id, err)
		}
	}
	e.formatDisk(t, "d1", "SER1")
	if err := e.m.SetSpindown(e.ctx, "anna", "d1", 7); apierr.From(err).Code != apierr.InvalidParams {
		t.Fatalf("%v", err)
	}
	if err := e.m.SetSpindown(e.ctx, "anna", "nope", 60); apierr.From(err).Code != apierr.NotFound {
		t.Fatalf("%v", err)
	}
	if len(e.agent.spindownReqs) != 0 {
		t.Fatal("Agent darf nicht aufgerufen werden")
	}
}

func TestReconcileSpindownAppliesAndSkipsMissing(t *testing.T) {
	e := newEnv(t)
	e.formatDisk(t, "d1", "SER1")
	e.formatDisk(t, "d2", "SER2")
	_ = e.m.SetSpindown(e.ctx, "a", "d1", 60)
	_ = e.m.SetSpindown(e.ctx, "a", "d2", 30)
	// Fehler einer Platte stoppt die andere nicht
	e.agent.spindownReqs = nil
	e.agent.spindownFail = map[string]bool{"/dev/sda": true}
	if err := e.m.ReconcileSpindown(e.ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.agent.spindownReqs) != 2 {
		t.Fatalf("%d Aufrufe: %v", len(e.agent.spindownReqs), e.agent.spindownReqs)
	}
	b, _ := json.Marshal(e.agent.spindownReqs)
	_ = b
	// fehlende Platte wird übersprungen
	e.agent.spindownFail = nil
	e.agent.spindownReqs = nil
	e.agent.disks = []Disk{liveDisks()[0], liveDisks()[1]} // d2 fehlt
	if err := e.m.ReconcileSpindown(e.ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.agent.spindownReqs) != 1 || e.agent.spindownReqs[0]["device"] != "/dev/sda" {
		t.Fatalf("%v", e.agent.spindownReqs)
	}
}
