package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/firewall"
	"github.com/betternas/betternas/internal/sysexec"
)

func TestSetFirewall(t *testing.T) {
	d, f, root := vmEnv(t)
	ctx := context.Background()
	cfg := firewall.Default()
	cfg.Enabled = true
	cfg.Rules["smb"] = firewall.Rule{Mode: firewall.ModeLAN}
	if err := SetFirewall(ctx, d, cfg, "192.168.1.5"); err != nil {
		t.Fatal(err)
	}
	if !f.Called("nft -c -f") || !f.Called("nft -f "+firewall.RulesFile) || !f.Called("systemd-run --unit=bnas-fw-rollback") || !f.Called("systemctl enable "+firewall.BootUnit) {
		t.Fatal(f.Calls)
	}
	if r := read(t, root, "var/lib/betternas/firewall/rules.nft"); !strings.Contains(r, "policy drop") {
		t.Fatal(r)
	}
	if !strings.Contains(read(t, root, "var/lib/betternas/firewall/rollback.sh"), "rm -f") {
		t.Fatal("ohne Vorversion muss der Rückfall die Firewall ausschalten")
	}
	// Eine zweite Änderung wartet auf die Bestätigung.
	f.Responses["systemctl is-active bnas-fw-rollback.timer"] = sysexec.Result{Stdout: []byte("active\n")}
	if apierr.From(SetFirewall(ctx, d, cfg, "")).Code != apierr.Conflict {
		t.Fatal("zweite Änderung muss abgelehnt werden")
	}
	if err := ConfirmFirewall(ctx, d); err != nil || !f.Called("systemctl stop bnas-fw-rollback.timer") {
		t.Fatal(err, f.Calls)
	}
	// Nach der Bestätigung gibt es keine ausstehende Änderung mehr; Sicherung wird beim nächsten Mal angelegt.
	f.Responses["systemctl is-active bnas-fw-rollback.timer"] = sysexec.Result{ExitCode: 3, Stdout: []byte("inactive\n")}
	cfg.Rules["ssh"] = firewall.Rule{Mode: firewall.ModeLAN}
	if err := SetFirewall(ctx, d, cfg, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, root, "var/lib/betternas/firewall/rollback.sh"), "cp ") {
		t.Fatal("mit Vorversion muss der Rückfall sie wiederherstellen")
	}
	if g := GetFirewall(ctx, d); !g.Config.Enabled || g.Config.Rules["ssh"].Mode != firewall.ModeLAN {
		t.Fatalf("%+v", g.Config)
	}
}

func TestSetFirewallRejects(t *testing.T) {
	d, f, _ := vmEnv(t)
	ctx := context.Background()
	cfg := firewall.Default()
	cfg.Enabled = true
	cfg.Rules["web"] = firewall.Rule{Mode: firewall.ModeCustom, Networks: []string{"10.9.0.0/16"}}
	if apierr.From(SetFirewall(ctx, d, cfg, "192.168.1.5")).Code != apierr.Conflict {
		t.Fatal("Aussperren muss abgelehnt werden")
	}
	if f.Called("nft -f") {
		t.Fatal("nichts darf geladen werden")
	}
	// nft -c schlägt fehl → nichts geändert
	cfg.Rules["web"] = firewall.Rule{Mode: firewall.ModeAll}
	f.Responses["nft -c -f "+firewall.StateDir+"/rules.nft.new"] = sysexec.Result{ExitCode: 1, Stderr: []byte("Error")}
	if apierr.From(SetFirewall(ctx, d, cfg, "")).Code != apierr.InvalidParams || f.Called("systemd-run") {
		t.Fatal(f.Calls)
	}
}

func TestSetFirewallLoadFailureRollsBack(t *testing.T) {
	d, f, _ := vmEnv(t)
	cfg := firewall.Default()
	cfg.Enabled = true
	f.Responses["nft -f "+firewall.RulesFile] = sysexec.Result{ExitCode: 1, Stderr: []byte("kaputt")}
	if err := SetFirewall(context.Background(), d, cfg, ""); apierr.From(err).Code != apierr.Internal || !f.Called("/bin/sh "+firewall.RollbackFile) {
		t.Fatal(err, f.Calls)
	}
}

func TestRevertFirewall(t *testing.T) {
	d, f, _ := vmEnv(t)
	ctx := context.Background()
	if apierr.From(RevertFirewall(ctx, d)).Code != apierr.Conflict {
		t.Fatal("ohne ausstehende Änderung nicht möglich")
	}
	cfg := firewall.Default()
	if err := SetFirewall(ctx, d, cfg, ""); err != nil {
		t.Fatal(err)
	}
	f.Responses["systemctl is-active bnas-fw-rollback.timer"] = sysexec.Result{Stdout: []byte("active\n")}
	if err := RevertFirewall(ctx, d); err != nil || !f.Called("/bin/sh "+firewall.RollbackFile) {
		t.Fatal(err, f.Calls)
	}
}
