package snapraid_test

import (
	"testing"

	"github.com/betternas/betternas/internal/snapraid"
	"github.com/betternas/betternas/internal/storage"
)

// snapraid darf storage nicht importieren (Zyklus); die duplizierten Werte müssen übereinstimmen.
func TestConstantsMatchStorage(t *testing.T) {
	if snapraid.ManagedHeader != storage.ManagedHeader {
		t.Fatal("ManagedHeader")
	}
	if snapraid.DiskPath("x") != storage.DiskPath("x") || snapraid.ParityPath("x") != storage.ParityPath("x") {
		t.Fatal("paths")
	}
	if snapraid.PoolNameRe.String() != storage.PoolNameRe.String() {
		t.Fatal("PoolNameRe")
	}
}
