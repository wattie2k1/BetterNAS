package vm

import (
	"strings"
	"testing"
)

func base() ConfigUpdate {
	return ConfigUpdate{VCPUs: 2, MemoryMiB: 4096, SecureBoot: true, Network: "default"}
}

func TestParseTuning(t *testing.T) {
	c, err := ParseConfig(haosXML(t))
	if err != nil {
		t.Fatal(err)
	}
	if c.CPUMode != "host-passthrough" || c.Sockets != 0 || c.Hyperv || c.Clock != "utc" || len(c.ExtraNICs) != 0 {
		t.Fatalf("%+v", c.Tuning)
	}
	if len(c.DiskOpts) != 1 || c.DiskOpts[0].Cache != "default" || c.DiskOpts[0].Discard != "ignore" {
		t.Fatalf("%+v", c.DiskOpts)
	}
}

func TestApplyCPUTopologyAndMode(t *testing.T) {
	u := base()
	u.VCPUs = 8
	u.CPUMode = "host-model"
	u.Topology = &Topology{Sockets: 1, Threads: 2}
	out, _, err := ApplyConfig(string(haosXML(t)), u)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "<topology sockets='1' dies='1' cores='4' threads='2'/>") || !strings.Contains(out, "<cpu mode='host-model'") || strings.Contains(out, "migratable") {
		t.Fatalf("%s", out)
	}
	c, _ := ParseConfig([]byte(out))
	if c.Sockets != 1 || c.Threads != 2 || c.CPUMode != "host-model" || c.VCPUs != 8 {
		t.Fatalf("%+v", c)
	}
	// Mit vorhandener Topologie muss eine neue vCPU-Zahl dazu passen oder Topologie mit angepasst werden.
	u2 := base()
	u2.VCPUs = 6
	if out2, _, err := ApplyConfig(out, u2); err != nil || !strings.Contains(out2, "cores='3' threads='2'") {
		t.Fatalf("%v\n%s", err, out2)
	}
	u2.VCPUs = 5
	if _, _, err := ApplyConfig(out, u2); err == nil {
		t.Fatal("5 vCPUs passen nicht zu Threads=2")
	}
	// Ungültige Eingaben
	for _, bad := range []ConfigUpdate{
		{VCPUs: 6, MemoryMiB: 2048, Network: "default", TuningUpdate: TuningUpdate{Topology: &Topology{Sockets: 4, Threads: 1}}},
		{VCPUs: 2, MemoryMiB: 2048, Network: "default", TuningUpdate: TuningUpdate{CPUMode: "custom"}},
	} {
		if bad.Validate() == nil {
			t.Errorf("%+v", bad)
		}
	}
}

func TestApplyHypervClock(t *testing.T) {
	on := true
	u := base()
	u.Hyperv, u.Clock = &on, "localtime"
	out, _, err := ApplyConfig(string(haosXML(t)), u)
	if err != nil || !strings.Contains(out, "<hyperv mode='custom'>") || !strings.Contains(out, "<clock offset='localtime'") {
		t.Fatalf("%v\n%s", err, out)
	}
	c, _ := ParseConfig([]byte(out))
	if !c.Hyperv || c.Clock != "localtime" {
		t.Fatalf("%+v", c.Tuning)
	}
	off := false
	u.Hyperv, u.Clock = &off, ""
	out2, _, _ := ApplyConfig(out, u)
	if strings.Contains(out2, "hyperv") || !strings.Contains(out2, "localtime") {
		t.Fatalf("%s", out2)
	}
}

func TestApplyDiskOpts(t *testing.T) {
	u := base()
	opts := []DiskOpt{{Path: "/srv/vms/disks/homeassistant.qcow2", Cache: "none", Discard: "unmap"}}
	u.DiskOpts = &opts
	out, _, err := ApplyConfig(string(haosXML(t)), u)
	if err != nil || !strings.Contains(out, "<driver name='qemu' type='qcow2' cache='none' discard='unmap'/>") {
		t.Fatalf("%v\n%s", err, out)
	}
	opts[0].Cache, opts[0].Discard = "default", "ignore"
	if out2, _, _ := ApplyConfig(out, u); !strings.Contains(out2, "<driver name='qemu' type='qcow2'/>") {
		t.Fatalf("%s", out2)
	}
	opts[0].Path = "/srv/vms/disks/fremd.qcow2"
	if _, _, err := ApplyConfig(string(haosXML(t)), u); err == nil {
		t.Fatal("fremde Disk")
	}
	opts[0] = DiskOpt{Path: "/srv/vms/disks/homeassistant.qcow2", Cache: "unsafe", Discard: "unmap"}
	if u.Validate() == nil {
		t.Fatal("unsafe darf nicht erlaubt sein")
	}
}

func TestApplyExtraNICs(t *testing.T) {
	u := base()
	nics := []NIC{{Network: "bridge:br0", Model: "e1000e"}, {Network: "direct:eno1"}}
	u.ExtraNICs = &nics
	out, _, err := ApplyConfig(string(haosXML(t)), u)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := ParseConfig([]byte(out))
	if len(c.ExtraNICs) != 2 || c.ExtraNICs[0].Network != "bridge:br0" || c.ExtraNICs[0].Model != "e1000e" || c.ExtraNICs[1].Model != "virtio" || c.ExtraNICs[1].Network != "direct:eno1" {
		t.Fatalf("%+v", c.ExtraNICs)
	}
	// libvirt vergibt MACs; hier simulieren wir das durch feste Adressen.
	out = strings.Replace(out, "<interface type='bridge'>", "<interface type='bridge'>\n      <mac address='52:54:00:00:00:01'/>", 1)
	out = strings.Replace(out, "<interface type='direct'>", "<interface type='direct'>\n      <mac address='52:54:00:00:00:02'/>", 1)
	c, _ = ParseConfig([]byte(out))
	if c.ExtraNICs[0].MAC != "52:54:00:00:00:01" {
		t.Fatalf("%+v", c.ExtraNICs)
	}
	// Erste bleibt (anderes Netz), zweite entfällt, neue kommt dazu; Primärkarte unberührt.
	keep := []NIC{{Network: "default", MAC: "52:54:00:00:00:01"}, {Network: "default", Model: "rtl8139"}}
	u.ExtraNICs = &keep
	out2, _, err := ApplyConfig(out, u)
	if err != nil {
		t.Fatal(err)
	}
	c, _ = ParseConfig([]byte(out2))
	if len(c.ExtraNICs) != 2 || c.ExtraNICs[0].MAC != "52:54:00:00:00:01" || c.ExtraNICs[0].Network != "default" || c.ExtraNICs[0].Model != "e1000e" || c.ExtraNICs[1].Model != "rtl8139" {
		t.Fatalf("%+v\n%s", c.ExtraNICs, out2)
	}
	if strings.Contains(out2, "00:00:02") || c.MAC != "52:54:00:48:4f:ce" {
		t.Fatal("zweite Karte nicht entfernt oder Primär-MAC verändert")
	}
	none := []NIC{}
	u.ExtraNICs = &none
	if out3, _, _ := ApplyConfig(out2, u); strings.Count(out3, "<interface") != 1 {
		t.Fatal("alle Zusatzkarten müssen entfernt sein")
	}
	for _, bad := range [][]NIC{
		{{Network: "a b"}}, {{Network: "default", Model: "ne2k"}}, {{Network: "default", MAC: "zz"}},
		{{Network: "default", MAC: "52:54:00:00:00:01"}, {Network: "default", MAC: "52:54:00:00:00:01"}},
		make([]NIC, 5),
	} {
		bad := bad
		for i := range bad {
			if bad[i].Network == "" {
				bad[i].Network = "default"
			}
		}
		u.ExtraNICs = &bad
		if u.Validate() == nil {
			t.Errorf("%+v", bad)
		}
	}
}
