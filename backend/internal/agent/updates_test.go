package agent

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/sysexec"
)

func updatesEnv(t *testing.T) (Deps, *sysexec.FakeRunner) {
	t.Helper()
	f := &sysexec.FakeRunner{Responses: map[string]sysexec.Result{}}
	nowUnix = func() int64 { return 1700000000 }
	t.Cleanup(func() { nowUnix = defaultNowUnix })
	return Deps{Runner: f, FS: sysexec.NewRootFS(t.TempDir())}, f
}

func TestParseSimulatedUpgrade(t *testing.T) {
	b, err := os.ReadFile("testdata/apt-simulate.txt")
	if err != nil {
		t.Fatal(err)
	}
	pkgs := ParseUpgradeSimulation(b)
	want := []UpdatePackage{
		{Name: "betternas", Current: "0.0.8-dev", Candidate: "0.1.0"},
		{Name: "libc6", Current: "2.41-6", Candidate: "2.41-7"},
		{Name: "libnewdep1", Current: "", Candidate: "1.0-1"},
		{Name: "samba", Current: "2:4.22.3+dfsg-1", Candidate: "2:4.22.3+dfsg-2"},
	}
	if len(pkgs) != len(want) {
		t.Fatalf("%+v", pkgs)
	}
	for i := range want {
		if pkgs[i] != want[i] {
			t.Errorf("%d: erwartet %+v, bekommen %+v", i, want[i], pkgs[i])
		}
	}
}

func TestCheckUpdatesRunsUpdateThenSimulation(t *testing.T) {
	d, f := updatesEnv(t)
	b, _ := os.ReadFile("testdata/apt-simulate.txt")
	f.Responses["apt-get -s -o Debug::NoLocking=1 dist-upgrade"] = sysexec.Result{Stdout: b}
	pkgs, err := CheckUpdates(context.Background(), d)
	if err != nil || len(pkgs) != 4 {
		t.Fatalf("%v %+v", err, pkgs)
	}
	assertOrder(t, f, "systemctl list-units", "apt-get update", "apt-get -s -o Debug::NoLocking=1 dist-upgrade")
}

func TestCheckUpdatesRefusesWhileInstalling(t *testing.T) {
	d, f := updatesEnv(t)
	f.Responses["systemctl list-units"] = sysexec.Result{Stdout: []byte("betternas-update-1699999999.service loaded active running BetterNAS Updates\n")}
	if _, err := CheckUpdates(context.Background(), d); code(err) != apierr.Conflict {
		t.Fatalf("%v", err)
	}
	if f.Called("apt-get") {
		t.Fatal("apt-get darf nicht parallel laufen")
	}
}

func TestInstallUpdatesUsesTransientUnit(t *testing.T) {
	d, f := updatesEnv(t)
	unit, err := InstallUpdates(context.Background(), d)
	if err != nil || unit != "betternas-update-1700000000" {
		t.Fatalf("%q %v", unit, err)
	}
	var call string
	for _, c := range f.Calls {
		if strings.HasPrefix(c, "systemd-run ") {
			call = c
		}
		if strings.HasPrefix(c, "apt-get") {
			t.Fatalf("apt-get darf nicht im Agent-Prozess laufen: %q", c)
		}
	}
	for _, want := range []string{"--unit=betternas-update-1700000000", "--no-block", "--property=RemainAfterExit=yes", "--setenv=DEBIAN_FRONTEND=noninteractive", "apt-get -y", "--force-confold", "dist-upgrade"} {
		if !strings.Contains(call, want) {
			t.Errorf("%q fehlt in %q", want, call)
		}
	}
}

func TestInstallRefusesWhileRunning(t *testing.T) {
	d, f := updatesEnv(t)
	f.Responses["systemctl list-units"] = sysexec.Result{Stdout: []byte("betternas-update-1699999999.service loaded active running BetterNAS Updates\n")}
	if _, err := InstallUpdates(context.Background(), d); code(err) != apierr.Conflict {
		t.Fatalf("%v", err)
	}
	if f.Called("systemd-run") {
		t.Fatal("kein zweiter Lauf")
	}
}

func TestStatusRejectsForeignUnit(t *testing.T) {
	d, f := updatesEnv(t)
	for _, u := range []string{"ssh.service", "betternas-update-1; reboot", "betternas-update-", "../x"} {
		if _, err := UpdateStatus(context.Background(), d, u); code(err) != apierr.InvalidParams {
			t.Errorf("%q: %v", u, err)
		}
	}
	if f.Called("systemctl show") || f.Called("journalctl") {
		t.Fatal("fremde Units dürfen nicht abgefragt werden")
	}
}

func TestStatusStates(t *testing.T) {
	cases := map[string]struct {
		show string
		want string
	}{
		"läuft":       {"ActiveState=active\nSubState=running\nResult=success\n", "running"},
		"erfolgreich": {"ActiveState=active\nSubState=exited\nResult=success\n", "succeeded"},
		"fehlgeschl.": {"ActiveState=failed\nSubState=failed\nResult=exit-code\n", "failed"},
		"startet":     {"ActiveState=activating\nSubState=start\nResult=success\n", "running"},
		"unbekannt":   {"ActiveState=inactive\nSubState=dead\nResult=success\n", "succeeded"},
	}
	for name, c := range cases {
		d, f := updatesEnv(t)
		f.Responses["systemctl show"] = sysexec.Result{Stdout: []byte(c.show)}
		f.Responses["journalctl"] = sysexec.Result{Stdout: []byte("Unpacking betternas\n")}
		st, err := UpdateStatus(context.Background(), d, "betternas-update-1700000000")
		if err != nil || st.State != c.want || !strings.Contains(st.Log, "Unpacking") {
			t.Errorf("%s: %+v %v", name, st, err)
		}
	}
}
