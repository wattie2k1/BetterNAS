package storage

import (
	"os"
	"strings"
	"testing"
)

func load(t *testing.T, name string, opts IDOptions) []Disk {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	disks, err := ParseLsblk(data, opts)
	if err != nil {
		t.Fatal(err)
	}
	return disks
}

func byDevice(disks []Disk, dev string) *Disk {
	for i := range disks {
		if disks[i].Device == dev {
			return &disks[i]
		}
	}
	return nil
}

func TestParseBasic(t *testing.T) {
	disks := load(t, "lsblk-basic.json", IDOptions{})
	if len(disks) != 3 {
		t.Fatalf("want 3 disks, got %d: %+v", len(disks), disks)
	}
	nvme := byDevice(disks, "/dev/nvme0n1")
	if nvme == nil || !nvme.System || nvme.Manageable {
		t.Fatalf("nvme must be system and unmanageable: %+v", nvme)
	}
	sda := byDevice(disks, "/dev/sda")
	if sda.ID != "wwn-0x50014ee2b5d1a2c3" || !sda.Manageable || !sda.Rotational {
		t.Fatalf("sda: %+v", sda)
	}
	sdb := byDevice(disks, "/dev/sdb")
	if sdb.ID != "WDC_WD40EFRX-68N32N0_WD-WCC7K1234567" {
		t.Fatalf("sdb id: %s", sdb.ID)
	}
	if len(sdb.Partitions) != 1 || sdb.Partitions[0].FSType != "ext4" || sdb.Partitions[0].Device != "/dev/sdb1" {
		t.Fatalf("sdb partitions: %+v", sdb.Partitions)
	}
	if sdb.SizeBytes != 4000787030016 {
		t.Fatalf("size %d", sdb.SizeBytes)
	}
}

func TestDuplicateSerials(t *testing.T) {
	disks := load(t, "lsblk-dupes.json", IDOptions{})
	for _, dev := range []string{"/dev/sdc", "/dev/sdd"} {
		d := byDevice(disks, dev)
		if d == nil || d.Manageable || d.Reason != "Doppelte Kennung (z. B. USB-Gehäuse)" {
			t.Fatalf("%s: %+v", dev, d)
		}
	}
}

func TestNoIdentity(t *testing.T) {
	disks := load(t, "lsblk-dupes.json", IDOptions{})
	vda := byDevice(disks, "/dev/vda")
	if vda == nil || vda.Manageable || vda.Reason != "Keine eindeutige Kennung" {
		t.Fatalf("vda: %+v", vda)
	}
}

func TestLoopOnlyWhenAllowed(t *testing.T) {
	if d := byDevice(load(t, "lsblk-dupes.json", IDOptions{}), "/dev/loop0"); d != nil {
		t.Fatal("loop must be ignored by default")
	}
	disks := load(t, "lsblk-dupes.json", IDOptions{AllowLoop: true})
	d := byDevice(disks, "/dev/loop0")
	if d == nil || d.ID != "loop-loop0" || !d.Manageable {
		t.Fatalf("loop0: %+v", d)
	}
	if byDevice(disks, "/dev/loop1") != nil {
		t.Fatal("zero-size loop must be ignored")
	}
}

func TestDiskIDSanitizing(t *testing.T) {
	id, ok := DiskID("Model", "A:B,C/../D E", "", "sdx", IDOptions{})
	if !ok {
		t.Fatal("expected ok")
	}
	for _, bad := range []string{":", ",", "/", " "} {
		if strings.Contains(id, bad) {
			t.Fatalf("id %q contains %q", id, bad)
		}
	}
	id, _ = DiskID("", "", "..x", "sdx", IDOptions{})
	if strings.HasPrefix(id, ".") {
		t.Fatalf("id %q starts with dot", id)
	}
	id, _ = DiskID("-Model", "S", "", "sdx", IDOptions{})
	if strings.HasPrefix(id, "-") {
		t.Fatalf("id %q starts with dash", id)
	}
}

func TestFSLabelLength(t *testing.T) {
	l := FSLabel("wwn-0x50014ee2b5d1a2c3")
	if len(l) != 12 || !strings.HasPrefix(l, "bnas-") {
		t.Fatalf("label %q", l)
	}
}
