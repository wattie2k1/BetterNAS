// Package firewall erzeugt die nftables-Regeln für die eingehenden Verbindungen des NAS.
//
// BetterNAS verwaltet ausschließlich die eigene Tabelle "inet bnas_fw" (Hook input). Docker (iptables) und
// libvirt bleiben unberührt; veröffentlichte Docker-Ports laufen über FORWARD und werden hier nicht gefiltert.
package firewall

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
)

const (
	Table = "bnas_fw"

	StateDir     = "/var/lib/betternas/firewall"
	ConfigFile   = StateDir + "/config.json"
	RulesFile    = StateDir + "/rules.nft"
	StateFile    = StateDir + "/state.json"
	RollbackFile = StateDir + "/rollback.sh"
	BackupSuffix = ".bak"

	// RollbackUnit ist die transiente systemd-Unit für den automatischen Rückfall.
	RollbackUnit = "bnas-fw-rollback"
	// BootUnit lädt die Regeln beim Systemstart.
	BootUnit     = "bnas-firewall.service"
	BootUnitFile = "/etc/systemd/system/" + BootUnit

	// ConfirmSeconds ist die Frist für die Bestätigung einer Änderung.
	ConfirmSeconds = 120

	ModeAll    = "all"    // aus jedem Netz erreichbar
	ModeLAN    = "lan"    // nur aus privaten Netzen
	ModeCustom = "custom" // nur aus den angegebenen Netzen

	MaxNetworks = 20
)

// Service beschreibt einen Dienst, dessen Erreichbarkeit einstellbar ist.
type Service struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Ports []string `json:"ports"` // z. B. "tcp/443"; SSH wird zur Laufzeit ergänzt
	// Required: der Dienst darf nicht ganz gesperrt werden (Aussperrschutz). Die Modi bleiben wählbar.
	Required bool `json:"required"`
}

// Services sind die steuerbaren Dienste in fester Reihenfolge.
var Services = []Service{
	{ID: "web", Label: "Weboberfläche (HTTPS)", Ports: []string{"tcp/80", "tcp/443"}, Required: true},
	{ID: "ssh", Label: "SSH", Ports: []string{"tcp/22"}},
	{ID: "smb", Label: "Windows-Freigaben (SMB)", Ports: []string{"tcp/139", "tcp/445", "udp/137", "udp/138"}},
	{ID: "nfs", Label: "NFS-Freigaben", Ports: []string{"tcp/111", "udp/111", "tcp/2049", "udp/2049"}},
	{ID: "mdns", Label: "Namensauflösung im LAN (mDNS)", Ports: []string{"udp/5353"}},
}

// Rule legt fest, woher ein Dienst erreichbar ist.
type Rule struct {
	Mode     string   `json:"mode"`
	Networks []string `json:"networks"`
}

// Config ist die gespeicherte Firewall-Konfiguration.
type Config struct {
	Enabled bool            `json:"enabled"`
	Rules   map[string]Rule `json:"rules"`
}

// State ist der Zustand einer laufenden Änderung.
type State struct {
	Pending  bool  `json:"pending"`
	Deadline int64 `json:"deadline"`
}

// lanNets sind die privaten Netze, die der Modus "lan" freigibt.
var (
	lanNets4 = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16"}
	lanNets6 = []string{"fc00::/7", "fe80::/10"}
)

// Default ist die Ausgangskonfiguration: Firewall aus, alle Dienste aus jedem Netz erreichbar.
func Default() Config {
	c := Config{Rules: map[string]Rule{}}
	for _, s := range Services {
		c.Rules[s.ID] = Rule{Mode: ModeAll, Networks: []string{}}
	}
	return c
}

// ServiceByID liefert die Dienstdefinition.
func ServiceByID(id string) (Service, bool) {
	for _, s := range Services {
		if s.ID == id {
			return s, true
		}
	}
	return Service{}, false
}

// Normalize ergänzt fehlende Dienste und bereinigt die Netze. Fehler bei ungültigen Eingaben.
func (c *Config) Normalize() error {
	bad := func(msg string) error { return apierr.New(apierr.InvalidParams, msg) }
	out := Default()
	out.Enabled = c.Enabled
	for id, r := range c.Rules {
		if _, ok := ServiceByID(id); !ok {
			return bad("Unbekannter Dienst: " + id)
		}
		switch r.Mode {
		case ModeAll, ModeLAN:
			r.Networks = []string{}
		case ModeCustom:
			if len(r.Networks) == 0 {
				return bad("Für „nur diese Netze“ muss mindestens ein Netz angegeben werden")
			}
			if len(r.Networks) > MaxNetworks {
				return bad(fmt.Sprintf("Höchstens %d Netze je Dienst", MaxNetworks))
			}
			seen := map[string]bool{}
			nets := []string{}
			for _, n := range r.Networks {
				p, err := ParseNet(n)
				if err != nil {
					return bad("Ungültiges Netz: " + strings.TrimSpace(n) + " (z. B. 192.168.1.0/24 oder 2001:db8::/32)")
				}
				if !seen[p] {
					seen[p] = true
					nets = append(nets, p)
				}
			}
			r.Networks = nets
		default:
			return bad("Ungültiger Zugriffsmodus")
		}
		out.Rules[id] = r
	}
	*c = out
	return nil
}

// ParseNet prüft eine Adresse oder ein Netz und liefert die kanonische CIDR-Schreibweise.
func ParseNet(s string) (string, error) {
	s = strings.TrimSpace(s)
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Masked().String(), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil || a.Zone() != "" {
		return "", fmt.Errorf("ungültig")
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()).String(), nil
}

// Allows sagt, ob die Regel Verkehr von ip durchlässt.
func (r Rule) Allows(ip string) bool {
	a, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return false
	}
	a = a.Unmap().WithZone("")
	switch r.Mode {
	case ModeAll:
		return true
	case ModeLAN:
		return contains(append(append([]string{}, lanNets4...), lanNets6...), a) || a.IsLoopback()
	case ModeCustom:
		return contains(r.Networks, a)
	}
	return false
}

func contains(nets []string, a netip.Addr) bool {
	for _, n := range nets {
		if p, err := netip.ParsePrefix(n); err == nil && p.Contains(a) {
			return true
		}
	}
	return false
}

// CheckLockout verhindert, dass die Änderung die Verbindung des Bearbeiters zur Weboberfläche kappt.
func (c Config) CheckLockout(clientIP string) error {
	if !c.Enabled || clientIP == "" {
		return nil
	}
	if !c.Rules["web"].Allows(clientIP) {
		return apierr.New(apierr.Conflict, "Mit dieser Regel wäre die Weboberfläche für Ihre eigene Adresse ("+clientIP+") gesperrt. Nehmen Sie Ihr Netz in die Liste auf.")
	}
	return nil
}

func splitPort(p string) (proto, port string) {
	proto, port, _ = strings.Cut(p, "/")
	return
}

// setLine liefert die Mengendefinition für den Quelladress-Vergleich.
func addrSets(r Rule) (v4, v6 []string) {
	var nets []string
	switch r.Mode {
	case ModeLAN:
		v4, v6 = append(v4, lanNets4...), append(v6, lanNets6...)
		return
	case ModeCustom:
		nets = r.Networks
	}
	for _, n := range nets {
		if p, err := netip.ParsePrefix(n); err == nil {
			if p.Addr().Is4() {
				v4 = append(v4, n)
			} else {
				v6 = append(v6, n)
			}
		}
	}
	return
}

// Render erzeugt die nft-Datei. Die ersten beiden Zeilen legen die Tabelle an und löschen sie, damit das Laden
// atomar ersetzt. Ist die Firewall aus, enthält die Datei nur diese beiden Zeilen. sshPort überschreibt tcp/22.
func Render(c Config, sshPort int) string {
	var b strings.Builder
	b.WriteString("# Verwaltet von BetterNAS – nicht manuell bearbeiten\n")
	fmt.Fprintf(&b, "table inet %s\ndelete table inet %s\n", Table, Table)
	if !c.Enabled {
		return b.String()
	}
	fmt.Fprintf(&b, "table inet %s {\n\tchain input {\n\t\ttype filter hook input priority filter; policy drop;\n", Table)
	b.WriteString("\t\tiif lo accept\n")
	b.WriteString("\t\tct state established,related accept\n")
	b.WriteString("\t\tct state invalid drop\n")
	b.WriteString("\t\tmeta l4proto { icmp, ipv6-icmp } accept\n")
	b.WriteString("\t\tudp sport 67 udp dport 68 accept\n")
	b.WriteString("\t\tudp sport 547 udp dport 546 accept\n")
	b.WriteString("\t\tiifname { \"docker0\", \"br-*\", \"virbr*\" } accept\n")
	for _, s := range Services {
		r, ok := c.Rules[s.ID]
		if !ok {
			r = Rule{Mode: ModeAll}
		}
		ports := s.Ports
		if s.ID == "ssh" && sshPort > 0 {
			ports = []string{fmt.Sprintf("tcp/%d", sshPort)}
		}
		byProto := map[string][]string{}
		for _, p := range ports {
			proto, port := splitPort(p)
			byProto[proto] = append(byProto[proto], port)
		}
		protos := make([]string, 0, len(byProto))
		for p := range byProto {
			protos = append(protos, p)
		}
		sort.Strings(protos)
		fmt.Fprintf(&b, "\t\t# %s\n", s.Label)
		for _, proto := range protos {
			set := "{ " + strings.Join(byProto[proto], ", ") + " }"
			if r.Mode == ModeAll {
				fmt.Fprintf(&b, "\t\t%s dport %s accept\n", proto, set)
				continue
			}
			v4, v6 := addrSets(r)
			if len(v4) > 0 {
				fmt.Fprintf(&b, "\t\tip saddr { %s } %s dport %s accept\n", strings.Join(v4, ", "), proto, set)
			}
			if len(v6) > 0 {
				fmt.Fprintf(&b, "\t\tip6 saddr { %s } %s dport %s accept\n", strings.Join(v6, ", "), proto, set)
			}
		}
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

// RollbackScript stellt Konfiguration und Regeln aus der Sicherung wieder her und lädt sie. Ohne Sicherung gilt
// "Firewall aus".
func RollbackScript(hadBackup bool) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n# BetterNAS: Firewall-Änderung zurücknehmen\n")
	if hadBackup {
		fmt.Fprintf(&b, "cp %[1]s%[3]s %[1]s && cp %[2]s%[3]s %[2]s\n", ConfigFile, RulesFile, BackupSuffix)
	} else {
		fmt.Fprintf(&b, "rm -f %s\nprintf '%%s\\n' 'table inet %s' 'delete table inet %s' > %s\n", ConfigFile, Table, Table, RulesFile)
	}
	fmt.Fprintf(&b, "nft -f %s\nrm -f %s\n", RulesFile, StateFile)
	return b.String()
}

// RenderBootUnit liefert die systemd-Unit, die die Regeln beim Systemstart lädt.
func RenderBootUnit() string {
	return `[Unit]
Description=BetterNAS: Firewall-Regeln laden
Wants=network-pre.target
Before=network-pre.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/sbin/nft -f ` + RulesFile + `

[Install]
WantedBy=multi-user.target
`
}
