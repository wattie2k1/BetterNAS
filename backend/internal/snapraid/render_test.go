package snapraid

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func mediaState() State {
	return State{Pools: []PoolConfig{PoolConfig{Pool: "media", Data: []string{"d1", "d2", "d3"}, Parity: []string{"p1", "p2"}}.WithDefaults()}}
}

func TestRenderGolden(t *testing.T) {
	files, err := Render(mediaState())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 {
		t.Fatalf("want 4 files, got %d: %v", len(files), files)
	}
	for p, content := range files {
		if !strings.HasPrefix(string(content), ManagedHeader+"\n") {
			t.Fatalf("%s: missing managed header", p)
		}
		golden(t, filepath.Base(p), content)
	}
	conf := string(files[ConfPath("media")])
	for _, want := range []string{
		"parity /srv/parity/p1/snapraid.parity",
		"2-parity /srv/parity/p2/snapraid.2-parity",
		"content /var/lib/betternas/snapraid/media.content",
		"content /srv/disks/d1/.snapraid.content",
		"data d2 /srv/disks/d2/",
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("conf misses %q", want)
		}
	}
	if !strings.Contains(string(files[TimerPath("sync", "media")]), "Unit=betternas-snapraid-sync@media.service") {
		t.Error("sync timer unit")
	}
}

func TestRenderContentSingleDataDisk(t *testing.T) {
	files, err := Render(State{Pools: []PoolConfig{PoolConfig{Pool: "solo", Data: []string{"d1"}, Parity: []string{"p1"}}.WithDefaults()}})
	if err != nil {
		t.Fatal(err)
	}
	conf := string(files[ConfPath("solo")])
	for _, want := range []string{"content /srv/disks/d1/.snapraid.content", "content /srv/parity/p1/.snapraid.content", "content /var/lib/betternas/snapraid/solo.content"} {
		if !strings.Contains(conf, want) {
			t.Errorf("conf misses %q:\n%s", want, conf)
		}
	}
}

func TestRenderRejectsInvalid(t *testing.T) {
	if _, err := Render(State{Pools: []PoolConfig{PoolConfig{Pool: "x", Data: []string{"d1"}}}}); err == nil {
		t.Fatal("invalid state rendered")
	}
}
