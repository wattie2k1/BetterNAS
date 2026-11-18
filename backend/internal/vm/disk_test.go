package vm

import "testing"

func TestDiskResizeValidate(t *testing.T) {
	ok := DiskResize{Path: "/srv/vms/disks/a.qcow2", SizeGiB: 10}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []DiskResize{
		{Path: "/etc/passwd", SizeGiB: 10},
		{Path: "/srv/vms/disks/../a.qcow2", SizeGiB: 10},
		{Path: "/srv/vms/disks/a'.qcow2", SizeGiB: 10},
		{Path: ok.Path, SizeGiB: 0},
		{Path: ok.Path, SizeGiB: MaxDiskGiB + 1},
	} {
		if bad.Validate() == nil {
			t.Errorf("%+v sollte ungültig sein", bad)
		}
	}
}

func TestParseImageInfo(t *testing.T) {
	v, a, err := ParseImageInfo([]byte(`{"virtual-size": 1073741824, "actual-size": 4096}`))
	if err != nil || v != 1<<30 || a != 4096 {
		t.Fatalf("%d %d %v", v, a, err)
	}
	if _, _, err := ParseImageInfo([]byte(`{}`)); err == nil {
		t.Fatal("leer")
	}
	if _, _, err := ParseImageInfo([]byte(`kaputt`)); err == nil {
		t.Fatal("kaputt")
	}
}
