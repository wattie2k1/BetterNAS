package agent

import (
	"bytes"
	"context"
	"log/slog"
	"path"
	"sort"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/storage"
)

const unitDir = "/etc/systemd/system"

// ApplyResult nennt die neu angelegten oder geänderten Units.
type ApplyResult struct {
	Changed []string `json:"changed"`
}

func isPoolUnit(name string) bool { return strings.HasPrefix(name, "srv-pools-") }

// orderUnits sortiert Units: Datenplatten vor Pools (bzw. umgekehrt mit poolsFirst).
func orderUnits(names []string, poolsFirst bool) []string {
	out := append([]string(nil), names...)
	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := isPoolUnit(out[i]), isPoolUnit(out[j])
		if pi != pj {
			return pi == poolsFirst
		}
		return out[i] < out[j]
	})
	return out
}

func whereOf(content []byte) string {
	for _, line := range strings.Split(string(content), "\n") {
		if v, ok := strings.CutPrefix(line, "Where="); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// managedUnits liefert die vorhandenen, von BetterNAS verwalteten Mount-Units.
func managedUnits(d Deps) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, pattern := range []string{"srv-disks-*.mount", "srv-parity-*.mount", "srv-pools-*.mount"} {
		files, err := d.FS.Glob(path.Join(unitDir, pattern))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			b, err := d.FS.ReadFile(f)
			if err != nil {
				return nil, err
			}
			if bytes.HasPrefix(b, []byte(storage.ManagedHeader+"\n")) {
				out[path.Base(f)] = b
			}
		}
	}
	return out, nil
}

// stopUnit stoppt eine Mount-Unit und meldet busy, wenn der Mountpoint danach noch eingehängt ist.
func stopUnit(ctx context.Context, d Deps, unit, where string) error {
	res, err := d.Runner.Run(ctx, nil, "systemctl", "stop", unit)
	if err != nil {
		return err
	}
	still, err := d.Runner.Run(ctx, nil, "findmnt", "--mountpoint", where)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 || still.ExitCode == 0 {
		if isPoolUnit(unit) {
			return apierr.New(apierr.PoolBusy, "Der Pool wird gerade verwendet. Bitte offene Dateien schließen und erneut versuchen.").With("unit", unit)
		}
		return apierr.New(apierr.DiskInUse, "Der Datenträger wird gerade verwendet.").With("unit", unit)
	}
	return nil
}

// Apply bringt die Mount-Units auf den Soll-Zustand. Unveränderte Units bleiben unberührt.
// Lässt sich eine geänderte Unit nicht stoppen (belegt), werden alle Dateien zurückgesetzt.
func Apply(ctx context.Context, d Deps, st storage.State) (ApplyResult, error) {
	desired, err := storage.RenderUnits(st)
	if err != nil {
		return ApplyResult{}, err
	}
	existing, err := managedUnits(d)
	if err != nil {
		return ApplyResult{}, err
	}
	var stale, added, modified []string
	for name := range existing {
		if _, ok := desired[name]; !ok {
			stale = append(stale, name)
		}
	}
	for name, content := range desired {
		old, ok := existing[name]
		switch {
		case !ok:
			added = append(added, name)
		case !bytes.Equal(old, content):
			modified = append(modified, name)
		}
	}
	res := ApplyResult{Changed: orderUnits(append(append([]string{}, added...), modified...), false)}
	if len(stale)+len(res.Changed) == 0 {
		syncScrubTimers(ctx, d, st.Pools, nil, st.ScrubSchedule)
		return ApplyResult{Changed: []string{}}, nil
	}
	// Prüf-Timer entfernter btrfs-Pools abschalten (die Unit selbst wird unten gestoppt und gelöscht).
	var staleBtrfs []string
	for _, name := range stale {
		if isPoolUnit(name) && bytes.Contains(existing[name], []byte("\nType=btrfs\n")) {
			staleBtrfs = append(staleBtrfs, strings.TrimSuffix(strings.TrimPrefix(name, "srv-pools-"), ".mount"))
		}
	}

	syncScrubTimers(ctx, d, nil, staleBtrfs, st.ScrubSchedule)

	// 1. Veraltete Units entfernen – Pools vor Datenplatten.
	for _, name := range orderUnits(stale, true) {
		if err := stopUnit(ctx, d, name, whereOf(existing[name])); err != nil {
			return ApplyResult{}, err
		}
		if _, err := run(ctx, d.Runner, nil, "systemctl", "disable", name); err != nil {
			return ApplyResult{}, err
		}
		if err := d.FS.Remove(path.Join(unitDir, name)); err != nil {
			return ApplyResult{}, err
		}
	}

	// 2. Neue und geänderte Dateien schreiben, Mountpoints anlegen.
	for _, name := range res.Changed {
		if err := d.FS.WriteFile(path.Join(unitDir, name), desired[name], 0o644); err != nil {
			return ApplyResult{}, err
		}
		where := whereOf(desired[name])
		if err := d.FS.MkdirAll(where, 0o755); err != nil {
			return ApplyResult{}, err
		}
		protectMountpoint(ctx, d, where)
	}
	if _, err := run(ctx, d.Runner, nil, "systemctl", "daemon-reload"); err != nil {
		return ApplyResult{}, err
	}

	// 3. Geänderte Units stoppen; ist eine belegt, alles zurücksetzen.
	var stopped []string
	for _, name := range orderUnits(modified, false) {
		if err := stopUnit(ctx, d, name, whereOf(existing[name])); err != nil {
			rollback(ctx, d, existing, added, modified, append(stopped, name))
			return ApplyResult{}, err
		}
		stopped = append(stopped, name)
	}

	// 4. Neue und geänderte Units aktivieren und starten – Datenplatten vor Pools.
	for _, name := range res.Changed {
		if _, err := run(ctx, d.Runner, nil, "systemctl", "enable", "--now", name); err != nil {
			return ApplyResult{}, err
		}
	}
	syncScrubTimers(ctx, d, st.Pools, nil, st.ScrubSchedule)
	return res, nil
}

// protectMountpoint macht einen leeren, nicht eingehängten Einhängeordner unveränderlich (chattr +i).
// Fehlt eine Platte oder ein Pool später, landen Dateien (auch über Freigaben) nicht unbemerkt auf der Systemplatte.
// Ist die Platte eingehängt, liegt ihr Dateisystem darüber und bleibt normal beschreibbar.
func protectMountpoint(ctx context.Context, d Deps, where string) {
	res, err := d.Runner.Run(ctx, nil, "findmnt", "--mountpoint", where)
	if err != nil || res.ExitCode == 0 {
		return // eingehängt (oder unbekannt): nicht anfassen
	}
	if r, err := d.Runner.Run(ctx, nil, "chattr", "+i", where); err != nil || r.ExitCode != 0 {
		slog.Warn("einhängeordner konnte nicht geschützt werden", "dir", where, "stderr", strings.TrimSpace(string(r.Stderr)))
	}
}

func rollback(ctx context.Context, d Deps, existing map[string][]byte, added, modified, toStart []string) {
	for _, name := range modified {
		_ = d.FS.WriteFile(path.Join(unitDir, name), existing[name], 0o644)
	}
	for _, name := range added {
		_ = d.FS.Remove(path.Join(unitDir, name))
	}
	_, _ = d.Runner.Run(ctx, nil, "systemctl", "daemon-reload")
	for _, name := range orderUnits(toStart, false) {
		_, _ = d.Runner.Run(ctx, nil, "systemctl", "start", name)
	}
}
