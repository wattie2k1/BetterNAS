package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/snapraid"
	"github.com/betternas/betternas/internal/sysexec"
)

const spFixD1 = spConf + " -d d1 fix"

func markerExists(root string) bool {
	_, err := os.Stat(filepath.Join(root, snapraid.FixingPath("media")))
	return err == nil
}

func TestFixWritesMarkerAndRunsFix(t *testing.T) {
	d, f, root := snapraidEnv(t)
	applySP(t, d, 500)
	markerAtCall := false
	f.Hook = func(call string) (sysexec.Result, bool) {
		if call == spFixD1 {
			markerAtCall = markerExists(root)
		}
		return sysexec.Result{}, false
	}
	f.Responses[spFixD1] = sysexec.Result{Stdout: []byte("Everything OK\n12 recovered errors\n0 unrecoverable errors\n")}
	if err := RunSnapraidDisk(context.Background(), d, "media", "fix", false, "d1"); err != nil {
		t.Fatal(err)
	}
	if !markerAtCall {
		t.Fatal("Marker muss vor dem fix-Aufruf existieren")
	}
	if markerExists(root) {
		t.Fatal("Marker nach Erfolg nicht entfernt")
	}
	st, _ := SnapraidStatus(context.Background(), d, "media")
	if st.LastFix == nil || st.LastFix.Result != snapraid.ResultOK || st.Recovery != nil {
		t.Fatalf("%+v %+v", st.LastFix, st.Recovery)
	}
}

func TestFixUnrecoverableKeepsMarker(t *testing.T) {
	d, f, root := snapraidEnv(t)
	applySP(t, d, 500)
	f.Responses[spFixD1] = sysexec.Result{ExitCode: 2, Stdout: []byte("unrecoverable /srv/disks/d1/a.mkv\nunrecoverable /srv/disks/d1/b.mkv\n3 recovered errors\n2 unrecoverable errors\n")}
	if err := RunSnapraidDisk(context.Background(), d, "media", "fix", false, "d1"); err == nil {
		t.Fatal("Fehler erwartet")
	}
	st, _ := SnapraidStatus(context.Background(), d, "media")
	if st.LastFix == nil || st.LastFix.Result != snapraid.ResultErrors || st.LastFix.Unrecoverable != 2 {
		t.Fatalf("%+v", st.LastFix)
	}
	if !markerExists(root) || st.Recovery == nil || st.Recovery.Disk != "d1" || st.Recovery.State != "open" {
		t.Fatalf("Marker/Recovery: %v %+v", markerExists(root), st.Recovery)
	}
}

func TestFixFailedKeepsMarker(t *testing.T) {
	d, f, root := snapraidEnv(t)
	applySP(t, d, 500)
	f.Responses[spFixD1] = sysexec.Result{ExitCode: 1, Stderr: []byte("boom")}
	if err := RunSnapraidDisk(context.Background(), d, "media", "fix", false, "d1"); err == nil {
		t.Fatal("Fehler erwartet")
	}
	st, _ := SnapraidStatus(context.Background(), d, "media")
	if st.LastFix == nil || st.LastFix.Result != snapraid.ResultFailed || !strings.Contains(st.LastFix.Message, "boom") || !markerExists(root) {
		t.Fatalf("%+v marker=%v", st.LastFix, markerExists(root))
	}
}

func TestFixRejectsUnknownOrBadDisk(t *testing.T) {
	d, f, root := snapraidEnv(t)
	applySP(t, d, 500)
	for _, disk := range []string{"", "nope", "../x", "d1;rm", "d 1"} {
		if err := RunSnapraidDisk(context.Background(), d, "media", "fix", false, disk); code(err) != apierr.InvalidParams {
			t.Errorf("%q: %v", disk, err)
		}
	}
	// ein Plattenname bei anderen Aktionen ist ebenfalls ungültig
	if err := RunSnapraidDisk(context.Background(), d, "media", "sync", false, "d1"); code(err) != apierr.InvalidParams {
		t.Errorf("sync mit disk: %v", err)
	}
	if f.Called("snapraid") || markerExists(root) {
		t.Fatalf("snapraid lief oder Marker geschrieben: %v", f.Calls)
	}
}

func TestFixAcceptsAliasName(t *testing.T) {
	d, f, _ := snapraidEnv(t)
	st := snapraid.State{Pools: []snapraid.PoolConfig{snapraid.PoolConfig{
		Pool: "media", Data: []string{"d1", "new2"}, Parity: []string{"p1"}, Names: map[string]string{"new2": "old2"},
	}.WithDefaults()}}
	if _, err := ApplySnapraid(context.Background(), d, st); err != nil {
		t.Fatal(err)
	}
	if err := RunSnapraidDisk(context.Background(), d, "media", "fix", false, "old2"); err != nil {
		t.Fatal(err)
	}
	if !f.Called(spConf + " -d old2 fix") {
		t.Fatalf("%v", f.Calls)
	}
	// die technische ID ist nicht der SnapRAID-Name
	if err := RunSnapraidDisk(context.Background(), d, "media", "fix", false, "new2"); code(err) != apierr.InvalidParams {
		t.Fatalf("%v", err)
	}
}

func TestSyncAndScrubBlockedWhileMarker(t *testing.T) {
	d, f, root := snapraidEnv(t)
	applySP(t, d, 500)
	if err := RecoveryBegin(context.Background(), d, "media", "d1"); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"sync", "scrub"} {
		err := RunSnapraid(context.Background(), d, "media", action, false)
		if code(err) != apierr.Conflict || !strings.Contains(err.Error(), "Wiederherstellung") {
			t.Fatalf("%s: %v", action, err)
		}
	}
	if f.Called("snapraid") {
		t.Fatalf("snapraid lief trotz offener Wiederherstellung: %v", f.Calls)
	}
	st := readState(t, root)
	if st.LastSync == nil || st.LastSync.Result != snapraid.ResultBlocked || st.LastScrub == nil || st.LastScrub.Result != snapraid.ResultBlocked {
		t.Fatalf("%+v %+v", st.LastSync, st.LastScrub)
	}
	// diff liest nur und bleibt erlaubt
	f.Responses[spDiff] = diffOut(0, 0)
	if err := RunSnapraid(context.Background(), d, "media", "diff", false); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryBeginCancelAndStatus(t *testing.T) {
	d, _, root := snapraidEnv(t)
	applySP(t, d, 500)
	if err := RecoveryBegin(context.Background(), d, "media", "unbekannt"); code(err) != apierr.InvalidParams {
		t.Fatalf("%v", err)
	}
	if err := RecoveryCancel(context.Background(), d, "media"); code(err) != apierr.NotFound {
		t.Fatalf("Abbrechen ohne Marker: %v", err)
	}
	if err := RecoveryBegin(context.Background(), d, "media", "d2"); err != nil {
		t.Fatal(err)
	}
	st, _ := SnapraidStatus(context.Background(), d, "media")
	if st.Recovery == nil || st.Recovery.Disk != "d2" || st.Recovery.Since.IsZero() {
		t.Fatalf("%+v", st.Recovery)
	}
	if err := RecoveryCancel(context.Background(), d, "media"); err != nil {
		t.Fatal(err)
	}
	if markerExists(root) {
		t.Fatal("Marker nicht entfernt")
	}
	if st, _ := SnapraidStatus(context.Background(), d, "media"); st.Recovery != nil {
		t.Fatalf("%+v", st.Recovery)
	}
}

func TestParseArgsDisk(t *testing.T) {
	a, p, force, disk, err := parseSnapraidArgs([]string{"fix", "media", "--disk", "old1"})
	if err != nil || a != "fix" || p != "media" || force || disk != "old1" {
		t.Fatal(a, p, force, disk, err)
	}
	for _, bad := range [][]string{{"fix", "media"}, {"fix", "media", "--disk"}, {"sync", "media", "--disk", "d1"}, {"fix", "media", "--disk", "../x"}} {
		if _, _, _, _, err := parseSnapraidArgs(bad); err == nil {
			t.Errorf("%v akzeptiert", bad)
		}
	}
}

func TestStartSnapraidDiskArgs(t *testing.T) {
	d, f, _ := snapraidEnv(t)
	applySP(t, d, 500)
	if _, err := StartSnapraidDisk(context.Background(), d, "media", "fix", false, "d1"); err != nil {
		t.Fatal(err)
	}
	if !f.Called("systemd-run --unit=betternas-snapraid-media --collect /usr/sbin/betternas-agent snapraid-run fix media --disk d1") {
		t.Fatalf("%s", strings.Join(f.Calls, "\n"))
	}
	if _, err := StartSnapraidDisk(context.Background(), d, "media", "fix", false, ""); code(err) != apierr.InvalidParams {
		t.Fatalf("ohne Platte: %v", err)
	}
}
