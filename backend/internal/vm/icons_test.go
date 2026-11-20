package vm

import (
	"context"
	"testing"
)

var testPNG = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)

func TestIconLifecycle(t *testing.T) {
	m, _, _ := newMgr(t, &stubAgent{})
	ctx := context.Background()
	if _, err := m.GetIcon(ctx, "debian"); err == nil {
		t.Fatal("ohne Icon muss NotFound kommen")
	}
	if err := m.SetIcon(ctx, "anna", "debian", []byte("<html></html>")); err == nil {
		t.Fatal("Nicht-Bild muss abgelehnt werden")
	}
	if err := m.SetIcon(ctx, "anna", "debian", testPNG); err != nil {
		t.Fatal(err)
	}
	ic, err := m.GetIcon(ctx, "debian")
	if err != nil || ic.ContentType != "image/png" || ic.Version == "" {
		t.Fatalf("%+v %v", ic, err)
	}
	// Umbenennen nimmt das Icon mit.
	if err := m.Rename(ctx, "anna", "debian", Rename{NewName: "bookworm"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetIcon(ctx, "bookworm"); err != nil {
		t.Fatalf("Icon nach Umbenennen: %v", err)
	}
	if _, err := m.GetIcon(ctx, "debian"); err == nil {
		t.Fatal("altes Icon muss weg sein")
	}
	// Löschen der VM räumt das Icon auf.
	if err := m.Delete(ctx, "anna", "bookworm", false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetIcon(ctx, "bookworm"); err == nil {
		t.Fatal("Icon der gelöschten VM muss weg sein")
	}
}
