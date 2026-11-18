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

const managedSources = `# Verwaltet von BetterNAS – nicht manuell bearbeiten
Types: deb
URIs: https://wattie2k1.github.io/BetterNAS
Suites: stable
Components: main
Signed-By: /usr/share/keyrings/betternas-archive-keyring.gpg
`

func writeSources(t *testing.T, d Deps, content string) {
	t.Helper()
	if err := d.FS.WriteFile(aptSourcesPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readSources(t *testing.T, d Deps) string {
	t.Helper()
	b, err := d.FS.ReadFile(aptSourcesPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUpdateChannelReadsSuites(t *testing.T) {
	d, _ := updatesEnv(t)
	if ch, err := UpdateChannel(context.Background(), d); err != nil || ch != "" {
		t.Fatalf("ohne Datei: %q %v", ch, err)
	}
	writeSources(t, d, strings.Replace(managedSources, "Suites: stable", "Suites: testing", 1))
	if ch, err := UpdateChannel(context.Background(), d); err != nil || ch != "testing" {
		t.Fatalf("testing: %q %v", ch, err)
	}
	writeSources(t, d, strings.Replace(managedSources, "Suites: stable", "Suites: bookworm", 1))
	if ch, _ := UpdateChannel(context.Background(), d); ch != "" {
		t.Fatalf("unbekannte Suite muss leer sein: %q", ch)
	}
}

func TestSetUpdateChannelRewritesOnlySuites(t *testing.T) {
	d, _ := updatesEnv(t)
	writeSources(t, d, managedSources)
	if err := SetUpdateChannel(context.Background(), d, "testing"); err != nil {
		t.Fatal(err)
	}
	if got, want := readSources(t, d), strings.Replace(managedSources, "Suites: stable", "Suites: testing", 1); got != want {
		t.Fatalf("erwartet\n%s\nbekommen\n%s", want, got)
	}
}

func TestSetUpdateChannelAddsMissingHeader(t *testing.T) {
	d, _ := updatesEnv(t)
	writeSources(t, d, strings.TrimPrefix(managedSources, "# Verwaltet von BetterNAS – nicht manuell bearbeiten\n"))
	if err := SetUpdateChannel(context.Background(), d, "testing"); err != nil {
		t.Fatal(err)
	}
	got := readSources(t, d)
	if !strings.HasPrefix(got, "# Verwaltet von BetterNAS – nicht manuell bearbeiten\n") || !strings.Contains(got, "Suites: testing\n") {
		t.Fatalf("%s", got)
	}
}

func TestSetUpdateChannelRejectsInvalid(t *testing.T) {
	d, _ := updatesEnv(t)
	writeSources(t, d, managedSources)
	for _, ch := range []string{"beta", "", "stable\nURIs: http://evil", "Stable", "stable ", "testing\r"} {
		if err := SetUpdateChannel(context.Background(), d, ch); code(err) != apierr.InvalidParams {
			t.Errorf("%q: %v", ch, err)
		}
	}
	if readSources(t, d) != managedSources {
		t.Fatal("Datei darf unverändert bleiben")
	}
}

func TestSetUpdateChannelRefusesForeignFile(t *testing.T) {
	d, _ := updatesEnv(t)
	foreign := "Types: deb\nURIs: http://example.org/deb\nSuites: stable\nComponents: main\n"
	writeSources(t, d, foreign)
	if err := SetUpdateChannel(context.Background(), d, "testing"); code(err) != apierr.Conflict {
		t.Fatalf("%v", err)
	}
	if readSources(t, d) != foreign {
		t.Fatal("fremde Datei darf nicht verändert werden")
	}
}

func TestSetUpdateChannelMissingFile(t *testing.T) {
	d, _ := updatesEnv(t)
	if err := SetUpdateChannel(context.Background(), d, "testing"); code(err) != apierr.NotFound {
		t.Fatalf("%v", err)
	}
	if d.FS.Exists(aptSourcesPath) {
		t.Fatal("Datei darf nicht angelegt werden")
	}
}

func TestSetUpdateChannelIdempotent(t *testing.T) {
	d, _ := updatesEnv(t)
	writeSources(t, d, managedSources)
	for i := 0; i < 2; i++ {
		if err := SetUpdateChannel(context.Background(), d, "stable"); err != nil {
			t.Fatal(err)
		}
	}
	if readSources(t, d) != managedSources {
		t.Fatal("gleicher Kanal ändert nichts")
	}
}

func TestRegisteredUpdateChannelMethods(t *testing.T) {
	d, _ := updatesEnv(t)
	writeSources(t, d, managedSources)
	c := rpcClient(t, d)
	ctx := context.Background()
	var got struct {
		Channel string `json:"channel"`
	}
	if err := c.Call(ctx, "updates.channel", nil, &got); err != nil || got.Channel != "stable" {
		t.Fatalf("updates.channel: %v %+v", err, got)
	}
	if err := c.Call(ctx, "updates.setchannel", map[string]string{"channel": "testing"}, nil); err != nil {
		t.Fatalf("updates.setchannel: %v", err)
	}
	if err := c.Call(ctx, "updates.channel", nil, &got); err != nil || got.Channel != "testing" {
		t.Fatalf("nach setchannel: %v %+v", err, got)
	}
	if err := c.Call(ctx, "updates.setchannel", map[string]string{"channel": "x"}, nil); apierr.From(err).Code != apierr.InvalidParams {
		t.Fatalf("ungültig: %v", err)
	}
}
