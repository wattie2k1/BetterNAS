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
