package vm

import (
	"strings"
	"testing"
)

func TestPCIApplyAndParse(t *testing.T) {
	list := []string{"0000:00:1b.0", "0000:01:00.1"}
	u := base()
	u.PCI = &list
	out, _, err := ApplyConfig(string(haosXML(t)), u)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"type='pci' managed='yes'", "domain='0x0000' bus='0x00' slot='0x1b' function='0x0'", "bus='0x01' slot='0x00' function='0x1'"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q fehlt:\n%s", want, out)
		}
	}
	c, _ := ParseConfig([]byte(out))
	if len(c.PCI) != 2 || c.PCI[0] != "0000:00:1b.0" || c.PCI[1] != "0000:01:00.1" || len(c.USB) != 0 {
		t.Fatalf("%+v %+v", c.PCI, c.USB)
	}
	if same, _, _ := ApplyConfig(out, u); same != out {
		t.Fatal("unverändert darf die XML nicht ändern")
	}
	// USB-Hostdevs werden von PCI nicht berührt und umgekehrt
	usb := []USBDevice{{Vendor: "1a86", Product: "7523"}}
	u2 := base()
	u2.USB = &usb
	out2, _, _ := ApplyConfig(out, u2)
	c, _ = ParseConfig([]byte(out2))
	if len(c.PCI) != 2 || len(c.USB) != 1 {
		t.Fatalf("%+v %+v", c.PCI, c.USB)
	}
	none := []string{}
	u3 := base()
	u3.PCI = &none
	out3, _, _ := ApplyConfig(out2, u3)
	c, _ = ParseConfig([]byte(out3))
	if len(c.PCI) != 0 || len(c.USB) != 1 {
		t.Fatalf("%+v %+v", c.PCI, c.USB)
	}
	u3.PCI = nil
	if keep, _, _ := ApplyConfig(out, u3); !strings.Contains(keep, "type='pci'") {
		t.Fatal("nil muss die Geräte behalten")
	}
}

func TestPCIValidate(t *testing.T) {
	u := base()
	for _, bad := range [][]string{{"0000:00:1B.0"}, {"00:1b.0"}, {"0000:00:1b.8"}, {"x"}, {"0000:00:1b.0", "0000:00:1b.0"}, make([]string, 9)} {
		bad := bad
		u.PCI = &bad
		if u.Validate() == nil {
			t.Errorf("%v sollte ungültig sein", bad)
		}
	}
	ok := []string{"0000:00:1b.0", "0000:0a:00.7"}
	u.PCI = &ok
	if u.Validate() != nil {
		t.Fatal("gültig")
	}
}
