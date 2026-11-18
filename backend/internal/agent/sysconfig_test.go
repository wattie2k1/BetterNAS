package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/sysexec"
)

func sysEnv(t *testing.T) (Deps, *sysexec.FakeRunner, string) {
	t.Helper()
	d, f, root := vmEnv(t)
	writeFile(t, root, "/proc/sys/kernel/hostname")
	f.Responses["timedatectl show"] = sysexec.Result{Stdout: []byte("Timezone=Europe/Berlin\nNTP=yes\nNTPSynchronized=yes\nTimeUSec=Wed 2026-11-18 11:45:34 CET\n")}
	f.Responses["timedatectl list-timezones --no-pager"] = sysexec.Result{Stdout: []byte("Europe/Berlin\nEurope/London\nUTC\n")}
	return d, f, root
}

func ptr[T any](v T) *T { return &v }

func TestGetSysSettings(t *testing.T) {
	d, _, _ := sysEnv(t)
	s, err := GetSysSettings(context.Background(), d)
	if err != nil || s.Timezone != "Europe/Berlin" || !s.NTP || !s.NTPSynced || len(s.NTPServers) != 0 {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestSetSysSettings(t *testing.T) {
	d, f, root := sysEnv(t)
	writeFile(t, root, "/etc/hosts")
	if err := d.FS.WriteFile("/etc/hosts", []byte("127.0.0.1\tlocalhost\n127.0.1.1\tbetternas.local betternas\n::1\tlocalhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := SetSysSettings(context.Background(), d, SysSettingsChange{
		Hostname: ptr("nas-neu"), Timezone: ptr("Europe/London"), NTP: ptr(false), NTPServers: ptr([]string{"Pool.ntp.org", "192.168.1.1"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"hostnamectl set-hostname nas-neu", "timedatectl set-timezone Europe/London", "timedatectl set-ntp false", "systemctl restart systemd-timesyncd"} {
		if !f.Called(c) {
			t.Errorf("%s fehlt: %v", c, f.Calls)
		}
	}
	if h := read(t, root, "etc/hosts"); !strings.Contains(h, "127.0.1.1\tnas-neu.local nas-neu") || strings.Contains(h, "betternas.local") || !strings.Contains(h, "::1") {
		t.Fatal(h)
	}
	if ts := read(t, root, "etc/systemd/timesyncd.conf.d/10-bnas.conf"); !strings.Contains(ts, "NTP=pool.ntp.org 192.168.1.1") {
		t.Fatal(ts)
	}
}

func TestSetSysSettingsRejectsInvalid(t *testing.T) {
	d, f, _ := sysEnv(t)
	for name, c := range map[string]SysSettingsChange{
		"hostname groß":  {Hostname: ptr("NAS")},
		"hostname punkt": {Hostname: ptr("a.b")},
		"hostname inj":   {Hostname: ptr("a b; reboot")},
		"zone":           {Timezone: ptr("Mars/Olympus")},
		"zone inj":       {Timezone: ptr("UTC; reboot")},
		"ntp inj":        {NTPServers: ptr([]string{"a\nb"})},
		"ntp viele":      {NTPServers: ptr([]string{"a.de", "b.de", "c.de", "d.de", "e.de"})},
	} {
		if code(SetSysSettings(context.Background(), d, c)) != apierr.InvalidParams {
			t.Errorf("%s: Fehler erwartet", name)
		}
	}
	if f.Called("hostnamectl") || f.Called("timedatectl set") || f.Called("systemctl") {
		t.Fatal("bei ungültigen Werten darf nichts geändert werden", f.Calls)
	}
}

func TestRewriteHostsAppends(t *testing.T) {
	if got := rewriteHosts("127.0.0.1\tlocalhost\n", "x"); got != "127.0.0.1\tlocalhost\n127.0.1.1\tx.local x\n" {
		t.Fatal(got)
	}
}

func TestPower(t *testing.T) {
	d, f, _ := sysEnv(t)
	if err := Power(context.Background(), d, "reboot"); err != nil || !f.Called("systemd-run --unit=bnas-power --on-active=3 systemctl reboot") {
		t.Fatal(err, f.Calls)
	}
	if code(Power(context.Background(), d, "kill")) != apierr.InvalidParams {
		t.Fatal("ungültig")
	}
}
