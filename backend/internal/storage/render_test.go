package storage

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
)

var update = flag.Bool("update", false, "Golden-Files aktualisieren")

func TestEscapePath(t *testing.T) {
	cases := map[string]string{
		"/srv/disks/WDC_WD40EFRX-68N32N0_WD-WCC7K1234567": `srv-disks-WDC_WD40EFRX\x2d68N32N0_WD\x2dWCC7K1234567`,
		"/srv/pools/media":                  "srv-pools-media",
		"/srv/disks/.x":                     "srv-disks-.x", // Referenz: systemd-escape --path
		"/.x":                               `\x2ex`,
		"/srv/disks/wwn-0x50014ee2b5d1a2c3": `srv-disks-wwn\x2d0x50014ee2b5d1a2c3`,
	}
	for in, want := range cases {
		if got := EscapePath(in); got != want {
			t.Errorf("%s: got %s want %s", in, got, want)
		}
	}
	if UnitName("/srv/pools/media") != "srv-pools-media.mount" {
		t.Fatal(UnitName("/srv/pools/media"))
	}
}

func testState() State {
	return State{
		Disks: []DiskMount{
			{DiskID: "wwn-0x50014ee2b5d1a2c3", FSUUID: "11111111-1111-1111-1111-111111111111", FSType: "ext4"},
			{DiskID: "WDC_WD40EFRX-68N32N0_WD-WCC7K1234567", FSUUID: "22222222-2222-2222-2222-222222222222", FSType: "xfs"},
		},
		Pools: []Pool{{Name: "media", Branches: []string{"wwn-0x50014ee2b5d1a2c3", "WDC_WD40EFRX-68N32N0_WD-WCC7K1234567"}, CreatePolicy: "epmfs", MinFree: "20G"}},
	}
}

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
	units, err := RenderUnits(testState())
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 3 {
		t.Fatalf("want 3 units, got %d", len(units))
	}
	for name, content := range units {
		if !strings.HasPrefix(string(content), ManagedHeader+"\n") {
			t.Fatalf("%s: missing managed header", name)
		}
		golden(t, name, content)
	}
}

func TestPoolUsesWantsNotRequires(t *testing.T) {
	units, _ := RenderUnits(testState())
	pool := string(units["srv-pools-media.mount"])
	if !strings.Contains(pool, "Wants=") || strings.Contains(pool, "Requires=") {
		t.Fatal(pool)
	}
	if !strings.Contains(pool, "What=/srv/disks/wwn-0x50014ee2b5d1a2c3:/srv/disks/WDC_WD40EFRX-68N32N0_WD-WCC7K1234567") {
		t.Fatal(pool)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]func(*State){
		"branch ohne disk": func(s *State) { s.Pools[0].Branches = append(s.Pools[0].Branches, "fehlt") },
		"disk in zwei pools": func(s *State) {
			s.Pools = append(s.Pools, Pool{Name: "b", Branches: []string{"wwn-0x50014ee2b5d1a2c3"}, CreatePolicy: "mfs", MinFree: "1G"})
		},
		"policy":            func(s *State) { s.Pools[0].CreatePolicy = "rand" },
		"minfree injection": func(s *State) { s.Pools[0].MinFree = "20G,allow_other" },
		"name traversal":    func(s *State) { s.Pools[0].Name = "../etc" },
		"name gross":        func(s *State) { s.Pools[0].Name = "Media" },
		"name doppelpunkt":  func(s *State) { s.Pools[0].Name = "a:b" },
		"leere branches":    func(s *State) { s.Pools[0].Branches = nil },
		"doppelter pool":    func(s *State) { s.Pools = append(s.Pools, Pool{Name: "media", Branches: nil}) },
		"disk-id unsicher":  func(s *State) { s.Disks[0].DiskID = "a:b"; s.Pools[0].Branches[0] = "a:b" },
		"fstype":            func(s *State) { s.Disks[0].FSType = "ntfs" },
		"uuid":              func(s *State) { s.Disks[0].FSUUID = "x y" },
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			s := testState()
			mod(&s)
			err := s.Validate()
			var ae *apierr.Error
			if !errors.As(err, &ae) || ae.Code != apierr.InvalidParams {
				t.Fatalf("got %v", err)
			}
		})
	}
	if err := testState().Validate(); err != nil {
		t.Fatal(err)
	}
}

// Kehrt eine Platte zurück (USB neu verbunden, später eingeschaltet), soll systemd sie
// und ihren Pool von selbst wieder einhängen.
func TestUnitsRemountWhenDeviceReturns(t *testing.T) {
	units, _ := RenderUnits(testState())
	disk := string(units[`srv-disks-wwn\x2d0x50014ee2b5d1a2c3.mount`])
	if !strings.Contains(disk, "WantedBy=multi-user.target "+`dev-disk-by\x2duuid-11111111\x2d1111\x2d1111\x2d1111\x2d111111111111.device`+"\n") {
		t.Fatalf("Platten-Unit wird beim Erscheinen des Geräts nicht gestartet:\n%s", disk)
	}
	pool := string(units["srv-pools-media.mount"])
	want := "WantedBy=multi-user.target " + `srv-disks-wwn\x2d0x50014ee2b5d1a2c3.mount srv-disks-WDC_WD40EFRX\x2d68N32N0_WD\x2dWCC7K1234567.mount` + "\n"
	if !strings.Contains(pool, want) {
		t.Fatalf("Pool wird beim Einhängen seiner Platten nicht gestartet:\n%s", pool)
	}
}

func parityState() State {
	st := testState()
	st.Disks = append(st.Disks, DiskMount{DiskID: "p1", FSUUID: "33333333-3333-3333-3333-333333333333", FSType: "ext4", Role: "parity"})
	return st
}

func TestRenderParityUnit(t *testing.T) {
	units, err := RenderUnits(parityState())
	if err != nil {
		t.Fatal(err)
	}
	got, ok := units["srv-parity-p1.mount"]
	if !ok {
		t.Fatalf("no parity unit: %v", units)
	}
	if !strings.Contains(string(got), "Where=/srv/parity/p1") || !strings.Contains(string(got), "Paritätsplatte p1") {
		t.Fatalf("%s", got)
	}
	golden(t, "srv-parity-p1.mount", got)
	if _, ok := units["srv-disks-p1.mount"]; ok {
		t.Fatal("parity disk also rendered as data disk")
	}
}

func TestValidateRejectsParityInPool(t *testing.T) {
	st := parityState()
	st.Pools[0].Branches = append(st.Pools[0].Branches, "p1")
	if err := st.Validate(); err == nil {
		t.Fatal("parity disk in pool accepted")
	}
	st = parityState()
	st.Disks[2].Role = "bogus"
	if err := st.Validate(); err == nil {
		t.Fatal("unknown role accepted")
	}
}

func TestPoolUnitHasNfsOptions(t *testing.T) {
	files, err := RenderUnits(testState())
	if err != nil {
		t.Fatal(err)
	}
	unit := string(files[UnitName(PoolPath("media"))])
	for _, want := range []string{"noforget", "inodecalc=path-hash", "nfsopenhack=all"} {
		if !strings.Contains(unit, want) {
			t.Errorf("Pool-Unit ohne %s:\n%s", want, unit)
		}
	}
}
