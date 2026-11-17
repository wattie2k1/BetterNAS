package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/netcfg"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/vm"
)

const legacyNetworkFile = netcfg.Dir + "/10-betternas.network"

func readBridgeState(d Deps) (netcfg.State, bool) {
	var st netcfg.State
	b, err := d.FS.ReadFile(netcfg.StateFile)
	if err != nil || json.Unmarshal(b, &st) != nil {
		return st, false
	}
	return st, true
}

func writeBridgeState(d Deps, st netcfg.State) error {
	b, _ := json.Marshal(st)
	return d.FS.WriteFile(netcfg.StateFile, b, 0o600)
}

func timerActive(ctx context.Context, d Deps) bool {
	res, err := d.Runner.Run(ctx, nil, "systemctl", "is-active", netcfg.RollbackUnit+".timer")
	return err == nil && res.ExitCode == 0 && strings.TrimSpace(string(res.Stdout)) == "active"
}

// BridgeStatus liefert den Zustand der Bridge br0.
func BridgeStatus(ctx context.Context, d Deps) (netcfg.Status, error) {
	cands, err := VMInterfaces(ctx, d)
	if err != nil {
		return netcfg.Status{}, err
	}
	st := netcfg.Status{Candidates: cands}
	state, ok := readBridgeState(d)
	if ok && d.FS.Exists(netcfg.Dir+"/"+netcfg.NetdevFile) {
		st.Active = true
		st.Interface = state.Interface
		st.Pending = state.Pending && timerActive(ctx, d)
		if st.Pending {
			st.Deadline = state.Deadline
		}
	}
	return st, nil
}

// vmIfaces liefert die Netzwerkschnittstellen aller VMs.
func vmIfaces(ctx context.Context, d Deps) ([]vm.DomIface, error) {
	st, err := VMStatus(ctx, d)
	if err != nil || !st.Installed {
		return nil, nil // ohne libvirt gibt es keine VMs
	}
	list, err := ListVMs(ctx, d)
	if err != nil {
		return nil, err
	}
	var out []vm.DomIface
	for _, v := range list {
		res, err := virsh(ctx, d, nil, "domiflist", v.Name)
		if err != nil || res.ExitCode != 0 {
			return nil, apierr.New(apierr.Internal, "Die Netzwerkkarten der VM „"+v.Name+"“ konnten nicht gelesen werden")
		}
		out = append(out, vm.ParseDomIfList(res.Stdout)...)
	}
	return out, nil
}

// runRollback führt das Rückfall-Skript sofort aus und stoppt den Timer.
func runRollback(ctx context.Context, d Deps) {
	_, _ = d.Runner.Run(ctx, nil, "systemctl", "stop", netcfg.RollbackUnit+".timer")
	_, _ = d.Runner.Run(ctx, nil, "/bin/sh", netcfg.RollbackFile)
}

// BridgeEnable richtet br0 ein. Die Konfiguration gilt nur vorläufig: ohne Bestätigung (BridgeConfirm) stellt ein
// systemd-Timer nach ConfirmSeconds die alte Konfiguration wieder her.
func BridgeEnable(ctx context.Context, d Deps, iface string) error {
	mutate.Lock()
	defer mutate.Unlock()
	if !netcfg.IfaceRe.MatchString(iface) {
		return apierr.New(apierr.InvalidParams, "Ungültige Netzwerkschnittstelle")
	}
	cands, err := VMInterfaces(ctx, d)
	if err != nil {
		return err
	}
	found := false
	for _, c := range cands {
		found = found || c == iface
	}
	if !found {
		return apierr.New(apierr.InvalidParams, "Die Schnittstelle ist nicht verfügbar: "+iface)
	}
	if d.FS.Exists(netcfg.Dir+"/"+netcfg.NetdevFile) || d.FS.Exists("/sys/class/net/"+netcfg.Name) {
		return apierr.New(apierr.Conflict, "Die Bridge br0 ist bereits eingerichtet")
	}
	used, err := vmIfaces(ctx, d)
	if err != nil {
		return err
	}
	for _, i := range used {
		if i.Type == "direct" && i.Source == iface {
			return apierr.New(apierr.Conflict, "Eine VM nutzt "+iface+" als LAN-Netz (macvtap). Stelle diese VM zuerst auf ein anderes Netz um.")
		}
	}
	orig, err := d.FS.ReadFile(legacyNetworkFile)
	if err != nil {
		return apierr.New(apierr.Conflict, "Die Netzwerkkonfiguration von BetterNAS wurde nicht gefunden")
	}
	sections, err := netcfg.ExtractSections(string(orig), iface)
	if err != nil {
		return err
	}
	macRaw, err := d.FS.ReadFile("/sys/class/net/" + iface + "/address")
	mac := strings.ToLower(strings.TrimSpace(string(macRaw)))
	if err != nil || !netcfg.MACRe.MatchString(mac) {
		return apierr.New(apierr.Internal, "Die MAC-Adresse von "+iface+" konnte nicht gelesen werden")
	}

	if err := d.FS.MkdirAll(netcfg.StateDir, 0o755); err != nil {
		return err
	}
	deadline := time.Now().Add(netcfg.ConfirmSeconds * time.Second).Unix()
	if err := d.FS.WriteFile(netcfg.RollbackFile, []byte(netcfg.RollbackScript(iface)), 0o755); err != nil {
		return err
	}
	if err := writeBridgeState(d, netcfg.State{Interface: iface, Pending: true, Deadline: deadline}); err != nil {
		return err
	}
	_, _ = d.Runner.Run(ctx, nil, "systemctl", "reset-failed", netcfg.RollbackUnit+".service")
	res, err := d.Runner.Run(ctx, nil, "systemd-run", "--unit="+netcfg.RollbackUnit, fmt.Sprintf("--on-active=%d", netcfg.ConfirmSeconds),
		"--description=BetterNAS: Bridge br0 zurücknehmen, falls nicht bestätigt", "/bin/sh", netcfg.RollbackFile)
	if err != nil || res.ExitCode != 0 {
		_ = d.FS.Remove(netcfg.StateFile)
		return apierr.New(apierr.Internal, "Der automatische Rückfall konnte nicht eingerichtet werden; es wurde nichts geändert").With("detail", strings.TrimSpace(string(res.Stderr)))
	}

	files := map[string]string{
		netcfg.NetdevFile: netcfg.RenderNetdev(mac),
		netcfg.PortFile:   netcfg.RenderPort(iface),
		netcfg.BridgeFile: netcfg.RenderBridge(sections),
	}
	for name, content := range files {
		if err := d.FS.WriteFile(netcfg.Dir+"/"+name, []byte(content), 0o644); err != nil {
			runRollback(ctx, d)
			return err
		}
	}
	for _, args := range [][]string{{"reload"}, {"reconfigure", iface, netcfg.Name}} {
		if res, err := d.Runner.Run(ctx, nil, "networkctl", args...); err != nil || res.ExitCode != 0 {
			runRollback(ctx, d)
			return apierr.New(apierr.Internal, "Die Bridge konnte nicht aktiviert werden; die alte Konfiguration wurde wiederhergestellt").With("detail", strings.TrimSpace(string(res.Stderr)))
		}
	}
	return nil
}

// BridgeConfirm bestätigt die neue Konfiguration und stoppt den automatischen Rückfall.
func BridgeConfirm(ctx context.Context, d Deps) error {
	mutate.Lock()
	defer mutate.Unlock()
	st, ok := readBridgeState(d)
	if !ok || !st.Pending || !timerActive(ctx, d) {
		return apierr.New(apierr.Conflict, "Es wartet keine Bridge-Änderung auf Bestätigung")
	}
	if res, err := d.Runner.Run(ctx, nil, "systemctl", "stop", netcfg.RollbackUnit+".timer"); err != nil || res.ExitCode != 0 {
		return apierr.New(apierr.Internal, "Der automatische Rückfall konnte nicht gestoppt werden").With("detail", strings.TrimSpace(string(res.Stderr)))
	}
	st.Pending, st.Deadline = false, 0
	return writeBridgeState(d, st)
}

// BridgeDisable nimmt die Bridge zurück (nur wenn keine VM sie nutzt).
func BridgeDisable(ctx context.Context, d Deps) error {
	mutate.Lock()
	defer mutate.Unlock()
	st, ok := readBridgeState(d)
	if !ok || !d.FS.Exists(netcfg.Dir+"/"+netcfg.NetdevFile) {
		return apierr.New(apierr.Conflict, "Die Bridge br0 ist nicht eingerichtet")
	}
	used, err := vmIfaces(ctx, d)
	if err != nil {
		return err
	}
	for _, i := range used {
		if i.Type == "bridge" && i.Source == netcfg.Name {
			return apierr.New(apierr.Conflict, "Eine VM nutzt die Bridge br0. Stelle sie zuerst auf ein anderes Netz um.")
		}
	}
	if err := d.FS.WriteFile(netcfg.RollbackFile, []byte(netcfg.RollbackScript(st.Interface)), 0o755); err != nil {
		return err
	}
	runRollback(ctx, d)
	return nil
}

func registerBridge(s *rpc.Server, d Deps) {
	s.Handle("network.bridge.status", func(ctx context.Context, _ json.RawMessage) (any, error) { return BridgeStatus(ctx, d) })
	s.Handle("network.bridge.enable", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Interface string `json:"interface"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return nil, BridgeEnable(ctx, d, req.Interface)
	})
	s.Handle("network.bridge.confirm", func(ctx context.Context, _ json.RawMessage) (any, error) { return nil, BridgeConfirm(ctx, d) })
	s.Handle("network.bridge.disable", func(ctx context.Context, _ json.RawMessage) (any, error) { return nil, BridgeDisable(ctx, d) })
}
