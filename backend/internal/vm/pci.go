package vm

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// MaxPCIDevices ist die Höchstzahl durchgereichter PCI-Geräte je VM.
const MaxPCIDevices = 8

var (
	pciAddrRe   = regexp.MustCompile(`^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`)
	rePCIHostdv = regexp.MustCompile(`(?s)[ \t]*<hostdev\b[^>]*type='pci'[^>]*>.*?</hostdev>\n?`)
)

// PCIDevice ist ein PCI-Gerät des Hosts, das sich an eine VM durchreichen ließe (oder nicht).
type PCIDevice struct {
	Address string `json:"address"`           // 0000:01:00.0
	ID      string `json:"id"`                // Hersteller:Gerät, z. B. 8086:8c20
	Name    string `json:"name"`              // Klartextname aus der udev-Datenbank, sonst die ID
	Kind    string `json:"kind"`              // gpu | audio | usb | net | storage | other
	Group   int    `json:"group"`             // IOMMU-Gruppe; -1 = keine
	Driver  string `json:"driver,omitempty"`  // aktuell am Host gebundener Treiber
	Blocked string `json:"blocked,omitempty"` // Grund, warum sich das Gerät nicht durchreichen lässt; "" = erlaubt
	Note    string `json:"note,omitempty"`    // Hinweis, der trotzdem erlaubt
}

// PCIInfo ist die Geräteliste samt IOMMU-Status.
type PCIInfo struct {
	IOMMU   bool        `json:"iommu"`
	Devices []PCIDevice `json:"devices"`
}

// ValidPCIAddress prüft eine PCI-Adresse (Kleinbuchstaben, „0000:00:1b.0“).
func ValidPCIAddress(a string) bool { return pciAddrRe.MatchString(a) }

func validPCI(list []string) bool {
	if len(list) > MaxPCIDevices {
		return false
	}
	seen := map[string]bool{}
	for _, a := range list {
		if !ValidPCIAddress(a) || seen[a] {
			return false
		}
		seen[a] = true
	}
	return true
}

func samePCI(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, x := range a {
		set[x] = true
	}
	for _, x := range b {
		if !set[x] {
			return false
		}
	}
	return true
}

// pciHostdevXML ist das <hostdev> für ein PCI-Gerät; libvirt (managed) löst es beim Start vom Host-Treiber
// und gibt es beim Beenden der VM wieder zurück.
func pciHostdevXML(addr string) string {
	dom, _ := strconv.ParseUint(addr[0:4], 16, 16)
	bus, _ := strconv.ParseUint(addr[5:7], 16, 8)
	slot, _ := strconv.ParseUint(addr[8:10], 16, 8)
	fn, _ := strconv.ParseUint(addr[11:12], 16, 8)
	return fmt.Sprintf("    <hostdev mode='subsystem' type='pci' managed='yes'>\n      <source>\n        <address domain='0x%04x' bus='0x%02x' slot='0x%02x' function='0x%x'/>\n      </source>\n    </hostdev>\n", dom, bus, slot, fn)
}

// pciAddr formatiert die Attribute eines <address>-Elements als „0000:01:00.0“.
func pciAddr(domain, bus, slot, function string) string {
	h := func(s string) uint64 {
		n, _ := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(s), "0x"), 16, 32)
		return n
	}
	return fmt.Sprintf("%04x:%02x:%02x.%x", h(domain), h(bus), h(slot), h(function))
}

func applyPCI(out string, want []string) (string, error) {
	if !strings.Contains(out, "  </devices>") {
		return "", invalid("Die VM hat keinen Geräte-Abschnitt")
	}
	out = rePCIHostdv.ReplaceAllString(out, "")
	var b strings.Builder
	for _, a := range want {
		b.WriteString(pciHostdevXML(a))
	}
	return strings.Replace(out, "  </devices>", b.String()+"  </devices>", 1), nil
}
