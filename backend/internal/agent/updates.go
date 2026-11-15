package agent

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/betternas/betternas/internal/apierr"
)

// UpdatePackage ist ein Paket, das aktualisiert oder neu installiert würde.
type UpdatePackage struct {
	Name      string `json:"name"`
	Current   string `json:"current"` // leer bei neu zu installierenden Paketen
	Candidate string `json:"candidate"`
}

// UpdateRun beschreibt den Zustand eines Update-Laufs.
type UpdateRun struct {
	State string `json:"state"` // running | succeeded | failed
	Log   string `json:"log"`
}

const updateUnitPrefix = "betternas-update-"

var (
	defaultNowUnix = func() int64 { return time.Now().Unix() }
	nowUnix        = defaultNowUnix

	updateUnitRe = regexp.MustCompile(`^betternas-update-[0-9]+$`)
	instRe       = regexp.MustCompile(`^Inst (\S+) (?:\[([^\]]+)\] )?\((\S+) `)
)

// ParseUpgradeSimulation liest die Ausgabe von "apt-get -s dist-upgrade" (Zeilen "Inst …").
func ParseUpgradeSimulation(out []byte) []UpdatePackage {
	pkgs := []UpdatePackage{}
	for _, line := range strings.Split(string(out), "\n") {
		if m := instRe.FindStringSubmatch(line); m != nil {
			pkgs = append(pkgs, UpdatePackage{Name: m[1], Current: m[2], Candidate: m[3]})
		}
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Name < pkgs[j].Name })
	return pkgs
}

// updateRunning meldet, ob gerade ein Update-Lauf aktiv ist.
func updateRunning(ctx context.Context, d Deps) (bool, error) {
	res, err := d.Runner.Run(ctx, nil, "systemctl", "list-units", "--type=service", "--state=running", "--no-legend", "--plain", updateUnitPrefix+"*")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(res.Stdout)) != "", nil
}

var errUpdateRunning = apierr.New(apierr.Conflict, "Ein Update läuft bereits.")

// CheckUpdates aktualisiert die Paketlisten und liefert die anstehenden Änderungen.
func CheckUpdates(ctx context.Context, d Deps) ([]UpdatePackage, error) {
	if running, err := updateRunning(ctx, d); err != nil {
		return nil, err
	} else if running {
		return nil, errUpdateRunning
	}
	res, err := d.Runner.Run(ctx, nil, "apt-get", "update")
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, apierr.New(apierr.Internal, "Die Paketlisten konnten nicht aktualisiert werden. Ist das NAS mit dem Internet verbunden?").
			With("stderr", strings.TrimSpace(string(res.Stderr)))
	}
	out, err := run(ctx, d.Runner, nil, "apt-get", "-s", "-o", "Debug::NoLocking=1", "dist-upgrade")
	if err != nil {
		return nil, err
	}
	return ParseUpgradeSimulation(out), nil
}

// InstallUpdates startet "apt-get dist-upgrade" in einer eigenen transienten systemd-Unit. Die Unit läuft
// weiter, wenn das Update betternas-api und betternas-agent selbst neu startet.
func InstallUpdates(ctx context.Context, d Deps) (string, error) {
	if running, err := updateRunning(ctx, d); err != nil {
		return "", err
	} else if running {
		return "", errUpdateRunning
	}
	// Beendete Läufe (RemainAfterExit) aufräumen.
	_, _ = d.Runner.Run(ctx, nil, "systemctl", "stop", updateUnitPrefix+"*")
	_, _ = d.Runner.Run(ctx, nil, "systemctl", "reset-failed", updateUnitPrefix+"*")

	unit := fmt.Sprintf("%s%d", updateUnitPrefix, nowUnix())
	_, err := run(ctx, d.Runner, nil, "systemd-run",
		"--unit="+unit, "--no-block", "--description=BetterNAS Updates",
		"--property=RemainAfterExit=yes",
		"--setenv=DEBIAN_FRONTEND=noninteractive", "--setenv=APT_LISTCHANGES_FRONTEND=none",
		"apt-get", "-y", "-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold", "dist-upgrade")
	if err != nil {
		return "", err
	}
	return unit, nil
}

// UpdateStatus liefert Zustand und Protokoll eines Update-Laufs. Nur eigene Update-Units sind erlaubt.
func UpdateStatus(ctx context.Context, d Deps, unit string) (UpdateRun, error) {
	if !updateUnitRe.MatchString(unit) {
		return UpdateRun{}, apierr.New(apierr.InvalidParams, "Ungültige Update-Kennung")
	}
	out, err := run(ctx, d.Runner, nil, "systemctl", "show", "-p", "ActiveState", "-p", "SubState", "-p", "Result", unit)
	if err != nil {
		return UpdateRun{}, err
	}
	props := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			props[k] = strings.TrimSpace(v)
		}
	}
	state := "succeeded"
	switch {
	case props["ActiveState"] == "failed" || (props["Result"] != "" && props["Result"] != "success"):
		state = "failed"
	case props["ActiveState"] == "activating" || props["SubState"] == "running" || props["SubState"] == "start":
		state = "running"
	}
	logOut, _ := d.Runner.Run(ctx, nil, "journalctl", "-u", unit+".service", "-o", "cat", "-n", "200", "--no-pager")
	return UpdateRun{State: state, Log: strings.TrimSpace(string(logOut.Stdout))}, nil
}
