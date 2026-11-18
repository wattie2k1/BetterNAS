// Package netcfg beschreibt die optionale Host-Bridge br0 für VMs (systemd-networkd).
package netcfg

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
)

const (
	// Dir enthält die networkd-Dateien.
	Dir = "/etc/systemd/network"
	// ManagedHeader kennzeichnet von BetterNAS erzeugte Dateien.
	ManagedHeader = "# Verwaltet von BetterNAS – nicht manuell bearbeiten"

	NetdevFile = "05-bnas-br0.netdev"
	PortFile   = "05-bnas-br0-port.network"
	BridgeFile = "05-bnas-br0.network"

	// StateDir hält Zustand und Rückfall-Skript.
	StateDir     = "/var/lib/betternas/bridge"
	StateFile    = StateDir + "/state.json"
	RollbackFile = StateDir + "/rollback.sh"
	// RollbackUnit ist die transiente systemd-Unit (Timer + Service) für den automatischen Rückfall.
	RollbackUnit = "bnas-br0-rollback"
	// ConfirmSeconds ist die Frist, binnen der die neue Konfiguration bestätigt werden muss.
	ConfirmSeconds = 120

	// FirewallUnit erlaubt Verkehr innerhalb von br0 trotz Docker (siehe RenderFirewallUnit).
	FirewallUnit     = "bnas-br0-fw.service"
	FirewallUnitFile = "/etc/systemd/system/" + FirewallUnit

	// Name der Bridge.
	Name = "br0"
)

var (
	// MACRe ist eine MAC-Adresse.
	MACRe = regexp.MustCompile(`^([0-9a-f]{2}:){5}[0-9a-f]{2}$`)
	// IfaceRe ist eine Netzwerkschnittstelle des Hosts.
	IfaceRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,14}$`)
)

// Status beschreibt den Bridge-Zustand.
type Status struct {
	// Active: br0 existiert und ist von BetterNAS eingerichtet.
	Active bool `json:"active"`
	// Interface ist die Schnittstelle, die an br0 hängt.
	Interface string `json:"interface"`
	// Pending: eingerichtet, aber noch nicht bestätigt (Rückfall läuft).
	Pending bool `json:"pending"`
	// Deadline (Unix-Sekunden) der Bestätigung, solange Pending.
	Deadline int64 `json:"deadline"`
	// Addresses sind die IPv4-Adressen von br0 (nur wenn aktiv); über sie ist das NAS nach der Umstellung erreichbar.
	Addresses []string `json:"addresses"`
	// Hostname des NAS (erreichbar als <Hostname>.local).
	Hostname string `json:"hostname"`
	// Candidates sind die möglichen Schnittstellen (physisch, ohne WLAN).
	Candidates []string `json:"candidates"`
}

// State ist der gespeicherte Zustand (state.json).
type State struct {
	Interface string `json:"interface"`
	Pending   bool   `json:"pending"`
	Deadline  int64  `json:"deadline"`
}

// RenderNetdev erzeugt die .netdev-Datei; br0 übernimmt die MAC der Schnittstelle, damit DHCP die gleiche Adresse vergibt.
func RenderNetdev(mac string) string {
	return fmt.Sprintf("%s\n[NetDev]\nName=%s\nKind=bridge\nMACAddress=%s\n", ManagedHeader, Name, mac)
}

// RenderPort erzeugt die .network-Datei, die die Schnittstelle in die Bridge hängt.
func RenderPort(iface string) string {
	return fmt.Sprintf("%s\n[Match]\nName=%s\n\n[Network]\nBridge=%s\n", ManagedHeader, iface, Name)
}

// RenderBridge erzeugt die .network-Datei der Bridge aus den bisherigen Netzwerk-Abschnitten der Schnittstelle.
func RenderBridge(sections string) string {
	return fmt.Sprintf("%s\n[Match]\nName=%s\n\n%s\n", ManagedHeader, Name, strings.TrimSpace(sections))
}

// ExtractSections liefert alle Abschnitte außer [Match] aus der bisherigen .network-Datei der Schnittstelle.
// Die Datei muss ausschließlich auf diese Schnittstelle passen; sonst wurde sie manuell angepasst und wird nicht umgebaut.
func ExtractSections(content, iface string) (string, error) {
	bad := func() (string, error) {
		return "", apierr.New(apierr.Conflict, "Die Netzwerkkonfiguration von "+iface+" wurde manuell angepasst und kann nicht automatisch umgebaut werden")
	}
	var match, rest []string
	cur := ""
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			cur = t
		}
		switch {
		case cur == "[Match]":
			if t != "" && t != "[Match]" && !strings.HasPrefix(t, "#") {
				match = append(match, t)
			}
		case cur != "":
			rest = append(rest, line)
		}
	}
	if len(match) != 1 || match[0] != "Name="+iface {
		return bad()
	}
	out := strings.TrimSpace(strings.Join(rest, "\n"))
	if !strings.Contains(out, "[Network]") {
		return bad()
	}
	return out, nil
}

// RollbackScript stellt den Zustand ohne Bridge wieder her (Dateien weg, br0 löschen, Schnittstelle neu konfigurieren).
func RollbackScript(iface string) string {
	return fmt.Sprintf(`#!/bin/sh
# Von BetterNAS erzeugt: stellt die Netzwerkkonfiguration ohne Bridge wieder her.
rm -f %[1]s/%[2]s %[1]s/%[3]s %[1]s/%[4]s
ip link set %[5]s down 2>/dev/null
ip link del %[5]s 2>/dev/null
networkctl reload
networkctl reconfigure %[6]s
rm -f %[7]s
systemctl disable --now %[8]s 2>/dev/null
iptables -D DOCKER-USER -i %[5]s -o %[5]s -j ACCEPT 2>/dev/null
rm -f %[9]s
systemctl daemon-reload
`, Dir, NetdevFile, PortFile, BridgeFile, Name, iface, StateFile, FirewallUnit, FirewallUnitFile)
}

// RenderFirewallUnit liefert die systemd-Unit für die Firewall-Regel der Bridge.
// Docker setzt die FORWARD-Policy auf DROP, und mit br_netfilter läuft auch der Verkehr zwischen den Ports von br0
// (VM ↔ Router, z. B. DHCP) durch iptables. Ohne diese Regel bekommen VMs keine IPv4-Adresse. Die Regel steht in
// DOCKER-USER, das Docker nie überschreibt; die Unit hängt an docker.service und läuft nach dessen Neustart erneut.
func RenderFirewallUnit() string {
	rule := "-i " + Name + " -o " + Name + " -j ACCEPT"
	return `# Verwaltet von BetterNAS – nicht manuell bearbeiten
[Unit]
Description=BetterNAS: Verkehr innerhalb der Bridge ` + Name + ` trotz Docker-Firewall erlauben
After=docker.service
PartOf=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c 'iptables -nL DOCKER-USER >/dev/null 2>&1 || exit 0; iptables -C DOCKER-USER ` + rule + ` 2>/dev/null || iptables -I DOCKER-USER 1 ` + rule + `'

[Install]
WantedBy=multi-user.target
`
}

// ParseAddrs liest die IPv4-Adressen aus der Ausgabe von `ip -4 -o addr show`.
func ParseAddrs(out []byte) []string {
	addrs := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		for i, w := range f {
			if w == "inet" && i+1 < len(f) {
				addrs = append(addrs, strings.SplitN(f[i+1], "/", 2)[0])
			}
		}
	}
	return addrs
}
