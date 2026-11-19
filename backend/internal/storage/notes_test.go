package storage

import (
	"strings"
	"testing"
)

func noteOf(t *testing.T, e *env, id string) *DiskNote {
	t.Helper()
	ds, err := e.m.ListDisks(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ds {
		if d.ID == id {
			return d.Note
		}
	}
	t.Fatalf("%s fehlt", id)
	return nil
}

func TestNoteSaveUpdateAndClear(t *testing.T) {
	e := newEnv(t)
	if noteOf(t, e, "d1") != nil {
		t.Fatal("ohne Angaben kein Note")
	}
	in := DiskNote{Name: "  Schacht 3 ", Notes: "gebraucht gekauft", Purchased: "2024-03-01", WarrantyUntil: "2027-03-01"}
	if err := e.m.SetNote(e.ctx, "admin", "d1", in); err != nil {
		t.Fatal(err)
	}
	got := noteOf(t, e, "d1")
	if got == nil || got.Name != "Schacht 3" || got.Purchased != "2024-03-01" || got.WarrantyUntil != "2027-03-01" || got.Notes != "gebraucht gekauft" {
		t.Fatalf("%+v", got)
	}
	if noteOf(t, e, "d2") != nil {
		t.Fatal("andere Platte unberührt")
	}
	if err := e.m.SetNote(e.ctx, "admin", "d1", DiskNote{Name: "Neu"}); err != nil {
		t.Fatal(err)
	}
	if got := noteOf(t, e, "d1"); got.Name != "Neu" || got.Notes != "" || got.Purchased != "" {
		t.Fatalf("ersetzt, nicht gemischt: %+v", got)
	}
	if err := e.m.SetNote(e.ctx, "admin", "d1", DiskNote{Name: "  "}); err != nil {
		t.Fatal(err)
	}
	if noteOf(t, e, "d1") != nil {
		t.Fatal("leere Angaben löschen die Notiz")
	}
}

func TestNoteValidation(t *testing.T) {
	e := newEnv(t)
	for name, n := range map[string]DiskNote{
		"Name zu lang":  {Name: strings.Repeat("ä", 61)},
		"Notiz zu lang": {Notes: strings.Repeat("x", 1001)},
		"Datum":         {Purchased: "01.03.2024"},
		"Garantie":      {WarrantyUntil: "2027-13-40"},
	} {
		if codeOf(e.m.SetNote(e.ctx, "admin", "d1", n)) != "invalid_params" {
			t.Errorf("%s muss abgelehnt werden", name)
		}
	}
	if err := e.m.SetNote(e.ctx, "admin", "d1", DiskNote{Name: strings.Repeat("ä", 60)}); err != nil {
		t.Fatalf("60 Zeichen (Umlaute) sind erlaubt: %v", err)
	}
	if codeOf(e.m.SetNote(e.ctx, "admin", "nope", DiskNote{Name: "x"})) != "not_found" {
		t.Error("unbekannte Platte")
	}
}

func TestNoteSurvivesMissingDisk(t *testing.T) {
	e := newEnv(t)
	e.twoDataDisks(t)
	if err := e.m.SetNote(e.ctx, "admin", "d1", DiskNote{Name: "Ersatz"}); err != nil {
		t.Fatal(err)
	}
	e.agent.disks = e.agent.disks[:1] // d1 verschwunden
	ds, err := e.m.ListDisks(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ds {
		if d.ID == "d1" && (d.State != "missing" || d.Note == nil || d.Note.Name != "Ersatz") {
			t.Fatalf("%+v", d)
		}
	}
	if err := e.m.SetNote(e.ctx, "admin", "d1", DiskNote{Name: "Auch fehlend änderbar"}); err != nil {
		t.Fatalf("fehlende, bekannte Platte: %v", err)
	}
}

func TestNoteTempLimit(t *testing.T) {
	e := newEnv(t)
	if err := e.m.SetNote(e.ctx, "admin", "d1", DiskNote{TempWarnC: 20}); err == nil {
		t.Fatal("zu niedrige Grenze akzeptiert")
	}
	if err := e.m.SetNote(e.ctx, "admin", "d1", DiskNote{TempWarnC: 95}); err == nil {
		t.Fatal("zu hohe Grenze akzeptiert")
	}
	if err := e.m.SetNote(e.ctx, "admin", "d1", DiskNote{TempWarnC: 45}); err != nil {
		t.Fatal(err)
	}
	if got := noteOf(t, e, "d1"); got == nil || got.TempWarnC != 45 {
		t.Fatalf("%+v", got)
	}
	if err := e.m.SetNote(e.ctx, "admin", "d1", DiskNote{}); err != nil {
		t.Fatal(err)
	}
	if noteOf(t, e, "d1") != nil {
		t.Fatal("0 = globale Schwelle, Eintrag leer")
	}
}
