package shares

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/storage"
)

var update = flag.Bool("update", false, "Golden-Files aktualisieren")

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", "golden", name)
	if *update {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (mit -update erzeugen)", err)
	}
	if string(want) != string(got) {
		t.Fatalf("%s differs:\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

func TestRenderGolden(t *testing.T) {
	files, err := Render(State{Shares: testShares()})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"filme.conf", "oeffentlich.conf", "privat.conf", "shares.conf"}
	if len(files) != len(want) {
		t.Fatalf("erwartet %v, bekommen %d Dateien", want, len(files))
	}
	for _, name := range want {
		content, ok := files[name]
		if !ok {
			t.Fatalf("%s fehlt", name)
		}
		if !strings.HasPrefix(string(content), storage.ManagedHeader+"\n") {
			t.Fatalf("%s: Kopfzeile fehlt", name)
		}
		golden(t, name, content)
	}
}

func TestRenderEmptyStillWritesIncludeList(t *testing.T) {
	files, err := Render(State{})
	if err != nil || len(files) != 1 {
		t.Fatalf("%v %v", files, err)
	}
	if _, ok := files["shares.conf"]; !ok {
		t.Fatal("shares.conf fehlt")
	}
}

func TestRenderRejectsInvalid(t *testing.T) {
	sh := testShares()[0]
	sh.Subdir = "../x"
	if _, err := Render(State{Shares: []Share{sh}}); err == nil {
		t.Fatal("ungültige Freigabe darf nicht gerendert werden")
	}
}

func nfsShares() State {
	st := State{Shares: testShares()}
	st.Shares[0].NFS = NFS{Enabled: true, Clients: []string{"192.168.1.0/24", "nas.local"}, Squash: "root"}
	st.Shares[1].NFS = NFS{Enabled: true, Clients: []string{"10.0.0.5"}, ReadOnly: true, Squash: "all"}
	return st
}

func TestRenderExportsGolden(t *testing.T) {
	out := RenderExports(nfsShares())
	if !strings.HasPrefix(string(out), storage.ManagedHeader+"\n") {
		t.Fatal("Kopfzeile fehlt")
	}
	golden(t, "betternas.exports", out)
	if strings.Contains(string(out), "/srv/pools/daten/Privat") {
		t.Error("Freigabe ohne NFS darf nicht exportiert werden")
	}
}

func TestRenderExportsEmpty(t *testing.T) {
	if got := string(RenderExports(State{Shares: testShares()})); got != storage.ManagedHeader+"\n" {
		t.Fatalf("%q", got)
	}
}

func TestRenderExportsEscapesSpacesInPath(t *testing.T) {
	s := testShares()[0]
	s.Subdir = "Meine Filme"
	s.NFS = NFS{Enabled: true, Clients: []string{"nas.local"}, Squash: "root"}
	got := string(RenderExports(State{Shares: []Share{s}}))
	if !strings.Contains(got, `/srv/pools/media/Meine\040Filme nas.local(`) {
		t.Fatalf("%s", got)
	}
}
