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
		if v != 5 || n != 5 {
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

func TestSharesMigration(t *testing.T) {
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
	mustExec(`INSERT INTO pools(name, create_policy, min_free, created_at) VALUES ('media','epmfs','20G','now')`)
	mustExec(`INSERT INTO shares(name, pool, subdir, browsable, guest, recycle, fruit, created_at) VALUES ('Filme','media','Filme',1,0,1,1,'now')`)
	mustExec(`INSERT INTO share_acl(share_name, principal_type, principal_name, access) VALUES ('Filme','user','anna','rw')`)
	// Namen sind ohne Beachtung der Groß-/Kleinschreibung eindeutig.
	if _, err := d.Exec(`INSERT INTO shares(name, pool, subdir, browsable, guest, recycle, fruit, created_at) VALUES ('filme','media','x',1,0,0,0,'now')`); err == nil {
		t.Fatal("doppelter Name (anders geschrieben) wurde akzeptiert")
	}
	if _, err := d.Exec(`INSERT INTO share_acl(share_name, principal_type, principal_name, access) VALUES ('Filme','user','bob','execute')`); err == nil {
		t.Fatal("ungültiger Zugriff akzeptiert")
	}
	mustExec(`DELETE FROM shares WHERE name = 'FILME'`)
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM share_acl`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("ACL nicht mitgelöscht: %d %v", n, err)
	}
}
