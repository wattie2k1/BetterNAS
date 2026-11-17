package storage

import (
	"slices"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/snapraid"
)

// replaceEnv: Pool media (d1) mit SnapRAID (Parität d2); d3 (4 TB, Datenplatte ohne Pool) wartet als Ersatz.
func replaceEnv(t *testing.T) *env {
	t.Helper()
	e := snapEnv(t)
	e.agent.disks[3].SizeBytes = 4e12 // d3 so groß wie d1
	e.importAs(t, "d3", "data")
	e.enable(t, "media", "d2")
	e.agent.autoRuns = true
	return e
}

func (e *env) branches(t *testing.T, pool string) []string {
	t.Helper()
	ps, err := e.m.loadPools(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.Name == pool {
			return p.Branches
		}
	}
	t.Fatal("Pool fehlt")
	return nil
}

func (e *env) idx(method string) int { return slices.Index(e.agent.calls, method) }

func TestReplaceDiskHappyPath(t *testing.T) {
	e := replaceEnv(t)
	e.agent.calls, e.agent.started, e.agent.applied, e.agent.snapApplied = nil, nil, nil, nil
	jid, err := e.m.ReplaceDisk(e.ctx, "anna", "media", "d1", "d3")
	e.mustSucceed(t, jid, err)

	// Reihenfolge: Marker → Speicher → SnapRAID → fix → sync
	order := []string{"snapraid.recovery.begin", "storage.apply", "snapraid.apply", "snapraid.start", "snapraid.start"}
	pos := -1
	for _, m := range order {
		i := slices.Index(e.agent.calls[pos+1:], m)
		if i < 0 {
			t.Fatalf("%s fehlt nach Position %d in %v", m, pos, e.agent.calls)
		}
		pos += i + 1
	}
	if got := e.agent.recovery[0]; got != "begin:media:d1" {
		t.Fatalf("%v", e.agent.recovery)
	}
	if len(e.agent.started) != 2 || e.agent.started[0]["action"] != "fix" || e.agent.started[0]["disk"] != "d1" || e.agent.started[1]["action"] != "sync" {
		t.Fatalf("%v", e.agent.started)
	}
	if got := e.branches(t, "media"); !slices.Equal(got, []string{"d3"}) {
		t.Fatalf("Branches %v", got)
	}
	if n := e.lastSnap().Pools[0]; !slices.Equal(n.Data, []string{"d3"}) || n.Names["d3"] != "d1" {
		t.Fatalf("SnapRAID-Zustand %+v", n)
	}
	st := e.agent.lastApplied()
	for _, d := range st.Disks {
		if d.DiskID == "d1" {
			t.Fatal("alte Platte noch im Speicherzustand")
		}
	}
	var n int
	e.m.db.QueryRow(`SELECT count(*) FROM disks WHERE id = 'd1'`).Scan(&n)
	if n != 0 {
		t.Fatal("alte Disk-Zeile nicht gelöscht")
	}
	list, _ := audit.List(e.ctx, e.m.db, 50)
	found := false
	for _, a := range list {
		if a.Action == "snapraid.replace" && a.Target == "media" && a.Actor == "anna" {
			found = true
		}
	}
	if !found {
		t.Fatal("kein Audit-Eintrag")
	}
}

func TestReplaceDiskRejects(t *testing.T) {
	e := replaceEnv(t)
	// zweiter Pool mit d3 → „schon in einem Pool“
	e.agent.disks = append(e.agent.disks, Disk{ID: "d5", Device: "/dev/sdd", Serial: "SER5", Model: "HDD5", SizeBytes: 4e12, Manageable: true,
		Partitions: []Partition{{Device: "/dev/sdd1", FSType: "ext4", UUID: "uuid-5", Label: "x", Mountpoints: []string{}}}})
	e.importAs(t, "d5", "data")
	jid, err := e.m.CreatePool(e.ctx, "anna", PoolInput{Name: "other", Branches: []string{"d5"}})
	e.mustSucceed(t, jid, err)
	// d3 so verkleinern, dass sie zu klein ist (4e12 → 1e12 in der DB)
	e.m.db.Exec(`INSERT INTO disks(id, role, fs_type, fs_uuid, label, model, serial, size_bytes, created_at) VALUES ('small','data','ext4','u','l','M','S',1000000000000,'now')`)
	e.agent.disks = append(e.agent.disks, Disk{ID: "small", Device: "/dev/sde", Serial: "S", SizeBytes: 1e12, Manageable: true, Partitions: []Partition{}})

	cases := []struct {
		name, pool, old, new string
		code                 string
	}{
		{"gleiche Platte", "media", "d1", "d1", apierr.InvalidParams},
		{"zu kleine Ersatzplatte", "media", "d1", "small", apierr.InvalidParams},
		{"Paritätsplatte als Ersatz", "media", "d1", "d2", apierr.InvalidParams},
		{"unbekannte Ersatzplatte", "media", "d1", "gibtsnicht", apierr.InvalidParams},
		{"Ersatzplatte schon in Pool", "media", "d1", "d5", apierr.Conflict},
		{"alte Platte gehört nicht zum Pool", "media", "d5", "d3", apierr.InvalidParams},
		{"Pool ohne SnapRAID", "other", "d5", "d3", apierr.Conflict},
		{"unbekannter Pool", "nope", "d1", "d3", apierr.NotFound},
	}
	beforeBranches := e.branches(t, "media")
	for _, c := range cases {
		e.agent.calls, e.agent.applied = nil, nil
		if _, err := e.m.ReplaceDisk(e.ctx, "anna", c.pool, c.old, c.new); codeOf(err) != c.code {
			t.Errorf("%s: erwartet %s, bekam %v", c.name, c.code, err)
		}
		if len(e.agent.applied) != 0 || slices.Contains(e.agent.calls, "snapraid.recovery.begin") {
			t.Errorf("%s: Agent wurde verändert aufgerufen: %v", c.name, e.agent.calls)
		}
	}
	if !slices.Equal(e.branches(t, "media"), beforeBranches) {
		t.Fatal("Branches verändert")
	}
	// offene Wiederherstellung → conflict
	e.agent.statuses = []snapraid.Status{{Enabled: true, Recovery: &snapraid.Recovery{Disk: "d1", State: "open"}}}
	if _, err := e.m.ReplaceDisk(e.ctx, "anna", "media", "d1", "d3"); codeOf(err) != apierr.Conflict {
		t.Fatalf("offene Wiederherstellung: %v", err)
	}
}

func TestReplaceDiskStorageApplyFailureLeavesDbUntouched(t *testing.T) {
	e := replaceEnv(t)
	e.agent.recovery = nil
	e.agent.applyErr = apierr.New(apierr.Internal, "storage kaputt")
	jid, err := e.m.ReplaceDisk(e.ctx, "anna", "media", "d1", "d3")
	if err != nil {
		t.Fatal(err)
	}
	if j := e.wait(t, jid); j.Status != "failed" {
		t.Fatalf("Job: %s", j.Status)
	}
	if !slices.Equal(e.branches(t, "media"), []string{"d1"}) {
		t.Fatal("DB verändert")
	}
	if got := e.agent.recovery; len(got) != 2 || got[0] != "begin:media:d1" || got[1] != "cancel:media" {
		t.Fatalf("Marker nicht zurückgenommen: %v", got)
	}
}

func TestReplaceDiskSnapraidApplyFailureRollsBack(t *testing.T) {
	e := replaceEnv(t)
	e.agent.recovery, e.agent.applied = nil, nil
	e.agent.snapErr = apierr.New(apierr.Internal, "snapraid kaputt")
	jid, _ := e.m.ReplaceDisk(e.ctx, "anna", "media", "d1", "d3")
	if j := e.wait(t, jid); j.Status != "failed" {
		t.Fatalf("Job: %s", j.Status)
	}
	if !slices.Equal(e.branches(t, "media"), []string{"d1"}) {
		t.Fatal("DB verändert")
	}
	// Speicherzustand wurde auf den alten zurückgesetzt (zwei storage.apply: neu, dann alt)
	if len(e.agent.applied) != 2 || !slices.Equal(findPool(e.agent.lastApplied(), "media").Branches, []string{"d1"}) {
		t.Fatalf("kein Rollback: %d Aufrufe", len(e.agent.applied))
	}
	if got := e.agent.recovery; got[len(got)-1] != "cancel:media" {
		t.Fatalf("%v", got)
	}
}

func TestReplaceDiskFixFailureKeepsNewState(t *testing.T) {
	e := replaceEnv(t)
	e.agent.fixResult = snapraid.ResultErrors
	e.agent.started, e.agent.recovery = nil, nil
	jid, _ := e.m.ReplaceDisk(e.ctx, "anna", "media", "d1", "d3")
	if j := e.wait(t, jid); j.Status != "failed" {
		t.Fatalf("Job: %s", j.Status)
	}
	if !slices.Equal(e.branches(t, "media"), []string{"d3"}) {
		t.Fatal("neuer Zustand muss bleiben")
	}
	if n := e.lastSnap().Pools[0]; n.Names["d3"] != "d1" {
		t.Fatalf("%+v", n)
	}
	if len(e.agent.started) != 1 {
		t.Fatalf("sync darf nach fehlgeschlagenem fix nicht laufen: %v", e.agent.started)
	}
	for _, r := range e.agent.recovery {
		if r == "cancel:media" {
			t.Fatal("Marker darf bei fix-Fehler nicht zurückgenommen werden")
		}
	}
}

func TestReplaceAliasChainKeepsOriginalName(t *testing.T) {
	e := replaceEnv(t)
	jid, err := e.m.ReplaceDisk(e.ctx, "anna", "media", "d1", "d3")
	e.mustSucceed(t, jid, err)
	e.agent.disks = append(e.agent.disks, Disk{ID: "d5", Device: "/dev/sdd", Serial: "SER5", Model: "HDD5", SizeBytes: 4e12, Manageable: true,
		Partitions: []Partition{{Device: "/dev/sdd1", FSType: "ext4", UUID: "uuid-5", Label: "x", Mountpoints: []string{}}}})
	e.importAs(t, "d5", "data")
	e.agent.statuses = nil
	jid, err = e.m.ReplaceDisk(e.ctx, "anna", "media", "d3", "d5")
	e.mustSucceed(t, jid, err)
	n := e.lastSnap().Pools[0]
	if !slices.Equal(n.Data, []string{"d5"}) || n.Names["d5"] != "d1" || len(n.Names) != 1 {
		t.Fatalf("%+v", n)
	}
	if got := e.agent.recovery[len(e.agent.recovery)-1]; got != "begin:media:d1" {
		t.Fatalf("zweiter Austausch muss den ursprünglichen Namen verwenden: %v", e.agent.recovery)
	}
}

func TestRetryFixUsesRecoveryDisk(t *testing.T) {
	e := replaceEnv(t)
	e.agent.statuses = []snapraid.Status{{Enabled: true, Recovery: &snapraid.Recovery{Disk: "old1", State: "open"}}}
	e.agent.started = nil
	jid, err := e.m.RetryFix(e.ctx, "anna", "media")
	e.mustSucceed(t, jid, err)
	if len(e.agent.started) != 2 || e.agent.started[0]["action"] != "fix" || e.agent.started[0]["disk"] != "old1" || e.agent.started[1]["action"] != "sync" {
		t.Fatalf("%v", e.agent.started)
	}
}

func TestRetryFixWithoutRecoveryConflicts(t *testing.T) {
	e := replaceEnv(t)
	if _, err := e.m.RetryFix(e.ctx, "anna", "media"); codeOf(err) != apierr.Conflict {
		t.Fatalf("%v", err)
	}
}

func TestCancelRecoveryCallsAgent(t *testing.T) {
	e := replaceEnv(t)
	e.agent.recovery = nil
	if err := e.m.CancelRecovery(e.ctx, "anna", "media"); err != nil {
		t.Fatal(err)
	}
	if len(e.agent.recovery) != 1 || e.agent.recovery[0] != "cancel:media" {
		t.Fatalf("%v", e.agent.recovery)
	}
	list, _ := audit.List(e.ctx, e.m.db, 20)
	found := false
	for _, a := range list {
		if a.Action == "snapraid.recovery.cancel" && a.Target == "media" {
			found = true
		}
	}
	if !found {
		t.Fatal("kein Audit-Eintrag")
	}
}
