package agent

import (
	"bytes"
	"context"
	"path"
	"sort"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/shares"
	"github.com/betternas/betternas/internal/storage"
)

const (
	smbConf     = "/etc/samba/smb.conf"
	exportsDir  = "/etc/exports.d"
	exportsFile = exportsDir + "/betternas.exports"
)

func isManaged(b []byte) bool { return bytes.HasPrefix(b, []byte(storage.ManagedHeader+"\n")) }

// ApplyShares schreibt die Samba-Konfiguration der Freigaben, prüft sie mit testparm und lädt Samba neu.
// Besteht testparm nicht, werden alle Dateien zurückgesetzt und Samba bleibt unberührt.
func ApplyShares(ctx context.Context, d Deps, st shares.State) (ApplyResult, error) {
	files, err := shares.Render(st)
	if err != nil {
		return ApplyResult{}, err
	}
	if b, err := d.FS.ReadFile(smbConf); err != nil || !isManaged(b) {
		return ApplyResult{}, apierr.New(apierr.Conflict, "Die Samba-Konfiguration wird nicht von BetterNAS verwaltet und wird nicht verändert.")
	}

	// NFS: Voraussetzungen prüfen, bevor irgendetwas geändert wird.
	exports := shares.RenderExports(st)
	hasNFS := false
	nfsPools := map[string]bool{}
	for _, s := range st.Shares {
		if s.NFS.Enabled {
			hasNFS = true
			nfsPools[s.Pool] = true
		}
	}
	oldExports, haveExports := d.FS.ReadFile(exportsFile)
	haveOld := haveExports == nil
	if haveOld && !isManaged(oldExports) {
		if hasNFS {
			return ApplyResult{}, apierr.New(apierr.Conflict, "Die Datei "+exportsFile+" existiert und wird nicht von BetterNAS verwaltet.")
		}
		haveOld = false // fremde Datei: nie anfassen
		oldExports = nil
	}
	pools := make([]string, 0, len(nfsPools))
	for p := range nfsPools {
		pools = append(pools, p)
	}
	sort.Strings(pools)
	for _, p := range pools {
		res, err := d.Runner.Run(ctx, nil, "findmnt", "-n", "-o", "OPTIONS", "--mountpoint", storage.PoolPath(p))
		if err != nil {
			return ApplyResult{}, err
		}
		if res.ExitCode != 0 {
			return ApplyResult{}, apierr.New(apierr.InvalidParams, "Der Pool "+p+" ist nicht eingehängt.").With("pool", p)
		}
		if !strings.Contains(string(res.Stdout), "noforget") {
			return ApplyResult{}, apierr.New(apierr.Conflict, "Der Pool "+p+" muss für NFS neu eingehängt werden (Neustart des NAS), damit die NFS-Optionen wirksam werden.").With("pool", p)
		}
	}
	needExports := (hasNFS || haveOld) && (!haveOld || !bytes.Equal(oldExports, exports))

	// Pools müssen eingehängt sein, sonst landen Freigabeordner auf der Systemplatte.
	for _, s := range st.Shares {
		res, err := d.Runner.Run(ctx, nil, "findmnt", "-t", "fuse.mergerfs", "--mountpoint", storage.PoolPath(s.Pool))
		if err != nil {
			return ApplyResult{}, err
		}
		if res.ExitCode != 0 {
			return ApplyResult{}, apierr.New(apierr.InvalidParams, "Der Pool "+s.Pool+" ist nicht eingehängt.").With("pool", s.Pool)
		}
		if !d.FS.Exists(s.Path()) {
			if _, err := run(ctx, d.Runner, nil, "install", "-d", "-m", "2775", "-o", "root", "-g", "users", s.Path()); err != nil {
				return ApplyResult{}, err
			}
		}
	}

	existing := map[string][]byte{}
	paths, err := d.FS.Glob(path.Join(shares.ConfDir, "*.conf"))
	if err != nil {
		return ApplyResult{}, err
	}
	for _, p := range paths {
		b, err := d.FS.ReadFile(p)
		if err != nil {
			return ApplyResult{}, err
		}
		if isManaged(b) {
			existing[path.Base(p)] = b
		}
	}
	var changed, stale []string
	for name, content := range files {
		if old, ok := existing[name]; !ok || !bytes.Equal(old, content) {
			changed = append(changed, name)
		}
	}
	for name := range existing {
		if _, ok := files[name]; !ok {
			stale = append(stale, name)
		}
	}
	sort.Strings(changed)
	if len(changed)+len(stale) == 0 && !needExports {
		return ApplyResult{Changed: []string{}}, nil
	}

	rollback := func() {
		for _, name := range changed {
			if old, ok := existing[name]; ok {
				_ = d.FS.WriteFile(path.Join(shares.ConfDir, name), old, 0o644)
			} else {
				_ = d.FS.Remove(path.Join(shares.ConfDir, name))
			}
		}
		for _, name := range stale {
			_ = d.FS.WriteFile(path.Join(shares.ConfDir, name), existing[name], 0o644)
		}
	}
	for _, name := range changed {
		if err := d.FS.WriteFile(path.Join(shares.ConfDir, name), files[name], 0o644); err != nil {
			rollback()
			return ApplyResult{}, err
		}
	}
	for _, name := range stale {
		if err := d.FS.Remove(path.Join(shares.ConfDir, name)); err != nil {
			rollback()
			return ApplyResult{}, err
		}
	}

	res, err := d.Runner.Run(ctx, nil, "testparm", "-s")
	if err != nil {
		rollback()
		return ApplyResult{}, err
	}
	if res.ExitCode != 0 {
		rollback()
		return ApplyResult{}, apierr.New(apierr.InvalidParams, "Samba lehnt die Konfiguration ab; es wurde nichts geändert.").
			With("testparm", strings.TrimSpace(string(res.Stderr)))
	}

	if needExports {
		restoreExports := func() {
			if haveOld {
				_ = d.FS.WriteFile(exportsFile, oldExports, 0o644)
			} else {
				_ = d.FS.Remove(exportsFile)
			}
		}
		if err := d.FS.MkdirAll(exportsDir, 0o755); err != nil {
			rollback()
			return ApplyResult{}, err
		}
		if err := d.FS.WriteFile(exportsFile, exports, 0o644); err != nil {
			restoreExports()
			rollback()
			return ApplyResult{}, err
		}
		res, err := d.Runner.Run(ctx, nil, "exportfs", "-ra")
		if err != nil || res.ExitCode != 0 {
			restoreExports()
			rollback()
			if err != nil {
				return ApplyResult{}, err
			}
			return ApplyResult{}, apierr.New(apierr.InvalidParams, "Der NFS-Export wurde abgelehnt; es wurde nichts geändert.").
				With("exportfs", strings.TrimSpace(string(res.Stderr)))
		}
		changed = append(changed, path.Base(exportsFile))
	}

	if active, err := d.Runner.Run(ctx, nil, "systemctl", "is-active", "smbd"); err == nil && active.ExitCode == 0 {
		if _, err := run(ctx, d.Runner, nil, "smbcontrol", "smbd", "reload-config"); err != nil {
			return ApplyResult{}, err
		}
	}
	return ApplyResult{Changed: changed}, nil
}
