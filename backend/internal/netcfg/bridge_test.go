package netcfg

import (
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
)

const orig = "# Verwaltet von BetterNAS – nicht manuell bearbeiten\n[Match]\nName=eno1\n\n[Network]\nDHCP=yes\nMulticastDNS=no\n"

func TestExtractSections(t *testing.T) {
	s, err := ExtractSections(orig, "eno1")
	if err != nil || s != "[Network]\nDHCP=yes\nMulticastDNS=no" {
		t.Fatalf("%q %v", s, err)
	}
	for name, c := range map[string]string{
		"andere Schnittstelle": orig,
		"zusätzliches Match":   strings.Replace(orig, "Name=eno1", "Name=eno1\nMACAddress=aa:bb:cc:dd:ee:ff", 1),
		"kein Network":         "[Match]\nName=eno1\n",
		"Wildcard":             strings.Replace(orig, "Name=eno1", "Name=en*", 1),
	} {
		iface := "eno1"
		if name == "andere Schnittstelle" {
			iface = "eth0"
		}
		if _, err := ExtractSections(c, iface); err == nil || apierr.From(err).Code != apierr.Conflict {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRender(t *testing.T) {
	nd := RenderNetdev("aa:bb:cc:dd:ee:ff")
	if !strings.Contains(nd, "Kind=bridge") || !strings.Contains(nd, "MACAddress=aa:bb:cc:dd:ee:ff") {
		t.Fatal(nd)
	}
	if p := RenderPort("eno1"); !strings.Contains(p, "Name=eno1") || !strings.Contains(p, "Bridge=br0") {
		t.Fatal(p)
	}
	s, _ := ExtractSections(orig, "eno1")
	if b := RenderBridge(s); !strings.Contains(b, "Name=br0") || !strings.Contains(b, "DHCP=yes") || strings.Contains(b, "eno1") {
		t.Fatal(b)
	}
	if r := RollbackScript("eno1"); !strings.Contains(r, "ip link del br0") || !strings.Contains(r, "networkctl reconfigure eno1") {
		t.Fatal(r)
	}
}
