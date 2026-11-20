package storage

import (
	"strings"
	"testing"
)

// maintPool legt einen btrfs-Pool aus d1 und d2 an und stellt einen passenden Agent-Status ein
// (eingehängt, beide Platten da; die Partitionen verbinden Geräte mit Kennungen).
func (e *env) maintPoolOf(t *testing.T, profile string) {
	t.Helper()
	jid, err := e.m.CreatePool(e.ctx, "anna", e.btrfsInput(t, profile))
	e.mustSucceed(t, jid, err)
	e.agent.disks[1].Partitions = []Partition{{Device: "/dev/sda1", FSType: "btrfs", Mountpoints: []string{}}}
	e.agent.disks[2].Partitions = []Partition{{Device: "/dev/sdb1", FSType: "btrfs", Mountpoints: []string{}}}
	e.agent.btrfsStatus = &BtrfsStatus{Mounted: true, Devices: []BtrfsDevice{
		{DevID: 1, Path: "/dev/sda1", SizeBytes: 100}, {DevID: 2, Path: "/dev/sdb1", SizeBytes: 100},
	}}
	e.agent.maint = nil
}

// newDiskInput liefert die Bestätigung für das Löschen und Aufnehmen der Platte id.
func (e *env) newDiskInput(t *testing.T, id string) BtrfsDiskInput {
	t.Helper()
	p, err := e.m.PreviewBtrfsPool(e.ctx, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	return BtrfsDiskInput{DiskID: id, ConfirmToken: p.ConfirmToken, ConfirmName: "tresor"}
}

func (e *env) poolBranches(t *testing.T) []string {
	t.Helper()
	pools, err := e.m.ListPools(e.ctx)
	if err != nil || len(pools) != 1 {
		t.Fatalf("%v %+v", err, pools)
	}
	return pools[0].Branches
}

func (e *env) maintHas(sub string) bool {
	for _, c := range e.agent.maint {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

func TestParseBtrfsReplace(t *testing.T) {
	cases := map[string]BtrfsReplace{
		"Never started": {State: "never"},
		"":              {State: "never"},
		"12.3% done, 1 write errs, 2 uncorr. read errs":                                                      {State: "running", Percent: 12.3, WriteErrs: 1, ReadErrs: 2},
		"Started on 20.Nov 10:00:00, finished on 20.Nov 10:00:05, 0 write errs, 0 uncorr. read errs":         {State: "finished", Percent: 100},
		"Started on 20.Nov 10:00:00, canceled on 20.Nov 10:00:05 at 3.1%, 0 write errs, 4 uncorr. read errs": {State: "canceled", ReadErrs: 4},
	}
	for in, want := range cases {
		if got := ParseBtrfsReplace(in); got != want {
			t.Errorf("%q: %+v statt %+v", in, got, want)
		}
	}
}

func TestParseBtrfsBalanceProgress(t *testing.T) {
	if p, ok := ParseBtrfsBalanceProgress("Balance on '/srv/pools/x' is running\n3 out of about 10 chunks balanced (4 considered),  70% left\n"); !ok || p != 30 {
		t.Fatalf("%d %v", p, ok)
	}
	if p, ok := ParseBtrfsBalanceProgress("Balance on '/x' is running\n"); !ok || p != 0 {
		t.Fatalf("%d %v", p, ok)
	}
	if _, ok := ParseBtrfsBalanceProgress("No balance found on '/x'\n"); ok {
		t.Fatal("keine Balance aktiv")
	}
}

func TestReplaceBtrfsDisk(t *testing.T) {
	e := newEnv(t)
	e.maintPoolOf(t, "raid1")
	in := e.newDiskInput(t, "d3")
	in.OldDiskID = "d1"
	jid, err := e.m.ReplaceBtrfsDisk(e.ctx, "anna", "tresor", in)
	e.mustSucceed(t, jid, err)

	if !e.maintHas(`pool.btrfs.replace.start {"name":"tresor"`) || !e.maintHas(`"devId":1`) || !e.maintHas(`"diskId":"d3"`) || !e.maintHas("pool.btrfs.resize") {
		t.Fatalf("%v", e.agent.maint)
	}
	if b := e.poolBranches(t); len(b) != 2 || b[0] != "d3" || b[1] != "d2" {
		t.Fatalf("%v", b)
	}
	disks, _ := e.m.ListDisks(e.ctx)
	for _, d := range disks {
		if d.ID == "d1" && d.Role != "unassigned" {
			t.Fatalf("alte Platte muss frei sein: %+v", d)
		}
		if d.ID == "d3" && d.Pool != "tresor" {
			t.Fatalf("neue Platte muss im Pool sein: %+v", d)
		}
	}
	last := e.agent.applied[len(e.agent.applied)-1]
	if len(last.Disks) != 2 || last.Pools[0].Branches[0] != "d3" {
		t.Fatalf("%+v", last)
	}
}

func TestReplaceBtrfsDiskRejects(t *testing.T) {
	e := newEnv(t)
	e.maintPoolOf(t, "raid1")
	good := e.newDiskInput(t, "d3")
	good.OldDiskID = "d1"

	bad := good
	bad.OldDiskID = "d3"
	if _, err := e.m.ReplaceBtrfsDisk(e.ctx, "anna", "tresor", bad); codeOf(err) != "invalid_params" {
		t.Fatalf("kein Mitglied: %v", err)
	}
	bad = good
	bad.ConfirmName = "x"
	if _, err := e.m.ReplaceBtrfsDisk(e.ctx, "anna", "tresor", bad); codeOf(err) != "invalid_params" {
		t.Fatalf("Name: %v", err)
	}
	bad = good
	bad.ConfirmToken = "unbekannt"
	if _, err := e.m.ReplaceBtrfsDisk(e.ctx, "anna", "tresor", bad); codeOf(err) != "conflict" {
		t.Fatalf("Token: %v", err)
	}
	e.agent.btrfsStatus.Mounted = false
	if _, err := e.m.ReplaceBtrfsDisk(e.ctx, "anna", "tresor", good); codeOf(err) != "conflict" {
		t.Fatalf("nicht eingehängt: %v", err)
	}
	e.agent.btrfsStatus.Mounted = true
	e.agent.btrfsStatus.Op = &BtrfsOp{Op: "balance", Running: true}
	if _, err := e.m.ReplaceBtrfsDisk(e.ctx, "anna", "tresor", good); codeOf(err) != "conflict" {
		t.Fatalf("Wartung läuft: %v", err)
	}
	if len(e.agent.maint) != 0 {
		t.Fatalf("nichts darf gestartet sein: %v", e.agent.maint)
	}
}

func TestReplaceMissingDiskUsesItsDevID(t *testing.T) {
	e := newEnv(t)
	e.maintPoolOf(t, "raid1")
	// d1 fehlt: btrfs listet devid 1 als fehlend ohne Pfad.
	e.agent.btrfsStatus = &BtrfsStatus{Mounted: true, Missing: 1, Devices: []BtrfsDevice{{DevID: 1, Missing: true}, {DevID: 2, Path: "/dev/sdb1"}}}
	in := e.newDiskInput(t, "d3")
	in.OldDiskID = "d1"
	jid, err := e.m.ReplaceBtrfsDisk(e.ctx, "anna", "tresor", in)
	e.mustSucceed(t, jid, err)
	if !e.maintHas(`"devId":1`) {
		t.Fatalf("%v", e.agent.maint)
	}
}

func TestReplaceFailureKeepsState(t *testing.T) {
	e := newEnv(t)
	e.maintPoolOf(t, "raid1")
	e.agent.replaceStatuses = []BtrfsReplace{{State: "running", Percent: 40}, {State: "canceled"}}
	in := e.newDiskInput(t, "d3")
	in.OldDiskID = "d1"
	jid, err := e.m.ReplaceBtrfsDisk(e.ctx, "anna", "tresor", in)
	if err != nil {
		t.Fatal(err)
	}
	if j := e.wait(t, jid); j.Status != "failed" || !strings.Contains(j.Log, "abgebrochen") {
		t.Fatalf("%+v", j)
	}
	if b := e.poolBranches(t); b[0] != "d1" {
		t.Fatalf("Zustand muss unverändert bleiben: %v", b)
	}
	// Die Reservierung ist wieder frei.
	if e.m.isBtrfsBusy("tresor") {
		t.Fatal("Pool noch als belegt markiert")
	}
}

func TestAddBtrfsDisk(t *testing.T) {
	e := newEnv(t)
	e.maintPoolOf(t, "raid1")
	in := e.newDiskInput(t, "d3")
	in.Balance = true
	jid, err := e.m.AddBtrfsDisk(e.ctx, "anna", "tresor", in)
	e.mustSucceed(t, jid, err)
	if !e.maintHas("pool.btrfs.device.add") || !e.maintHas(`pool.btrfs.op.start {"name":"tresor","op":"balance"}`) {
		t.Fatalf("%v", e.agent.maint)
	}
	if b := e.poolBranches(t); len(b) != 3 || b[2] != "d3" {
		t.Fatalf("%v", b)
	}
}

func TestAddBtrfsDiskRejectsDegraded(t *testing.T) {
	e := newEnv(t)
	e.maintPoolOf(t, "raid1")
	e.agent.btrfsStatus.Missing = 1
	if _, err := e.m.AddBtrfsDisk(e.ctx, "anna", "tresor", e.newDiskInput(t, "d3")); codeOf(err) != "conflict" {
		t.Fatalf("%v", err)
	}
}

func TestRemoveBtrfsDisk(t *testing.T) {
	e := newEnv(t)
	e.maintPoolOf(t, "single")
	if _, err := e.m.RemoveBtrfsDisk(e.ctx, "anna", "tresor", BtrfsDiskInput{DiskID: "d2"}); codeOf(err) != "invalid_params" {
		t.Fatalf("ohne Bestätigung: %v", err)
	}
	jid, err := e.m.RemoveBtrfsDisk(e.ctx, "anna", "tresor", BtrfsDiskInput{DiskID: "d2", ConfirmName: "tresor"})
	e.mustSucceed(t, jid, err)
	if !e.maintHas(`"op":"remove"`) || !e.maintHas(`"devId":2`) {
		t.Fatalf("%v", e.agent.maint)
	}
	if b := e.poolBranches(t); len(b) != 1 || b[0] != "d1" {
		t.Fatalf("%v", b)
	}
	// Das letzte Mitglied lässt sich nicht entfernen (Profil single braucht 1 Platte).
	if _, err := e.m.RemoveBtrfsDisk(e.ctx, "anna", "tresor", BtrfsDiskInput{DiskID: "d1", ConfirmName: "tresor"}); codeOf(err) != "invalid_params" {
		t.Fatalf("letzte Platte: %v", err)
	}
}

func TestRemoveBtrfsDiskRespectsProfileMinimum(t *testing.T) {
	e := newEnv(t)
	e.maintPoolOf(t, "raid1")
	_, err := e.m.RemoveBtrfsDisk(e.ctx, "anna", "tresor", BtrfsDiskInput{DiskID: "d2", ConfirmName: "tresor"})
	if codeOf(err) != "invalid_params" || !strings.Contains(err.Error(), "mindestens 2") {
		t.Fatalf("%v", err)
	}
}

func TestRemoveMissingDisk(t *testing.T) {
	e := newEnv(t)
	e.maintPoolOf(t, "single")
	// d2 fehlt und wird von btrfs nicht einmal mehr gelistet.
	e.agent.btrfsStatus = &BtrfsStatus{Mounted: true, Missing: 1, Devices: []BtrfsDevice{{DevID: 1, Path: "/dev/sda1"}}}
	jid, err := e.m.RemoveBtrfsDisk(e.ctx, "anna", "tresor", BtrfsDiskInput{DiskID: "d2", ConfirmName: "tresor"})
	e.mustSucceed(t, jid, err)
	if !e.maintHas(`"missing":true`) {
		t.Fatalf("%v", e.agent.maint)
	}
}

func TestConvertBtrfsProfile(t *testing.T) {
	e := newEnv(t)
	e.maintPoolOf(t, "raid1")
	// weniger Redundanz: Bestätigung nötig
	if _, err := e.m.ConvertBtrfsProfile(e.ctx, "anna", "tresor", "single", ""); codeOf(err) != "invalid_params" {
		t.Fatalf("ohne Bestätigung: %v", err)
	}
	if _, err := e.m.ConvertBtrfsProfile(e.ctx, "anna", "tresor", "raid10", ""); codeOf(err) != "invalid_params" {
		t.Fatalf("zu wenige Platten: %v", err)
	}
	if _, err := e.m.ConvertBtrfsProfile(e.ctx, "anna", "tresor", "raid1", ""); codeOf(err) != "conflict" {
		t.Fatalf("gleiches Profil: %v", err)
	}
	jid, err := e.m.ConvertBtrfsProfile(e.ctx, "anna", "tresor", "single", "tresor")
	e.mustSucceed(t, jid, err)
	if !e.maintHas(`"profile":"single"`) || !e.maintHas(`"metadata":"raid1"`) {
		t.Fatalf("%v", e.agent.maint)
	}
	pools, _ := e.m.ListPools(e.ctx)
	if pools[0].Profile != "single" {
		t.Fatalf("%+v", pools[0])
	}
	// Zurück auf mehr Redundanz braucht keine Bestätigung.
	jid, err = e.m.ConvertBtrfsProfile(e.ctx, "anna", "tresor", "raid1", "")
	e.mustSucceed(t, jid, err)
}

func TestBalanceBtrfs(t *testing.T) {
	e := newEnv(t)
	e.maintPoolOf(t, "raid1")
	if _, err := e.m.BalanceBtrfs(e.ctx, "anna", "tresor", 101); codeOf(err) != "invalid_params" {
		t.Fatalf("%v", err)
	}
	jid, err := e.m.BalanceBtrfs(e.ctx, "anna", "tresor", 50)
	e.mustSucceed(t, jid, err)
	if !e.maintHas(`"op":"balance","usage":50`) {
		t.Fatalf("%v", e.agent.maint)
	}
	// Schlägt die Wartung fehl, steht die Meldung im Job.
	e.agent.opStatuses = []BtrfsOp{{Op: "balance", Running: true, Progress: 10}, {Op: "balance", Result: "failed", Message: "No space left"}}
	jid, err = e.m.BalanceBtrfs(e.ctx, "anna", "tresor", 0)
	if err != nil {
		t.Fatal(err)
	}
	if j := e.wait(t, jid); j.Status != "failed" || !strings.Contains(j.Log, "No space left") {
		t.Fatalf("%+v", j)
	}
}

func TestMaintenanceBlocksParallelRunsAndDelete(t *testing.T) {
	e := newEnv(t)
	e.maintPoolOf(t, "raid1")
	release, err := e.m.beginBtrfs("tresor")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.BalanceBtrfs(e.ctx, "anna", "tresor", 0); codeOf(err) != "conflict" {
		t.Fatalf("parallel: %v", err)
	}
	if _, err := e.m.DeletePoolConfirm(e.ctx, "anna", "tresor", "tresor"); codeOf(err) != "conflict" {
		t.Fatalf("löschen: %v", err)
	}
	release()
	jid, err := e.m.DeletePoolConfirm(e.ctx, "anna", "tresor", "tresor")
	e.mustSucceed(t, jid, err)
}

func TestMountDegradedAndResetErrors(t *testing.T) {
	e := newEnv(t)
	e.maintPoolOf(t, "raid1")
	if err := e.m.MountBtrfsDegraded(e.ctx, "anna", "tresor"); err != nil {
		t.Fatal(err)
	}
	if err := e.m.ResetBtrfsErrors(e.ctx, "anna", "tresor"); err != nil {
		t.Fatal(err)
	}
	if !e.maintHas(`pool.btrfs.mountdegraded {"name":"tresor","fsUuid":"btrfs-uuid-1","compress":""}`) || !e.maintHas("pool.btrfs.resetstats") {
		t.Fatalf("%v", e.agent.maint)
	}
	if err := e.m.MountBtrfsDegraded(e.ctx, "anna", "gibtsnicht"); codeOf(err) != "not_found" {
		t.Fatalf("%v", err)
	}
}
