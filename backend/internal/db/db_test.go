package db

import (
	"path/filepath"
	"testing"
)

func TestOpenRunsMigrationsOnce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "b.db")
	for i := 0; i < 2; i++ {
		d, err := Open(p)
		if err != nil {
			t.Fatal(err)
		}
		var v, n int
		if err := d.QueryRow(`SELECT MAX(version), COUNT(*) FROM schema_version`).Scan(&v, &n); err != nil {
			t.Fatal(err)
		}
		if v != 1 || n != 1 {
			t.Fatalf("version %d rows %d", v, n)
		}
		d.Close()
	}
}

func TestDiskInTwoPoolsRejected(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO disks(id, role, fs_type, fs_uuid, label, size_bytes, created_at) VALUES ('d1','data','ext4','u','l',1,'now')`)
	mustExec(`INSERT INTO pools(name, create_policy, min_free, created_at) VALUES ('a','epmfs','20G','now'), ('b','epmfs','20G','now')`)
	mustExec(`INSERT INTO pool_branches(pool_name, disk_id, position) VALUES ('a','d1',0)`)
	if _, err := d.Exec(`INSERT INTO pool_branches(pool_name, disk_id, position) VALUES ('b','d1',0)`); err == nil {
		t.Fatal("disk in two pools accepted")
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	d, _ := Open(filepath.Join(t.TempDir(), "b.db"))
	defer d.Close()
	if _, err := d.Exec(`INSERT INTO pool_branches(pool_name, disk_id, position) VALUES ('x','y',0)`); err == nil {
		t.Fatal("foreign keys not enforced")
	}
}
