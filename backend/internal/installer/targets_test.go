package installer

import (
	"os"
	"testing"

	"github.com/betternas/betternas/internal/storage"
)

func liveDisks(t *testing.T) []storage.Disk {
	t.Helper()
	data, err := os.ReadFile("testdata/lsblk-live.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := storage.ParseLsblk(data, storage.IDOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

const mountinfo = `22 1 0:21 / / rw,relatime - overlay overlay rw
30 22 8:33 / /run/live/medium ro,noatime shared:12 - iso9660 /dev/sdc1 ro
`

func TestTargetsExcludeLiveMedium(t *testing.T) {
	disks := liveDisks(t)
	live := LiveMediumDevice([]byte(mountinfo), disks)
	if live != "/dev/sdc" {
		t.Fatalf("live medium %q", live)
	}
	ts := Targets(disks, live)
	if len(ts) != 2 {
		t.Fatalf("want 2 targets, got %+v", ts)
	}
	for _, tg := range ts {
		if tg.Device == "/dev/sdc" {
			t.Fatal("live medium offered as target")
		}
		if tg.Device == "/dev/sda" && (!tg.HasData || tg.Warning == "") {
			t.Fatalf("hdd: %+v", tg)
		}
		if tg.Device == "/dev/nvme0n1" && (tg.HasData || tg.Warning != "") {
			t.Fatalf("nvme: %+v", tg)
		}
	}
}

func TestTargetsExcludeSmall(t *testing.T) {
	for _, tg := range Targets(liveDisks(t), "") {
		if tg.Device == "/dev/sdd" {
			t.Fatal("16 GiB disk offered")
		}
	}
}

func gib(n uint64) uint64 { return n << 30 }

func TestPlanLayoutDefault(t *testing.T) {
	l, err := PlanLayout(Target{Device: "/dev/nvme0n1", SizeBytes: 500_000_000_000}, Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Partitions) != 4 || l.Partitions[2].SizeMiB != 32768 || l.Partitions[3].SizeMiB != 0 {
		t.Fatalf("%+v", l)
	}
	if l.Partitions[0].TypeCode != "EF02" || l.Partitions[1].TypeCode != "EF00" || l.Partitions[3].Mount != "/srv/appdata" {
		t.Fatalf("%+v", l)
	}
}

func TestPlanLayoutNoAppdataWhenSmall(t *testing.T) {
	l, err := PlanLayout(Target{Device: "/dev/sda", SizeBytes: gib(40)}, Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Partitions) != 3 || l.Partitions[2].SizeMiB != 0 || l.HasAppdata() {
		t.Fatalf("%+v", l)
	}
}

func TestPlanLayoutAppdataDisabled(t *testing.T) {
	a := Defaults()
	a.CreateAppdata = false
	l, _ := PlanLayout(Target{Device: "/dev/sda", SizeBytes: gib(500)}, a)
	if len(l.Partitions) != 3 || l.Partitions[2].SizeMiB != 0 {
		t.Fatalf("%+v", l)
	}
}

func TestPlanLayoutTooSmall(t *testing.T) {
	a := Defaults()
	a.RootSizeGiB = 64
	if _, err := PlanLayout(Target{Device: "/dev/sda", SizeBytes: gib(40)}, a); err == nil {
		t.Fatal("expected error")
	}
}

func TestPartitionDevice(t *testing.T) {
	cases := map[string]string{
		PartitionDevice("/dev/sda", 3):     "/dev/sda3",
		PartitionDevice("/dev/nvme0n1", 2): "/dev/nvme0n1p2",
		PartitionDevice("/dev/mmcblk0", 1): "/dev/mmcblk0p1",
		PartitionDevice("/dev/vda", 4):     "/dev/vda4",
	}
	want := []string{"/dev/sda3", "/dev/nvme0n1p2", "/dev/mmcblk0p1", "/dev/vda4"}
	for _, w := range want {
		if _, ok := cases[w]; !ok {
			t.Fatalf("missing %s in %v", w, cases)
		}
	}
}
