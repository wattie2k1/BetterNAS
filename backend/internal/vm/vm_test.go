package vm

import (
	"os"
	"strings"
	"testing"
)

func okSpec() Spec {
	return Spec{Name: "debian", OS: "linux", Firmware: "uefi", Network: "default", VCPUs: 2, MemoryMiB: 4096, DiskGiB: 40}
}

func TestSpecValidation(t *testing.T) {
	if err := okSpec().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Spec){
		"leer": func(s *Spec) { s.Name = "" }, "..": func(s *Spec) { s.Name = "../x" }, "gross": func(s *Spec) { s.Name = "A" },
		"leerzeichen": func(s *Spec) { s.Name = "a b" }, "semikolon": func(s *Spec) { s.Name = "a;b" },
		"lang": func(s *Spec) { s.Name = strings.Repeat("a", 41) },
		"cpu0": func(s *Spec) { s.VCPUs = 0 }, "cpu65": func(s *Spec) { s.VCPUs = 65 },
		"ram": func(s *Spec) { s.MemoryMiB = 255 }, "disk0": func(s *Spec) { s.DiskGiB = 0 }, "disk-gross": func(s *Spec) { s.DiskGiB = 65537 },
		"os": func(s *Spec) { s.OS = "bsd" }, "fw": func(s *Spec) { s.Firmware = "x" },
		"iso-pfad": func(s *Spec) { s.ISO = "../x.iso" }, "iso-endung": func(s *Spec) { s.ISO = "x.img" },
		"treiber": func(s *Spec) { s.DriverISO = "a/b.iso" },
		"netz":    func(s *Spec) { s.Network = "a b" }, "netz-xml": func(s *Spec) { s.Network = "a'/><x" },
		"tpm-bios": func(s *Spec) { s.Firmware = "bios"; s.TPM = true },
	}
	for name, mod := range cases {
		s := okSpec()
		mod(&s)
		if err := s.Validate(); err == nil {
			t.Errorf("%s: erwartete Fehler", name)
		}
	}
}

func TestDiskAndISOPath(t *testing.T) {
	if DiskPath("a") != "/srv/vms/disks/a.qcow2" || ISOPath("x.iso") != "/srv/vms/iso/x.iso" {
		t.Fatal("falsche Pfade")
	}
}

func TestBuildXMLEscapes(t *testing.T) {
	s := okSpec()
	s.Network = "a'/><x"
	if _, err := BuildXML(s, true); err == nil {
		t.Fatal("Netzname mit XML-Zeichen muss abgelehnt werden")
	}
	s = okSpec()
	s.ISO = "a&b.iso" // wird von ISORe abgelehnt
	if _, err := BuildXML(s, true); err == nil {
		t.Fatal("ISO mit & muss abgelehnt werden")
	}
	if esc("<a&'b>") != "&lt;a&amp;&#39;b&gt;" {
		t.Fatalf("esc: %q", esc("<a&'b>"))
	}
}

func TestBuildXMLVariants(t *testing.T) {
	x, err := BuildXML(okSpec(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"type='kvm'", "firmware='efi'", "bus='virtio'", "listen='127.0.0.1'", "network='default'", "/srv/vms/disks/debian.qcow2"} {
		if !strings.Contains(x, want) {
			t.Errorf("fehlt %q", want)
		}
	}
	if strings.Contains(x, "<tpm") {
		t.Error("TPM ohne tpm:true")
	}

	w := okSpec()
	w.OS, w.TPM, w.ISO = "windows", true, "win.iso"
	x, _ = BuildXML(w, true)
	for _, want := range []string{"bus='sata'", "e1000e", "<tpm", "hyperv", "/srv/vms/iso/win.iso"} {
		if !strings.Contains(x, want) {
			t.Errorf("Windows ohne Treiber: fehlt %q", want)
		}
	}
	w.DriverISO = "virtio-win.iso"
	x, _ = BuildXML(w, true)
	if !strings.Contains(x, "bus='virtio'") || strings.Count(x, "device='cdrom'") != 2 {
		t.Error("Windows mit Treiber-ISO: virtio und zwei CD-Laufwerke erwartet")
	}

	b := okSpec()
	b.Firmware = "bios"
	x, _ = BuildXML(b, false)
	if strings.Contains(x, "firmware='efi'") || !strings.Contains(x, "type='qemu'") {
		t.Error("BIOS/ohne KVM falsch")
	}
}

func read(t *testing.T, f string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseDomInfo(t *testing.T) {
	v, err := ParseDomInfo(read(t, "dominfo.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if v.Name != "debian" || v.State != "running" || v.VCPUs != 4 || v.MemoryMiB != 4096 || !v.Autostart || v.UUID == "" {
		t.Fatalf("%+v", v)
	}
	if v, _ := ParseDomInfo([]byte("State: shut off\nAutostart: disable\n")); v.State != "shutoff" || v.Autostart {
		t.Fatalf("%+v", v)
	}
}

func TestParseDisks(t *testing.T) {
	d, err := ParseDisks(read(t, "dumpxml.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 2 || d[0] != "/srv/vms/disks/debian.qcow2" || d[1] != "/etc/passwd" {
		t.Fatalf("%v", d)
	}
}

func TestParseNetworks(t *testing.T) {
	n, _ := ParseNetworks(read(t, "net-list.txt"))
	if len(n) != 2 || n[0] != "default" || n[1] != "isolated" {
		t.Fatalf("%v", n)
	}
}

func TestDirectNetwork(t *testing.T) {
	s := okSpec()
	s.Network = "direct:eno1"
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	x, err := BuildXML(s, true)
	if err != nil || !strings.Contains(x, "<interface type='direct'>") || !strings.Contains(x, "<source dev='eno1' mode='bridge'/>") {
		t.Fatalf("%v\n%s", err, x)
	}
	for _, bad := range []string{"direct:", "direct:E0", "direct:a b", "direct:../x", "direct:a'/><x", "direct:abcdefghijklmnop"} {
		s.Network = bad
		if s.Validate() == nil {
			t.Errorf("%q muss abgelehnt werden", bad)
		}
	}
}

func TestParseSnapshots(t *testing.T) {
	out := []byte(" Name    Creation Time               State\n------------------------------------------------\n vor-update   2026-11-17 10:49:12 +0100   shutoff\n s2   2026-11-17 11:00:00 +0100   running\n\n")
	l := ParseSnapshots(out)
	if len(l) != 2 || l[0].Name != "vor-update" || l[0].State != "shutoff" || l[1].Created != "2026-11-17 11:00:00 +0100" {
		t.Fatalf("%+v", l)
	}
	if got := ParseSnapshots([]byte(" Name   Creation Time   State\n----\n")); got == nil || len(got) != 0 {
		t.Fatal("leere Liste erwartet")
	}
}

func TestParseDisplayPort(t *testing.T) {
	if p, ok := ParseDisplayPort([]byte("vnc://127.0.0.1:2\n")); !ok || p != 5902 {
		t.Fatalf("%d %v", p, ok)
	}
	for _, bad := range []string{"", "spice://127.0.0.1:0", "vnc://127.0.0.1:x", "vnc://h:1000"} {
		if _, ok := ParseDisplayPort([]byte(bad)); ok {
			t.Errorf("%q", bad)
		}
	}
}
