package agent

import (
	"context"
	"encoding/json"
	"path"
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
		case n == "", strings.HasPrefix(n, "."), strings.ContainsAny(n, "\n\r"):
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
