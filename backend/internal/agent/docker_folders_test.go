package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/docker"
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
	if err != nil || len(roots.Dirs) != 2 || roots.Dirs[0].Path != "/srv/pools" || roots.Dirs[1].Path != "/srv" || roots.Parent != "" {
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
	for _, bad := range []string{"/etc", "/srv/pools/../../etc", "/usr", "relativ"} {
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

func TestListFoldersRootsIncludeDisks(t *testing.T) {
	d, _ := folderEnv(t, map[string]string{}, map[string]string{})
	_ = d.FS.MkdirAll("/srv/disks", 0o755)
	_ = d.FS.MkdirAll("/mnt", 0o755)
	roots, err := ListFolders(context.Background(), d, "")
	var got []string
	for _, r := range roots.Dirs {
		got = append(got, r.Path)
	}
	if err != nil || strings.Join(got, ",") != "/srv/pools,/srv/disks,/srv,/mnt" {
		t.Fatalf("%v %v", got, err)
	}
}

func configuredPaths(t *testing.T) {
	t.Helper()
	p := docker.Paths{Appdata: "/srv/pools/a/cfg", Dockerdata: "/srv/pools/b/dd"}
	curPaths.Store(&p)
}

func TestListFoldersHidesReservedUnderBothRoots(t *testing.T) {
	d, _ := folderEnv(t, map[string]string{"/srv/pools/a/cfg": "/srv/pools/a/cfg", "/srv/pools/b/dd": "/srv/pools/b/dd"},
		map[string]string{"/srv/pools/a/cfg": "stacks\x00backups\x00reverse-proxy\x00emby\x00docker\x00", "/srv/pools/b/dd": "docker\x00stacks\x00emby\x00"})
	configuredPaths(t)
	for root, want := range map[string]string{"/srv/pools/a/cfg": "docker,emby", "/srv/pools/b/dd": "emby,stacks"} {
		l, err := ListFolders(context.Background(), d, root)
		var names []string
		for _, f := range l.Dirs {
			names = append(names, f.Name)
		}
		if err != nil || strings.Join(names, ",") != want {
			t.Errorf("%s: %v %v", root, names, err)
		}
	}
}

func TestCreateFolderInDisksAndConfiguredRoots(t *testing.T) {
	d, f := mkdirEnv(t, map[string]string{"/srv/disks/ab": "/srv/disks/ab", "/srv/pools/b/dd": "/srv/pools/b/dd"})
	configuredPaths(t)
	ctx := context.Background()
	if _, err := CreateFolder(ctx, d, "/srv/disks/ab", "daten"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateFolder(ctx, d, "/srv/pools/b/dd", "emby"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateFolder(ctx, d, "/srv/pools/b/dd", "docker"); err == nil {
		t.Fatal("reservierter Name unter dockerdata akzeptiert")
	}
	if !f.Called("mkdir -m 755 -- /srv/disks/ab/daten") {
		t.Fatalf("%v", f.Calls)
	}
}

func TestCreateFolderRefusesRootsThemselves(t *testing.T) {
	d, _ := mkdirEnv(t, map[string]string{"/srv/pools": "/srv/pools", "/srv/disks": "/srv/disks"})
	for _, p := range []string{"/srv/pools", "/srv/disks"} {
		if _, err := CreateFolder(context.Background(), d, p, "x"); err == nil {
			t.Errorf("%s akzeptiert", p)
		}
	}
}

func TestFolderRPCsWorkWhenNotConfigured(t *testing.T) {
	d, _ := folderEnv(t, map[string]string{"/srv/pools/tank": "/srv/pools/tank"}, map[string]string{"/srv/pools/tank": "Medien\x00"})
	resetPaths(t)
	c := rpcClient(t, d) // initPaths: nicht eingerichtet
	var l docker.FolderList
	if err := c.Call(context.Background(), "folder.list", map[string]string{"path": "/srv/pools/tank"}, &l); err != nil || len(l.Dirs) != 1 {
		t.Fatalf("%+v %v", l, err)
	}
}

func TestPickerBrowsesFilesystemAndCreatesDockerdata(t *testing.T) {
	d, f := folderEnv(t, map[string]string{"/srv": "/srv", "/srv/dockerdata": "/srv/dockerdata", "/mnt": "/mnt", "/mnt/ssd": "/mnt/ssd"},
		map[string]string{"/srv": "appdata\x00pools\x00dockerdata\x00", "/mnt/ssd": "daten\x00"})
	ctx := context.Background()
	l, err := ListFolders(ctx, d, "/srv")
	if err != nil || l.Parent != "" || len(l.Dirs) != 3 {
		t.Fatalf("%+v %v", l, err)
	}
	if _, err := ListFolders(ctx, d, "/mnt/ssd"); err != nil {
		t.Fatal(err)
	}
	if got, err := CreateFolder(ctx, d, "/srv", "dockerdata"); err != nil || got.Path != "/srv/dockerdata" {
		t.Fatalf("%+v %v", got, err)
	}
	if !f.Called("mkdir -m 755 -- /srv/dockerdata") {
		t.Fatalf("%v", f.Calls)
	}
	for _, p := range []string{"/etc", "/var/lib", "/usr", "/"} {
		if _, err := ListFolders(ctx, d, p); err == nil {
			t.Errorf("%s darf nicht listbar sein", p)
		}
	}
}
