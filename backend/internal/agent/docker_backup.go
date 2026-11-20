package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/docker"
)

// Datensicherung vor einem Update: Gesichert werden nur App-Daten (Ordner unter appdata und dockerdata, die ein Dienst
// beschreiben darf, und die benannten Docker-Volumes des Stacks), niemals Pools mit Medien. Die Sicherung gehört
// zum Rollback-Stand des Updates (genau eine je Stack) und wird mit ihm zusammen verworfen.

const (
	backupSlack = 100 << 20 // Reserve auf dem Datenträger zusätzlich zu den 10 % Aufschlag
	backupIDMax = 12
)

func noDataReason() string {
	if p := curPaths.Load(); p != nil {
		return "Der Stack hat keine Daten, die gesichert werden können (gesichert werden Ordner unter " + p.Appdata + " und " + p.Dockerdata + " sowie Docker-Volumes, keine Pools)."
	}
	return "Der Stack hat keine Daten, die gesichert werden können."
}

// backupRoot (Wurzel der Sicherungen, für die Platzprüfung) und backupBase folgen den eingerichteten Speicherorten
// (Aufruf nur, wenn Docker eingerichtet ist).
func backupRoot() string {
	if p := curPaths.Load(); p != nil {
		return p.Dockerdata
	}
	return "/nonexistent/docker-not-configured"
}

func backupBase() string {
	if p := curPaths.Load(); p != nil {
		return p.BackupBase()
	}
	return "/nonexistent/docker-not-configured/backups/stacks"
}

var (
	// bindPathRe: Zeichen, die ein gesicherter Pfad enthalten darf (kein Zeilenumbruch, keine Shell-Sonderzeichen).
	bindPathRe = regexp.MustCompile(`^/[A-Za-z0-9_.+@ /-]+$`)
	// bindRestRe: Teil unterhalb der Wurzel; beginnt nicht mit „/“.
	bindRestRe = regexp.MustCompile(`^[A-Za-z0-9_.+@ -][A-Za-z0-9_./+@ -]*$`)
	volPathRe  = regexp.MustCompile(`^/[A-Za-z0-9_./-]+/volumes/[A-Za-z0-9][A-Za-z0-9_.-]*/_data$`)
	volNameRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	backupIDRe = regexp.MustCompile(`^[0-9]{1,12}$`)
)

func hasDotDot(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// safeBindPath sagt, ob ein Ordner unter appdata oder dockerdata gesichert und wiederhergestellt werden darf:
// bereinigt, nur sichere Zeichen, echter Unterordner einer der beiden Wurzeln und nicht in einem Bereich,
// der BetterNAS selbst gehört (Stacks, Proxy, Sicherungen, Docker-data-root).
func safeBindPath(p string) bool {
	cur := curPaths.Load()
	if cur == nil || path.Clean(p) != p || hasDotDot(p) || !bindPathRe.MatchString(p) {
		return false
	}
	for _, root := range cur.Roots() {
		rest, ok := strings.CutPrefix(p, root+"/")
		if !ok || !bindRestRe.MatchString(rest) {
			continue
		}
		first, _, _ := strings.Cut(rest, "/")
		if !cur.Reserved(root, first) {
			return true
		}
	}
	return false
}

// safeSource prüft eine Quelle aus einer Sicherungsbeschreibung, bevor daran etwas gelöscht oder entpackt wird.
func safeSource(s docker.BackupSource) bool {
	switch s.Kind {
	case "bind":
		return safeBindPath(s.Path)
	case "volume":
		return path.Clean(s.Path) == s.Path && !hasDotDot(s.Path) && volPathRe.MatchString(s.Path)
	}
	return false
}

func backupDir(name string) string { return path.Join(backupBase(), name) }
func backupArchive(name, id string) string {
	return path.Join(backupDir(name), id+".tar.gz")
}
func backupManifest(name, id string) string { return path.Join(backupDir(name), id+".json") }

// readBackup liefert die Beschreibung einer Sicherung, wenn Beschreibung und Archiv vorhanden sind.
func readBackup(d Deps, name, id string) (docker.DataBackup, bool) {
	var m docker.DataBackup
	if !backupIDRe.MatchString(id) || !d.FS.Exists(backupArchive(name, id)) {
		return m, false
	}
	b, err := d.FS.ReadFile(backupManifest(name, id))
	if err != nil || json.Unmarshal(b, &m) != nil || m.ID != id {
		return m, false
	}
	return m, true
}

// removeBackup löscht Archiv, Beschreibung und angefangene Archive einer Sicherung.
func removeBackup(d Deps, name, id string) {
	if !backupIDRe.MatchString(id) {
		return
	}
	for _, p := range []string{backupArchive(name, id) + ".part", backupArchive(name, id), backupManifest(name, id)} {
		removeFile(d, p)
	}
	// Ist das Verzeichnis leer, verschwindet es mit.
	if rest, err := d.FS.Glob(path.Join(backupDir(name), "*")); err == nil && len(rest) == 0 && d.FS.Exists(backupDir(name)) {
		_ = d.FS.Remove(backupDir(name))
	}
}

type composeConfig struct {
	Services map[string]struct {
		Volumes []struct {
			Type     string `json:"type"`
			Source   string `json:"source"`
			ReadOnly bool   `json:"read_only"`
		} `json:"volumes"`
	} `json:"services"`
	Volumes map[string]struct {
		Name     string          `json:"name"`
		External json.RawMessage `json:"external"`
	} `json:"volumes"`
}

func isExternal(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null" && s != "false"
}

// StackBackupPlan ermittelt, was vor einem Update gesichert würde, wie groß das ist und ob der Platz reicht.
func StackBackupPlan(ctx context.Context, d Deps, name string) (docker.BackupPlan, error) {
	plan := docker.BackupPlan{Sources: []docker.BackupSource{}}
	if err := checkStackName(name); err != nil {
		return plan, err
	}
	if !d.FS.Exists(composePath(name)) {
		return plan, apierr.New(apierr.NotFound, "Stack nicht gefunden")
	}
	res, err := d.Runner.Run(ctx, nil, "docker", composeArgs(name, "config", "--format", "json")...)
	if err != nil {
		return plan, apierr.New(apierr.Internal, "Docker ist nicht verfügbar")
	}
	if res.ExitCode != 0 {
		return plan, apierr.New(apierr.InvalidParams, "Die Compose-Datei ist ungültig: "+strings.TrimSpace(string(res.Stderr)))
	}
	var cfg composeConfig
	if err := json.Unmarshal(res.Stdout, &cfg); err != nil {
		return plan, apierr.New(apierr.Internal, "Die Compose-Konfiguration ist nicht lesbar")
	}

	seen := map[string]bool{}
	var binds, vols []docker.BackupSource
	services := make([]string, 0, len(cfg.Services))
	for s := range cfg.Services {
		services = append(services, s)
	}
	sort.Strings(services)
	dockerRoot := ""
	for _, s := range services {
		for _, v := range cfg.Services[s].Volumes {
			switch v.Type {
			case "bind":
				p := path.Clean(v.Source)
				if v.ReadOnly || !safeBindPath(p) || seen["b:"+p] {
					continue
				}
				seen["b:"+p] = true
				binds = append(binds, docker.BackupSource{Kind: "bind", Name: p, Path: p})
			case "volume":
				key := v.Source
				def := cfg.Volumes[key]
				volName := def.Name
				if volName == "" {
					volName = name + "_" + key
				}
				if v.ReadOnly || isExternal(def.External) || !volNameRe.MatchString(volName) || seen["v:"+volName] {
					continue
				}
				seen["v:"+volName] = true
				if dockerRoot == "" {
					if out, err := run(ctx, d.Runner, nil, "docker", "info", "--format", "{{.DockerRootDir}}"); err == nil {
						dockerRoot = strings.TrimSpace(string(out))
					}
				}
				out, err := run(ctx, d.Runner, nil, "docker", "volume", "inspect", "--format", "{{.Mountpoint}}", volName)
				if err != nil {
					continue // das Volume gibt es noch nicht
				}
				mp := strings.TrimSpace(string(out))
				// Nur der Standardort im Docker-Datenverzeichnis wird angefasst.
				if dockerRoot == "" || mp != dockerRoot+"/volumes/"+volName+"/_data" {
					continue
				}
				vols = append(vols, docker.BackupSource{Kind: "volume", Name: volName, Path: mp})
			}
		}
	}
	sort.Slice(binds, func(i, j int) bool { return binds[i].Path < binds[j].Path })
	sort.Slice(vols, func(i, j int) bool { return vols[i].Path < vols[j].Path })
	// Ordner in einem anderen gesicherten Ordner sind schon enthalten.
	for _, b := range binds {
		nested := false
		for _, o := range binds {
			if o.Path != b.Path && strings.HasPrefix(b.Path, o.Path+"/") {
				nested = true
			}
		}
		if !nested {
			plan.Sources = append(plan.Sources, b)
		}
	}
	plan.Sources = append(plan.Sources, vols...)

	if len(plan.Sources) == 0 {
		plan.Reason = noDataReason()
		return plan, nil
	}
	args := []string{"-sb", "--"}
	for _, s := range plan.Sources {
		args = append(args, s.Path)
	}
	sizes := map[string]int64{}
	if res, err := d.Runner.Run(ctx, nil, "du", args...); err == nil { // Teilergebnisse zählen auch bei Fehlern einzelner Dateien
		for _, line := range strings.Split(string(res.Stdout), "\n") {
			if n, p, ok := strings.Cut(line, "\t"); ok {
				if v, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64); err == nil {
					sizes[p] = v
				}
			}
		}
	}
	for i := range plan.Sources {
		plan.Sources[i].Bytes = sizes[plan.Sources[i].Path]
		plan.TotalBytes += plan.Sources[i].Bytes
	}
	if out, err := run(ctx, d.Runner, nil, "df", "--output=avail", "-B1", backupRoot()); err == nil {
		if f := strings.Fields(string(out)); len(f) >= 2 {
			plan.FreeBytes, _ = strconv.ParseInt(f[len(f)-1], 10, 64)
		}
	}
	need := plan.TotalBytes + plan.TotalBytes/10 + backupSlack
	if plan.FreeBytes < need {
		plan.Reason = fmt.Sprintf("Zu wenig Platz für die Sicherung: benötigt werden etwa %d MB, frei sind %d MB.", need>>20, plan.FreeBytes>>20)
		return plan, nil
	}
	plan.Possible = true
	return plan, nil
}

// shQuote setzt jedes Argument in einfache Anführungszeichen (ein „'“ im Argument wird maskiert).
func shQuote(a []string) string {
	q := make([]string, len(a))
	for i, s := range a {
		q[i] = "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return strings.Join(q, " ")
}

// prepareBackup legt die Beschreibung der Sicherung an und liefert den Befehl, der sie erstellt.
func prepareBackup(d Deps, name, id string, sources []docker.BackupSource) (string, error) {
	if err := d.FS.MkdirAll(backupDir(name), 0o700); err != nil {
		return "", err
	}
	man := docker.DataBackup{ID: id, CreatedAt: time.Unix(nowUnix(), 0).UTC().Format(time.RFC3339), Sources: sources}
	b, _ := json.Marshal(man)
	if err := d.FS.WriteFile(backupManifest(name, id), b, 0o600); err != nil {
		return "", err
	}
	part := backupArchive(name, id) + ".part"
	tar := []string{"tar", "-C", "/", "--numeric-owner", "-czf", part, "--"}
	for _, s := range sources {
		tar = append(tar, strings.TrimPrefix(s.Path, "/"))
	}
	return strings.Join([]string{
		shQuote([]string{"mkdir", "-p", backupDir(name)}),
		shQuote([]string{"chmod", "700", backupDir(name)}),
		shQuote(tar),
		shQuote([]string{"mv", part, backupArchive(name, id)}),
	}, " && "), nil
}

// updateWithBackupScript: neue Images laden (Stack läuft weiter), Stack anhalten, Daten sichern, neu starten.
// Schlägt die Sicherung fehl, wird der Stack wieder gestartet und das Update abgebrochen.
func updateWithBackupScript(name, id, backupCmd string) string {
	part := backupArchive(name, id) + ".part"
	c := func(args ...string) string { return shQuote(append([]string{"docker"}, composeArgs(name, args...)...)) }
	return strings.Join([]string{
		"set -e",
		c("pull"),
		c("stop"),
		"if ! { " + backupCmd + "; }; then echo 'Datensicherung fehlgeschlagen, Update abgebrochen.'; " +
			shQuote([]string{"rm", "-f", part}) + "; " + c("start") + "; exit 1; fi",
		c("up", "-d", "--remove-orphans"),
	}, "\n")
}

// restoreCommands liefert die Befehle, die die gesicherten Daten zurückspielen: Archiv prüfen, Ordner leeren, entpacken.
func restoreCommands(d Deps, name, id string) ([]string, error) {
	man, ok := readBackup(d, name, id)
	if !ok {
		return nil, apierr.New(apierr.NotFound, "Zu diesem Rollback-Stand gibt es keine Datensicherung.")
	}
	for _, s := range man.Sources {
		if !safeSource(s) {
			return nil, apierr.New(apierr.Internal, "Die Datensicherung enthält einen unzulässigen Pfad und wird nicht zurückgespielt.")
		}
	}
	archive := backupArchive(name, id)
	cmds := []string{shQuote([]string{"gzip", "-t", archive})}
	for _, s := range man.Sources {
		cmds = append(cmds, shQuote([]string{"find", s.Path, "-mindepth", "1", "-delete"}))
	}
	cmds = append(cmds, shQuote([]string{"tar", "-C", "/", "--numeric-owner", "-xzpf", archive}))
	return cmds, nil
}
