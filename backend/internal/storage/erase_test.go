package storage

import (
	"testing"
	"time"
)

func eraseOf(t *testing.T, e *env, id string) *Erase {
	t.Helper()
	ds, err := e.m.ListDisks(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ds {
		if d.ID == id {
			return d.Erase
		}
	}
	t.Fatalf("%s fehlt", id)
	return nil
}

func previewToken(t *testing.T, e *env, id string) string {
	t.Helper()
	p, err := e.m.PreviewErase(e.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return p.ConfirmToken
}

func TestEraseRequiresConfirmationAndRuns(t *testing.T) {
	e := newEnv(t)
	e.m.erasePoll = 5 * time.Millisecond
	e.agent.eraseResult = "done"
	tok := previewToken(t, e, "d1")
	if codeOf(e.m.StartErase(e.ctx, "admin", "d1", EraseRequest{Method: "zero", ConfirmSerial: "falsch", ConfirmToken: tok})) != "invalid_params" {
		t.Fatal("falsche Seriennummer")
	}
	if codeOf(e.m.StartErase(e.ctx, "admin", "d1", EraseRequest{Method: "zero", ConfirmSerial: "SER1", ConfirmToken: "kaputt"})) != "conflict" {
		t.Fatal("unbekanntes Token")
	}
	if codeOf(e.m.StartErase(e.ctx, "admin", "d2", EraseRequest{Method: "zero", ConfirmSerial: "SER2", ConfirmToken: tok})) != "conflict" {
		t.Fatal("Token einer anderen Platte")
	}
	if len(e.agent.erases) != 0 {
		t.Fatalf("nichts darf gestartet sein: %v", e.agent.erases)
	}
	if err := e.m.StartErase(e.ctx, "admin", "d1", EraseRequest{Method: "zero", ConfirmSerial: "SER1", ConfirmToken: tok}); err != nil {
		t.Fatal(err)
	}
	if x := eraseOf(t, e, "d1"); x == nil || !x.Running || x.Method != "zero" {
		t.Fatalf("%+v", x)
	}
	// Token ist verbraucht.
	if codeOf(e.m.StartErase(e.ctx, "admin", "d1", EraseRequest{Method: "zero", ConfirmSerial: "SER1", ConfirmToken: tok})) != "conflict" {
		t.Fatal("Token nur einmal")
	}
	waitFor(t, func() bool { x := eraseOf(t, e, "d1"); return x != nil && !x.Running })
	if x := eraseOf(t, e, "d1"); x.Result != "done" {
		t.Fatalf("%+v", x)
	}
	if len(e.agent.erases) != 1 || e.agent.erases[0] != "/dev/sda:zero" {
		t.Fatalf("%v", e.agent.erases)
	}
}

func TestEraseRefusesSystemAndUsedDisks(t *testing.T) {
	e := newEnv(t)
	if _, err := e.m.PreviewErase(e.ctx, "sys"); codeOf(err) != "system_disk" {
		t.Fatalf("Systemplatte: %v", err)
	}
	// d1 wird als Datenplatte benutzt → erst freigeben.
	e.formatDisk(t, "d1", "SER1")
	if _, err := e.m.PreviewErase(e.ctx, "d1"); codeOf(err) != "conflict" {
		t.Fatalf("benutzte Platte: %v", err)
	}
	if _, err := e.m.PreviewErase(e.ctx, "nope"); codeOf(err) != "not_found" {
		t.Fatal("unbekannt")
	}
	// TRIM nicht auf drehenden Platten.
	tok := previewToken(t, e, "d2")
	e.agent.disks[2].Rotational = true
	if codeOf(e.m.StartErase(e.ctx, "admin", "d2", EraseRequest{Method: "discard", ConfirmSerial: "SER2", ConfirmToken: tok})) != "invalid_params" {
		t.Fatal("discard auf HDD")
	}
}

func TestEraseAbortAndFailure(t *testing.T) {
	e := newEnv(t)
	e.m.erasePoll = 5 * time.Millisecond
	e.agent.eraseResult, e.agent.eraseError = "failed", "E/A-Fehler"
	tok := previewToken(t, e, "d2")
	if err := e.m.StartErase(e.ctx, "admin", "d2", EraseRequest{Method: "zero", ConfirmSerial: "SER2", ConfirmToken: tok}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { x := eraseOf(t, e, "d2"); return x != nil && !x.Running })
	if x := eraseOf(t, e, "d2"); x.Result != "failed" || x.Error != "E/A-Fehler" {
		t.Fatalf("%+v", x)
	}
	if codeOf(e.m.AbortErase(e.ctx, "admin", "d2")) != "conflict" {
		t.Fatal("kein Lauf zum Abbrechen")
	}
}
