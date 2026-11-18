package vm

import (
	"strings"
	"testing"
)

func TestMaxMemoryAndVCPUs(t *testing.T) {
	// Spielraum: aktuell 2 vCPUs/4 GiB, Maximum 4 vCPUs/8 GiB
	u := ConfigUpdate{VCPUs: 2, MaxVCPUs: 4, MemoryMiB: 4096, MaxMemoryMiB: 8192, SecureBoot: true, Network: "default"}
	out, _, err := ApplyConfig(string(haosXML(t)), u)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "<memory unit='KiB'>8388608</memory>") || !strings.Contains(out, "<currentMemory unit='KiB'>4194304</currentMemory>") || !strings.Contains(out, "<vcpu placement='static' current='2'>4</vcpu>") {
		t.Fatalf("%s", out)
	}
	c, _ := ParseConfig([]byte(out))
	if c.VCPUs != 2 || c.MaxVCPUs != 4 || c.MemoryMiB != 4096 || c.MaxMemoryMiB != 8192 {
		t.Fatalf("%+v", c)
	}
	// Ohne Maximum: wie zuvor, kein Spielraum – auch beim Verkleinern.
	u2 := ConfigUpdate{VCPUs: 1, MemoryMiB: 2048, SecureBoot: true, Network: "default"}
	out2, _, _ := ApplyConfig(out, u2)
	if strings.Contains(out2, "current=") || !strings.Contains(out2, "<memory unit='KiB'>2097152</memory>") || !strings.Contains(out2, "<currentMemory unit='KiB'>2097152</currentMemory>") {
		t.Fatalf("%s", out2)
	}
	// currentMemory fehlt in der XML, aber Spielraum gewünscht → wird ergänzt
	noCur := strings.Replace(string(haosXML(t)), "  <currentMemory unit='KiB'>4194304</currentMemory>\n", "", 1)
	out3, _, _ := ApplyConfig(noCur, u)
	if c, _ = ParseConfig([]byte(out3)); c.MemoryMiB != 4096 || c.MaxMemoryMiB != 8192 {
		t.Fatalf("%+v", c)
	}
	for _, bad := range []ConfigUpdate{
		{VCPUs: 4, MaxVCPUs: 2, MemoryMiB: 2048, Network: "default"},
		{VCPUs: 2, MaxVCPUs: 65, MemoryMiB: 2048, Network: "default"},
		{VCPUs: 2, MemoryMiB: 4096, MaxMemoryMiB: 2048, Network: "default"},
		{VCPUs: 2, MemoryMiB: 4096, MaxMemoryMiB: MaxMemoryMiB + 1, Network: "default"},
	} {
		if bad.Validate() == nil {
			t.Errorf("%+v", bad)
		}
	}
}

func TestTopologyUsesMaxVCPUs(t *testing.T) {
	u := ConfigUpdate{VCPUs: 2, MaxVCPUs: 8, MemoryMiB: 4096, SecureBoot: true, Network: "default", TuningUpdate: TuningUpdate{Topology: &Topology{Sockets: 1, Threads: 2}}}
	out, _, err := ApplyConfig(string(haosXML(t)), u)
	if err != nil || !strings.Contains(out, "cores='4' threads='2'") {
		t.Fatalf("%v\n%s", err, out)
	}
	u.MaxVCPUs = 7
	if u.Validate() == nil {
		t.Fatal("7 teilbar durch 2 nicht")
	}
}

func TestLiveUpdateValidateAndHelpers(t *testing.T) {
	m, v, e := 4096, 2, ""
	ok := LiveUpdate{MemoryMiB: &m, VCPUs: &v, Media: &e}
	if ok.Validate() != nil || ok.Empty() {
		t.Fatal("gültig")
	}
	if !(LiveUpdate{}).Empty() {
		t.Fatal("leer")
	}
	bad1, bad2, bad3 := 10, 99, "x/../y.iso"
	for _, b := range []LiveUpdate{{MemoryMiB: &bad1}, {VCPUs: &bad2}, {Media: &bad3}, {USB: &[]USBDevice{{Vendor: "zz", Product: "1"}}}} {
		if b.Validate() == nil {
			t.Errorf("%+v", b)
		}
	}
	x := strings.Replace(string(haosXML(t)), "  </devices>", "    <disk type='file' device='cdrom'>\n      <target dev='sdb' bus='sata'/>\n    </disk>\n  </devices>", 1)
	if CDROMTarget(x) != "sdb" || CDROMTarget(string(haosXML(t))) != "" {
		t.Fatal("CDROMTarget")
	}
	a, b := USBDevice{Vendor: "1a86", Product: "7523"}, USBDevice{Vendor: "10c4", Product: "ea60"}
	add, rem := DiffUSB([]USBDevice{a}, []USBDevice{b})
	if len(add) != 1 || add[0] != b || len(rem) != 1 || rem[0] != a {
		t.Fatal(add, rem)
	}
}
