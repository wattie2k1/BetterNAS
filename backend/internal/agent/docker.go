package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/docker"
	"github.com/betternas/betternas/internal/rpc"
)

// StackFiles und StackCheck leben im Paket docker (gemeinsam mit der API).
type (
	StackFiles = docker.StackFiles
	StackCheck = docker.StackCheck
)

// StackSave ist die Eingabe von stack.save. Create verlangt, dass der Stack noch nicht existiert.
type StackSave struct {
	StackFiles
	Create bool `json:"create"`
}

func checkStackName(name string) error {
	if !docker.StackNameRe.MatchString(name) {
		return apierr.New(apierr.InvalidParams, "Ungültiger Stackname (erlaubt: Kleinbuchstaben, Ziffern, - und _, bis 40 Zeichen)")
	}
	return nil
}

func composePath(name string) string { return path.Join(docker.StackDir(name), docker.ComposeFile) }
func envPath(name string) string     { return path.Join(docker.StackDir(name), docker.EnvFile) }

// composeArgs baut `docker compose -p <name> --project-directory <dir> <args…>`.
func composeArgs(name string, args ...string) []string {
	return append([]string{"compose", "-p", name, "--project-directory", docker.StackDir(name), "-f", composePath(name)}, args...)
}

// ListStacks liefert alle Stacks (Verzeichnisse mit compose.yaml) samt Laufzustand.
func ListStacks(ctx context.Context, d Deps) ([]docker.StackInfo, error) {
	files, err := d.FS.Glob(path.Join(docker.StacksDir, "*", docker.ComposeFile))
	if err != nil {
		return nil, err
	}
	state := map[string]docker.StackInfo{}
	if res, err := d.Runner.Run(ctx, nil, "docker", "compose", "ls", "-a", "--format", "json"); err == nil && res.ExitCode == 0 {
		if m, err := docker.ParseComposeLs(res.Stdout); err == nil {
			state = m
		}
	}
	list := []docker.StackInfo{}
	for _, f := range files {
		name := path.Base(path.Dir(f))
		if !docker.StackNameRe.MatchString(name) {
			continue
		}
		si := state[name]
		si.Name = name
		if rb, ok := readRollback(d, name, rollbackFile); ok {
			si.Rollback = rb.CreatedAt
			_, si.RollbackData = readBackup(d, name, rb.Backup)
		}
		list = append(list, si)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

// GetStack liest compose.yaml und .env eines Stacks.
func GetStack(_ context.Context, d Deps, name string) (StackFiles, error) {
	if err := checkStackName(name); err != nil {
		return StackFiles{}, err
	}
	b, err := d.FS.ReadFile(composePath(name))
	if errors.Is(err, os.ErrNotExist) {
		return StackFiles{}, apierr.New(apierr.NotFound, "Stack nicht gefunden")
	}
	if err != nil {
		return StackFiles{}, err
	}
	sf := StackFiles{Name: name, Compose: string(b)}
	if e, err := d.FS.ReadFile(envPath(name)); err == nil {
		sf.Env = string(e)
	}
	return sf, nil
}

// SaveStack schreibt compose.yaml (0600) und .env (0600) im Stack-Verzeichnis (0700).
// Ohne Validierung: Entwürfe dürfen gespeichert werden; deployt wird nur, was `compose config` besteht.
func SaveStack(_ context.Context, d Deps, in StackSave) error {
	if err := checkStackName(in.Name); err != nil {
		return err
	}
	if len(in.Compose) > docker.MaxComposeBytes || len(in.Env) > docker.MaxEnvBytes {
		return apierr.New(apierr.InvalidParams, "Die Datei ist zu groß")
	}
	if strings.TrimSpace(in.Compose) == "" {
		return apierr.New(apierr.InvalidParams, "Die compose.yaml darf nicht leer sein")
	}
	if in.Create && d.FS.Exists(composePath(in.Name)) {
		return apierr.New(apierr.Conflict, "Ein Stack mit diesem Namen existiert bereits")
	}
	if err := d.FS.MkdirAll(docker.StackDir(in.Name), 0o700); err != nil {
		return err
	}
	if err := d.FS.WriteFile(composePath(in.Name), []byte(in.Compose), 0o600); err != nil {
		return err
	}
	if in.Env == "" {
		if d.FS.Exists(envPath(in.Name)) {
			return d.FS.Remove(envPath(in.Name))
		}
		return nil
	}
	return d.FS.WriteFile(envPath(in.Name), []byte(in.Env), 0o600)
}

// ValidateStack prüft den Stack mit `docker compose config -q`.
func ValidateStack(ctx context.Context, d Deps, name string) (StackCheck, error) {
	if err := checkStackName(name); err != nil {
		return StackCheck{}, err
	}
	if !d.FS.Exists(composePath(name)) {
		return StackCheck{}, apierr.New(apierr.NotFound, "Stack nicht gefunden")
	}
	res, err := d.Runner.Run(ctx, nil, "docker", append(composeArgs(name), "config", "-q")...)
	if err != nil {
		return StackCheck{}, apierr.New(apierr.Internal, "Docker ist nicht verfügbar")
	}
	if res.ExitCode != 0 {
		return StackCheck{Message: strings.TrimSpace(string(res.Stderr))}, nil
	}
	return StackCheck{OK: true}, nil
}

// DeleteStack fährt den Stack herunter (Volumes bleiben) und löscht dessen Verzeichnis.
func DeleteStack(ctx context.Context, d Deps, name string) error {
	if err := checkStackName(name); err != nil {
		return err
	}
	if !d.FS.Exists(composePath(name)) {
		return apierr.New(apierr.NotFound, "Stack nicht gefunden")
	}
	if running, err := stackRunning(ctx, d, name); err != nil {
		return err
	} else if running {
		return apierr.New(apierr.Conflict, "Für diesen Stack läuft gerade ein Vorgang.")
	}
	if res, err := d.Runner.Run(ctx, nil, "docker", composeArgs(name, "down", "--remove-orphans")...); err != nil {
		return apierr.New(apierr.Internal, "Docker ist nicht verfügbar")
	} else if res.ExitCode != 0 {
		return apierr.New(apierr.Internal, "Der Stack konnte nicht gestoppt werden").With("stderr", strings.TrimSpace(string(res.Stderr)))
	}
	_ = StackRollbackClear(ctx, d, name)
	dir := docker.StackDir(name)
	rest, err := d.FS.Glob(path.Join(dir, "*"))
	if err != nil {
		return err
	}
	for _, f := range append(rest, envPath(name)) {
		if d.FS.Exists(f) {
			if err := d.FS.Remove(f); err != nil {
				return err
			}
		}
	}
	return d.FS.Remove(dir)
}

// --- Läufe ---

var errStackRunning = apierr.New(apierr.Conflict, "Für diesen Stack läuft bereits ein Vorgang.")

type runUnit struct {
	Name    string
	Running bool
}

// listRunUnits liefert alle Stack-Lauf-Units (auch beendete, wegen RemainAfterExit).
func listRunUnits(ctx context.Context, d Deps) ([]runUnit, error) {
	res, err := d.Runner.Run(ctx, nil, "systemctl", "list-units", "--type=service", "--all", "--no-legend", "--plain", docker.RunUnitPrefix+"*")
	if err != nil {
		return nil, err
	}
	var units []runUnit
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		name := strings.TrimSuffix(f[0], ".service")
		if !docker.RunUnitRe.MatchString(name) {
			continue
		}
		units = append(units, runUnit{Name: name, Running: f[2] == "activating" || (f[2] == "active" && f[3] == "running")})
	}
	return units, nil
}

func stackUnitRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(docker.RunUnitPrefix+name) + `-[0-9]+$`)
}

func stackRunning(ctx context.Context, d Deps, name string) (bool, error) {
	units, err := listRunUnits(ctx, d)
	if err != nil {
		return false, err
	}
	re := stackUnitRe(name)
	for _, u := range units {
		if u.Running && re.MatchString(u.Name) {
			return true, nil
		}
	}
	return false, nil
}

// RunStack startet eine Stack-Aktion in einer transienten systemd-Unit und liefert deren Namen.
// up/update laufen nur mit gültiger Compose-Datei.
func RunStack(ctx context.Context, d Deps, name, action string) (string, error) {
	return RunStackOpts(ctx, d, name, action, docker.RunOptions{})
}

// RunStackOpts wie RunStack, mit Zusatzwünschen (Datensicherung vor dem Update, Daten beim Rollback zurückspielen).
func RunStackOpts(ctx context.Context, d Deps, name, action string, opts docker.RunOptions) (string, error) {
	if err := checkStackName(name); err != nil {
		return "", err
	}
	if !docker.StackActions[action] {
		return "", apierr.New(apierr.InvalidParams, "Unbekannte Stack-Aktion")
	}
	if (opts.Backup && action != "update") || (opts.RestoreData && action != "rollback") {
		return "", apierr.New(apierr.InvalidParams, "Diese Option passt nicht zur Aktion.")
	}
	if !d.FS.Exists(composePath(name)) {
		return "", apierr.New(apierr.NotFound, "Stack nicht gefunden")
	}
	if running, err := stackRunning(ctx, d, name); err != nil {
		return "", err
	} else if running {
		return "", errStackRunning
	}
	var rollbackCmd string
	if action == "rollback" {
		var err error
		if rollbackCmd, err = rollbackCommand(d, name, opts.RestoreData); err != nil {
			return "", err
		}
	}
	if action == "up" || action == "update" || action == "rollback" {
		chk, err := ValidateStack(ctx, d, name)
		if err != nil {
			return "", err
		}
		if !chk.OK {
			return "", apierr.New(apierr.InvalidParams, "Die Compose-Datei ist ungültig: "+chk.Message)
		}
	}
	var backupID, updateScript string
	if action == "update" && opts.Backup {
		plan, err := StackBackupPlan(ctx, d, name)
		if err != nil {
			return "", err
		}
		if !plan.Possible {
			return "", apierr.New(apierr.Conflict, "Datensicherung nicht möglich: "+plan.Reason)
		}
		backupID = strconv.FormatInt(nowUnix(), 10)
		cmd, err := prepareBackup(d, name, backupID, plan.Sources)
		if err != nil {
			return "", err
		}
		updateScript = updateWithBackupScript(name, backupID, cmd)
	}
	if action == "update" {
		snapshotStack(ctx, d, name, backupID)
	}
	// Beendete Läufe dieses Stacks aufräumen.
	if units, err := listRunUnits(ctx, d); err == nil {
		re := stackUnitRe(name)
		for _, u := range units {
			if !u.Running && re.MatchString(u.Name) {
				_, _ = d.Runner.Run(ctx, nil, "systemctl", "stop", u.Name+".service")
				_, _ = d.Runner.Run(ctx, nil, "systemctl", "reset-failed", u.Name+".service")
			}
		}
	}

	unit := fmt.Sprintf("%s%s-%d", docker.RunUnitPrefix, name, nowUnix())
	args := []string{"--unit=" + unit, "--no-block", "--description=BetterNAS Stack " + name + " (" + action + ")",
		"--property=RemainAfterExit=yes"}
	quote := func(a []string) string { return "'" + strings.Join(a, "' '") + "'" }
	switch action {
	case "up":
		args = append(args, "docker")
		args = append(args, composeArgs(name, "up", "-d", "--remove-orphans")...)
	case "down":
		args = append(args, "docker")
		args = append(args, composeArgs(name, "down", "--remove-orphans")...)
	case "update":
		if updateScript != "" {
			args = append(args, "sh", "-c", updateScript)
			break
		}
		// Feste Zeichenfolge; der Stackname ist durch StackNameRe gesichert.
		pull := quote(append([]string{"docker"}, composeArgs(name, "pull")...))
		up := quote(append([]string{"docker"}, composeArgs(name, "up", "-d", "--remove-orphans")...))
		args = append(args, "sh", "-c", pull+" && "+up)
	case "rollback":
		args = append(args, "sh", "-c", rollbackCmd)
	default: // stop, start, restart, pull
		args = append(args, "docker")
		args = append(args, composeArgs(name, action)...)
	}
	if _, err := run(ctx, d.Runner, nil, "systemd-run", args...); err != nil {
		return "", err
	}
	return unit, nil
}

// StackRunStatus liefert Zustand und Protokoll eines Stack-Laufs. Nur eigene Units sind erlaubt.
func StackRunStatus(ctx context.Context, d Deps, unit string) (docker.RunState, error) {
	if !docker.RunUnitRe.MatchString(unit) {
		return docker.RunState{}, apierr.New(apierr.InvalidParams, "Ungültige Lauf-Kennung")
	}
	out, err := run(ctx, d.Runner, nil, "systemctl", "show", "-p", "ActiveState", "-p", "SubState", "-p", "Result", unit)
	if err != nil {
		return docker.RunState{}, err
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
	return docker.RunState{State: state, Log: strings.TrimSpace(string(logOut.Stdout))}, nil
}

// registerDocker registriert die Docker-Methoden des Agents.
func registerDocker(s *rpc.Server, d Deps) {
	named := func(p json.RawMessage) (string, error) {
		var req struct {
			Name string `json:"name"`
		}
		return req.Name, decode(p, &req)
	}
	s.Handle("docker.status", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return DockerStatus(ctx, d)
	})
	s.Handle("stack.list", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return ListStacks(ctx, d)
	})
	s.Handle("stack.get", func(ctx context.Context, p json.RawMessage) (any, error) {
		name, err := named(p)
		if err != nil {
			return nil, err
		}
		return GetStack(ctx, d, name)
	})
	s.Handle("stack.save", func(ctx context.Context, p json.RawMessage) (any, error) {
		var in StackSave
		if err := decode(p, &in); err != nil {
			return nil, err
		}
		mutate.Lock()
		defer mutate.Unlock()
		if err := SaveStack(ctx, d, in); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	})
	s.Handle("stack.validate", func(ctx context.Context, p json.RawMessage) (any, error) {
		name, err := named(p)
		if err != nil {
			return nil, err
		}
		return ValidateStack(ctx, d, name)
	})
	s.Handle("stack.delete", func(ctx context.Context, p json.RawMessage) (any, error) {
		name, err := named(p)
		if err != nil {
			return nil, err
		}
		mutate.Lock()
		defer mutate.Unlock()
		if err := DeleteStack(ctx, d, name); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	})
	s.Handle("stack.backupplan", func(ctx context.Context, p json.RawMessage) (any, error) {
		name, err := named(p)
		if err != nil {
			return nil, err
		}
		return StackBackupPlan(ctx, d, name)
	})
	s.Handle("stack.run", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Name   string `json:"name"`
			Action string `json:"action"`
			docker.RunOptions
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		unit, err := RunStackOpts(ctx, d, req.Name, req.Action, req.RunOptions)
		if err != nil {
			return nil, err
		}
		return map[string]string{"unit": unit}, nil
	})
	for method, fn := range map[string]func(context.Context, Deps, string) error{
		"stack.rollback.commit": StackRollbackCommit,
		"stack.rollback.clear":  StackRollbackClear,
	} {
		s.Handle(method, func(ctx context.Context, p json.RawMessage) (any, error) {
			name, err := named(p)
			if err != nil {
				return nil, err
			}
			if err := fn(ctx, d, name); err != nil {
				return nil, err
			}
			return map[string]bool{"ok": true}, nil
		})
	}
	s.Handle("stack.runstatus", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct{ Unit string }
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return StackRunStatus(ctx, d, req.Unit)
	})
	s.Handle("container.list", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return ListContainers(ctx, d)
	})
	s.Handle("container.action", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct{ ID, Action string }
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		if err := ContainerAction(ctx, d, req.ID, req.Action); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	})
}

// --- Container und Status ---

// ListContainers liefert alle Container (auch gestoppte).
func ListContainers(ctx context.Context, d Deps) ([]docker.Container, error) {
	out, err := run(ctx, d.Runner, nil, "docker", "ps", "-a", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, apierr.New(apierr.Internal, "Docker ist nicht verfügbar").With("detail", err.Error())
	}
	return docker.ParseContainers(out)
}

// ContainerAction führt start/stop/restart/remove für genau eine Container-ID aus.
func ContainerAction(ctx context.Context, d Deps, id, action string) error {
	if !docker.ContainerIDRe.MatchString(id) {
		return apierr.New(apierr.InvalidParams, "Ungültige Container-ID")
	}
	if !docker.ContainerActions[action] {
		return apierr.New(apierr.InvalidParams, "Unbekannte Container-Aktion")
	}
	var args []string
	if action == "remove" {
		args = []string{"rm", "-f", id}
	} else {
		args = []string{action, id}
	}
	_, err := run(ctx, d.Runner, nil, "docker", args...)
	return err
}

// DockerStatus meldet, ob Docker installiert und aktiv ist.
func DockerStatus(ctx context.Context, d Deps) (docker.Status, error) {
	var st docker.Status
	if res, err := d.Runner.Run(ctx, nil, "docker", "--version"); err != nil || res.ExitCode != 0 {
		return st, nil
	}
	st.Installed = true
	res, err := d.Runner.Run(ctx, nil, "systemctl", "is-active", "docker.service")
	if err != nil || strings.TrimSpace(string(res.Stdout)) != "active" {
		return st, nil
	}
	st.Active = true
	if v, err := d.Runner.Run(ctx, nil, "docker", "version", "--format", "{{.Server.Version}}"); err == nil && v.ExitCode == 0 {
		st.Version = strings.TrimSpace(string(v.Stdout))
	}
	return st, nil
}
