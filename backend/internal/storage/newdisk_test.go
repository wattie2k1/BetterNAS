package storage

import "testing"

func newFlags(t *testing.T, e *env) map[string]bool {
	t.Helper()
	ds, err := e.m.ListDisks(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, d := range ds {
		out[d.ID] = d.New
	}
	return out
}

func plugged() Disk {
	return Disk{ID: "d9", Device: "/dev/sdz", Serial: "SER9", Model: "NEU", SizeBytes: 8e12, Manageable: true, Partitions: []Partition{}}
}

func TestExistingDisksAreBaselinedNewOnesAreFlagged(t *testing.T) {
	e := newEnv(t)
	for id, isNew := range newFlags(t, e) {
		if isNew {
			t.Fatalf("%s: vorhandene Platten dürfen beim ersten Lauf nicht „neu“ sein", id)
		}
	}
	e.agent.disks = append(e.agent.disks, plugged())
	got := newFlags(t, e)
	if !got["d9"] || got["d1"] || got["sys"] {
		t.Fatalf("%v", got)
	}
	if !newFlags(t, e)["d9"] {
		t.Fatal("bleibt neu, bis sie eingerichtet oder ignoriert wird")
	}
}

func TestNewDiskClearsWhenSetUpOrDismissed(t *testing.T) {
	e := newEnv(t)
	newFlags(t, e)
	e.agent.disks = append(e.agent.disks, plugged())
	newFlags(t, e)
	e.formatDisk(t, "d9", "SER9")
	if newFlags(t, e)["d9"] {
		t.Fatal("eingerichtete Platte ist nicht mehr neu")
	}

	e.agent.disks = append(e.agent.disks, Disk{ID: "d8", Device: "/dev/sdy", Serial: "SER8", SizeBytes: 1e12, Manageable: true, Partitions: []Partition{}})
	if !newFlags(t, e)["d8"] {
		t.Fatal("d8 muss neu sein")
	}
	if err := e.m.DismissNew(e.ctx, "admin", "d8"); err != nil {
		t.Fatal(err)
	}
	if newFlags(t, e)["d8"] {
		t.Fatal("ignorierte Platte bleibt ausgeblendet")
	}
	if codeOf(e.m.DismissNew(e.ctx, "admin", "nope")) != "not_found" {
		t.Error("unbekannte Platte")
	}
}

func TestNonManageableAndSystemDisksAreNeverNew(t *testing.T) {
	e := newEnv(t)
	newFlags(t, e)
	usb := plugged()
	usb.Manageable, usb.Reason = false, "Eingehängt"
	sys := Disk{ID: "sys2", Device: "/dev/nvme1n1", System: true, SizeBytes: 1e11, Partitions: []Partition{}}
	e.agent.disks = append(e.agent.disks, usb, sys)
	got := newFlags(t, e)
	if got["d9"] || got["sys2"] {
		t.Fatalf("%v", got)
	}
}
