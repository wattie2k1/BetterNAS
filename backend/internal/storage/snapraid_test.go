package storage

import (
	"strings"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/snapraid"
)

func (e *env) lastSnap() snapraid.State {
	e.agent.mu.Lock()
	defer e.agent.mu.Unlock()
	return e.agent.snapApplied[len(e.agent.snapApplied)-1]
}

// snapEnv: d1 (Daten, 4 TB) im Pool media, d2 (Parität, 4 TB); d3 (2 TB) wird bei Bedarf übernommen.
func snapEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	e.formatDisk(t, "d1", "SER1")
	p, err := e.m.PreviewFormat(e.ctx, "d2")
	if err != nil {
		t.Fatal(err)
	}
	jid, err := e.m.Format(e.ctx, "anna", "d2", FormatRequest{FSType: "ext4", Role: "parity", ConfirmSerial: "SER2", ConfirmToken: p.ConfirmToken})
	e.mustSucceed(t, jid, err)
	jid, err = e.m.CreatePool(e.ctx, "anna", PoolInput{Name: "media", Branches: []string{"d1"}})
	e.mustSucceed(t, jid, err)
	return e
}

func (e *env) importAs(t *testing.T, id, role string) {
	t.Helper()
	jid, err := e.m.Import(e.ctx, "anna", id, role)
	e.mustSucceed(t, jid, err)
}

func (e *env) enable(t *testing.T, pool string, parity ...string) {
	t.Helper()
	jid, err := e.m.EnableSnapraid(e.ctx, "anna", pool, SnapraidInput{Parity: parity})
	e.mustSucceed(t, jid, err)
}

func TestEnableSnapraidStoresAndApplies(t *testing.T) {
	e := snapEnv(t)
	e.enable(t, "media", "d2")
	st := e.lastSnap()
	if len(st.Pools) != 1 || st.Pools[0].Pool != "media" || strings.Join(st.Pools[0].Data, ",") != "d1" ||
		strings.Join(st.Pools[0].Parity, ",") != "d2" || st.Pools[0].SyncSchedule != snapraid.DefaultSync || st.Pools[0].DeleteThreshold != 500 {
		t.Fatalf("%+v", st)
	}
	pools, _ := e.m.ListPools(e.ctx)
	if pools[0].Snapraid == nil || strings.Join(pools[0].Snapraid.Parity, ",") != "d2" || pools[0].Snapraid.ScrubPercent != 8 {
		t.Fatalf("%+v", pools[0].Snapraid)
	}
	disks, _ := e.m.ListDisks(e.ctx)
	for _, d := range disks {
		if d.ID == "d2" && (d.Role != "parity" || d.Pool != "media") {
			t.Fatalf("%+v", d)
		}
	}
	// Ändern: eigene Einstellungen bleiben erhalten.
	jid, err := e.m.EnableSnapraid(e.ctx, "anna", "media", SnapraidInput{Parity: []string{"d2"}, Settings: snapraid.Settings{DeleteThreshold: 42, ScrubPercent: 20}})
	e.mustSucceed(t, jid, err)
	if got := e.lastSnap().Pools[0]; got.DeleteThreshold != 42 || got.ScrubPercent != 20 {
		t.Fatalf("%+v", got)
	}
}

func TestEnableSnapraidRejectsSmallParity(t *testing.T) {
	e := snapEnv(t)
	e.importAs(t, "d3", "parity") // 2 TB < größte Datenplatte 4 TB
	_, err := e.m.EnableSnapraid(e.ctx, "anna", "media", SnapraidInput{Parity: []string{"d3"}})
	if codeOf(err) != apierr.InvalidParams || !strings.Contains(apierr.From(err).Message, "d3") {
		t.Fatalf("%v", err)
	}
}

func TestEnableSnapraidRejectsNonParityDisk(t *testing.T) {
	e := snapEnv(t)
	if _, err := e.m.EnableSnapraid(e.ctx, "anna", "media", SnapraidInput{Parity: []string{"d1"}}); codeOf(err) != apierr.InvalidParams {
		t.Fatalf("%v", err)
	}
	if _, err := e.m.EnableSnapraid(e.ctx, "anna", "media", SnapraidInput{}); codeOf(err) != apierr.InvalidParams {
		t.Fatalf("no parity: %v", err)
	}
	if _, err := e.m.EnableSnapraid(e.ctx, "anna", "nope", SnapraidInput{Parity: []string{"d2"}}); codeOf(err) != apierr.NotFound {
		t.Fatalf("pool: %v", err)
	}
	if _, err := e.m.EnableSnapraid(e.ctx, "anna", "media", SnapraidInput{Parity: []string{"d2"}, Settings: snapraid.Settings{SyncSchedule: "x; y"}}); codeOf(err) != apierr.InvalidParams {
		t.Fatalf("schedule: %v", err)
	}
}

func TestEnableSnapraidRejectsParityUsedElsewhere(t *testing.T) {
	e := snapEnv(t)
	e.importAs(t, "d3", "data")
	jid, err := e.m.CreatePool(e.ctx, "anna", PoolInput{Name: "other", Branches: []string{"d3"}})
	e.mustSucceed(t, jid, err)
	e.enable(t, "other", "d2")
	if _, err := e.m.EnableSnapraid(e.ctx, "anna", "media", SnapraidInput{Parity: []string{"d2"}}); codeOf(err) != apierr.Conflict {
		t.Fatalf("%v", err)
	}
}

func TestEnableSnapraidFailureKeepsDB(t *testing.T) {
	e := snapEnv(t)
	e.agent.snapErr = apierr.New(apierr.InvalidParams, "Der Datenträger d2 ist nicht eingehängt.")
	jid, err := e.m.EnableSnapraid(e.ctx, "anna", "media", SnapraidInput{Parity: []string{"d2"}})
	if err != nil {
		t.Fatal(err)
	}
	if j := e.wait(t, jid); j.Status != jobs.StatusFailed {
		t.Fatalf("%+v", j)
	}
	if pools, _ := e.m.ListPools(e.ctx); pools[0].Snapraid != nil {
		t.Fatalf("db changed: %+v", pools[0].Snapraid)
	}
}

func TestDisableSnapraidApplies(t *testing.T) {
	e := snapEnv(t)
	e.enable(t, "media", "d2")
	jid, err := e.m.DisableSnapraid(e.ctx, "anna", "media")
	e.mustSucceed(t, jid, err)
	if len(e.lastSnap().Pools) != 0 {
		t.Fatalf("%+v", e.lastSnap())
	}
	pools, _ := e.m.ListPools(e.ctx)
	disks, _ := e.m.ListDisks(e.ctx)
	if pools[0].Snapraid != nil {
		t.Fatal("still enabled")
	}
	for _, d := range disks {
		if d.ID == "d2" && (d.Pool != "" || d.Role != "parity") {
			t.Fatalf("%+v", d)
		}
	}
}

func TestUpdatePoolRefusesRemovingBranchWithSnapraid(t *testing.T) {
	e := snapEnv(t)
	e.importAs(t, "d3", "data")
	jid, err := e.m.UpdatePool(e.ctx, "anna", "media", PoolInput{Branches: []string{"d1", "d3"}})
	e.mustSucceed(t, jid, err)
	e.enable(t, "media", "d2")
	_, err = e.m.UpdatePool(e.ctx, "anna", "media", PoolInput{Branches: []string{"d3"}})
	if codeOf(err) != apierr.Conflict || !strings.Contains(apierr.From(err).Message, "SnapRAID") {
		t.Fatalf("%v", err)
	}
}

func TestUpdatePoolAddingBranchReappliesSnapraid(t *testing.T) {
	e := snapEnv(t)
	e.enable(t, "media", "d2")
	e.importAs(t, "d3", "data")
	jid, err := e.m.UpdatePool(e.ctx, "anna", "media", PoolInput{Branches: []string{"d1", "d3"}})
	e.mustSucceed(t, jid, err)
	if got := strings.Join(e.lastSnap().Pools[0].Data, ","); got != "d1,d3" {
		t.Fatalf("%s", got)
	}
}

func TestReleaseRefusesParityDiskInUse(t *testing.T) {
	e := snapEnv(t)
	e.enable(t, "media", "d2")
	_, err := e.m.Release(e.ctx, "anna", "d2")
	if ae := apierr.From(err); ae.Code != apierr.DiskInUse || ae.Details["pool"] != "media" {
		t.Fatalf("%v", err)
	}
	jid, err := e.m.DisableSnapraid(e.ctx, "anna", "media")
	e.mustSucceed(t, jid, err)
	jid, err = e.m.Release(e.ctx, "anna", "d2")
	e.mustSucceed(t, jid, err)
}

func TestDeletePoolDisablesSnapraid(t *testing.T) {
	e := snapEnv(t)
	e.enable(t, "media", "d2")
	jid, err := e.m.DeletePool(e.ctx, "anna", "media")
	e.mustSucceed(t, jid, err)
	if len(e.lastSnap().Pools) != 0 {
		t.Fatalf("%+v", e.lastSnap())
	}
	var n int
	e.m.db.QueryRow(`SELECT count(*) FROM snapraid_parity`).Scan(&n)
	if n != 0 {
		t.Fatal("parity rows left")
	}
	// Die Paritätsplatte ist wieder frei.
	jid, err = e.m.Release(e.ctx, "anna", "d2")
	e.mustSucceed(t, jid, err)
}

func TestRunSnapraidRequiresEnabled(t *testing.T) {
	e := snapEnv(t)
	if _, err := e.m.RunSnapraid(e.ctx, "anna", "media", "sync", false); codeOf(err) != apierr.Conflict {
		t.Fatalf("%v", err)
	}
	e.enable(t, "media", "d2")
	if _, err := e.m.RunSnapraid(e.ctx, "anna", "media", "rm", false); codeOf(err) != apierr.InvalidParams {
		t.Fatalf("%v", err)
	}
}

func TestRunSnapraidFollowsStatusUntilDone(t *testing.T) {
	e := snapEnv(t)
	e.enable(t, "media", "d2")
	done := time.Now().UTC()
	e.agent.statuses = []snapraid.Status{
		{Enabled: true},
		{Enabled: true, Running: true},
		{Enabled: true, Running: true},
		{Enabled: true, LastSync: &snapraid.RunState{Action: "sync", Result: snapraid.ResultOK, At: done}, Log: "== sync =="},
	}
	jid, err := e.m.RunSnapraid(e.ctx, "anna", "media", "sync", true)
	e.mustSucceed(t, jid, err)
	if len(e.agent.started) != 1 || e.agent.started[0]["force"] != true || e.agent.started[0]["action"] != "sync" || e.agent.started[0]["pool"] != "media" {
		t.Fatalf("%+v", e.agent.started)
	}
	if j, _ := e.jobs.Get(e.ctx, jid); !strings.Contains(j.Log, "== sync ==") || j.Kind != "snapraid.sync" {
		t.Fatalf("%+v", j)
	}
}

func TestRunSnapraidBlockedFailsJob(t *testing.T) {
	e := snapEnv(t)
	e.enable(t, "media", "d2")
	e.agent.statuses = []snapraid.Status{
		{Enabled: true},
		{Enabled: true, LastSync: &snapraid.RunState{Action: "sync", Result: snapraid.ResultBlocked, Message: "Sync gestoppt: 501 Dateien gelöscht", Removed: 501, At: time.Now().UTC()}},
	}
	jid, err := e.m.RunSnapraid(e.ctx, "anna", "media", "sync", false)
	if err != nil {
		t.Fatal(err)
	}
	if j := e.wait(t, jid); j.Status != jobs.StatusFailed || !strings.Contains(j.Log, "501") {
		t.Fatalf("%+v", j)
	}
}

func TestSnapraidStatusPassesThrough(t *testing.T) {
	e := snapEnv(t)
	if s, err := e.m.SnapraidStatus(e.ctx, "media"); err != nil || s.Enabled {
		t.Fatalf("%+v %v", s, err)
	}
	e.enable(t, "media", "d2")
	e.agent.statuses = []snapraid.Status{{Running: true}}
	if s, err := e.m.SnapraidStatus(e.ctx, "media"); err != nil || !s.Enabled || !s.Running {
		t.Fatalf("%+v %v", s, err)
	}
	if _, err := e.m.SnapraidStatus(e.ctx, "nope"); codeOf(err) != apierr.NotFound {
		t.Fatal(err)
	}
}

func TestReconcileAppliesSnapraid(t *testing.T) {
	e := snapEnv(t)
	e.enable(t, "media", "d2")
	n := e.agent.called("snapraid.apply")
	if err := e.m.Reconcile(e.ctx); err != nil {
		t.Fatal(err)
	}
	if e.agent.called("snapraid.apply") != n+1 {
		t.Fatal("Reconcile hat snapraid.apply nicht aufgerufen")
	}
}
