package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/db"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	for _, q := range []string{
		`INSERT INTO pools(name, create_policy, min_free, created_at) VALUES ('media','epmfs','20G','now')`,
		`INSERT INTO sessions(token_hash, username, csrf, expires_at) VALUES ('t','anna','c','2999-01-01T00:00:00Z')`,
		`INSERT INTO jobs(id, kind, target, status, started_by, started_at) VALUES ('j1','x','y','succeeded','anna','now')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

type entry struct {
	name string
	data []byte
	typ  byte
}

func buildArchive(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		if err := tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0o600, Size: int64(len(e.data)), Typeflag: typ}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// parts liest Manifest und DB-Bytes aus einem frisch erzeugten Archiv.
func parts(t *testing.T, path string) (manifest, dbBytes []byte) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, _ := gzip.NewReader(f)
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		switch h.Name {
		case "manifest.json":
			manifest = b
		case "betternas.db":
			dbBytes = b
		}
	}
}

func validArchive(t *testing.T) (dir string, info Info, manifest, dbBytes []byte) {
	t.Helper()
	d := testDB(t)
	dir = t.TempDir()
	info, err := Create(context.Background(), d, dir, "0.5.0-test")
	if err != nil {
		t.Fatal(err)
	}
	manifest, dbBytes = parts(t, filepath.Join(dir, info.Name))
	return dir, info, manifest, dbBytes
}

func TestCreateIsConsistentAndOpenable(t *testing.T) {
	d := testDB(t)
	dir := t.TempDir()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // schreibt weiter, während das Backup läuft
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				d.Exec(`INSERT INTO audit(ts, actor, action, target, result) VALUES ('t','a','x','y','ok')`)
			}
		}
	}()
	info, err := Create(context.Background(), d, dir, "0.5.0-test")
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if !NameRe.MatchString(info.Name) {
		t.Fatalf("Name %q", info.Name)
	}
	if info.Manifest.Counts["pools"] != 1 || info.Manifest.Version != "0.5.0-test" || info.Manifest.Format != Format {
		t.Fatalf("%+v", info.Manifest)
	}
	// Archiv enthält genau zwei Einträge
	f, _ := os.Open(filepath.Join(dir, info.Name))
	defer f.Close()
	gz, _ := gzip.NewReader(f)
	tr := tar.NewReader(gz)
	var names []string
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		names = append(names, h.Name)
	}
	if strings.Join(names, ",") != "manifest.json,betternas.db" {
		t.Fatalf("%v", names)
	}
	// entpackte DB ist öffnbar und konsistent; Sitzungen und Aufträge sind leer
	_, dbBytes := parts(t, filepath.Join(dir, info.Name))
	p := filepath.Join(t.TempDir(), "x.db")
	os.WriteFile(p, dbBytes, 0o600)
	c, err := sql.Open("sqlite", "file:"+p)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var ok string
	if err := c.QueryRow(`PRAGMA integrity_check`).Scan(&ok); err != nil || ok != "ok" {
		t.Fatalf("%q %v", ok, err)
	}
	var pools, sess, jobs int
	c.QueryRow(`SELECT COUNT(*) FROM pools`).Scan(&pools)
	c.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&sess)
	c.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&jobs)
	if pools != 1 || sess != 0 || jobs != 0 {
		t.Fatalf("pools=%d sessions=%d jobs=%d", pools, sess, jobs)
	}
	// ein zweites Backup in derselben Sekunde überschreibt das erste nicht
	info2, err := Create(context.Background(), d, dir, "0.5.0-test")
	if err != nil || info2.Name == info.Name {
		t.Fatalf("%v %q", err, info2.Name)
	}
}

func TestInspectAcceptsValidArchive(t *testing.T) {
	dir, info, _, _ := validArchive(t)
	f, _ := os.Open(filepath.Join(dir, info.Name))
	defer f.Close()
	tmp := t.TempDir()
	m, dbPath, err := Inspect(f, tmp, 99)
	if err != nil || m.Counts["pools"] != 1 || dbPath != filepath.Join(tmp, "betternas.db") {
		t.Fatalf("%+v %q %v", m, dbPath, err)
	}
}

func TestInspectRejectsBadArchives(t *testing.T) {
	dir, info, manifest, dbBytes := validArchive(t)
	own, err := SchemaVersion(testDB(t))
	if err != nil {
		t.Fatal(err)
	}
	good := func(m []byte) []entry {
		return []entry{{name: "manifest.json", data: m}, {name: "betternas.db", data: dbBytes}}
	}
	mutate := func(old, new string) []byte { return []byte(strings.Replace(string(manifest), old, new, 1)) }
	full, _ := os.ReadFile(filepath.Join(dir, info.Name))
	cases := map[string][]byte{
		"traversal":     buildArchive(t, append(good(manifest), entry{name: "../../etc/passwd", data: []byte("x")})...),
		"absolut":       buildArchive(t, append(good(manifest), entry{name: "/abs", data: []byte("x")})...),
		"zusatzdatei":   buildArchive(t, append(good(manifest), entry{name: "extra.txt", data: []byte("x")})...),
		"symlink":       buildArchive(t, entry{name: "manifest.json", data: manifest}, entry{name: "betternas.db", typ: tar.TypeSymlink}),
		"ohneManifest":  buildArchive(t, entry{name: "betternas.db", data: dbBytes}),
		"ohneDB":        buildArchive(t, entry{name: "manifest.json", data: manifest}),
		"format2":       buildArchive(t, good(mutate(`"format":1`, `"format":2`))...),
		"schemaNeuer":   buildArchive(t, good(mutate(`"schemaVersion":`, `"schemaVersion":9`))...),
		"abgeschnitten": full[:len(full)/2],
		"dbMuell":       buildArchive(t, entry{name: "manifest.json", data: manifest}, entry{name: "betternas.db", data: []byte("das ist keine datenbank, nur text")}),
		"kaputtesJSON":  buildArchive(t, good([]byte("{"))...),
		"keinGzip":      []byte("hallo"),
	}
	for name, data := range cases {
		tmp := t.TempDir()
		_, _, err := Inspect(bytes.NewReader(data), tmp, own)
		if err == nil || apierr.From(err).Code != apierr.InvalidParams {
			t.Errorf("%s: erwartet invalid_params, bekam %v", name, err)
		}
		// nichts außerhalb von tmp geschrieben (tmp selbst darf Reste haben, aber keine fremden Pfade)
		if _, statErr := os.Stat("/etc/passwd.x"); statErr == nil {
			t.Errorf("%s: Datei außerhalb geschrieben", name)
		}
	}
}

func TestStageAndApplyPending(t *testing.T) {
	dir, info, _, dbBytes := validArchive(t)
	f, _ := os.Open(filepath.Join(dir, info.Name))
	defer f.Close()
	tmp := t.TempDir()
	m, dbPath, err := Inspect(f, tmp, 99)
	if err != nil {
		t.Fatal(err)
	}
	rdir := filepath.Join(t.TempDir(), "restore")
	if err := Stage(rdir, dbPath, m); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "betternas.db")
	os.WriteFile(target, []byte("ALT"), 0o600)
	os.WriteFile(target+"-wal", []byte("w"), 0o600)
	os.WriteFile(target+"-shm", []byte("s"), 0o600)
	applied, err := ApplyPending(rdir, target, 3)
	if err != nil || !applied {
		t.Fatalf("%v %v", applied, err)
	}
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, dbBytes) {
		t.Fatal("DB nicht getauscht")
	}
	for _, s := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(target + s); err == nil {
			t.Errorf("%s blieb liegen", s)
		}
	}
	old, _ := filepath.Glob(target + ".pre-restore-*")
	if len(old) != 1 {
		t.Fatalf("%v", old)
	}
	if b, _ := os.ReadFile(old[0]); string(b) != "ALT" {
		t.Fatal("alte DB nicht gesichert")
	}
	for _, n := range []string{"pending.db", "pending.json"} {
		if _, err := os.Stat(filepath.Join(rdir, n)); err == nil {
			t.Errorf("%s nicht entfernt", n)
		}
	}
	if applied, err := ApplyPending(rdir, target, 3); err != nil || applied {
		t.Fatalf("zweiter Aufruf: %v %v", applied, err)
	}
}

func TestApplyPendingPrunesOldRestores(t *testing.T) {
	target := filepath.Join(t.TempDir(), "betternas.db")
	for i := 1; i <= 5; i++ {
		os.WriteFile(fmt.Sprintf("%s.pre-restore-2026010%d-000000", target, i), []byte("x"), 0o600)
	}
	rdir := t.TempDir()
	os.WriteFile(filepath.Join(rdir, "pending.db"), []byte("neu"), 0o600)
	os.WriteFile(target, []byte("alt"), 0o600)
	if _, err := ApplyPending(rdir, target, 3); err != nil {
		t.Fatal(err)
	}
	old, _ := filepath.Glob(target + ".pre-restore-*")
	if len(old) != 3 {
		t.Fatalf("%v", old)
	}
}

func TestApplyPendingWithoutPendingIsNoop(t *testing.T) {
	target := filepath.Join(t.TempDir(), "betternas.db")
	os.WriteFile(target, []byte("alt"), 0o600)
	if applied, err := ApplyPending(t.TempDir(), target, 3); err != nil || applied {
		t.Fatalf("%v %v", applied, err)
	}
	if b, _ := os.ReadFile(target); string(b) != "alt" {
		t.Fatal("DB verändert")
	}
}

func TestApplyPendingKeepsOldDbOnFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignoriert Verzeichnisrechte")
	}
	tdir := t.TempDir()
	target := filepath.Join(tdir, "betternas.db")
	os.WriteFile(target, []byte("alt"), 0o600)
	rdir := t.TempDir()
	os.WriteFile(filepath.Join(rdir, "pending.db"), []byte("neu"), 0o600)
	os.Chmod(tdir, 0o500)
	defer os.Chmod(tdir, 0o700)
	if _, err := ApplyPending(rdir, target, 3); err == nil {
		t.Fatal("Fehler erwartet")
	}
	if b, _ := os.ReadFile(target); string(b) != "alt" {
		t.Fatal("alte DB verändert")
	}
	if _, err := os.Stat(filepath.Join(rdir, "pending.db")); err != nil {
		t.Fatal("pending.db verloren")
	}
}

func TestListDeletePrune(t *testing.T) {
	dir := t.TempDir()
	mk := func(i int) string {
		name := fmt.Sprintf("betternas-backup-2026110%d-020000.tar.gz", i)
		m := fmt.Sprintf(`{"format":1,"createdAt":"2026-11-0%dT02:00:00Z","version":"v%d","schemaVersion":5,"host":"nas","counts":{}}`, i, i)
		os.WriteFile(filepath.Join(dir, name), buildArchive(t, entry{name: "manifest.json", data: []byte(m)}, entry{name: "betternas.db", data: []byte("x")}), 0o600)
		return name
	}
	for i := 1; i <= 9; i++ {
		mk(i)
	}
	os.WriteFile(filepath.Join(dir, "fremd.txt"), []byte("x"), 0o600)
	l, err := List(dir)
	if err != nil || len(l) != 9 || l[0].Manifest.Version != "v9" || l[8].Manifest.Version != "v1" {
		t.Fatalf("%d %v %+v", len(l), err, l)
	}
	if err := Prune(dir, 7); err != nil {
		t.Fatal(err)
	}
	if l, _ := List(dir); len(l) != 7 || l[6].Manifest.Version != "v3" {
		t.Fatalf("%+v", l)
	}
	if err := Delete(dir, l[0].Name); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../x", "foo.tar.gz", "fremd.txt", ""} {
		if err := Delete(dir, bad); apierr.From(err).Code != apierr.InvalidParams {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if err := Delete(dir, "betternas-backup-20260101-000000.tar.gz"); apierr.From(err).Code != apierr.NotFound {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "fremd.txt")); err != nil {
		t.Fatal("Prune/Delete haben fremde Datei berührt")
	}
}
