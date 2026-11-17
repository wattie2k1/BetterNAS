package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/sysexec"
)

const origNet = "# Verwaltet von BetterNAS – nicht manuell bearbeiten\n[Match]\nName=eno1\n\n[Network]\nDHCP=yes\nMulticastDNS=no\n"

func bridgeEnv(t *testing.T) (Deps, *sysexec.FakeRunner, string) {
	t.Helper()
	d, f, root := vmEnv(t)
	writeFile(t, root, "/sys/class/net/eno1/device/x")
	if err := os.WriteFile(filepath.Join(root, "sys/class/net/eno1/address"), []byte("D8:CB:8A:22:15:2B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "etc/systemd/network"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc/systemd/network/10-betternas.network"), []byte(origNet), 0o644); err != nil {
		t.Fatal(err)
	}
	f.Responses["systemctl is-active bnas-br0-rollback.timer"] = sysexec.Result{Stdout: []byte("active\n")}
	return d, f, root
}

func read(t *testing.T, root, p string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, p))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBridgeEnableConfirmDisable(t *testing.T) {
	d, f, root := bridgeEnv(t)
	ctx := context.Background()
	if st, _ := BridgeStatus(ctx, d); st.Active || len(st.Candidates) != 1 {
		t.Fatalf("%+v", st)
	}
	if err := BridgeEnable(ctx, d, "eno1"); err != nil {
		t.Fatal(err)
	}
	// Rückfall wird VOR der Änderung scharf gemacht, danach wird angewendet.
	assertOrder(t, f, "systemd-run --unit=bnas-br0-rollback --on-active=120", "networkctl reload", "networkctl reconfigure eno1 br0")
	if nd := read(t, root, "etc/systemd/network/05-bnas-br0.netdev"); !strings.Contains(nd, "MACAddress=d8:cb:8a:22:15:2b") {
		t.Fatal(nd)
	}
	if b := read(t, root, "etc/systemd/network/05-bnas-br0.network"); !strings.Contains(b, "DHCP=yes") || !strings.Contains(b, "Name=br0") {
		t.Fatal(b)
	}
	if !strings.Contains(read(t, root, "etc/systemd/network/05-bnas-br0-port.network"), "Bridge=br0") {
		t.Fatal("port")
	}
	if read(t, root, "etc/systemd/network/10-betternas.network") != origNet {
		t.Fatal("die bisherige Konfiguration darf nicht verändert werden")
	}
	st, _ := BridgeStatus(ctx, d)
	if !st.Active || !st.Pending || st.Interface != "eno1" || st.Deadline == 0 {
		t.Fatalf("%+v", st)
	}
	if code(BridgeEnable(ctx, d, "eno1")) != apierr.Conflict {
		t.Fatal("doppelt")
	}
	if err := BridgeConfirm(ctx, d); err != nil || !f.Called("systemctl stop bnas-br0-rollback.timer") {
		t.Fatal(err, f.Calls)
	}
	f.Responses["systemctl is-active bnas-br0-rollback.timer"] = sysexec.Result{ExitCode: 3, Stdout: []byte("inactive\n")}
	st, _ = BridgeStatus(ctx, d)
	if !st.Active || st.Pending {
		t.Fatalf("%+v", st)
	}
	if code(BridgeConfirm(ctx, d)) != apierr.Conflict {
		t.Fatal("zweite Bestätigung")
	}
	if err := BridgeDisable(ctx, d); err != nil || !f.Called("/bin/sh /var/lib/betternas/bridge/rollback.sh") {
		t.Fatal(err, f.Calls)
	}
}

func TestBridgeEnableRollsBackOnApplyError(t *testing.T) {
	d, f, _ := bridgeEnv(t)
	f.Responses["networkctl reload"] = sysexec.Result{ExitCode: 1, Stderr: []byte("boom")}
	if code(BridgeEnable(context.Background(), d, "eno1")) != apierr.Internal {
		t.Fatal("Fehler erwartet")
	}
	if !f.Called("/bin/sh /var/lib/betternas/bridge/rollback.sh") || !f.Called("systemctl stop bnas-br0-rollback.timer") {
		t.Fatal(f.Calls)
	}
}

func TestBridgeEnableChangesNothingWithoutRollbackTimer(t *testing.T) {
	d, f, root := bridgeEnv(t)
	f.Responses["systemd-run"] = sysexec.Result{ExitCode: 1, Stderr: []byte("nope")}
	if code(BridgeEnable(context.Background(), d, "eno1")) != apierr.Internal {
		t.Fatal("Fehler erwartet")
	}
	if _, err := os.Stat(filepath.Join(root, "etc/systemd/network/05-bnas-br0.netdev")); err == nil || f.Called("networkctl") {
		t.Fatal("nichts darf geändert werden")
	}
}

func TestBridgeEnableRefusals(t *testing.T) {
	ctx := context.Background()
	d, f, root := bridgeEnv(t)
	for _, bad := range []string{"", "eth9", "E0", "a b"} {
		if c := code(BridgeEnable(ctx, d, bad)); c != apierr.InvalidParams {
			t.Errorf("%q: %s", bad, c)
		}
	}
	// manuell angepasste Konfiguration
	if err := os.WriteFile(filepath.Join(root, "etc/systemd/network/10-betternas.network"), []byte("[Match]\nName=eno1\nMACAddress=aa:bb:cc:dd:ee:ff\n[Network]\nDHCP=yes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code(BridgeEnable(ctx, d, "eno1")) != apierr.Conflict {
		t.Fatal("manuelle Konfiguration")
	}
	// VM mit macvtap auf eno1
	d, f, _ = bridgeEnv(t)
	f.Responses[v+"list --all --name"] = sysexec.Result{Stdout: []byte("debian\n")}
	f.Responses[v+"dominfo debian"] = sysexec.Result{Stdout: []byte("Name: debian\nState: shut off\n")}
	f.Responses[v+"domiflist debian"] = sysexec.Result{Stdout: []byte(" Interface   Type     Source   Model    MAC\n-----------------------------------------------\n -           direct   eno1     virtio   52:54:00:aa:bb:cc\n")}
	if code(BridgeEnable(ctx, d, "eno1")) != apierr.Conflict || f.Called("systemd-run") {
		t.Fatal("VM mit LAN-Netz über eno1", f.Calls)
	}
}

func TestBridgeDisableRefusedWhileVMUsesIt(t *testing.T) {
	ctx := context.Background()
	d, f, _ := bridgeEnv(t)
	if err := BridgeEnable(ctx, d, "eno1"); err != nil {
		t.Fatal(err)
	}
	f.Responses[v+"list --all --name"] = sysexec.Result{Stdout: []byte("debian\n")}
	f.Responses[v+"dominfo debian"] = sysexec.Result{Stdout: []byte("Name: debian\nState: shut off\n")}
	f.Responses[v+"domiflist debian"] = sysexec.Result{Stdout: []byte(" Interface   Type     Source   Model    MAC\n-----------------------------------------------\n -           bridge   br0      virtio   52:54:00:aa:bb:cc\n")}
	n := len(f.Calls)
	if code(BridgeDisable(ctx, d)) != apierr.Conflict || f.Called("/bin/sh /var/lib/betternas/bridge/rollback.sh") || len(f.Calls) < n {
		t.Fatal(f.Calls)
	}
}
