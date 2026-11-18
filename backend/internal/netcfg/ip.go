package netcfg

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
)

const (
	// IPDropinName ist die Drop-in-Datei mit der statischen IP-Konfiguration.
	IPDropinName = "10-bnas-ip.conf"
	// MainNetworkFile ist die .network-Datei der physischen Schnittstelle (ohne Bridge).
	MainNetworkFile = "10-betternas.network"

	// IPStateDir hält Zustand, Sicherung und Rückfall-Skript der IP-Konfiguration.
	IPStateDir     = "/var/lib/betternas/network"
	IPStateFile    = IPStateDir + "/state.json"
	IPBackupFile   = IPStateDir + "/ip.conf.bak"
	IPRollbackFile = IPStateDir + "/rollback.sh"
	// IPRollbackUnit ist die transiente systemd-Unit für den automatischen Rückfall.
	IPRollbackUnit = "bnas-net-rollback"

	ModeDHCP   = "dhcp"
	ModeStatic = "static"
)

var domainRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

// IPConfig ist die IPv4-Konfiguration der Hauptschnittstelle. IPv6 bleibt bei der automatischen Konfiguration.
type IPConfig struct {
	Mode    string   `json:"mode"`
	Address string   `json:"address"` // CIDR, z. B. 192.168.1.10/24
	Gateway string   `json:"gateway"`
	DNS     []string `json:"dns"`
	Domains []string `json:"domains"`
}

// IPState ist der gespeicherte Zustand einer laufenden Änderung.
type IPState struct {
	Interface string `json:"interface"`
	Pending   bool   `json:"pending"`
	Deadline  int64  `json:"deadline"`
}

// Validate prüft die Konfiguration. Bei DHCP werden die Felder ignoriert und geleert.
func (c *IPConfig) Validate() error {
	bad := func(msg string) error { return apierr.New(apierr.InvalidParams, msg) }
	switch c.Mode {
	case ModeDHCP:
		*c = IPConfig{Mode: ModeDHCP, DNS: []string{}, Domains: []string{}}
		return nil
	case ModeStatic:
	default:
		return bad("Ungültiger Modus")
	}
	pfx, err := netip.ParsePrefix(strings.TrimSpace(c.Address))
	if err != nil || !pfx.Addr().Is4() {
		return bad("Ungültige IPv4-Adresse (erwartet z. B. 192.168.1.10/24)")
	}
	if pfx.Bits() < 8 || pfx.Bits() > 30 {
		return bad("Die Netzmaske muss zwischen /8 und /30 liegen")
	}
	addr := pfx.Addr()
	if addr.IsUnspecified() || addr.IsLoopback() || addr.IsMulticast() || addr.IsLinkLocalUnicast() {
		return bad("Diese IPv4-Adresse ist nicht zulässig")
	}
	net := pfx.Masked()
	if addr == net.Addr() || addr == lastAddr(net) {
		return bad("Die Adresse darf nicht die Netz- oder Broadcast-Adresse sein")
	}
	gw, err := netip.ParseAddr(strings.TrimSpace(c.Gateway))
	if err != nil || !gw.Is4() {
		return bad("Ungültiges Gateway")
	}
	if !net.Contains(gw) || gw == addr || gw == net.Addr() || gw == lastAddr(net) {
		return bad("Das Gateway muss im selben Netz liegen und sich von der Adresse unterscheiden")
	}
	if len(c.DNS) == 0 || len(c.DNS) > 3 {
		return bad("Gib 1 bis 3 DNS-Server an")
	}
	dns := make([]string, 0, len(c.DNS))
	for _, s := range c.DNS {
		a, err := netip.ParseAddr(strings.TrimSpace(s))
		if err != nil || a.IsUnspecified() || a.IsMulticast() {
			return bad("Ungültiger DNS-Server: " + s)
		}
		dns = append(dns, a.String())
	}
	if len(c.Domains) > 3 {
		return bad("Höchstens 3 Suchdomänen")
	}
	domains := make([]string, 0, len(c.Domains))
	for _, d := range c.Domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if len(d) > 253 || !domainRe.MatchString(d) {
			return bad("Ungültige Suchdomäne: " + d)
		}
		domains = append(domains, d)
	}
	c.Address, c.Gateway = pfx.String(), gw.String()
	c.DNS, c.Domains = dns, domains
	return nil
}

func lastAddr(p netip.Prefix) netip.Addr {
	b := p.Masked().Addr().As4()
	host := uint32(1)<<(32-p.Bits()) - 1
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	v |= host
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

// RenderIPDropin erzeugt das Drop-in für statische Konfiguration (überschreibt DHCP=yes der Haupt-Datei).
func RenderIPDropin(c IPConfig) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n[Network]\nDHCP=no\nAddress=%s\nGateway=%s\n", ManagedHeader, c.Address, c.Gateway)
	for _, d := range c.DNS {
		fmt.Fprintf(&b, "DNS=%s\n", d)
	}
	if len(c.Domains) > 0 {
		fmt.Fprintf(&b, "Domains=%s\n", strings.Join(c.Domains, " "))
	}
	return b.String()
}

// ParseIPDropin liest ein von RenderIPDropin erzeugtes Drop-in zurück.
func ParseIPDropin(content string) IPConfig {
	c := IPConfig{Mode: ModeStatic, DNS: []string{}, Domains: []string{}}
	for _, line := range strings.Split(content, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "Address":
			c.Address = v
		case "Gateway":
			c.Gateway = v
		case "DNS":
			c.DNS = append(c.DNS, v)
		case "Domains":
			c.Domains = strings.Fields(v)
		}
	}
	return c
}

// IPTarget liefert .network-Datei und Schnittstelle, auf der die IP-Konfiguration gilt.
func IPTarget(bridgeActive bool, mainContent string) (file, iface string) {
	if bridgeActive {
		return BridgeFile, Name
	}
	for _, line := range strings.Split(mainContent, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Name="); ok {
			return MainNetworkFile, v
		}
	}
	return MainNetworkFile, ""
}

// DropinPath ist der Pfad des Drop-ins zu einer .network-Datei.
func DropinPath(file string) string { return Dir + "/" + file + ".d/" + IPDropinName }

// IPRollbackScript stellt das Drop-in aus der Sicherung wieder her (oder entfernt es) und konfiguriert neu.
func IPRollbackScript(dropin, iface string, hadBackup bool) string {
	restore := fmt.Sprintf("rm -f %s", dropin)
	if hadBackup {
		restore = fmt.Sprintf("cp %s %s", IPBackupFile, dropin)
	}
	return fmt.Sprintf(`#!/bin/sh
# Von BetterNAS erzeugt: stellt die vorherige IP-Konfiguration wieder her.
%s
networkctl reload
networkctl reconfigure %s
rm -f %s
`, restore, iface, IPStateFile)
}

// IfaceInfo beschreibt eine Netzwerkschnittstelle des Hosts.
type IfaceInfo struct {
	Name      string   `json:"name"`
	State     string   `json:"state"`
	MAC       string   `json:"mac"`
	Addresses []string `json:"addresses"` // IPv4 als CIDR
}

// Current ist die tatsächlich wirksame Konfiguration der Hauptschnittstelle.
type Current struct {
	Addresses []string `json:"addresses"`
	Gateway   string   `json:"gateway"`
	DNS       []string `json:"dns"`
	Domains   []string `json:"domains"`
}

// IPStatus beschreibt die Netzwerkkonfiguration des Hosts.
type IPStatus struct {
	Interface    string      `json:"interface"`
	BridgeActive bool        `json:"bridgeActive"`
	Config       IPConfig    `json:"config"`
	Current      Current     `json:"current"`
	Interfaces   []IfaceInfo `json:"interfaces"`
	Pending      bool        `json:"pending"`
	Deadline     int64       `json:"deadline"`
	Hostname     string      `json:"hostname"`
}

var virtualIface = regexp.MustCompile(`^(lo|docker\d*|veth.*|br-.*|virbr.*|vnet.*|macvtap.*|tap.*)$`)

// ParseIfaces liest `ip -j addr` und lässt virtuelle Schnittstellen (Docker, libvirt, Loopback) weg.
func ParseIfaces(out []byte) []IfaceInfo {
	var raw []struct {
		Name      string `json:"ifname"`
		State     string `json:"operstate"`
		MAC       string `json:"address"`
		AddrInfos []struct {
			Family string `json:"family"`
			Local  string `json:"local"`
			Prefix int    `json:"prefixlen"`
		} `json:"addr_info"`
	}
	list := []IfaceInfo{}
	if json.Unmarshal(out, &raw) != nil {
		return list
	}
	for _, r := range raw {
		if virtualIface.MatchString(r.Name) {
			continue
		}
		i := IfaceInfo{Name: r.Name, State: strings.ToLower(r.State), MAC: r.MAC, Addresses: []string{}}
		for _, a := range r.AddrInfos {
			if a.Family == "inet" {
				i.Addresses = append(i.Addresses, fmt.Sprintf("%s/%d", a.Local, a.Prefix))
			}
		}
		list = append(list, i)
	}
	return list
}

// ParseDefaultGateway liest das Standard-Gateway aus `ip -j route show default`.
func ParseDefaultGateway(out []byte) string {
	var raw []struct {
		Gateway string `json:"gateway"`
	}
	if json.Unmarshal(out, &raw) != nil || len(raw) == 0 {
		return ""
	}
	return raw[0].Gateway
}

// ParseResolvectl liest die Werte hinter "Link N (name):" aus `resolvectl dns|domain <iface>`.
func ParseResolvectl(out []byte) []string {
	s := string(out)
	if i := strings.Index(s, "):"); i >= 0 {
		s = s[i+2:]
	}
	return append([]string{}, strings.Fields(s)...)
}
