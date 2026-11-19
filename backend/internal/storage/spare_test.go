package storage

import "testing"

func spareOf(t *testing.T, e *env, id string) bool {
	t.Helper()
	ds, err := e.m.ListDisks(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ds {
		if d.ID == id {
			return d.Spare
		}
	}
	t.Fatalf("%s fehlt", id)
	return false
}

func TestSpareMarkAndUnmark(t *testing.T) {
	e := newEnv(t)
	if err := e.m.SetSpare(e.ctx, "admin", "d1", true); err != nil {
		t.Fatal(err)
	}
	if !spareOf(t, e, "d1") || spareOf(t, e, "d2") {
		t.Fatal("nur d1 ist Ersatzplatte")
	}
	if err := e.m.SetSpare(e.ctx, "admin", "d1", false); err != nil {
		t.Fatal(err)
	}
	if spareOf(t, e, "d1") {
		t.Fatal("Markierung muss weg sein")
	}
}

func TestSpareRefusals(t *testing.T) {
	e := newEnv(t)
	if codeOf(e.m.SetSpare(e.ctx, "admin", "sys", true)) != "conflict" {
		t.Error("Systemplatte")
	}
	if codeOf(e.m.SetSpare(e.ctx, "admin", "nope", true)) != "not_found" {
		t.Error("unbekannt")
	}
}

func TestSpareFlagIgnoredOnceDiskIsUsed(t *testing.T) {
	e := newEnv(t)
	if err := e.m.SetSpare(e.ctx, "admin", "d2", true); err != nil {
		t.Fatal(err)
	}
	// Als Paritätsplatte formatiert: die Markierung bleibt gespeichert, gilt aber nicht mehr.
	p, err := e.m.PreviewFormat(e.ctx, "d2")
	if err != nil {
		t.Fatal(err)
	}
	id, err := e.m.Format(e.ctx, "admin", "d2", FormatRequest{FSType: "ext4", Role: "parity", ConfirmSerial: "SER2", ConfirmToken: p.ConfirmToken})
	e.mustSucceed(t, id, err)
	if spareOf(t, e, "d2") {
		t.Fatal("Paritätsplatte ist keine Ersatzplatte")
	}
	if codeOf(e.m.SetSpare(e.ctx, "admin", "d2", true)) != "conflict" {
		t.Error("Paritätsplatte darf nicht markiert werden")
	}
}
