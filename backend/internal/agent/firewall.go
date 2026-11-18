package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/firewall"
	"github.com/betternas/betternas/internal/rpc"
)

// FirewallStatus ist der Zustand der Firewall für die Oberfläche.
type FirewallStatus struct {
	Config   firewall.Config    `json:"config"`
	Services []firewall.Service `json:"services"`
	Active   bool               `json:"active"` // Tabelle ist im Kernel geladen
	Pending  bool               `json:"pending"`
	Deadline int64              `json:"deadline"`
	SSHPort  int                `json:"sshPort"`
}

func readFirewallConfig(d Deps) firewall.Config {
	c := firewall.Default()
	if b, err := d.FS.ReadFile(firewall.ConfigFile); err == nil {
		var in firewall.Config
		if json.Unmarshal(b, &in) == nil && in.Normalize() == nil {
			c = in
		}
	}
	return c
}

func readFirewallState(d Deps) (firewall.State, bool) {
	var st firewall.State
	b, err := d.FS.ReadFile(firewall.StateFile)
	if err != nil || json.Unmarshal(b, &st) != nil {
		return st, false
	}
	return st, true
}

func firewallTimerActive(ctx context.Context, d Deps) bool {
	return unitActive(ctx, d, firewall.RollbackUnit+".timer")
}

func firewallPending(ctx context.Context, d Deps) (firewall.State, bool) {
	st, ok := readFirewallState(d)
	return st, ok && st.Pending && firewallTimerActive(ctx, d)
}

// GetFirewall liefert Konfiguration und Zustand.
func GetFirewall(ctx context.Context, d Deps) FirewallStatus {
	port := sshPort(d)
	svcs := make([]firewall.Service, len(firewall.Services))
	copy(svcs, firewall.Services)
	for i := range svcs {
		if svcs[i].ID == "ssh" {
			svcs[i].Ports = []string{fmt.Sprintf("tcp/%d", port)}
		}
	}
	st := FirewallStatus{Config: readFirewallConfig(d), Services: svcs, SSHPort: port}
	if res, err := d.Runner.Run(ctx, nil, "nft", "list", "table", "inet", firewall.Table); err == nil && res.ExitCode == 0 {
		st.Active = true
	}
	if ps, ok := firewallPending(ctx, d); ok {
		st.Pending, st.Deadline = true, ps.Deadline
	}
	return st
}

func firewallRollbackNow(ctx context.Context, d Deps) {
	_, _ = d.Runner.Run(ctx, nil, "systemctl", "stop", firewall.RollbackUnit+".timer")
	_, _ = d.Runner.Run(ctx, nil, "/bin/sh", firewall.RollbackFile)
}

// SetFirewall übernimmt die Konfiguration. Sie gilt nur vorläufig: ohne Bestätigung stellt ein Timer nach
// ConfirmSeconds die vorherige wieder her. clientIP ist die Adresse des Bearbeiters (Aussperrschutz).
func SetFirewall(ctx context.Context, d Deps, cfg firewall.Config, clientIP string) error {
	mutate.Lock()
	defer mutate.Unlock()
	if err := cfg.Normalize(); err != nil {
		return err
	}
	if err := cfg.CheckLockout(clientIP); err != nil {
		return err
	}
	if _, ok := firewallPending(ctx, d); ok {
		return apierr.New(apierr.Conflict, "Eine Firewall-Änderung wartet noch auf Bestätigung")
	}
	rules := firewall.Render(cfg, sshPort(d))
	// Vorab prüfen, ohne zu laden.
	tmp := firewall.StateDir + "/rules.nft.new"
	if err := d.FS.MkdirAll(firewall.StateDir, 0o755); err != nil {
		return err
	}
	if err := d.FS.WriteFile(tmp, []byte(rules), 0o600); err != nil {
		return err
	}
	defer d.FS.Remove(tmp)
	if res, err := d.Runner.Run(ctx, nil, "nft", "-c", "-f", tmp); err != nil || res.ExitCode != 0 {
		return apierr.New(apierr.InvalidParams, "nftables lehnt die Regeln ab; es wurde nichts geändert.").With("detail", strings.TrimSpace(string(res.Stderr)))
	}

	oldCfg, ecErr := d.FS.ReadFile(firewall.ConfigFile)
	oldRules, erErr := d.FS.ReadFile(firewall.RulesFile)
	hadBackup := ecErr == nil && erErr == nil
	if hadBackup {
		if err := d.FS.WriteFile(firewall.ConfigFile+firewall.BackupSuffix, oldCfg, 0o600); err != nil {
			return err
		}
		if err := d.FS.WriteFile(firewall.RulesFile+firewall.BackupSuffix, oldRules, 0o600); err != nil {
			return err
		}
	}
	if err := d.FS.WriteFile(firewall.RollbackFile, []byte(firewall.RollbackScript(hadBackup)), 0o755); err != nil {
		return err
	}
	st, _ := json.Marshal(firewall.State{Pending: true, Deadline: time.Now().Add(firewall.ConfirmSeconds * time.Second).Unix()})
	if err := d.FS.WriteFile(firewall.StateFile, st, 0o600); err != nil {
		return err
	}
	_, _ = d.Runner.Run(ctx, nil, "systemctl", "reset-failed", firewall.RollbackUnit+".service")
	res, err := d.Runner.Run(ctx, nil, "systemd-run", "--unit="+firewall.RollbackUnit, fmt.Sprintf("--on-active=%d", firewall.ConfirmSeconds),
		"--description=BetterNAS: Firewall-Änderung zurücknehmen, falls nicht bestätigt", "/bin/sh", firewall.RollbackFile)
	if err != nil || res.ExitCode != 0 {
		_ = d.FS.Remove(firewall.StateFile)
		return apierr.New(apierr.Internal, "Der automatische Rückfall konnte nicht eingerichtet werden; es wurde nichts geändert").With("detail", strings.TrimSpace(string(res.Stderr)))
	}
	cb, _ := json.MarshalIndent(cfg, "", "  ")
	if err := d.FS.WriteFile(firewall.ConfigFile, append(cb, '\n'), 0o600); err != nil {
		firewallRollbackNow(ctx, d)
		return err
	}
	if err := d.FS.WriteFile(firewall.RulesFile, []byte(rules), 0o600); err != nil {
		firewallRollbackNow(ctx, d)
		return err
	}
	if res, err := d.Runner.Run(ctx, nil, "nft", "-f", firewall.RulesFile); err != nil || res.ExitCode != 0 {
		firewallRollbackNow(ctx, d)
		return apierr.New(apierr.Internal, "Die Regeln konnten nicht geladen werden; die vorherigen wurden wiederhergestellt").With("detail", strings.TrimSpace(string(res.Stderr)))
	}
	if err := ensureFirewallBootUnit(ctx, d); err != nil {
		firewallRollbackNow(ctx, d)
		return err
	}
	return nil
}

// reloadFirewallForSSHPort passt die geladenen Regeln an einen neuen SSH-Port an, damit eine aktive Firewall den
// neuen Port freigibt. Der Aufrufer hält die mutate-Sperre.
func reloadFirewallForSSHPort(ctx context.Context, d Deps, port int) error {
	cfg := readFirewallConfig(d)
	if !cfg.Enabled {
		return nil
	}
	rules := firewall.Render(cfg, port)
	if err := d.FS.WriteFile(firewall.RulesFile, []byte(rules), 0o600); err != nil {
		return err
	}
	if res, err := d.Runner.Run(ctx, nil, "nft", "-f", firewall.RulesFile); err != nil || res.ExitCode != 0 {
		return apierr.New(apierr.Internal, "Die Firewall-Regel für den neuen SSH-Port konnte nicht geladen werden").With("detail", strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

func ensureFirewallBootUnit(ctx context.Context, d Deps) error {
	if err := d.FS.WriteFile(firewall.BootUnitFile, []byte(firewall.RenderBootUnit()), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", firewall.BootUnit}} {
		if res, err := d.Runner.Run(ctx, nil, "systemctl", args...); err != nil || res.ExitCode != 0 {
			return apierr.New(apierr.Internal, "Das Laden der Firewall beim Start konnte nicht eingerichtet werden").With("detail", strings.TrimSpace(string(res.Stderr)))
		}
	}
	return nil
}

// ConfirmFirewall bestätigt die neue Konfiguration und stoppt den Rückfall.
func ConfirmFirewall(ctx context.Context, d Deps) error {
	mutate.Lock()
	defer mutate.Unlock()
	ps, ok := firewallPending(ctx, d)
	if !ok {
		return apierr.New(apierr.Conflict, "Es wartet keine Firewall-Änderung auf Bestätigung")
	}
	if res, err := d.Runner.Run(ctx, nil, "systemctl", "stop", firewall.RollbackUnit+".timer"); err != nil || res.ExitCode != 0 {
		return apierr.New(apierr.Internal, "Der automatische Rückfall konnte nicht gestoppt werden").With("detail", strings.TrimSpace(string(res.Stderr)))
	}
	ps.Pending, ps.Deadline = false, 0
	b, _ := json.Marshal(ps)
	return d.FS.WriteFile(firewall.StateFile, b, 0o600)
}

// RevertFirewall nimmt eine unbestätigte Änderung sofort zurück.
func RevertFirewall(ctx context.Context, d Deps) error {
	mutate.Lock()
	defer mutate.Unlock()
	if _, ok := firewallPending(ctx, d); !ok {
		return apierr.New(apierr.Conflict, "Es wartet keine Firewall-Änderung auf Bestätigung")
	}
	firewallRollbackNow(ctx, d)
	return nil
}

func registerFirewall(s *rpc.Server, d Deps) {
	s.Handle("firewall.get", func(ctx context.Context, _ json.RawMessage) (any, error) { return GetFirewall(ctx, d), nil })
	s.Handle("firewall.set", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Config   firewall.Config `json:"config"`
			ClientIP string          `json:"clientIp"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return nil, SetFirewall(ctx, d, req.Config, req.ClientIP)
	})
	s.Handle("firewall.confirm", func(ctx context.Context, _ json.RawMessage) (any, error) { return nil, ConfirmFirewall(ctx, d) })
	s.Handle("firewall.revert", func(ctx context.Context, _ json.RawMessage) (any, error) { return nil, RevertFirewall(ctx, d) })
}
