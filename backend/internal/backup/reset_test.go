package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/betternas/betternas/internal/db"
)

type resetAgent struct{ err error }

func (a resetAgent) Call(context.Context, string, any, any) error { return a.err }

func TestApplyFactoryReset(t *testing.T) {
	dir := t.TempDir()
	rdir := filepath.Join(dir, "restore")
	db := filepath.Join(dir, "betternas.db")
	if ok, err := ApplyFactoryReset(rdir, db, 2); ok || err != nil {
		t.Fatalf("ohne Marker nichts tun: %v %v", ok, err)
	}
	os.WriteFile(db, []byte("alt"), 0o600)
	os.WriteFile(db+"-wal", []byte("wal"), 0o600)
	if err := StageReset(rdir); err != nil {
		t.Fatal(err)
	}
	ok, err := ApplyFactoryReset(rdir, db, 2)
	if !ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	if _, err := os.Stat(db); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("DB muss weg sein")
	}
	if _, err := os.Stat(filepath.Join(rdir, resetMarker)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Marker muss weg sein")
	}
	saved, _ := filepath.Glob(db + ".pre-reset-*")
	if len(saved) != 2 { // DB + WAL
		t.Fatalf("%v", saved)
	}
	if ok, _ := ApplyFactoryReset(rdir, db, 2); ok {
		t.Fatal("zweiter Lauf ohne Marker")
	}
}

func TestFactoryResetRollsBackOnRestartError(t *testing.T) {
	tmp := t.TempDir()
	rdir := filepath.Join(tmp, "restore")
	d, err := db.Open(filepath.Join(tmp, "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	m := &Manager{db: d, restoreDir: rdir, agent: resetAgent{err: errors.New("x")}}
	if err := m.FactoryReset(context.Background(), "alice"); err == nil {
		t.Fatal("Fehler erwartet")
	}
	if _, err := os.Stat(filepath.Join(rdir, resetMarker)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Marker muss zurückgenommen sein")
	}
}
