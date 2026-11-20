package storage

import (
	"strings"
	"testing"
	"time"
)

func btrfsState() State {
	const u = "33333333-3333-3333-3333-333333333333"
	return State{
		Disks: []DiskMount{
			{DiskID: "a", FSUUID: u, FSType: "btrfs"},
			{DiskID: "b", FSUUID: u, FSType: "btrfs"},
			{DiskID: "c", FSUUID: "22222222-2222-2222-2222-222222222222", FSType: "ext4"},
		},
		Pools: []Pool{{Name: "tresor", Type: PoolBtrfs, Branches: []string{"a", "b"}, Profile: "raid1", FSUUID: u, CreatePolicy: DefaultPolicy, MinFree: DefaultMinFree}},
	}
}

func TestBtrfsValidate(t *testing.T) {
	if err := btrfsState().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*State){
		"Profil":       func(s *State) { s.Pools[0].Profile = "raid5" },
		"mindestens":   func(s *State) { s.Pools[0].Profile = "raid1c3" },
		"UUID":         func(s *State) { s.Pools[0].FSUUID = "" },
		"Kompression":  func(s *State) { s.Pools[0].Compress = "lzo" },
		"gehört nicht": func(s *State) { s.Disks[1].FSUUID = "44444444-4444-4444-4444-444444444444" },
		"Typ":          func(s *State) { s.Pools[0].Type = "zfs" },
	}
	for want, mod := range cases {
		s := btrfsState()
		mod(&s)
		err := s.Validate()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
	}
}

func TestBtrfsMinDisks(t *testing.T) {
	want := map[string]int{"single": 1, "raid1": 2, "raid1c3": 3, "raid10": 4, "raid5": 0}
	for p, n := range want {
		if BtrfsMinDisks(p) != n {
			t.Errorf("%s: %d", p, BtrfsMinDisks(p))
		}
	}
}

func TestRenderBtrfsPool(t *testing.T) {
	units, err := RenderUnits(btrfsState())
	if err != nil {
		t.Fatal(err)
	}
	// Mitglieder haben keine eigene Unit; die Einzelplatte schon.
	if len(units) != 2 {
		t.Fatalf("%d Units", len(units))
	}
	if _, ok := units[UnitName(DiskPath("a"))]; ok {
		t.Fatal("Mitglied darf keine eigene Unit haben")
	}
	u := string(units["srv-pools-tresor.mount"])
	for _, want := range []string{ManagedHeader, "Type=btrfs", "Where=/srv/pools/tresor", "What=/dev/disk/by-uuid/33333333-3333-3333-3333-333333333333", "Options=noatime,nofail\n", ".device"} {
		if !strings.Contains(u, want) {
			t.Errorf("fehlt %q in:\n%s", want, u)
		}
	}
	if strings.Contains(u, "compress") || strings.Contains(u, "Wants=") {
		t.Errorf("unerwartet:\n%s", u)
	}
	s := btrfsState()
	s.Pools[0].Compress = "zstd:3"
	units, _ = RenderUnits(s)
	if !strings.Contains(string(units["srv-pools-tresor.mount"]), "Options=noatime,nofail,compress=zstd:3") {
		t.Fatalf("%s", units["srv-pools-tresor.mount"])
	}
}

// previewAndCreate legt einen btrfs-Pool aus d1 und d2 an.
func (e *env) btrfsInput(t *testing.T, profile string) PoolInput {
	t.Helper()
	p, err := e.m.PreviewBtrfsPool(e.ctx, []string{"d1", "d2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Disks) != 2 || p.ConfirmToken == "" {
		t.Fatalf("%+v", p)
	}
	return PoolInput{Name: "tresor", Type: PoolBtrfs, Branches: []string{"d1", "d2"}, Profile: profile, ConfirmToken: p.ConfirmToken, ConfirmName: "tresor"}
}

func TestCreateBtrfsPool(t *testing.T) {
	e := newEnv(t)
	in := e.btrfsInput(t, "raid1")
	jid, err := e.m.CreatePool(e.ctx, "anna", in)
	e.mustSucceed(t, jid, err)

	if len(e.agent.btrfsReqs) != 1 || e.agent.btrfsReqs[0].Profile != "raid1" || len(e.agent.btrfsReqs[0].Disks) != 2 || e.agent.btrfsReqs[0].Disks[0].Device != "/dev/sda" {
		t.Fatalf("%+v", e.agent.btrfsReqs)
	}
	last := e.agent.applied[len(e.agent.applied)-1]
	if len(last.Pools) != 1 || last.Pools[0].FSUUID != "btrfs-uuid-1" || len(last.Disks) != 2 || last.Disks[0].FSType != "btrfs" {
		t.Fatalf("%+v", last)
	}
	pools, _ := e.m.ListPools(e.ctx)
	if len(pools) != 1 || pools[0].Type != PoolBtrfs || pools[0].Profile != "raid1" || pools[0].Path != "/srv/pools/tresor" {
		t.Fatalf("%+v", pools)
	}
	disks, _ := e.m.ListDisks(e.ctx)
	for _, d := range disks {
		if (d.ID == "d1" || d.ID == "d2") && (d.Pool != "tresor" || d.Role != "data") {
			t.Fatalf("%+v", d)
		}
	}
	// Das Token ist verbraucht.
	if _, err := e.m.CreatePool(e.ctx, "anna", in); codeOf(err) != "conflict" {
		t.Fatalf("zweites Anlegen: %v", err)
	}
}

func TestCreateBtrfsPoolChecks(t *testing.T) {
	e := newEnv(t)
	in := e.btrfsInput(t, "raid1")

	bad := in
	bad.ConfirmName = "falsch"
	if _, err := e.m.CreatePool(e.ctx, "anna", bad); codeOf(err) != "invalid_params" {
		t.Fatalf("Name: %v", err)
	}
	bad = in
	bad.Profile = "raid1c3"
	if _, err := e.m.CreatePool(e.ctx, "anna", bad); codeOf(err) != "invalid_params" {
		t.Fatalf("Profil: %v", err)
	}
	bad = in
	bad.Branches = []string{"d1", "d3"}
	if _, err := e.m.CreatePool(e.ctx, "anna", bad); codeOf(err) != "conflict" {
		t.Fatalf("Auswahl: %v", err)
	}
	bad = in
	bad.ConfirmToken = "unbekannt"
	if _, err := e.m.CreatePool(e.ctx, "anna", bad); codeOf(err) != "conflict" {
		t.Fatalf("Token: %v", err)
	}
	// Gerät hat sich geändert.
	e.agent.disks[1].Device = "/dev/sdz"
	if _, err := e.m.CreatePool(e.ctx, "anna", in); codeOf(err) != "conflict" {
		t.Fatalf("Gerät: %v", err)
	}
	if len(e.agent.btrfsReqs) != 0 {
		t.Fatal("der Agent darf nichts angefasst haben")
	}
}

func TestCreateBtrfsPoolTokenExpires(t *testing.T) {
	e := newEnv(t)
	in := e.btrfsInput(t, "single")
	e.m.now = func() time.Time { return time.Now().Add(10 * time.Minute) }
	if _, err := e.m.CreatePool(e.ctx, "anna", in); codeOf(err) != "conflict" {
		t.Fatalf("%v", err)
	}
}

func TestPreviewBtrfsPoolRejects(t *testing.T) {
	e := newEnv(t)
	for name, ids := range map[string][]string{"leer": {}, "doppelt": {"d1", "d1"}, "System": {"sys", "d1"}, "unbekannt": {"gibtsnicht"}} {
		if _, err := e.m.PreviewBtrfsPool(e.ctx, ids); err == nil {
			t.Errorf("%s: kein Fehler", name)
		}
	}
	// Bereits verwaltete Platte
	e.formatDisk(t, "d1", "SER1")
	if _, err := e.m.PreviewBtrfsPool(e.ctx, []string{"d1", "d2"}); codeOf(err) != "conflict" {
		t.Fatalf("%v", err)
	}
}

func TestBtrfsPoolRestrictions(t *testing.T) {
	e := newEnv(t)
	jid, err := e.m.CreatePool(e.ctx, "anna", e.btrfsInput(t, "raid1"))
	e.mustSucceed(t, jid, err)

	if _, err := e.m.EnableSnapraid(e.ctx, "anna", "tresor", SnapraidInput{}); codeOf(err) != "conflict" {
		t.Fatalf("SnapRAID: %v", err)
	}
	if _, err := e.m.UpdatePool(e.ctx, "anna", "tresor", PoolInput{Branches: []string{"d1"}}); codeOf(err) != "conflict" {
		t.Fatalf("Ändern: %v", err)
	}
	if _, err := e.m.Release(e.ctx, "anna", "d1"); codeOf(err) != "disk_in_use" {
		t.Fatalf("Freigeben: %v", err)
	}
	// Ein mergerfs-Pool darf keine Mitglieder übernehmen.
	if _, err := e.m.CreatePool(e.ctx, "anna", PoolInput{Name: "andere", Branches: []string{"d1"}}); codeOf(err) != "conflict" {
		t.Fatalf("mergerfs: %v", err)
	}
	if _, err := e.m.CreatePool(e.ctx, "anna", PoolInput{Name: "x", Type: "zfs", Branches: []string{"d3"}}); codeOf(err) != "invalid_params" {
		t.Fatalf("Typ: %v", err)
	}
}

func TestDeleteBtrfsPoolReleasesDisks(t *testing.T) {
	e := newEnv(t)
	jid, err := e.m.CreatePool(e.ctx, "anna", e.btrfsInput(t, "raid1"))
	e.mustSucceed(t, jid, err)

	if _, err := e.m.DeletePool(e.ctx, "anna", "tresor"); codeOf(err) != "invalid_params" {
		t.Fatalf("ohne Bestätigung: %v", err)
	}
	if _, err := e.m.DeletePoolConfirm(e.ctx, "anna", "tresor", "falsch"); codeOf(err) != "invalid_params" {
		t.Fatalf("falsche Bestätigung: %v", err)
	}
	jid, err = e.m.DeletePoolConfirm(e.ctx, "anna", "tresor", "tresor")
	e.mustSucceed(t, jid, err)

	last := e.agent.applied[len(e.agent.applied)-1]
	if len(last.Pools) != 0 || len(last.Disks) != 0 {
		t.Fatalf("%+v", last)
	}
	disks, _ := e.m.ListDisks(e.ctx)
	for _, d := range disks {
		if d.ID == "d1" && (d.Role != "unassigned" || d.Pool != "") {
			t.Fatalf("%+v", d)
		}
	}
}
