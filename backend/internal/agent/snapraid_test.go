package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/snapraid"
	"github.com/betternas/betternas/internal/sysexec"
)

const (
	spConf = "snapraid -c /etc/betternas/snapraid/media.conf"
	spDiff = spConf + " diff"
)

func spState(threshold int) snapraid.State {
	return snapraid.State{Pools: []snapraid.PoolConfig{snapraid.PoolConfig{
		Pool: "media", Data: []string{"d1", "d2"}, Parity: []string{"p1"},
		Settings: snapraid.Settings{DeleteThreshold: threshold},
	}.WithDefaults()}}
}

func diffOut(added, removed int) sysexec.Result {
	out := "Comparing...\n\n  100 equal\n"
	out += itoa(added) + " added\n" + itoa(removed) + " removed\n0 updated\n0 moved\n0 copied\n0 restored\n"
	code := 0
	if added+removed > 0 {
		code = 2
	}
	return sysexec.Result{Stdout: []byte(out), ExitCode: code}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// snapraidEnv: alle Mounts vorhanden, Lock-Verzeichnis im Temp-Ordner.
func snapraidEnv(t *testing.T) (Deps, *sysexec.FakeRunner, string) {
	t.Helper()
	root := t.TempDir()
	lockDir = filepath.Join(t.TempDir(), "run")
	t.Cleanup(func() { lockDir = snapraid.LockDir })
	f := &sysexec.FakeRunner{Responses: map[string]sysexec.Result{}}
	return Deps{Runner: f, FS: sysexec.NewRootFS(root)}, f, root
}

func applySP(t *testing.T, d Deps, threshold int) {
	t.Helper()
	if _, err := ApplySnapraid(context.Background(), d, spState(threshold)); err != nil {
		t.Fatal(err)
	}
}

func readState(t *testing.T, root string) (s struct {
	LastSync, LastScrub *snapraid.RunState
	Diff                *snapraid.Diff
}) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, snapraid.StatePath("media")))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestApplySnapraidWritesFilesAndEnablesTimers(t *testing.T) {
	d, f, root := snapraidEnv(t)
	res, err := ApplySnapraid(context.Background(), d, spState(0))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changed) != 4 {
		t.Fatalf("%+v", res)
	}
	for _, p := range []string{snapraid.ConfPath("media"), snapraid.SettingsPath("media"), snapraid.TimerPath("sync", "media"), snapraid.TimerPath("scrub", "media")} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Fatal(p, err)
		}
	}
	assertOrder(t, f, "findmnt --mountpoint /srv/disks/d1", "systemctl daemon-reload",
		"systemctl enable --now betternas-snapraid-scrub-media.timer", "systemctl enable --now betternas-snapraid-sync-media.timer")
	// idempotent
	res, err = ApplySnapraid(context.Background(), d, spState(0))
	if err != nil || len(res.Changed) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestApplySnapraidRemovesStaleTimers(t *testing.T) {
	d, f, root := snapraidEnv(t)
	applySP(t, d, 0)
	if _, err := ApplySnapraid(context.Background(), d, snapraid.State{}); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, f, "systemctl disable --now betternas-snapraid-sync-media.timer")
	for _, p := range []string{snapraid.ConfPath("media"), snapraid.TimerPath("scrub", "media")} {
		if _, err := os.Stat(filepath.Join(root, p)); err == nil {
			t.Fatalf("%s kept", p)
		}
	}
}

func TestApplySnapraidRefusesUnmountedDisk(t *testing.T) {
	d, f, root := snapraidEnv(t)
	f.Responses["findmnt --mountpoint /srv/parity/p1"] = sysexec.Result{ExitCode: 1}
	_, err := ApplySnapraid(context.Background(), d, spState(0))
	if code(err) != apierr.InvalidParams {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, snapraid.ConfPath("media"))); err == nil {
		t.Fatal("conf written despite unmounted parity disk")
	}
}

func TestApplySnapraidLeavesForeignFiles(t *testing.T) {
	d, _, root := snapraidEnv(t)
	foreign := filepath.Join(root, snapraid.ConfDir, "media.conf")
	os.MkdirAll(filepath.Dir(foreign), 0o755)
	os.WriteFile(foreign, []byte("# meine eigene Konfiguration\n"), 0o644)
	if _, err := ApplySnapraid(context.Background(), d, spState(0)); code(err) != apierr.Conflict {
		t.Fatalf("foreign conf overwritten: %v", err)
	}
	if b, _ := os.ReadFile(foreign); !strings.HasPrefix(string(b), "# meine") {
		t.Fatal("foreign file modified")
	}
}

func TestRunSyncBlockedWhenDiskMissing(t *testing.T) {
	d, f, root := snapraidEnv(t)
	applySP(t, d, 0)
	f.Responses["findmnt --mountpoint /srv/disks/d2"] = sysexec.Result{ExitCode: 1}
	err := RunSnapraid(context.Background(), d, "media", "sync", false)
	if err == nil {
		t.Fatal("sync ran with a missing disk")
	}
	if f.Called(spConf+" sync") || f.Called(spDiff) {
		t.Fatal("snapraid executed with a missing disk")
	}
	st := readState(t, root)
	if st.LastSync == nil || st.LastSync.Result != snapraid.ResultBlocked || !strings.Contains(st.LastSync.Message, "d2") {
		t.Fatalf("%+v", st.LastSync)
	}
}

func TestRunSyncBlocksOnMassDelete(t *testing.T) {
	d, f, root := snapraidEnv(t)
	applySP(t, d, 500)
	f.Responses[spDiff] = diffOut(0, 501)
	if err := RunSnapraid(context.Background(), d, "media", "sync", false); code(err) != apierr.Conflict {
		t.Fatalf("%v", err)
	}
	if f.Called(spConf + " sync") {
		t.Fatal("sync ran despite mass delete")
	}
	st := readState(t, root)
	if st.LastSync.Result != snapraid.ResultBlocked || st.LastSync.Removed != 501 || st.Diff.Removed != 501 {
		t.Fatalf("%+v %+v", st.LastSync, st.Diff)
	}
}

func TestRunSyncForce(t *testing.T) {
	d, f, root := snapraidEnv(t)
	applySP(t, d, 500)
	f.Responses[spDiff] = diffOut(0, 501)
	if err := RunSnapraid(context.Background(), d, "media", "sync", true); err != nil {
		t.Fatal(err)
	}
	if !f.Called(spConf + " sync") {
		t.Fatal("forced sync did not run")
	}
	if st := readState(t, root); st.LastSync.Result != snapraid.ResultOK {
		t.Fatalf("%+v", st.LastSync)
	}
}

func TestRunSyncOKRecordsState(t *testing.T) {
	d, f, root := snapraidEnv(t)
	applySP(t, d, 500)
	f.Responses[spDiff] = diffOut(3, 1)
	if err := RunSnapraid(context.Background(), d, "media", "sync", false); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, f, spDiff, spConf+" sync")
	st := readState(t, root)
	if st.LastSync.Result != snapraid.ResultOK || st.LastSync.At.IsZero() || st.Diff.Unprotected() != 0 {
		t.Fatalf("%+v %+v", st.LastSync, st.Diff)
	}
	s, err := SnapraidStatus(context.Background(), d, "media")
	if err != nil || !s.Enabled || s.Running || s.LastSync == nil || s.Log == "" {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestRunSyncFailedRecorded(t *testing.T) {
	d, f, root := snapraidEnv(t)
	applySP(t, d, 500)
	f.Responses[spDiff] = diffOut(1, 0)
	f.Responses[spConf+" sync"] = sysexec.Result{ExitCode: 1, Stderr: []byte("kaputt")}
	if err := RunSnapraid(context.Background(), d, "media", "sync", false); err == nil {
		t.Fatal("failed sync reported as success")
	}
	if st := readState(t, root); st.LastSync.Result != snapraid.ResultFailed {
		t.Fatalf("%+v", st.LastSync)
	}
}

func TestRunScrubUsesSettingsAndRecordsErrors(t *testing.T) {
	d, f, root := snapraidEnv(t)
	applySP(t, d, 500)
	f.Responses[spConf+" -p 8 -o 10 scrub"] = sysexec.Result{ExitCode: 2, Stdout: []byte("DANGER! There are errors!")}
	if err := RunSnapraid(context.Background(), d, "media", "scrub", false); err == nil {
		t.Fatal("scrub errors reported as success")
	}
	if st := readState(t, root); st.LastScrub == nil || st.LastScrub.Result != snapraid.ResultErrors {
		t.Fatalf("%+v", st.LastScrub)
	}
}

func TestRunDiffStoresDiffOnly(t *testing.T) {
	d, f, root := snapraidEnv(t)
	applySP(t, d, 500)
	f.Responses[spDiff] = diffOut(2, 0)
	if err := RunSnapraid(context.Background(), d, "media", "diff", false); err != nil {
		t.Fatal(err)
	}
	if f.Called(spConf + " sync") {
		t.Fatal("diff must not sync")
	}
	if st := readState(t, root); st.Diff == nil || st.Diff.Added != 2 || st.LastSync != nil {
		t.Fatalf("%+v", st)
	}
}

func TestRunRefusesWhenLocked(t *testing.T) {
	d, f, _ := snapraidEnv(t)
	applySP(t, d, 500)
	l, err := lockPool("media")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := RunSnapraid(context.Background(), d, "media", "sync", false); code(err) != apierr.Conflict {
		t.Fatalf("%v", err)
	}
	if f.Called("snapraid") {
		t.Fatal("snapraid ran while locked")
	}
	if s, _ := SnapraidStatus(context.Background(), d, "media"); !s.Running {
		t.Fatal("status does not show running")
	}
	if _, err := StartSnapraid(context.Background(), d, "media", "sync", false); code(err) != apierr.Conflict {
		t.Fatalf("start while locked: %v", err)
	}
}

func TestStartSnapraidRejectsBadInput(t *testing.T) {
	d, _, _ := snapraidEnv(t)
	applySP(t, d, 500)
	for _, c := range [][2]string{{"media", "rm"}, {"../x", "sync"}, {"media; ls", "sync"}} {
		if _, err := StartSnapraid(context.Background(), d, c[0], c[1], false); code(err) != apierr.InvalidParams {
			t.Errorf("%v: %v", c, err)
		}
	}
	if _, err := StartSnapraid(context.Background(), d, "unbekannt", "sync", false); code(err) != apierr.NotFound {
		t.Errorf("unknown pool: %v", code(err))
	}
}

func TestStartSnapraidUsesTransientUnit(t *testing.T) {
	d, f, _ := snapraidEnv(t)
	applySP(t, d, 500)
	unit, err := StartSnapraid(context.Background(), d, "media", "sync", true)
	if err != nil || unit != "betternas-snapraid-media" {
		t.Fatalf("%q %v", unit, err)
	}
	if !f.Called("systemd-run --unit=betternas-snapraid-media --collect /usr/sbin/betternas-agent snapraid-run sync media --force") {
		t.Fatalf("calls:\n%s", strings.Join(f.Calls, "\n"))
	}
}

func TestSnapraidStatusWithoutState(t *testing.T) {
	d, _, _ := snapraidEnv(t)
	s, err := SnapraidStatus(context.Background(), d, "media")
	if err != nil || s.Enabled || s.Running || s.LastSync != nil {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestParseSnapraidArgs(t *testing.T) {
	a, p, force, err := parseSnapraidArgs([]string{"sync", "media", "--force"})
	if err != nil || a != "sync" || p != "media" || !force {
		t.Fatal(a, p, force, err)
	}
	for _, bad := range [][]string{{}, {"sync"}, {"rm", "media"}, {"sync", "../x"}, {"sync", "media", "--wipe"}} {
		if _, _, _, err := parseSnapraidArgs(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}
