package vm

import (
	"strings"
	"testing"
)

func TestRenameValidate(t *testing.T) {
	if (Rename{NewName: "neu-1"}).Validate() != nil {
		t.Fatal("gültig")
	}
	for _, n := range []string{"", "Gross", "-x", "a b", "../x", strings.Repeat("a", 41)} {
		if (Rename{NewName: n}).Validate() == nil {
			t.Errorf("%q", n)
		}
	}
}

func TestFileMovesAndApply(t *testing.T) {
	x := string(haosXML(t))
	disks := []string{"/srv/vms/disks/homeassistant.qcow2", "/srv/vms/disks/homeassistant-2.qcow2", "/srv/vms/disks/fremd.qcow2", "/srv/vms/disks/homeassistant-x.qcow2"}
	moves := FileMoves("homeassistant", "ha", disks, NvramFile(x))
	want := map[string]string{
		"/srv/vms/disks/homeassistant.qcow2":                "/srv/vms/disks/ha.qcow2",
		"/srv/vms/disks/homeassistant-2.qcow2":              "/srv/vms/disks/ha-2.qcow2",
		"/var/lib/libvirt/qemu/nvram/homeassistant_VARS.fd": "/var/lib/libvirt/qemu/nvram/ha_VARS.fd",
	}
	if len(moves) != len(want) {
		t.Fatalf("%v", moves)
	}
	for k, v := range want {
		if moves[k] != v {
			t.Errorf("%s: %q", k, moves[k])
		}
	}
	out := ApplyMoves(x, moves)
	if strings.Contains(out, "homeassistant.qcow2") || strings.Contains(out, "homeassistant_VARS") || !strings.Contains(out, "<source file='/srv/vms/disks/ha.qcow2'/>") || !strings.Contains(out, ">/var/lib/libvirt/qemu/nvram/ha_VARS.fd<") {
		t.Fatal(out)
	}
	// Name in <name> bleibt für domrename unverändert
	if !strings.Contains(out, "<name>homeassistant</name>") {
		t.Fatal("name darf nicht verändert werden")
	}
	// Fremde Namen mit Präfix werden nicht angefasst
	if m := FileMoves("homeassistant", "ha", []string{"/srv/vms/disks/homeassistant2.qcow2"}, ""); len(m) != 0 {
		t.Fatalf("%v", m)
	}
}
