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
)

func readIPState(d Deps) (netcfg.IPState, bool) {
	var st netcfg.IPState
	b, err := d.FS.ReadFile(netcfg.IPStateFile)
	if err != nil || json.Unmarshal(b, &st) != nil {
		return st, false
	}
	return st, true
}

func ipTimerActive(ctx context.Context, d Deps) bool {
	res, err := d.Runner.Run(ctx, nil, "systemctl", "is-active", netcfg.IPRollbackUnit+".timer")
	return err == nil && res.ExitCode == 0 && strings.TrimSpace(string(res.Stdout)) == "active"
}

// ipTarget bestimmt .network-Datei und Schnittstelle, die die IP-Konfiguration trägt.
func ipTarget(d Deps) (file, iface string, bridge bool, err error) {
	bridge = d.FS.Exists(netcfg.Dir + "/" + netcfg.NetdevFile)
	main, rerr := d.FS.ReadFile(netcfg.Dir + "/" + netcfg.MainNetworkFile)
	if !bridge && rerr != nil {
		return "", "", false, apierr.New(apierr.Conflict, "Die Netzwerkkonfiguration von BetterNAS wurde nicht gefunden")
	}
	file, iface = netcfg.IPTarget(bridge, string(main))
	if iface == "" || !netcfg.IfaceRe.MatchString(iface) {
		return "", "", false, apierr.New(apierr.Conflict, "Die Netzwerkkonfiguration wurde manuell angepasst und kann hier nicht bearbeitet werden")
	}
	return file, iface, bridge, nil
}

// NetworkStatus liefert die Netzwerkkonfiguration des Hosts.
func NetworkStatus(ctx context.Context, d Deps) (netcfg.IPStatus, error) {
	st := netcfg.IPStatus{Config: netcfg.IPConfig{Mode: netcfg.ModeDHCP, DNS: []string{}, Domains: []string{}}, Interfaces: []netcfg.IfaceInfo{},
		Current: netcfg.Current{Addresses: []string{}, DNS: []string{}, Domains: []string{}}}
	file, iface, bridge, err := ipTarget(d)
	if err != nil {
		return st, err
	}
	st.Interface, st.BridgeActive = iface, bridge
	if b, rerr := d.FS.ReadFile(netcfg.DropinPath(file)); rerr == nil {
		st.Config = netcfg.ParseIPDropin(string(b))
	}
	if ps, ok := readIPState(d); ok && ps.Pending && ipTimerActive(ctx, d) {
		st.Pending, st.Deadline = true, ps.Deadline
	}
	if res, e := d.Runner.Run(ctx, nil, "hostname"); e == nil && res.ExitCode == 0 {
		st.Hostname = strings.TrimSpace(string(res.Stdout))
	}
	if res, e := d.Runner.Run(ctx, nil, "ip", "-j", "addr"); e == nil && res.ExitCode == 0 {
		st.Interfaces = netcfg.ParseIfaces(res.Stdout)
		for _, i := range st.Interfaces {
			if i.Name == iface {
				st.Current.Addresses = i.Addresses
			}
		}
	}
	if res, e := d.Runner.Run(ctx, nil, "ip", "-j", "route", "show", "default"); e == nil && res.ExitCode == 0 {
		st.Current.Gateway = netcfg.ParseDefaultGateway(res.Stdout)
	}
	if res, e := d.Runner.Run(ctx, nil, "resolvectl", "dns", iface); e == nil && res.ExitCode == 0 {
		st.Current.DNS = netcfg.ParseResolvectl(res.Stdout)
	}
	if res, e := d.Runner.Run(ctx, nil, "resolvectl", "domain", iface); e == nil && res.ExitCode == 0 {
		st.Current.Domains = netcfg.ParseResolvectl(res.Stdout)
	}
	return st, nil
}

func ipRollbackNow(ctx context.Context, d Deps) {
	_, _ = d.Runner.Run(ctx, nil, "systemctl", "stop", netcfg.IPRollbackUnit+".timer")
	_, _ = d.Runner.Run(ctx, nil, "/bin/sh", netcfg.IPRollbackFile)
}

// NetworkApply schreibt die IP-Konfiguration. Sie gilt nur vorläufig: ohne Bestätigung stellt ein Timer nach
// ConfirmSeconds die vorherige Konfiguration wieder her.
func NetworkApply(ctx context.Context, d Deps, cfg netcfg.IPConfig) error {
	mutate.Lock()
	defer mutate.Unlock()
	if err := cfg.Validate(); err != nil {
		return err
	}
	if ps, ok := readIPState(d); ok && ps.Pending && ipTimerActive(ctx, d) {
		return apierr.New(apierr.Conflict, "Eine Netzwerk-Änderung wartet noch auf Bestätigung")
	}
	if bs, ok := readBridgeState(d); ok && bs.Pending && timerActive(ctx, d) {
		return apierr.New(apierr.Conflict, "Eine Bridge-Änderung wartet noch auf Bestätigung")
	}
	file, iface, _, err := ipTarget(d)
	if err != nil {
		return err
	}
	dropin := netcfg.DropinPath(file)
	old, oerr := d.FS.ReadFile(dropin)
	hadBackup := oerr == nil
	if err := d.FS.MkdirAll(netcfg.IPStateDir, 0o755); err != nil {
		return err
	}
	if hadBackup {
		if err := d.FS.WriteFile(netcfg.IPBackupFile, old, 0o600); err != nil {
			return err
		}
	} else {
		_ = d.FS.Remove(netcfg.IPBackupFile)
	}
	if err := d.FS.WriteFile(netcfg.IPRollbackFile, []byte(netcfg.IPRollbackScript(dropin, iface, hadBackup)), 0o755); err != nil {
		return err
	}
	st, _ := json.Marshal(netcfg.IPState{Interface: iface, Pending: true, Deadline: time.Now().Add(netcfg.ConfirmSeconds * time.Second).Unix()})
	if err := d.FS.WriteFile(netcfg.IPStateFile, st, 0o600); err != nil {
		return err
	}
	_, _ = d.Runner.Run(ctx, nil, "systemctl", "reset-failed", netcfg.IPRollbackUnit+".service")
	res, err := d.Runner.Run(ctx, nil, "systemd-run", "--unit="+netcfg.IPRollbackUnit, fmt.Sprintf("--on-active=%d", netcfg.ConfirmSeconds),
		"--description=BetterNAS: Netzwerk-Änderung zurücknehmen, falls nicht bestätigt", "/bin/sh", netcfg.IPRollbackFile)
	if err != nil || res.ExitCode != 0 {
		_ = d.FS.Remove(netcfg.IPStateFile)
		return apierr.New(apierr.Internal, "Der automatische Rückfall konnte nicht eingerichtet werden; es wurde nichts geändert").With("detail", strings.TrimSpace(string(res.Stderr)))
	}
	if cfg.Mode == netcfg.ModeStatic {
		if err := d.FS.MkdirAll(netcfg.Dir+"/"+file+".d", 0o755); err != nil {
			ipRollbackNow(ctx, d)
			return err
		}
		if err := d.FS.WriteFile(dropin, []byte(netcfg.RenderIPDropin(cfg)), 0o644); err != nil {
			ipRollbackNow(ctx, d)
			return err
		}
	} else if err := d.FS.Remove(dropin); err != nil && d.FS.Exists(dropin) {
		ipRollbackNow(ctx, d)
		return err
	}
	for _, args := range [][]string{{"reload"}, {"reconfigure", iface}} {
		if res, err := d.Runner.Run(ctx, nil, "networkctl", args...); err != nil || res.ExitCode != 0 {
			ipRollbackNow(ctx, d)
			return apierr.New(apierr.Internal, "Die Netzwerkkonfiguration konnte nicht angewendet werden; die vorherige wurde wiederhergestellt").With("detail", strings.TrimSpace(string(res.Stderr)))
		}
	}
	return nil
}

// NetworkConfirm bestätigt die neue Konfiguration und stoppt den Rückfall.
func NetworkConfirm(ctx context.Context, d Deps) error {
	mutate.Lock()
	defer mutate.Unlock()
	ps, ok := readIPState(d)
	if !ok || !ps.Pending || !ipTimerActive(ctx, d) {
		return apierr.New(apierr.Conflict, "Es wartet keine Netzwerk-Änderung auf Bestätigung")
	}
	if res, err := d.Runner.Run(ctx, nil, "systemctl", "stop", netcfg.IPRollbackUnit+".timer"); err != nil || res.ExitCode != 0 {
		return apierr.New(apierr.Internal, "Der automatische Rückfall konnte nicht gestoppt werden").With("detail", strings.TrimSpace(string(res.Stderr)))
	}
	ps.Pending, ps.Deadline = false, 0
	b, _ := json.Marshal(ps)
	return d.FS.WriteFile(netcfg.IPStateFile, b, 0o600)
}

// NetworkRevert nimmt eine unbestätigte Änderung sofort zurück.
func NetworkRevert(ctx context.Context, d Deps) error {
	mutate.Lock()
	defer mutate.Unlock()
	ps, ok := readIPState(d)
	if !ok || !ps.Pending || !ipTimerActive(ctx, d) {
		return apierr.New(apierr.Conflict, "Es wartet keine Netzwerk-Änderung auf Bestätigung")
	}
	ipRollbackNow(ctx, d)
	return nil
}

func registerNetwork(s *rpc.Server, d Deps) {
	s.Handle("network.status", func(ctx context.Context, _ json.RawMessage) (any, error) { return NetworkStatus(ctx, d) })
	s.Handle("network.configure", func(ctx context.Context, p json.RawMessage) (any, error) {
		var cfg netcfg.IPConfig
		if err := decode(p, &cfg); err != nil {
			return nil, err
		}
		return nil, NetworkApply(ctx, d, cfg)
	})
	s.Handle("network.confirm", func(ctx context.Context, _ json.RawMessage) (any, error) { return nil, NetworkConfirm(ctx, d) })
	s.Handle("network.revert", func(ctx context.Context, _ json.RawMessage) (any, error) { return nil, NetworkRevert(ctx, d) })
}
