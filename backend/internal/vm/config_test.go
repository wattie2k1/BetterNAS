package vm

import (
	"os"
	"strings"
	"testing"
)

func haosXML(t *testing.T) []byte {
	b, err := os.ReadFile("testdata/haos.xml")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseConfig(t *testing.T) {
	c, err := ParseConfig(haosXML(t))
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "homeassistant" || c.VCPUs != 2 || c.MemoryMiB != 4096 || c.Firmware != "uefi" || !c.SecureBoot || c.TPM {
		t.Fatalf("%+v", c)
	}
	if c.Network != "default" || c.HasCDROM || c.Media != "" || len(c.Disks) != 1 {
		t.Fatalf("%+v", c)
	}
}

func TestParseConfigVariants(t *testing.T) {
	x, _ := BuildXML(Spec{Name: "w", OS: "windows", Firmware: "uefi", Network: "direct:eno1", ISO: "win.iso", VCPUs: 4, MemoryMiB: 8192, DiskGiB: 80, TPM: true}, true)
	c, err := ParseConfig([]byte(x))
	if err != nil {
		t.Fatal(err)
	}
	if c.Network != "direct:eno1" || !c.HasCDROM || c.Media != "win.iso" || !c.TPM || c.SecureBoot {
		t.Fatalf("%+v", c)
	}
	x, _ = BuildXML(Spec{Name: "b", OS: "linux", Firmware: "bios", Network: BridgeNetwork, VCPUs: 1, MemoryMiB: 512, DiskGiB: 5}, false)
	c, _ = ParseConfig([]byte(x))
	if c.Firmware != "bios" || c.Network != BridgeNetwork || c.SecureBoot {
		t.Fatalf("%+v", c)
	}
}

func TestConfigUpdateValidate(t *testing.T) {
	ok := ConfigUpdate{VCPUs: 2, MemoryMiB: 2048, Network: "default"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, u := range []ConfigUpdate{
		{VCPUs: 0, MemoryMiB: 2048, Network: "default"},
		{VCPUs: 2, MemoryMiB: 10, Network: "default"},
		{VCPUs: 2, MemoryMiB: 2048, Network: "a b"},
		{VCPUs: 2, MemoryMiB: 2048, Network: "default", Media: "../x.iso"},
		{VCPUs: 2, MemoryMiB: 2048, Network: "default", Media: "x.qcow2"},
	} {
		if u.Validate() == nil {
			t.Errorf("%+v", u)
		}
	}
}

func TestApplyConfigBasics(t *testing.T) {
	out, rm, err := ApplyConfig(string(haosXML(t)), ConfigUpdate{VCPUs: 4, MemoryMiB: 8192, SecureBoot: true, Network: "direct:eno1", Media: "inst.iso"})
	if err != nil {
		t.Fatal(err)
	}
	if rm != "" {
		t.Fatalf("Secure Boot unverändert, nvram bleibt: %q", rm)
	}
	for _, want := range []string{"<vcpu placement='static'>4</vcpu>", "<memory unit='KiB'>8388608</memory>", "<currentMemory unit='KiB'>8388608</currentMemory>",
		"<interface type='direct'>", "<source dev='eno1' mode='bridge'/>", "52:54:00:48:4f:ce", "/srv/vms/iso/inst.iso", "device='cdrom'"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q fehlt:\n%s", want, out)
		}
	}
	c, err := ParseConfig([]byte(out))
	if err != nil || c.Network != "direct:eno1" || c.Media != "inst.iso" || c.MemoryMiB != 8192 {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestApplyConfigSecureBootOff(t *testing.T) {
	out, rm, err := ApplyConfig(string(haosXML(t)), ConfigUpdate{VCPUs: 2, MemoryMiB: 4096, SecureBoot: false, Network: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if rm != "/var/lib/libvirt/qemu/nvram/homeassistant_VARS.fd" {
		t.Fatalf("nvram-Pfad: %q", rm)
	}
	if strings.Contains(out, "<loader") || strings.Contains(out, "<nvram") || strings.Contains(out, "<feature enabled='yes'") {
		t.Fatalf("alte Firmware-Auswahl muss entfernt sein:\n%s", out)
	}
	if !strings.Contains(out, "<feature enabled='no' name='secure-boot'/>") || !strings.Contains(out, "<os firmware='efi'>") {
		t.Fatalf("%s", out)
	}
	c, _ := ParseConfig([]byte(out))
	if c.SecureBoot || c.Firmware != "uefi" {
		t.Fatalf("%+v", c)
	}
}

func TestApplyConfigEjectAndBios(t *testing.T) {
	x, _ := BuildXML(Spec{Name: "b", OS: "linux", Firmware: "bios", Network: "default", ISO: "a.iso", VCPUs: 1, MemoryMiB: 512, DiskGiB: 5}, true)
	out, rm, err := ApplyConfig(x, ConfigUpdate{VCPUs: 1, MemoryMiB: 512, Network: "default", Media: ""})
	if err != nil || rm != "" {
		t.Fatal(err, rm)
	}
	c, _ := ParseConfig([]byte(out))
	if !c.HasCDROM || c.Media != "" {
		t.Fatalf("leeres Laufwerk erwartet: %+v", c)
	}
	// Secure Boot bei BIOS-VMs ist wirkungslos und wird abgelehnt.
	if _, _, err := ApplyConfig(x, ConfigUpdate{VCPUs: 1, MemoryMiB: 512, Network: "default", SecureBoot: true}); err == nil {
		t.Fatal("Secure Boot ohne UEFI")
	}
}

func TestGuestAgent(t *testing.T) {
	// Neue VMs bekommen den Kanal für den QEMU-Guest-Agent (sauberes Herunterfahren).
	x, _ := BuildXML(okSpec(), true)
	if !strings.Contains(x, "org.qemu.guest_agent.0") {
		t.Fatal("BuildXML ohne Guest-Agent-Kanal")
	}
	c, _ := ParseConfig([]byte(x))
	if !c.GuestAgent {
		t.Fatalf("%+v", c)
	}
	// Bestehende VM ohne Kanal: einschalten und wieder ausschalten.
	base := ConfigUpdate{VCPUs: 2, MemoryMiB: 4096, SecureBoot: true, Network: "default"}
	if c, _ := ParseConfig(haosXML(t)); c.GuestAgent {
		t.Fatal("Fixture hat keinen Kanal")
	}
	base.GuestAgent = true
	out, _, err := ApplyConfig(string(haosXML(t)), base)
	if err != nil || strings.Count(out, "org.qemu.guest_agent.0") != 1 {
		t.Fatal(err, out)
	}
	if c, _ := ParseConfig([]byte(out)); !c.GuestAgent {
		t.Fatal("Kanal nicht erkannt")
	}
	base.GuestAgent = false
	out2, _, err := ApplyConfig(out, base)
	if err != nil || strings.Contains(out2, "guest_agent") {
		t.Fatal(err, out2)
	}
}
