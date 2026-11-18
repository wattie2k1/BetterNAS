package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/netcfg"
	"github.com/betternas/betternas/internal/sysexec"
)

func staticCfg() netcfg.IPConfig {
	return netcfg.IPConfig{Mode: netcfg.ModeStatic, Address: "192.168.1.10/24", Gateway: "192.168.1.1", DNS: []string{"192.168.1.1"}}
}

func TestNetworkApplyStaticConfirm(t *testing.T) {
	d, f, root := bridgeEnv(t)
	f.Responses["systemctl is-active bnas-net-rollback.timer"] = sysexec.Result{Stdout: []byte("active\n")}
	ctx := context.Background()
	if err := NetworkApply(ctx, d, staticCfg()); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, f, "systemd-run --unit=bnas-net-rollback --on-active=120", "networkctl reload", "networkctl reconfigure eno1")
	dp := read(t, root, "etc/systemd/network/10-betternas.network.d/10-bnas-ip.conf")
	if !strings.Contains(dp, "Address=192.168.1.10/24") || !strings.Contains(dp, "DHCP=no") {
		t.Fatal(dp)
	}
	if read(t, root, "etc/systemd/network/10-betternas.network") != origNet {
		t.Fatal("die Hauptdatei darf nicht verändert werden")
	}
	if rb := read(t, root, "var/lib/betternas/network/rollback.sh"); !strings.Contains(rb, "rm -f /etc/systemd/network/10-betternas.network.d/10-bnas-ip.conf") {
		t.Fatal(rb)
	}
	st, err := NetworkStatus(ctx, d)
	if err != nil || !st.Pending || st.Config.Mode != netcfg.ModeStatic || st.Config.Address != "192.168.1.10/24" || st.Interface != "eno1" {
		t.Fatalf("%+v %v", st, err)
	}
	if code(NetworkApply(ctx, d, staticCfg())) != apierr.Conflict {
		t.Fatal("zweite Änderung während Bestätigung")
	}
	if err := NetworkConfirm(ctx, d); err != nil || !f.Called("systemctl stop bnas-net-rollback.timer") {
		t.Fatal(err, f.Calls)
	}
	f.Responses["systemctl is-active bnas-net-rollback.timer"] = sysexec.Result{ExitCode: 3, Stdout: []byte("inactive\n")}
	if st, _ := NetworkStatus(ctx, d); st.Pending {
		t.Fatal("nach Bestätigung nicht mehr offen")
	}
	if code(NetworkConfirm(ctx, d)) != apierr.Conflict {
		t.Fatal("zweite Bestätigung")
	}
}

func TestNetworkApplyDHCPRemovesDropinAndKeepsBackup(t *testing.T) {
	d, f, root := bridgeEnv(t)
	f.Responses["systemctl is-active bnas-net-rollback.timer"] = sysexec.Result{Stdout: []byte("active\n")}
	ctx := context.Background()
	dir := filepath.Join(root, "etc/systemd/network/10-betternas.network.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := netcfg.RenderIPDropin(staticCfg())
	if err := os.WriteFile(filepath.Join(dir, "10-bnas-ip.conf"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NetworkApply(ctx, d, netcfg.IPConfig{Mode: netcfg.ModeDHCP}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "10-bnas-ip.conf")); err == nil {
		t.Fatal("Drop-in muss weg sein")
	}
	if read(t, root, "var/lib/betternas/network/ip.conf.bak") != old {
		t.Fatal("Sicherung fehlt")
	}
	if rb := read(t, root, "var/lib/betternas/network/rollback.sh"); !strings.Contains(rb, "cp /var/lib/betternas/network/ip.conf.bak") {
		t.Fatal(rb)
	}
	if err := NetworkRevert(ctx, d); err != nil || !f.Called("/bin/sh /var/lib/betternas/network/rollback.sh") {
		t.Fatal(err, f.Calls)
	}
}

func TestNetworkApplyInvalidChangesNothing(t *testing.T) {
	d, f, root := bridgeEnv(t)
	c := staticCfg()
	c.Gateway = "10.9.9.9"
	if code(NetworkApply(context.Background(), d, c)) != apierr.InvalidParams {
		t.Fatal("ungültig")
	}
	if len(f.Calls) > 5 || f.Called("systemd-run") || f.Called("networkctl") {
		t.Fatal(f.Calls)
	}
	if _, err := os.Stat(filepath.Join(root, "etc/systemd/network/10-betternas.network.d")); err == nil {
		t.Fatal("nichts darf geschrieben werden")
	}
}

func TestNetworkApplyRollsBackOnError(t *testing.T) {
	d, f, _ := bridgeEnv(t)
	f.Responses["networkctl reconfigure"] = sysexec.Result{ExitCode: 1, Stderr: []byte("boom")}
	if code(NetworkApply(context.Background(), d, staticCfg())) != apierr.Internal {
		t.Fatal("Fehler erwartet")
	}
	if !f.Called("/bin/sh /var/lib/betternas/network/rollback.sh") || !f.Called("systemctl stop bnas-net-rollback.timer") {
		t.Fatal(f.Calls)
	}
}

func TestNetworkApplyNeedsRollbackTimer(t *testing.T) {
	d, f, root := bridgeEnv(t)
	f.Responses["systemd-run"] = sysexec.Result{ExitCode: 1, Stderr: []byte("nope")}
	if code(NetworkApply(context.Background(), d, staticCfg())) != apierr.Internal {
		t.Fatal("Fehler erwartet")
	}
	if _, err := os.Stat(filepath.Join(root, "etc/systemd/network/10-betternas.network.d")); err == nil {
		t.Fatal("ohne Rückfall wird nichts geändert")
	}
}

func TestNetworkTargetIsBridgeWhenActive(t *testing.T) {
	d, f, root := bridgeEnv(t)
	f.Responses["systemctl is-active bnas-net-rollback.timer"] = sysexec.Result{Stdout: []byte("active\n")}
	ctx := context.Background()
	if err := BridgeEnable(ctx, d, "eno1"); err != nil {
		t.Fatal(err)
	}
	if err := BridgeConfirm(ctx, d); err != nil {
		t.Fatal(err)
	}
	f.Responses["systemctl is-active bnas-br0-rollback.timer"] = sysexec.Result{ExitCode: 3}
	if err := NetworkApply(ctx, d, staticCfg()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, root, "etc/systemd/network/05-bnas-br0.network.d/10-bnas-ip.conf"), "Address=192.168.1.10/24") {
		t.Fatal("Drop-in gehört zu br0")
	}
	if !f.Called("networkctl reconfigure br0") {
		t.Fatal(f.Calls)
	}
}

func TestBridgeEnableMovesStaticConfig(t *testing.T) {
	d, _, root := bridgeEnv(t)
	dir := filepath.Join(root, "etc/systemd/network/10-betternas.network.d")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "10-bnas-ip.conf"), []byte(netcfg.RenderIPDropin(staticCfg())), 0o644)
	if err := BridgeEnable(context.Background(), d, "eno1"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, root, "etc/systemd/network/05-bnas-br0.network.d/10-bnas-ip.conf"), "Address=192.168.1.10/24") {
		t.Fatal("statische IP muss auf die Bridge mitziehen")
	}
}
