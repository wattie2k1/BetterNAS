package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/sysexec"
)

func folderEnv(t *testing.T, real map[string]string, listing map[string]string) (Deps, *sysexec.FakeRunner) {
	t.Helper()
	d, f, _ := dockerEnv(t)
	for _, r := range []string{"/srv/pools", "/srv/appdata"} {
		_ = d.FS.MkdirAll(r, 0o755)
	}
	f.Hook = func(call string) (sysexec.Result, bool) {
		if p, ok := strings.CutPrefix(call, "realpath -e -- "); ok {
			if r, ok := real[p]; ok {
				return sysexec.Result{Stdout: []byte(r + "\n")}, true
			}
			return sysexec.Result{ExitCode: 1}, true
		}
		if rest, ok := strings.CutPrefix(call, "find "); ok {
			dir, _, _ := strings.Cut(rest, " ")
			return sysexec.Result{Stdout: []byte(listing[dir])}, true
		}
		return sysexec.Result{}, false
	}
	return d, f
}

func TestListFoldersRootsAndSubfolders(t *testing.T) {
	d, _ := folderEnv(t,
		map[string]string{"/srv/pools/tank": "/srv/pools/tank"},
		map[string]string{"/srv/pools/tank": "Medien\x00.hidden\x00backup\x00Fotos\x00"})
	ctx := context.Background()
	roots, err := ListFolders(ctx, d, "")
	if err != nil || len(roots.Dirs) != 2 || roots.Dirs[0].Path != "/srv/pools" || roots.Dirs[1].Path != "/srv/appdata" || roots.Parent != "" {
		t.Fatalf("%+v %v", roots, err)
	}
	l, err := ListFolders(ctx, d, "/srv/pools/tank")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range l.Dirs {
		names = append(names, f.Name+"="+f.Path)
	}
	want := "backup=/srv/pools/tank/backup,Fotos=/srv/pools/tank/Fotos,Medien=/srv/pools/tank/Medien"
	if strings.Join(names, ",") != want || l.Parent != "/srv/pools" || l.Path != "/srv/pools/tank" {
		t.Fatalf("%+v", l)
	}
}

func TestListFoldersParentOfRootIsOverview(t *testing.T) {
	d, _ := folderEnv(t, map[string]string{"/srv/pools": "/srv/pools"}, map[string]string{})
	l, err := ListFolders(context.Background(), d, "/srv/pools")
	if err != nil || l.Parent != "" {
		t.Fatalf("%+v %v", l, err)
	}
}

func TestListFoldersHidesDockerInternalsInAppdata(t *testing.T) {
	d, _ := folderEnv(t, map[string]string{"/srv/appdata": "/srv/appdata"},
		map[string]string{"/srv/appdata": "docker\x00stacks\x00jellyfin\x00"})
	l, err := ListFolders(context.Background(), d, "/srv/appdata")
	if err != nil || len(l.Dirs) != 1 || l.Dirs[0].Name != "jellyfin" {
		t.Fatalf("%+v %v", l, err)
	}
}

func TestListFoldersRejectsEscapes(t *testing.T) {
	d, f := folderEnv(t, map[string]string{
		"/srv/pools/link": "/etc", // Symlink aus dem Pool heraus
		"/srv/pools/ok":   "/srv/pools/ok",
	}, nil)
	ctx := context.Background()
	for _, bad := range []string{"/etc", "/srv/pools/../../etc", "/srv", "relativ"} {
		if _, err := ListFolders(ctx, d, bad); code(err) != apierr.InvalidParams {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if _, err := ListFolders(ctx, d, "/srv/pools/link"); code(err) != apierr.InvalidParams {
		t.Errorf("Symlink hinaus: %v", err)
	}
	if _, err := ListFolders(ctx, d, "/srv/pools/fehlt"); code(err) != apierr.NotFound {
		t.Errorf("fehlend: %v", err)
	}
	for _, c := range f.Calls {
		if strings.HasPrefix(c, "find /etc") {
			t.Fatalf("außerhalb gelistet: %v", f.Calls)
		}
	}
}
