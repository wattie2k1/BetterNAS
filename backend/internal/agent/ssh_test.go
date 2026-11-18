package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/firewall"
	"github.com/betternas/betternas/internal/sysexec"
)

const testKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl laptop"

func TestParseSSHKey(t *testing.T) {
	k, norm, err := ParseSSHKey("  " + testKey + "  ")
	if err != nil || k.Type != "ssh-ed25519" || k.Comment != "laptop" || !strings.HasPrefix(k.Fingerprint, "SHA256:") || norm != testKey {
		t.Fatal(k, norm, err)
	}
	for _, bad := range []string{"", "command=\"rm -rf /\" " + testKey, "ssh-ed25519", "ssh-ed25519 !!!", testKey + "\n" + testKey, "ssh-dss AAAAB3NzaC1kc3MAAACB", testKey + "\x01"} {
		if _, _, err := ParseSSHKey(bad); apierr.From(err).Code != apierr.InvalidParams {
			t.Errorf("%q muss abgelehnt werden", bad)
		}
	}
}

func TestSSHKeysLifecycle(t *testing.T) {
	d, f := accountsEnv(t)
	ctx := context.Background()
	if err := AddSSHKey(ctx, d, "wattie", testKey); err != nil {
		t.Fatal(err)
	}
	if !f.Called("chown 1000:1000 /home/wattie/.ssh/authorized_keys") {
		t.Fatal(f.Calls)
	}
	if apierr.From(AddSSHKey(ctx, d, "wattie", testKey)).Code != apierr.Conflict {
		t.Fatal("Duplikat")
	}
	if apierr.From(AddSSHKey(ctx, d, "nobody", testKey)).Code != apierr.NotFound {
		t.Fatal("Systembenutzer ohne Konto")
	}
	keys, _ := ListSSHKeys(ctx, d, "wattie")
	if len(keys) != 1 {
		t.Fatal(keys)
	}
	if apierr.From(RemoveSSHKey(ctx, d, "wattie", "SHA256:nope")).Code != apierr.NotFound {
		t.Fatal("unbekannter Schlüssel")
	}
	if err := RemoveSSHKey(ctx, d, "wattie", keys[0].Fingerprint); err != nil {
		t.Fatal(err)
	}
	if keys, _ = ListSSHKeys(ctx, d, "wattie"); len(keys) != 0 {
		t.Fatal(keys)
	}
}

func TestSetSSHSettings(t *testing.T) {
	d, f := accountsEnv(t)
	ctx := context.Background()
	f.Responses["systemctl is-active ssh"] = sysexec.Result{Stdout: []byte("active\n")}
	f.Responses["systemctl is-active ssh.socket"] = sysexec.Result{ExitCode: 3, Stdout: []byte("inactive\n")}
	// Passwort-Anmeldung ohne Schlüssel abschalten: abgelehnt.
	off := SSHSettings{Port: 22, PasswordAuth: false, PermitRootLogin: "no"}
	if apierr.From(SetSSHSettings(ctx, d, off)).Code != apierr.Conflict {
		t.Fatal("ohne Schlüssel darf die Passwort-Anmeldung nicht ausgeschaltet werden")
	}
	for _, bad := range []SSHSettings{{Port: 0, PermitRootLogin: "no"}, {Port: 443, PermitRootLogin: "no"}, {Port: 22, PermitRootLogin: "yes"}} {
		if apierr.From(SetSSHSettings(ctx, d, bad)).Code != apierr.InvalidParams {
			t.Errorf("%+v", bad)
		}
	}
	if err := AddSSHKey(ctx, d, "wattie", testKey); err != nil {
		t.Fatal(err)
	}
	if err := SetSSHSettings(ctx, d, SSHSettings{Port: 2222, PasswordAuth: false, PermitRootLogin: "no"}); err != nil {
		t.Fatal(err)
	}
	if !f.Called("sshd -t") || !f.Called("systemctl reload ssh") {
		t.Fatal(f.Calls)
	}
	g := GetSSHSettings(ctx, d)
	if g.Port != 2222 || g.PasswordAuth || g.PermitRootLogin != "no" || sshPort(d) != 2222 {
		t.Fatalf("%+v", g)
	}
	// Der letzte Schlüssel lässt sich bei ausgeschaltetem Passwort nicht löschen.
	keys, _ := ListSSHKeys(ctx, d, "wattie")
	if apierr.From(RemoveSSHKey(ctx, d, "wattie", keys[0].Fingerprint)).Code != apierr.Conflict {
		t.Fatal("letzter Schlüssel")
	}
	// sshd -t scheitert → Datei wie vorher
	f.Responses["sshd -t"] = sysexec.Result{ExitCode: 1, Stderr: []byte("bad")}
	if apierr.From(SetSSHSettings(ctx, d, SSHSettings{Port: 2200, PasswordAuth: false, PermitRootLogin: "no"})).Code != apierr.InvalidParams || sshPort(d) != 2222 {
		t.Fatal("bei sshd-Fehler muss alles beim Alten bleiben")
	}
}

func TestSetSSHSocketActivatedPort(t *testing.T) {
	d, f := accountsEnv(t)
	f.Responses["systemctl is-active ssh.socket"] = sysexec.Result{Stdout: []byte("active\n")}
	if apierr.From(SetSSHSettings(context.Background(), d, SSHSettings{Port: 2222, PasswordAuth: true, PermitRootLogin: "no"})).Code != apierr.Conflict {
		t.Fatal("Port bei ssh.socket")
	}
}

func TestSetSSHUnmanagedFile(t *testing.T) {
	d, _ := accountsEnv(t)
	_ = d.FS.WriteFile(sshDropinFile, []byte("Port 2200\n"), 0o644)
	if apierr.From(SetSSHSettings(context.Background(), d, SSHSettings{Port: 22, PasswordAuth: true, PermitRootLogin: "no"})).Code != apierr.Conflict {
		t.Fatal("fremde Datei nicht anfassen")
	}
}

func TestSSHPortChangeUpdatesFirewall(t *testing.T) {
	d, f := accountsEnv(t)
	ctx := context.Background()
	f.Responses["systemctl is-active ssh.socket"] = sysexec.Result{ExitCode: 3, Stdout: []byte("inactive\n")}
	cfg := firewall.Default()
	cfg.Enabled = true
	if err := SetFirewall(ctx, d, cfg, ""); err != nil {
		t.Fatal(err)
	}
	if err := SetSSHSettings(ctx, d, SSHSettings{Port: 2222, PasswordAuth: true, PermitRootLogin: "no"}); err != nil {
		t.Fatal(err)
	}
	b, _ := d.FS.ReadFile(firewall.RulesFile)
	if !strings.Contains(string(b), "tcp dport { 2222 } accept") {
		t.Fatal(string(b))
	}
}
