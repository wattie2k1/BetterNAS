// Package backup sichert und stellt die BetterNAS-Konfiguration wieder her (DB-Schnappschuss als tar.gz).
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // reiner Go-Treiber, kein cgo

	"github.com/betternas/betternas/internal/apierr"
)

// Format ist die Version des Archivformats.
const Format = 1

const (
	manifestName = "manifest.json"
	dbName       = "betternas.db"
	maxDBBytes   = 1 << 30
	maxMeta      = 1 << 20
)

// NameRe beschreibt gültige Backup-Dateinamen.
var NameRe = regexp.MustCompile(`^betternas-backup-\d{8}-\d{6}\.tar\.gz$`)

// Manifest beschreibt den Inhalt eines Archivs.
type Manifest struct {
	Format        int            `json:"format"`
	CreatedAt     time.Time      `json:"createdAt"`
	Version       string         `json:"version"`
	SchemaVersion int            `json:"schemaVersion"`
	Host          string         `json:"host"`
	Counts        map[string]int `json:"counts"`
}

// Info beschreibt eine abgelegte Sicherung.
type Info struct {
	Name     string   `json:"name"`
	Size     int64    `json:"size"`
	Manifest Manifest `json:"manifest"`
}

func invalid(msg string) error { return apierr.New(apierr.InvalidParams, msg) }

// SchemaVersion liefert die höchste angewendete Migration.
func SchemaVersion(d *sql.DB) (int, error) {
	var v int
	err := d.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&v)
	return v, err
}

func countRows(c *sql.DB, table string) int {
	var n int
	if err := c.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		return 0
	}
	return n
}

// Create schreibt eine neue Sicherung nach dir. Die DB wird per VACUUM INTO konsistent kopiert;
// Sitzungen und Aufträge bleiben draußen.
func Create(ctx context.Context, d *sql.DB, dir, version string) (Info, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Info{}, err
	}
	work, err := os.MkdirTemp(dir, ".work-")
	if err != nil {
		return Info{}, err
	}
	defer os.RemoveAll(work)
	copyPath := filepath.Join(work, dbName)
	if _, err := d.ExecContext(ctx, `VACUUM INTO ?`, copyPath); err != nil {
		return Info{}, fmt.Errorf("datenbank sichern: %w", err)
	}
	c, err := sql.Open("sqlite", "file:"+copyPath)
	if err != nil {
		return Info{}, err
	}
	schema, serr := SchemaVersion(c)
	if serr != nil {
		c.Close()
		return Info{}, serr
	}
	for _, q := range []string{`DELETE FROM sessions`, `DELETE FROM jobs`} {
		if _, err := c.ExecContext(ctx, q); err != nil {
			c.Close()
			return Info{}, err
		}
	}
	counts := map[string]int{}
	for _, t := range []string{"pools", "shares", "stacks", "disks"} {
		counts[t] = countRows(c, t)
	}
	if _, err := c.ExecContext(ctx, `VACUUM`); err != nil {
		c.Close()
		return Info{}, err
	}
	if err := c.Close(); err != nil {
		return Info{}, err
	}
	host, _ := os.Hostname()
	now := time.Now().UTC()
	m := Manifest{Format: Format, CreatedAt: now, Version: version, SchemaVersion: schema, Host: host, Counts: counts}
	mb, _ := json.Marshal(m)

	name, final := "", ""
	for ts := now; ; ts = ts.Add(time.Second) {
		name = "betternas-backup-" + ts.Format("20060102-150405") + ".tar.gz"
		final = filepath.Join(dir, name)
		if _, err := os.Stat(final); errors.Is(err, os.ErrNotExist) {
			break
		}
	}
	tmp := filepath.Join(work, "archive.tar.gz")
	if err := writeArchive(tmp, mb, copyPath); err != nil {
		return Info{}, err
	}
	if err := os.Rename(tmp, final); err != nil {
		return Info{}, err
	}
	st, err := os.Stat(final)
	if err != nil {
		return Info{}, err
	}
	return Info{Name: name, Size: st.Size(), Manifest: m}, nil
}

func writeArchive(path string, manifest []byte, dbPath string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: manifestName, Mode: 0o600, Size: int64(len(manifest)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err := tw.Write(manifest); err != nil {
		return err
	}
	db, err := os.Open(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	st, err := db.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: dbName, Mode: 0o600, Size: st.Size(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err := io.Copy(tw, db); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return f.Sync()
}

// Inspect entpackt ein Archiv nach tmpDir und prüft es (Einträge, Format, Schema, DB-Integrität).
// maxSchema ist die höchste Schema-Version, die diese BetterNAS-Version kennt.
func Inspect(r io.Reader, tmpDir string, maxSchema int) (Manifest, string, error) {
	var m Manifest
	gz, err := gzip.NewReader(r)
	if err != nil {
		return m, "", invalid("Das ist kein gültiges BetterNAS-Backup (kein .tar.gz).")
	}
	tr := tar.NewReader(gz)
	dbPath := filepath.Join(tmpDir, dbName)
	var haveManifest, haveDB bool
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return m, "", invalid("Das Backup ist beschädigt oder unvollständig.")
		}
		if h.Typeflag != tar.TypeReg {
			return m, "", invalid("Das Backup enthält unerwartete Einträge.")
		}
		switch h.Name {
		case manifestName:
			b, err := io.ReadAll(io.LimitReader(tr, maxMeta+1))
			if err != nil || len(b) > maxMeta {
				return m, "", invalid("Das Backup ist beschädigt oder unvollständig.")
			}
			if err := json.Unmarshal(b, &m); err != nil {
				return m, "", invalid("Das Manifest des Backups ist ungültig.")
			}
			haveManifest = true
		case dbName:
			f, err := os.OpenFile(dbPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return m, "", err
			}
			n, cerr := io.Copy(f, io.LimitReader(tr, maxDBBytes+1))
			f.Close()
			if cerr != nil {
				return m, "", invalid("Das Backup ist beschädigt oder unvollständig.")
			}
			if n > maxDBBytes {
				return m, "", invalid("Die Datenbank im Backup ist zu groß.")
			}
			haveDB = true
		default:
			return m, "", invalid("Das Backup enthält unerwartete Dateien: " + h.Name)
		}
	}
	if !haveManifest || !haveDB {
		return m, "", invalid("Das Backup ist unvollständig (Manifest oder Datenbank fehlt).")
	}
	if m.Format != Format {
		return m, "", invalid(fmt.Sprintf("Unbekanntes Backup-Format %d.", m.Format))
	}
	if m.SchemaVersion > maxSchema {
		return m, "", invalid("Das Backup stammt von einer neueren BetterNAS-Version. Bitte zuerst aktualisieren.")
	}
	if err := checkDB(dbPath, maxSchema); err != nil {
		return m, "", err
	}
	return m, dbPath, nil
}

func checkDB(path string, maxSchema int) error {
	bad := invalid("Die Datenbank im Backup ist beschädigt.")
	c, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return bad
	}
	defer c.Close()
	var ok string
	if err := c.QueryRow(`PRAGMA integrity_check`).Scan(&ok); err != nil || ok != "ok" {
		return bad
	}
	v, err := SchemaVersion(c)
	if err != nil {
		return bad
	}
	if v > maxSchema {
		return invalid("Das Backup stammt von einer neueren BetterNAS-Version. Bitte zuerst aktualisieren.")
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Stage legt die geprüfte DB als „ausstehende Wiederherstellung“ in dir ab (ersetzt eine vorhandene).
func Stage(dir, dbPath string, m Manifest) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(dir, "pending.db.tmp")
	if err := copyFile(dbPath, tmp); err != nil {
		return err
	}
	mb, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "pending.json.tmp"), mb, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(filepath.Join(dir, "pending.json.tmp"), filepath.Join(dir, "pending.json")); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "pending.db"))
}

// Unstage entfernt eine ausstehende Wiederherstellung.
func Unstage(dir string) {
	os.Remove(filepath.Join(dir, "pending.db"))
	os.Remove(filepath.Join(dir, "pending.json"))
}

// ApplyPending tauscht eine ausstehende Wiederherstellung gegen die DB. Aufruf vor dem Öffnen der DB.
// Die alte DB bleibt als <dbPath>.pre-restore-<zeit> liegen (neueste keep Stück).
func ApplyPending(dir, dbPath string, keep int) (bool, error) {
	pending := filepath.Join(dir, "pending.db")
	if _, err := os.Stat(pending); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	var saved string
	if _, err := os.Stat(dbPath); err == nil {
		saved = dbPath + ".pre-restore-" + time.Now().UTC().Format("20060102-150405")
		if err := os.Rename(dbPath, saved); err != nil {
			return false, err
		}
		// WAL und SHM gehören zur alten DB: mit ihr sichern, sonst gehen ungeschriebene Änderungen verloren.
		for _, s := range []string{"-wal", "-shm"} {
			if err := os.Rename(dbPath+s, saved+s); err != nil && !errors.Is(err, os.ErrNotExist) {
				os.Remove(dbPath + s)
			}
		}
	}
	if err := os.Rename(pending, dbPath); err != nil {
		if saved != "" {
			_ = os.Rename(saved, dbPath)
			for _, s := range []string{"-wal", "-shm"} {
				_ = os.Rename(saved+s, dbPath+s)
			}
		}
		return false, err
	}
	// Reste einer unsauber beendeten Sitzung dürfen nicht zur neuen DB passen.
	os.Remove(dbPath + "-wal")
	os.Remove(dbPath + "-shm")
	os.Remove(filepath.Join(dir, "pending.json"))
	all, _ := filepath.Glob(dbPath + ".pre-restore-*")
	var old []string
	for _, o := range all {
		if !strings.HasSuffix(o, "-wal") && !strings.HasSuffix(o, "-shm") {
			old = append(old, o)
		}
	}
	sort.Strings(old)
	for i := 0; i < len(old)-keep; i++ {
		os.Remove(old[i])
		os.Remove(old[i] + "-wal")
		os.Remove(old[i] + "-shm")
	}
	return true, nil
}

func readManifest(path string) (Manifest, error) {
	var m Manifest
	f, err := os.Open(path)
	if err != nil {
		return m, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return m, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			return m, err
		}
		if h.Name == manifestName {
			b, err := io.ReadAll(io.LimitReader(tr, maxMeta))
			if err != nil {
				return m, err
			}
			return m, json.Unmarshal(b, &m)
		}
	}
}

func names(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if e.Type().IsRegular() && NameRe.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out, nil
}

// List liefert die abgelegten Sicherungen, neueste zuerst.
func List(dir string) ([]Info, error) {
	ns, err := names(dir)
	if err != nil {
		return nil, err
	}
	out := []Info{}
	for _, n := range ns {
		st, err := os.Stat(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		m, _ := readManifest(filepath.Join(dir, n)) // unlesbares Manifest: Eintrag bleibt sichtbar
		out = append(out, Info{Name: n, Size: st.Size(), Manifest: m})
	}
	return out, nil
}

// Delete löscht eine Sicherung (nur gültige Namen).
func Delete(dir, name string) error {
	if !NameRe.MatchString(name) {
		return invalid("Ungültiger Backup-Name.")
	}
	if err := os.Remove(filepath.Join(dir, name)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return apierr.New(apierr.NotFound, "Backup nicht gefunden.")
		}
		return err
	}
	return nil
}

// Prune behält nur die keep neuesten Sicherungen.
func Prune(dir string, keep int) error {
	ns, err := names(dir)
	if err != nil {
		return err
	}
	for i := keep; i < len(ns); i++ {
		if err := os.Remove(filepath.Join(dir, ns[i])); err != nil {
			return err
		}
	}
	return nil
}
