package vm

import (
	"strings"
	"testing"
)

func TestNextDiskTargetAndAdd(t *testing.T) {
	x := string(haosXML(t))
	dev, bus, err := NextDiskTarget(x)
	if err != nil || dev != "vdb" || bus != "virtio" {
		t.Fatalf("%s %s %v", dev, bus, err)
	}
	out, err := AddDiskXML(x, "/srv/vms/disks/ha-2.qcow2", dev, bus)
	if err != nil || !strings.Contains(out, "<source file='/srv/vms/disks/ha-2.qcow2'/>") || !strings.Contains(out, "<target dev='vdb' bus='virtio'/>") {
		t.Fatalf("%v\n%s", err, out)
	}
	c, _ := ParseConfig([]byte(out))
	if len(c.Disks) != 2 || len(c.DiskOpts) != 2 {
		t.Fatalf("%+v", c.Disks)
	}
	if dev, _, _ = NextDiskTarget(out); dev != "vdc" {
		t.Fatal(dev)
	}
	if _, err := AddDiskXML(out, "/srv/vms/disks/ha-2.qcow2", "vdc", "virtio"); err == nil {
		t.Fatal("doppelt")
	}
}

func TestNextDiskTargetSataSkipsCDROM(t *testing.T) {
	x, _ := BuildXML(Spec{Name: "w", OS: "windows", Firmware: "uefi", Network: "default", ISO: "w.iso", DriverISO: "d.iso", VCPUs: 2, MemoryMiB: 2048, DiskGiB: 20}, true)
	// Windows ohne Treiber-ISO nutzt sata; mit Treiber-ISO virtio. Beides prüfen.
	if dev, bus, _ := NextDiskTarget(x); dev != "vdb" || bus != "virtio" {
		t.Fatalf("%s %s", dev, bus)
	}
	x, _ = BuildXML(Spec{Name: "w", OS: "windows", Firmware: "uefi", Network: "default", ISO: "w.iso", VCPUs: 2, MemoryMiB: 2048, DiskGiB: 20}, true)
	if dev, bus, _ := NextDiskTarget(x); dev != "sdc" || bus != "sata" { // sda = Disk, sdb = CD
		t.Fatalf("%s %s", dev, bus)
	}
}

func TestRemoveDiskXML(t *testing.T) {
	x, _ := AddDiskXML(string(haosXML(t)), "/srv/vms/disks/ha-2.qcow2", "vdb", "virtio")
	out, err := RemoveDiskXML(x, "/srv/vms/disks/ha-2.qcow2")
	if err != nil || strings.Contains(out, "ha-2") || !strings.Contains(out, "homeassistant.qcow2") {
		t.Fatalf("%v\n%s", err, out)
	}
	if out != string(haosXML(t)) {
		t.Fatal("Entfernen muss den Ausgangszustand wiederherstellen")
	}
	if _, err := RemoveDiskXML(x, "/srv/vms/disks/homeassistant.qcow2"); err == nil {
		t.Fatal("Systemfestplatte darf nicht entfernt werden")
	}
	if _, err := RemoveDiskXML(x, "/srv/vms/disks/gibtsnicht.qcow2"); err == nil {
		t.Fatal("fremde Datei")
	}
}

func TestDiskAddRemoveValidate(t *testing.T) {
	if (DiskAdd{SizeGiB: 1}).Validate() != nil || (DiskAdd{SizeGiB: 0}).Validate() == nil || (DiskAdd{SizeGiB: MaxDiskGiB + 1}).Validate() == nil {
		t.Fatal("DiskAdd")
	}
	if (DiskRemove{Path: "/srv/vms/disks/a-2.qcow2"}).Validate() != nil {
		t.Fatal("gültiger Pfad")
	}
	for _, p := range []string{"/etc/passwd", "/srv/vms/disks/../a", "/srv/vms/disks/a'b", ""} {
		if (DiskRemove{Path: p}).Validate() == nil {
			t.Errorf("%q", p)
		}
	}
}
