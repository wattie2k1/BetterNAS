package agent

import (
	"context"
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/docker"
	"github.com/betternas/betternas/internal/rpc"
)

const maxFolders = 500

// hiddenAppdata sind Ordner in /srv/appdata, die BetterNAS selbst gehören und nicht für Container gedacht sind.
var hiddenAppdata = map[string]bool{"docker": true, "stacks": true}

// ListFolders liefert die Unterordner von p (leer: die Wurzeln). Symbolische Links, die aus den erlaubten
// Wurzeln hinausführen, werden abgelehnt; versteckte Ordner (Punkt am Anfang) werden nicht angezeigt.
func ListFolders(ctx context.Context, d Deps, p string) (docker.FolderList, error) {
	p, err := docker.CleanFolder(p)
	if err != nil {
		return docker.FolderList{}, err
	}
	out := docker.FolderList{Path: p, Dirs: []docker.Folder{}}
	if p == "" {
		for _, r := range docker.FolderRoots {
			if d.FS.Exists(r) {
				out.Dirs = append(out.Dirs, docker.Folder{Name: path.Base(r), Path: r})
			}
		}
		return out, nil
	}
	if !docker.IsFolderRoot(p) {
		out.Parent = path.Dir(p)
	}
	res, err := d.Runner.Run(ctx, nil, "realpath", "-e", "--", p)
	if err != nil || res.ExitCode != 0 {
		return out, apierr.New(apierr.NotFound, "Ordner nicht gefunden")
	}
	real := strings.TrimSpace(string(res.Stdout))
	if !docker.UnderFolderRoot(real) {
		return out, apierr.New(apierr.InvalidParams, "Der Ordner liegt außerhalb der erlaubten Bereiche")
	}
	res, err = d.Runner.Run(ctx, nil, "find", real, "-mindepth", "1", "-maxdepth", "1", "-type", "d", "-printf", `%f\0`)
	if err != nil || res.ExitCode != 0 {
		return out, apierr.New(apierr.Internal, "Der Ordner konnte nicht gelesen werden")
	}
	var names []string
	for _, n := range strings.Split(string(res.Stdout), "\x00") {
		switch {
		case n == "", strings.HasPrefix(n, "."), n == "lost+found", strings.ContainsAny(n, "\n\r"):
		case p == "/srv/appdata" && hiddenAppdata[n]:
		default:
			names = append(names, n)
		}
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	if len(names) > maxFolders {
		names = names[:maxFolders]
	}
	for _, n := range names {
		out.Dirs = append(out.Dirs, docker.Folder{Name: n, Path: path.Join(p, n)})
	}
	return out, nil
}

func registerDockerFolders(s *rpc.Server, d Deps) {
	s.Handle("folder.create", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Path string `json:"path"`
			Name string `json:"name"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		mutate.Lock()
		defer mutate.Unlock()
		return CreateFolder(ctx, d, req.Path, req.Name)
	})
	s.Handle("folder.list", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Path string `json:"path"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return ListFolders(ctx, d, req.Path)
	})
}

// Namen neuer Ordner: dieselben Zeichen, die auch die Pfadfelder der Vorlagen akzeptieren (kein Leerzeichen, keine Umlaute).
var folderNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.+@-]{0,63}$`)

// reservedFolder sagt, ob ein (aufgelöster) Pfad zu den Bereichen gehört, die BetterNAS selbst verwaltet.
func reservedFolder(p string) bool {
	if rest, ok := strings.CutPrefix(p, "/srv/appdata/"); ok {
		first, _, _ := strings.Cut(rest, "/")
		return reservedAppdata[first]
	}
	return false
}

// CreateFolder legt einen Ordner unterhalb von parent an. Erlaubt ist das in /srv/appdata und innerhalb eines Pools,
// nicht in der Pool-Übersicht selbst (Pools entstehen unter „Speicherpools“) und nicht in den BetterNAS-Bereichen.
func CreateFolder(ctx context.Context, d Deps, parent, name string) (docker.Folder, error) {
	parent, err := docker.CleanFolder(parent)
	if err != nil {
		return docker.Folder{}, err
	}
	if parent == "" {
		return docker.Folder{}, apierr.New(apierr.InvalidParams, "Wähle zuerst einen Bereich, in dem der Ordner angelegt werden soll.")
	}
	if !folderNameRe.MatchString(name) || name == "lost+found" {
		return docker.Folder{}, apierr.New(apierr.InvalidParams, "Ungültiger Ordnername (erlaubt: Buchstaben ohne Umlaute, Ziffern und _ . + @ -, höchstens 64 Zeichen).")
	}
	if parent == "/srv/pools" {
		return docker.Folder{}, apierr.New(apierr.InvalidParams, "Hier lassen sich keine Ordner anlegen; öffne zuerst einen Pool.")
	}
	if parent == appdataRoot && reservedAppdata[name] {
		return docker.Folder{}, apierr.New(apierr.InvalidParams, "Dieser Name ist für BetterNAS reserviert.")
	}
	res, err := d.Runner.Run(ctx, nil, "realpath", "-e", "--", parent)
	if err != nil || res.ExitCode != 0 {
		return docker.Folder{}, apierr.New(apierr.NotFound, "Der übergeordnete Ordner wurde nicht gefunden.")
	}
	real := strings.TrimSpace(string(res.Stdout))
	if !docker.UnderFolderRoot(real) || real == "/srv/pools" || reservedFolder(real) || reservedFolder(parent) {
		return docker.Folder{}, apierr.New(apierr.InvalidParams, "In diesem Bereich lassen sich keine Ordner anlegen.")
	}
	if d.FS.Exists(path.Join(real, name)) {
		return docker.Folder{}, apierr.New(apierr.Conflict, "Ein Ordner mit diesem Namen existiert bereits.")
	}
	if _, err := run(ctx, d.Runner, nil, "mkdir", "-m", "755", "--", path.Join(real, name)); err != nil {
		return docker.Folder{}, apierr.New(apierr.Internal, "Der Ordner konnte nicht angelegt werden").With("detail", err.Error())
	}
	return docker.Folder{Name: name, Path: path.Join(parent, name)}, nil
}
