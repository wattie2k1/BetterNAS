package audit

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/betternas/betternas/internal/db"
)

func TestAuditRecordAndList(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	if err := Record(ctx, d, Entry{Actor: "anna", Action: "auth.login", Target: "anna", Result: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := Record(ctx, d, Entry{Actor: "anna", Action: "pool.create", Target: "media", Result: "ok", Details: map[string]any{"branches": 2.0}}); err != nil {
		t.Fatal(err)
	}
	list, err := List(ctx, d, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Action != "pool.create" || list[0].Details["branches"] != 2.0 || list[1].Action != "auth.login" {
		t.Fatalf("%+v", list)
	}
	if list[0].Time.IsZero() {
		t.Fatal("timestamp missing")
	}
}
