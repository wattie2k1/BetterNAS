package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/snapraid"
	"github.com/betternas/betternas/internal/sysexec"
)

// lockDir ist das Verzeichnis der Lauf-Sperren (in Tests überschrieben).
var lockDir = snapraid.LockDir

var snapraidActions = map[string]bool{"sync": true, "scrub": true, "diff": true}

const maxLogBytes = 64 << 10

func checkPoolAction(pool, action string) error {
	if !snapraid.PoolNameRe.MatchString(pool) {
		return apierr.New(apierr.InvalidParams, "Ungültiger Poolname")
	}
	if !snapraidActions[action] {
		return apierr.New(apierr.InvalidParams, "Unbekannte SnapRAID-Aktion (erlaubt: sync, scrub, diff)")
	}
	return nil
}

// lockPool nimmt die Lauf-Sperre eines Pools (flock); sie gilt, bis die Datei geschlossen wird.
func lockPool(pool string) (*os.File, error) {
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path.Join(lockDir, "snapraid-"+pool+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if err == unix.EWOULDBLOCK {
			return nil, apierr.New(apierr.Conflict, "Für diesen Pool läuft bereits ein SnapRAID-Vorgang.")
		}
		return nil, err
	}
	return f, nil
}

// ApplySnapraid schreibt Konfiguration und Timer aller Pools und aktiviert die Timer.
// Fremde Dateien gleichen Namens werden nie überschrieben; Platten müssen eingehängt sein.
func ApplySnapraid(ctx context.Context, d Deps, st snapraid.State) (ApplyResult, error) {
	files, err := snapraid.Render(st)
	if err != nil {
		return ApplyResult{}, err
	}
	for _, p := range st.Pools {
		for _, mp := range append(diskPaths(p.Data, snapraid.DiskPath), diskPaths(p.Parity, snapraid.ParityPath)...) {
			res, err := d.Runner.Run(ctx, nil, "findmnt", "--mountpoint", mp)
			if err != nil {
				return ApplyResult{}, err
			}
			if res.ExitCode != 0 {
				return ApplyResult{}, apierr.New(apierr.InvalidParams, "Der Datenträger "+path.Base(mp)+" ist nicht eingehängt.").With("pool", p.Pool)
			}
		}
	}

	existing := map[string][]byte{}
	for _, pattern := range []string{
		path.Join(snapraid.ConfDir, "*.conf"), path.Join(snapraid.ConfDir, "*.settings"),
		path.Join(snapraid.UnitDir, "betternas-snapraid-*.timer"),
	} {
		paths, err := d.FS.Glob(pattern)
		if err != nil {
			return ApplyResult{}, err
		}
		for _, p := range paths {
			b, err := d.FS.ReadFile(p)
			if err != nil {
				return ApplyResult{}, err
			}
			if isManaged(b) {
				existing[p] = b
			}
		}
	}
	for p := range files {
		if _, managed := existing[p]; !managed && d.FS.Exists(p) {
			return ApplyResult{}, apierr.New(apierr.Conflict, "Die Datei "+p+" existiert und wird nicht von BetterNAS verwaltet.")
		}
	}
	var changed, modified, stale []string
	for p, content := range files {
		old, ok := existing[p]
		switch {
		case !ok:
			changed = append(changed, p)
		case !bytes.Equal(old, content):
			changed = append(changed, p)
			modified = append(modified, p)
		}
	}
	for p := range existing {
		if _, ok := files[p]; !ok {
			stale = append(stale, p)
		}
	}
	sort.Strings(changed)
	sort.Strings(modified)
	sort.Strings(stale)
	if len(changed)+len(stale) == 0 {
		return ApplyResult{Changed: []string{}}, nil
	}

	for _, dir := range []string{snapraid.ConfDir, snapraid.StateDir} {
		if err := d.FS.MkdirAll(dir, 0o755); err != nil {
			return ApplyResult{}, err
		}
	}
	for _, p := range stale {
		if strings.HasSuffix(p, ".timer") {
			if _, err := run(ctx, d.Runner, nil, "systemctl", "disable", "--now", path.Base(p)); err != nil {
				return ApplyResult{}, err
			}
		}
	}
	rollback := func() {
		for _, p := range changed {
			if old, ok := existing[p]; ok {
				_ = d.FS.WriteFile(p, old, 0o644)
			} else {
				_ = d.FS.Remove(p)
			}
		}
	}
	for _, p := range changed {
		if err := d.FS.WriteFile(p, files[p], 0o644); err != nil {
			rollback()
			return ApplyResult{}, err
		}
	}
	for _, p := range stale {
		if err := d.FS.Remove(p); err != nil {
			return ApplyResult{}, err
		}
	}
	if _, err := run(ctx, d.Runner, nil, "systemctl", "daemon-reload"); err != nil {
		return ApplyResult{}, err
	}
	for _, p := range changed {
		if !strings.HasSuffix(p, ".timer") {
			continue
		}
		if _, err := run(ctx, d.Runner, nil, "systemctl", "enable", "--now", path.Base(p)); err != nil {
			return ApplyResult{}, err
		}
	}
	for _, p := range modified {
		if strings.HasSuffix(p, ".timer") {
			// geänderter Zeitplan: Timer neu starten, damit er ihn übernimmt
			if _, err := run(ctx, d.Runner, nil, "systemctl", "restart", path.Base(p)); err != nil {
				return ApplyResult{}, err
			}
		}
	}
	names := make([]string, 0, len(changed))
	for _, p := range changed {
		names = append(names, path.Base(p))
	}
	return ApplyResult{Changed: names}, nil
}

func diskPaths(ids []string, f func(string) string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = f(id)
	}
	return out
}

// StartSnapraid startet einen Lauf in einer eigenen transienten Unit und liefert deren Namen.
func StartSnapraid(ctx context.Context, d Deps, pool, action string, force bool) (string, error) {
	if err := checkPoolAction(pool, action); err != nil {
		return "", err
	}
	if !d.FS.Exists(snapraid.ConfPath(pool)) {
		return "", apierr.New(apierr.NotFound, "SnapRAID ist für diesen Pool nicht eingerichtet.")
	}
	l, err := lockPool(pool)
	if err != nil {
		return "", err
	}
	l.Close() // der Lauf selbst nimmt die Sperre
	unit := "betternas-snapraid-" + pool
	args := []string{"--unit=" + unit, "--collect", "/usr/sbin/betternas-agent", "snapraid-run", action, pool}
	if force {
		args = append(args, "--force")
	}
	if _, err := run(ctx, d.Runner, nil, "systemd-run", args...); err != nil {
		return "", err
	}
	return unit, nil
}

type spStateFile struct {
	LastSync  *snapraid.RunState `json:"lastSync,omitempty"`
	LastScrub *snapraid.RunState `json:"lastScrub,omitempty"`
	Diff      *snapraid.Diff     `json:"diff,omitempty"`
	DiffAt    *time.Time         `json:"diffAt,omitempty"`
}

func readSPState(d Deps, pool string) spStateFile {
	var s spStateFile
	if b, err := d.FS.ReadFile(snapraid.StatePath(pool)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func writeSPState(d Deps, pool string, s spStateFile) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := d.FS.MkdirAll(snapraid.StateDir, 0o755); err != nil {
		return err
	}
	return d.FS.WriteFile(snapraid.StatePath(pool), b, 0o644)
}

// SnapraidStatus liefert den gespeicherten Zustand und ob gerade ein Lauf aktiv ist.
func SnapraidStatus(_ context.Context, d Deps, pool string) (snapraid.Status, error) {
	if !snapraid.PoolNameRe.MatchString(pool) {
		return snapraid.Status{}, apierr.New(apierr.InvalidParams, "Ungültiger Poolname")
	}
	s := readSPState(d, pool)
	st := snapraid.Status{Enabled: d.FS.Exists(snapraid.ConfPath(pool)), LastSync: s.LastSync, LastScrub: s.LastScrub, Diff: s.Diff, DiffAt: s.DiffAt}
	if l, err := lockPool(pool); err != nil {
		st.Running = apierr.From(err).Code == apierr.Conflict
	} else {
		l.Close()
	}
	if b, err := d.FS.ReadFile(snapraid.LogPath(pool)); err == nil {
		lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		st.Log = strings.Join(lines[max(0, len(lines)-40):], "\n")
	}
	return st, nil
}

// confMountpoints liest die Einhängepunkte (Daten und Parität) aus der erzeugten snapraid.conf.
func confMountpoints(conf []byte) []string {
	var out []string
	for _, line := range strings.Split(string(conf), "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 3 && f[0] == "data":
			out = append(out, strings.TrimRight(f[2], "/"))
		case len(f) == 2 && (f[0] == "parity" || strings.HasSuffix(f[0], "-parity")):
			out = append(out, path.Dir(f[1]))
		}
	}
	return out
}

// RunSnapraid führt sync, scrub oder diff aus, hält das Ergebnis fest und bricht bei Gefahr ab.
func RunSnapraid(ctx context.Context, d Deps, pool, action string, force bool) error {
	if err := checkPoolAction(pool, action); err != nil {
		return err
	}
	conf, err := d.FS.ReadFile(snapraid.ConfPath(pool))
	if err != nil {
		return apierr.New(apierr.NotFound, "SnapRAID ist für diesen Pool nicht eingerichtet.")
	}
	lock, err := lockPool(pool)
	if err != nil {
		return err
	}
	defer lock.Close()

	var settings snapraid.Settings
	if b, err := d.FS.ReadFile(snapraid.SettingsPath(pool)); err == nil {
		settings, _ = snapraid.ParseSettings(b)
	}
	settings = settings.WithDefaults()

	state := readSPState(d, pool)
	var logBuf bytes.Buffer
	fmt.Fprintf(&logBuf, "== %s %s (%s) ==\n", action, time.Now().Format(time.RFC3339), pool)
	finish := func(rs snapraid.RunState, err error) error {
		rs.Action, rs.At = action, time.Now().UTC()
		switch action {
		case "sync":
			state.LastSync = &rs
		case "scrub":
			state.LastScrub = &rs
		}
		_ = writeSPState(d, pool, state)
		out := logBuf.Bytes()
		if len(out) > maxLogBytes {
			out = out[len(out)-maxLogBytes:]
		}
		_ = d.FS.WriteFile(snapraid.LogPath(pool), out, 0o644)
		return err
	}
	block := func(msg string, removed int) error {
		fmt.Fprintln(&logBuf, msg)
		return finish(snapraid.RunState{Result: snapraid.ResultBlocked, Message: msg, Removed: removed}, apierr.New(apierr.Conflict, msg))
	}

	// Nie mit fehlender Platte laufen.
	for _, mp := range confMountpoints(conf) {
		res, err := d.Runner.Run(ctx, nil, "findmnt", "--mountpoint", mp)
		if err != nil {
			return finish(snapraid.RunState{Result: snapraid.ResultFailed, Message: err.Error()}, err)
		}
		if res.ExitCode != 0 {
			return block("Datenträger "+path.Base(mp)+" ist nicht eingehängt – es wird nichts ausgeführt.", 0)
		}
	}

	sp := func(args ...string) (sysexec.Result, error) {
		res, err := d.Runner.Run(ctx, nil, "snapraid", append([]string{"-c", snapraid.ConfPath(pool)}, args...)...)
		logBuf.Write(res.Stdout)
		logBuf.Write(res.Stderr)
		return res, err
	}
	fail := func(res sysexec.Result, err error, what string) error {
		msg := what + " ist fehlgeschlagen"
		if err != nil {
			msg += ": " + err.Error()
		} else if s := strings.TrimSpace(string(res.Stderr)); s != "" {
			msg += ": " + s
		}
		return finish(snapraid.RunState{Result: snapraid.ResultFailed, Message: msg}, apierr.New(apierr.Internal, msg))
	}

	if action == "sync" || action == "diff" {
		res, err := sp("diff")
		if err != nil || (res.ExitCode != 0 && res.ExitCode != 2) {
			return fail(res, err, "Der Vergleich")
		}
		diff := snapraid.ParseDiff(res.Stdout)
		now := time.Now().UTC()
		state.Diff, state.DiffAt = &diff, &now
		if action == "diff" {
			return finish(snapraid.RunState{}, nil)
		}
		if diff.Removed > settings.DeleteThreshold && !force {
			return block("Sync gestoppt: "+strconv.Itoa(diff.Removed)+" Dateien gelöscht (Schwelle "+strconv.Itoa(settings.DeleteThreshold)+").", diff.Removed)
		}
		if res, err := sp("sync"); err != nil || res.ExitCode != 0 {
			return fail(res, err, "Die Synchronisierung")
		}
		protected := snapraid.Diff{Equal: diff.Equal + diff.Added + diff.Updated + diff.Moved + diff.Copied}
		state.Diff = &protected
		return finish(snapraid.RunState{Result: snapraid.ResultOK}, nil)
	}

	// scrub
	res, err := sp("-p", strconv.Itoa(settings.ScrubPercent), "-o", strconv.Itoa(settings.ScrubOlderDays), "scrub")
	switch {
	case err != nil:
		return fail(res, err, "Die Prüfung")
	case res.ExitCode != 0:
		msg := "Die Prüfung hat Fehler gefunden."
		return finish(snapraid.RunState{Result: snapraid.ResultErrors, Message: msg}, apierr.New(apierr.Internal, msg))
	}
	return finish(snapraid.RunState{Result: snapraid.ResultOK}, nil)
}

func parseSnapraidArgs(args []string) (action, pool string, force bool, err error) {
	if len(args) < 2 || len(args) > 3 {
		return "", "", false, fmt.Errorf("Aufruf: betternas-agent snapraid-run <sync|scrub|diff> <pool> [--force]")
	}
	action, pool = args[0], args[1]
	if len(args) == 3 {
		if args[2] != "--force" {
			return "", "", false, fmt.Errorf("unbekannte Option: %s", args[2])
		}
		force = true
	}
	if err := checkPoolAction(pool, action); err != nil {
		return "", "", false, err
	}
	return action, pool, force, nil
}

// SnapraidCLI ist der Kommandozeilenmodus `betternas-agent snapraid-run …` (von Timern und der GUI gestartet).
// Rückgabe ist der Exit-Code.
func SnapraidCLI(ctx context.Context, d Deps, args []string, stderr *os.File) int {
	action, pool, force, err := parseSnapraidArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := RunSnapraid(ctx, d, pool, action, force); err != nil {
		fmt.Fprintln(stderr, apierr.From(err).Message)
		return 1
	}
	return 0
}
