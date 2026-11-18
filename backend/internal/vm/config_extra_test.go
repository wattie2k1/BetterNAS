package vm

import (
	"strings"
	"testing"
)

func TestParseConfigBootMACUSB(t *testing.T) {
	c, err := ParseConfig(haosXML(t))
	if err != nil {
		t.Fatal(err)
	}
	if c.Boot != "cdrom" || c.MAC != "52:54:00:48:4f:ce" || len(c.USB) != 0 {
		t.Fatalf("%+v", c)
	}
	x := strings.Replace(string(haosXML(t)), "    <boot dev='cdrom'/>\n    <boot dev='hd'/>\n", "", 1)
	x = strings.Replace(x, "<source file='/srv/vms/disks/homeassistant.qcow2'/>", "<source file='/srv/vms/disks/homeassistant.qcow2'/>\n      <boot order='1'/>", 1)
	if c, _ = ParseConfig([]byte(x)); c.Boot != "custom" {
		t.Fatalf("Boot %q", c.Boot)
	}
}

func TestApplyConfigBoot(t *testing.T) {
	u := ConfigUpdate{VCPUs: 2, MemoryMiB: 4096, SecureBoot: true, Network: "default", BootFirst: "hd"}
	out, _, err := ApplyConfig(string(haosXML(t)), u)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "<boot dev='hd'/>\n    <boot dev='cdrom'/>") || strings.Count(out, "<boot dev=") != 2 {
		t.Fatalf("%s", out)
	}
	if c, _ := ParseConfig([]byte(out)); c.Boot != "hd" {
		t.Fatal(c.Boot)
	}
	// ohne Angabe bleibt die Reihenfolge
	u.BootFirst = ""
	if out, _, _ = ApplyConfig(string(haosXML(t)), u); !strings.Contains(out, "<boot dev='cdrom'/>\n    <boot dev='hd'/>") {
		t.Fatal("Reihenfolge verändert")
	}
	// Geräte-Reihenfolge ist nicht änderbar
	x := strings.Replace(string(haosXML(t)), "<source file='/srv/vms/disks/homeassistant.qcow2'/>", "<source file='/srv/vms/disks/homeassistant.qcow2'/>\n      <boot order='1'/>", 1)
	u.BootFirst = "hd"
	if _, _, err := ApplyConfig(x, u); err == nil {
		t.Fatal("custom muss abgelehnt werden")
	}
}

func TestApplyConfigMAC(t *testing.T) {
	u := ConfigUpdate{VCPUs: 2, MemoryMiB: 4096, SecureBoot: true, Network: "default", MAC: "52:54:00:AA:BB:CC"}
	out, _, err := ApplyConfig(string(haosXML(t)), u)
	if err != nil || !strings.Contains(out, "<mac address='52:54:00:aa:bb:cc'/>") || strings.Contains(out, "4f:ce") {
		t.Fatalf("%v\n%s", err, out)
	}
	// Interface ohne MAC-Element
	x := strings.Replace(string(haosXML(t)), "      <mac address='52:54:00:48:4f:ce'/>\n", "", 1)
	if out, _, err = ApplyConfig(x, u); err != nil || !strings.Contains(out, "<interface type='network'>\n      <mac address='52:54:00:aa:bb:cc'/>\n      <source") {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, bad := range []string{"zz:54:00:aa:bb:cc", "52:54:00:aa:bb", "53:54:00:aa:bb:cc", "52-54-00-aa-bb-cc"} {
		u.MAC = bad
		if u.Validate() == nil {
			t.Errorf("%q sollte ungültig sein", bad)
		}
	}
}

func TestApplyConfigUSB(t *testing.T) {
	list := []USBDevice{{Vendor: "1a86", Product: "7523"}, {Vendor: "10c4", Product: "ea60"}}
	u := ConfigUpdate{VCPUs: 2, MemoryMiB: 4096, SecureBoot: true, Network: "default", USB: &list}
	out, _, err := ApplyConfig(string(haosXML(t)), u)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<vendor id='0x1a86'/>", "<product id='0xea60'/>", "startupPolicy='optional'", "managed='yes'"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q fehlt", want)
		}
	}
	c, _ := ParseConfig([]byte(out))
	if len(c.USB) != 2 || c.USB[0].Vendor != "1a86" || c.USB[1].Product != "ea60" {
		t.Fatalf("%+v", c.USB)
	}
	// Unverändert → XML bleibt identisch; leere Liste entfernt alles; nil lässt es stehen.
	if same, _, _ := ApplyConfig(out, u); same != out {
		t.Fatal("unveränderte Liste darf die XML nicht ändern")
	}
	none := []USBDevice{}
	u.USB = &none
	if out2, _, _ := ApplyConfig(out, u); strings.Contains(out2, "hostdev") {
		t.Fatal("hostdev nicht entfernt")
	}
	u.USB = nil
	if out3, _, _ := ApplyConfig(out, u); !strings.Contains(out3, "<vendor id='0x1a86'/>") {
		t.Fatal("nil muss die Geräte behalten")
	}
	for _, bad := range [][]USBDevice{{{Vendor: "zzzz", Product: "7523"}}, {{Vendor: "1a86", Product: "7523"}, {Vendor: "1a86", Product: "7523"}}, make([]USBDevice, 9)} {
		bad := bad
		u.USB = &bad
		if u.Validate() == nil {
			t.Errorf("%+v sollte ungültig sein", bad)
		}
	}
}
