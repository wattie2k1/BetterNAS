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

func TestListFoldersHidesLostAndFound(t *testing.T) {
	d, _ := folderEnv(t, map[string]string{"/srv/pools/tank": "/srv/pools/tank"}, map[string]string{"/srv/pools/tank": "lost+found\x00Medien\x00"})
	l, err := ListFolders(context.Background(), d, "/srv/pools/tank")
	if err != nil || len(l.Dirs) != 1 || l.Dirs[0].Name != "Medien" {
		t.Fatalf("%+v %v", l, err)
	}
}

func mkdirEnv(t *testing.T, real map[string]string) (Deps, *sysexec.FakeRunner) {
	t.Helper()
	return folderEnv(t, real, nil)
}

func TestCreateFolder(t *testing.T) {
	d, f := mkdirEnv(t, map[string]string{"/srv/pools/tank/Medien": "/srv/pools/tank/Medien", "/srv/appdata": "/srv/appdata"})
	ctx := context.Background()
	got, err := CreateFolder(ctx, d, "/srv/pools/tank/Medien", "Filme-2024")
	if err != nil || got.Name != "Filme-2024" || got.Path != "/srv/pools/tank/Medien/Filme-2024" {
		t.Fatalf("%+v %v", got, err)
	}
	if !f.Called("mkdir -m 755 -- /srv/pools/tank/Medien/Filme-2024") {
		t.Fatalf("%v", f.Calls)
	}
	// In /srv/appdata selbst darf angelegt werden.
	if _, err := CreateFolder(ctx, d, "/srv/appdata", "jellyfin"); err != nil {
		t.Fatal(err)
	}
}

func TestCreateFolderRejectsBadNames(t *testing.T) {
	d, f := mkdirEnv(t, map[string]string{"/srv/pools/tank": "/srv/pools/tank"})
	long := strings.Repeat("a", 65)
	for _, bad := range []string{"", ".", "..", "a/b", "../x", ".versteckt", " vorn", "hinten ", "a\x00b", "a'b", "lost+found", "mit\nZeilenumbruch", "Ü-Umlaut", "mit Leerzeichen", "a;b", "a$b", long} {
		if _, err := CreateFolder(context.Background(), d, "/srv/pools/tank", bad); code(err) != apierr.InvalidParams {
			t.Errorf("%q: %v", bad, err)
		}
	}
	for _, c := range f.Calls {
		if strings.HasPrefix(c, "mkdir") {
			t.Fatalf("mkdir trotz ungültigem Namen: %v", f.Calls)
		}
	}
}

func TestCreateFolderRejectsWrongPlaces(t *testing.T) {
	d, f := mkdirEnv(t, map[string]string{
		"/srv/pools":              "/srv/pools",
		"/srv/pools/link":         "/etc",
		"/srv/pools/tank":         "/srv/pools/tank",
		"/srv/appdata/stacks":     "/srv/appdata/stacks",
		"/srv/appdata/stacks/x":   "/srv/appdata/stacks/x",
		"/srv/appdata/docker":     "/srv/appdata/docker",
		"/srv/appdata/backups":    "/srv/appdata/backups",
		"/srv/appdata/alias":      "/srv/appdata/docker", // Link auf einen gesperrten Bereich
		"/srv/appdata/ueber-pool": "/srv/pools",
	})
	ctx := context.Background()
	for _, tc := range []struct{ parent, name string }{
		{"", "x"},                        // Übersicht der Wurzeln
		{"/srv/pools", "neuerpool"},      // Pools werden nicht über Ordner angelegt
		{"/etc", "x"},                    // außerhalb
		{"/srv/pools/../../etc", "x"},    // ..
		{"/srv/pools/link", "x"},         // Link nach außen
		{"/srv/appdata/stacks", "x"},     // BetterNAS-Bereiche
		{"/srv/appdata/stacks/x", "y"},   //
		{"/srv/appdata/docker", "x"},     //
		{"/srv/appdata/backups", "x"},    //
		{"/srv/appdata/alias", "x"},      // Link auf Gesperrtes
		{"/srv/appdata/ueber-pool", "x"}, // Link auf die Pool-Wurzel
		{"/srv/appdata", "stacks"},       // reservierter Name
		{"/srv/appdata", "docker"},       //
		{"/srv/appdata", "backups"},      //
	} {
		if _, err := CreateFolder(ctx, d, tc.parent, tc.name); err == nil {
			t.Errorf("%q + %q wurde akzeptiert", tc.parent, tc.name)
		}
	}
	if _, err := CreateFolder(ctx, d, "/srv/pools/fehlt", "x"); code(err) != apierr.NotFound {
		t.Errorf("fehlender Elternordner: %v", err)
	}
	for _, c := range f.Calls {
		if strings.HasPrefix(c, "mkdir") {
			t.Fatalf("mkdir am falschen Ort: %v", f.Calls)
		}
	}
}

func TestCreateFolderConflictWhenExists(t *testing.T) {
	d, _ := mkdirEnv(t, map[string]string{"/srv/pools/tank": "/srv/pools/tank"})
	if err := d.FS.MkdirAll("/srv/pools/tank/da", 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateFolder(context.Background(), d, "/srv/pools/tank", "da"); code(err) != apierr.Conflict {
		t.Fatalf("%v", err)
	}
}
