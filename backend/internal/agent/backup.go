package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/backup"
	"github.com/betternas/betternas/internal/rpc"
)

// Sicherungen in einen frei gewählten Ordner. Die API ist sandboxed und schreibt nur in ihr Zustandsverzeichnis;
// Anlegen, Löschen und Aufräumen im Zielordner übernimmt der Agent. Dateien gehören root:betternas mit 0640, damit
// die API sie lesen (Liste, Download, Wiederherstellung) kann, andere Benutzer aber nicht.

// backupStage ist der Ordner, aus dem der Agent fertige Konfigurations-Sicherungen übernimmt.
const backupStage = "/var/lib/betternas/staging/"

// defaultBackupDir ist der Standardordner der API auf der Systemplatte; auch dorthin darf der Agent schreiben
// (appdata-Sicherung ohne eigenen Ordner).
const defaultBackupDir = "/var/lib/betternas/backups"

const (
	backupGroup = "betternas"
	probeName   = ".betternas-probe"
)

// backupMu serialisiert Sicherungsläufe, ohne die globale Sperre `mutate` minutenlang zu halten.
var backupMu sync.Mutex

// BackupTarget beschreibt einen vorbereiteten Sicherungsordner.
type BackupTarget struct {
	Dir  string `json:"dir"`  // aufgelöster Pfad
	Free int64  `json:"free"` // freie Bytes
	// Appdata ist der Ordner der App-Einstellungen; leer, solange Docker nicht eingerichtet ist.
	Appdata string `json:"appdata"`
}

// BackupResult beschreibt eine angelegte appdata-Sicherung.
type BackupResult struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	// Changed: Dateien haben sich während der Sicherung geändert (laufende Container).
	Changed bool `json:"changed"`
}

// targetDir prüft einen Sicherungsordner und liefert den aufgelösten Pfad. Er muss existieren.
func targetDir(ctx context.Context, d Deps, dir string) (string, error) {
	c := defaultBackupDir
	if dir != defaultBackupDir {
		var err error
		if c, err = backup.CleanDir(dir); err != nil {
			return "", err
		}
	}
	res, err := d.Runner.Run(ctx, nil, "realpath", "-e", "--", c)
	if err != nil || res.ExitCode != 0 {
		return "", apierr.New(apierr.NotFound, "Der Ordner für die Sicherungen wurde nicht gefunden.")
	}
	real := strings.TrimSpace(string(res.Stdout))
	if c == defaultBackupDir && real == defaultBackupDir {
		return real, nil
	}
	real, err = backup.CleanDir(real)
	if err != nil {
		return "", apierr.New(apierr.InvalidParams, "Der Ordner für die Sicherungen führt (über einen Link) in einen nicht erlaubten Bereich.")
	}
	return real, nil
}

func freeBytes(ctx context.Context, d Deps, dir string) int64 {
	out, err := run(ctx, d.Runner, nil, "df", "--output=avail", "-B1", dir)
	if err != nil {
		return 0
	}
	f := strings.Fields(string(out))
	if len(f) < 2 {
		return 0
	}
	n, _ := strconv.ParseInt(f[len(f)-1], 10, 64)
	return n
}

// BackupPrepare legt den Ordner bei Bedarf an und prüft, dass der Agent dort Dateien mit Besitzer root:betternas
// anlegen kann (Netzlaufwerke ohne Besitzer-Unterstützung scheitern hier mit einer lesbaren Meldung).
func BackupPrepare(ctx context.Context, d Deps, dir string) (BackupTarget, error) {
	c := defaultBackupDir
	mk := []string{"install", "-d", "-m", "0700", "-o", backupGroup, "-g", backupGroup, "--", c}
	if dir != defaultBackupDir {
		var err error
		if c, err = backup.CleanDir(dir); err != nil {
			return BackupTarget{}, err
		}
		mk = []string{"mkdir", "-p", "-m", "755", "--", c}
	}
	backupMu.Lock()
	defer backupMu.Unlock()
	if _, err := run(ctx, d.Runner, nil, mk[0], mk[1:]...); err != nil {
		return BackupTarget{}, apierr.New(apierr.InvalidParams, "Der Ordner konnte nicht angelegt werden.").With("detail", err.Error())
	}
	real, err := targetDir(ctx, d, c)
	if err != nil {
		return BackupTarget{}, err
	}
	probe := path.Join(real, probeName)
	if _, err := run(ctx, d.Runner, nil, "install", "-m", "0640", "-o", "root", "-g", backupGroup, "/dev/null", probe); err != nil {
		return BackupTarget{}, apierr.New(apierr.InvalidParams, "In diesem Ordner lassen sich keine Sicherungen ablegen (Schreibrechte oder Dateibesitzer nicht möglich, z. B. auf manchen Netzlaufwerken).").With("detail", err.Error())
	}
	_, _ = d.Runner.Run(ctx, nil, "rm", "-f", "--", probe)
	t := BackupTarget{Dir: real, Free: freeBytes(ctx, d, real)}
	if p := curPaths.Load(); p != nil {
		t.Appdata = p.Appdata
	}
	return t, nil
}

// BackupStore übernimmt eine fertige Konfigurations-Sicherung aus dem Staging-Ordner der API in den Zielordner.
func BackupStore(ctx context.Context, d Deps, src, dir string) error {
	name := path.Base(src)
	if path.Clean(src) != src || path.Dir(src)+"/" != backupStage || backup.KindOf(name) != backup.KindConfig {
		return apierr.New(apierr.InvalidParams, "Ungültige Quelle für die Sicherung.")
	}
	backupMu.Lock()
	defer backupMu.Unlock()
	real, err := targetDir(ctx, d, dir)
	if err != nil {
		return err
	}
	part := path.Join(real, "."+name+".part")
	if _, err := run(ctx, d.Runner, nil, "install", "-m", "0640", "-o", "root", "-g", backupGroup, "--", src, part); err != nil {
		_, _ = d.Runner.Run(ctx, nil, "rm", "-f", "--", part)
		return apierr.New(apierr.Internal, "Die Sicherung konnte nicht abgelegt werden.").With("detail", err.Error())
	}
	if _, err := run(ctx, d.Runner, nil, "mv", "-f", "--", part, path.Join(real, name)); err != nil {
		_, _ = d.Runner.Run(ctx, nil, "rm", "-f", "--", part)
		return apierr.New(apierr.Internal, "Die Sicherung konnte nicht abgelegt werden.").With("detail", err.Error())
	}
	_, _ = d.Runner.Run(ctx, nil, "rm", "-f", "--", src)
	return nil
}

// BackupDelete löscht eine Sicherung.
func BackupDelete(ctx context.Context, d Deps, dir, name string) error {
	if backup.KindOf(name) == "" {
		return apierr.New(apierr.InvalidParams, "Ungültiger Backup-Name.")
	}
	backupMu.Lock()
	defer backupMu.Unlock()
	real, err := targetDir(ctx, d, dir)
	if err != nil {
		return err
	}
	file := path.Join(real, name)
	res, err := d.Runner.Run(ctx, nil, "test", "-f", file)
	if err != nil || res.ExitCode != 0 {
		return apierr.New(apierr.NotFound, "Backup nicht gefunden.")
	}
	if _, err := run(ctx, d.Runner, nil, "rm", "--", file); err != nil {
		return apierr.New(apierr.Internal, "Die Sicherung konnte nicht gelöscht werden.").With("detail", err.Error())
	}
	return nil
}

// BackupPrune behält je Art die keep neuesten Sicherungen und löscht die übrigen.
func BackupPrune(ctx context.Context, d Deps, dir string, keep int) error {
	if keep < 1 || keep > 90 {
		return apierr.New(apierr.InvalidParams, "Ungültige Anzahl der Sicherungen.")
	}
	backupMu.Lock()
	defer backupMu.Unlock()
	real, err := targetDir(ctx, d, dir)
	if err != nil {
		return err
	}
	out, err := run(ctx, d.Runner, nil, "find", real, "-maxdepth", "1", "-type", "f", "-printf", "%f\\n")
	if err != nil {
		return apierr.New(apierr.Internal, "Der Ordner der Sicherungen konnte nicht gelesen werden.").With("detail", err.Error())
	}
	for _, n := range backup.Stale(strings.Split(string(out), "\n"), keep) {
		if _, err := run(ctx, d.Runner, nil, "rm", "-f", "--", path.Join(real, n)); err != nil {
			return apierr.New(apierr.Internal, "Alte Sicherungen konnten nicht gelöscht werden.").With("detail", err.Error())
		}
	}
	return nil
}

// BackupAppdata packt den Ordner appdata (Einstellungen der Docker-Apps) in ein Archiv im Zielordner.
// tar läuft als root und behält Besitzer und Rechte; Dateien, die sich währenddessen ändern (Exit 1), sind
// kein Fehler, werden aber gemeldet. Ein Zielordner unterhalb von appdata wird ausgeschlossen.
func BackupAppdata(ctx context.Context, d Deps, dir string) (BackupResult, error) {
	p, err := requirePaths()
	if err != nil {
		return BackupResult{}, err
	}
	src := p.Appdata
	if !d.FS.Exists(src) {
		return BackupResult{}, apierr.New(apierr.NotFound, "Der Ordner appdata existiert nicht.")
	}
	backupMu.Lock()
	defer backupMu.Unlock()
	real, err := targetDir(ctx, d, dir)
	if err != nil {
		return BackupResult{}, err
	}
	var size int64
	if out, err := d.Runner.Run(ctx, nil, "du", "-sb", "--", src); err == nil && out.ExitCode == 0 {
		size, _ = strconv.ParseInt(strings.Fields(string(out.Stdout) + " 0")[0], 10, 64)
	}
	if free := freeBytes(ctx, d, real); free < size+backupSlack {
		return BackupResult{}, apierr.New(apierr.Conflict, fmt.Sprintf("Zu wenig Platz im Ordner der Sicherungen: benötigt werden höchstens etwa %d MB, frei sind %d MB.", (size+backupSlack)>>20, free>>20))
	}
	name := backup.AppdataName(time.Now())
	part := path.Join(real, "."+name+".part")
	args := []string{"--numeric-owner", "-czpf", part}
	if rest, ok := strings.CutPrefix(real, src+"/"); ok {
		args = append(args, "--exclude=./"+rest)
	}
	args = append(args, "-C", src, ".")
	cleanup := func() { _, _ = d.Runner.Run(context.WithoutCancel(ctx), nil, "rm", "-f", "--", part) }
	res, err := d.Runner.Run(ctx, nil, "tar", args...)
	if err != nil || res.ExitCode > 1 {
		cleanup()
		detail := ""
		if err != nil {
			detail = err.Error()
		} else {
			detail = strings.TrimSpace(string(res.Stderr))
		}
		return BackupResult{}, apierr.New(apierr.Internal, "Die appdata-Sicherung ist fehlgeschlagen.").With("detail", detail)
	}
	for _, step := range [][]string{{"chown", "root:" + backupGroup, "--", part}, {"chmod", "0640", "--", part}, {"mv", "-f", "--", part, path.Join(real, name)}} {
		if _, err := run(ctx, d.Runner, nil, step[0], step[1:]...); err != nil {
			cleanup()
			return BackupResult{}, apierr.New(apierr.Internal, "Die appdata-Sicherung konnte nicht abgelegt werden.").With("detail", err.Error())
		}
	}
	var out int64
	if st, err := d.Runner.Run(ctx, nil, "stat", "-c", "%s", "--", path.Join(real, name)); err == nil && st.ExitCode == 0 {
		out, _ = strconv.ParseInt(strings.TrimSpace(string(st.Stdout)), 10, 64)
	}
	return BackupResult{Name: name, Size: out, Changed: res.ExitCode == 1}, nil
}

func registerBackup(s *rpc.Server, d Deps) {
	type dirReq struct {
		Dir string `json:"dir"`
	}
	s.Handle("backup.prepare", func(ctx context.Context, p json.RawMessage) (any, error) {
		var r dirReq
		if err := decode(p, &r); err != nil {
			return nil, err
		}
		return BackupPrepare(ctx, d, r.Dir)
	})
	s.Handle("backup.store", func(ctx context.Context, p json.RawMessage) (any, error) {
		var r struct {
			Src string `json:"src"`
			Dir string `json:"dir"`
		}
		if err := decode(p, &r); err != nil {
			return nil, err
		}
		return nil, BackupStore(ctx, d, r.Src, r.Dir)
	})
	s.Handle("backup.delete", func(ctx context.Context, p json.RawMessage) (any, error) {
		var r struct {
			Dir  string `json:"dir"`
			Name string `json:"name"`
		}
		if err := decode(p, &r); err != nil {
			return nil, err
		}
		return nil, BackupDelete(ctx, d, r.Dir, r.Name)
	})
	s.Handle("backup.prune", func(ctx context.Context, p json.RawMessage) (any, error) {
		var r struct {
			Dir  string `json:"dir"`
			Keep int    `json:"keep"`
		}
		if err := decode(p, &r); err != nil {
			return nil, err
		}
		return nil, BackupPrune(ctx, d, r.Dir, r.Keep)
	})
	s.Handle("backup.appdata", func(ctx context.Context, p json.RawMessage) (any, error) {
		var r dirReq
		if err := decode(p, &r); err != nil {
			return nil, err
		}
		return BackupAppdata(ctx, d, r.Dir)
	})
}
