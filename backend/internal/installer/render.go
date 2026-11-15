package installer

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// RenderInput enthält alles, was für die Konfigurationsdateien des Zielsystems nötig ist.
type RenderInput struct {
	Layout      Layout
	Answers     Answers
	UUIDs       map[string]string // Label → UUID
	Version     string
	InstalledAt time.Time
}

// RenderFiles liefert die Konfigurationsdateien des Zielsystems (Pfad im Ziel → Inhalt).
func RenderFiles(in RenderInput) (map[string][]byte, error) {
	a := in.Answers
	files := map[string][]byte{}

	var fstab strings.Builder
	fstab.WriteString("# Verwaltet von BetterNAS – nicht manuell bearbeiten\n")
	fstab.WriteString("# <dateisystem> <mountpoint> <typ> <optionen> <dump> <pass>\n")
	// Reihenfolge ist wichtig: / vor /boot/efi.
	ordered := make([]PartitionSpec, 0, len(in.Layout.Partitions))
	for _, m := range []string{"/", "/boot/efi", "/srv/appdata"} {
		for _, p := range in.Layout.Partitions {
			if p.Mount == m {
				ordered = append(ordered, p)
			}
		}
	}
	for _, p := range ordered {
		uuid, ok := in.UUIDs[p.Label]
		if !ok || uuid == "" {
			return nil, fmt.Errorf("UUID für %s fehlt", p.Label)
		}
		switch p.Mount {
		case "/":
			fmt.Fprintf(&fstab, "UUID=%s / ext4 errors=remount-ro 0 1\n", uuid)
		case "/boot/efi":
			fmt.Fprintf(&fstab, "UUID=%s /boot/efi vfat umask=0077 0 1\n", uuid)
		case "/srv/appdata":
			fmt.Fprintf(&fstab, "UUID=%s /srv/appdata ext4 defaults,noatime 0 2\n", uuid)
		}
	}
	files["/etc/fstab"] = []byte(fstab.String())

	files["/etc/hostname"] = []byte(a.Hostname + "\n")
	files["/etc/hosts"] = []byte(fmt.Sprintf("127.0.0.1\tlocalhost\n127.0.1.1\t%s.local %s\n::1\tlocalhost ip6-localhost ip6-loopback\n", a.Hostname, a.Hostname))
	files["/etc/timezone"] = []byte(a.Timezone + "\n")
	files["/etc/default/locale"] = []byte("LANG=" + a.Locale() + "\n")

	layout, variant := a.XKB()
	files["/etc/default/keyboard"] = []byte(fmt.Sprintf("XKBMODEL=\"pc105\"\nXKBLAYOUT=\"%s\"\nXKBVARIANT=\"%s\"\nXKBOPTIONS=\"\"\nBACKSPACE=\"guess\"\n", layout, variant))

	files["/etc/systemd/network/10-betternas.network"] = []byte(renderNetwork(a.Net))

	meta, _ := json.MarshalIndent(map[string]string{
		"version":     in.Version,
		"installedAt": in.InstalledAt.UTC().Format(time.RFC3339),
	}, "", "  ")
	files["/etc/betternas/installed.json"] = append(meta, '\n')
	return files, nil
}

func renderNetwork(n NetConfig) string {
	var b strings.Builder
	b.WriteString("# Verwaltet von BetterNAS – nicht manuell bearbeiten\n[Match]\n")
	if n.Interface != "" {
		fmt.Fprintf(&b, "Name=%s\n", n.Interface)
	} else {
		b.WriteString("Name=en* eth*\n")
	}
	b.WriteString("\n[Network]\n")
	if n.DHCP {
		b.WriteString("DHCP=yes\nMulticastDNS=no\n")
		return b.String()
	}
	fmt.Fprintf(&b, "Address=%s\nGateway=%s\n", n.Address, n.Gateway)
	for _, d := range n.DNS {
		fmt.Fprintf(&b, "DNS=%s\n", d)
	}
	return b.String()
}
