package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/sysexec"
	"github.com/betternas/betternas/internal/vm"
)

const haosDisk = "/srv/vms/disks/homeassistant.qcow2"

func diskEnv(t *testing.T, state string) (Deps, *sysexec.FakeRunner) {
	t.Helper()
	d, f, _ := configEnv(t, state)
	f.Responses["qemu-img info"] = sysexec.Result{Stdout: []byte(`{"virtual-size": 34359738368, "actual-size": 2147483648}`)}
	return d, f
}

func TestVMDisks(t *testing.T) {
	d, _ := diskEnv(t, "running")
	list, err := VMDisks(context.Background(), d, "homeassistant")
	if err != nil || len(list) != 1 || list[0].Path != haosDisk || list[0].SizeGiB != 32 || list[0].UsedBytes != 2<<30 {
		t.Fatalf("%+v %v", list, err)
	}
	if code(func() error { _, err := VMDisks(context.Background(), d, "unbekannt"); return err }()) != apierr.NotFound {
		t.Fatal("unbekannt")
	}
}

func TestVMDiskResize(t *testing.T) {
	d, f := diskEnv(t, "shut off")
	f.Responses[v+"snapshot-list homeassistant"] = sysexec.Result{Stdout: []byte("")}
	if err := VMDiskResize(context.Background(), d, "homeassistant", vm.DiskResize{Path: haosDisk, SizeGiB: 64}); err != nil {
		t.Fatal(err)
	}
	if !f.Called("qemu-img resize " + haosDisk + " 64G") {
		t.Fatal(f.Calls)
	}
}

func TestVMDiskResizeSnapshots(t *testing.T) {
	d, f := diskEnv(t, "shut off")
	f.Responses[v+"snapshot-list homeassistant"] = sysexec.Result{Stdout: []byte(snapTable)}
	req := vm.DiskResize{Path: haosDisk, SizeGiB: 64}
	err := VMDiskResize(context.Background(), d, "homeassistant", req)
	if code(err) != apierr.Conflict || !strings.Contains(err.Error(), "s1") {
		t.Fatalf("%v", err)
	}
	if f.Called("qemu-img resize") || f.Called(v+"snapshot-delete") {
		t.Fatal("ohne Bestätigung darf nichts passieren")
	}
	req.DeleteSnapshots = true
	if err := VMDiskResize(context.Background(), d, "homeassistant", req); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, f, v+"snapshot-delete homeassistant s1", "qemu-img resize")
}

func TestVMDiskResizeRejects(t *testing.T) {
	d, f := diskEnv(t, "running")
	req := vm.DiskResize{Path: haosDisk, SizeGiB: 64}
	if code(VMDiskResize(context.Background(), d, "homeassistant", req)) != apierr.Conflict {
		t.Fatal("laufende VM")
	}
	d, f = diskEnv(t, "shut off")
	req.SizeGiB = 32
	if code(VMDiskResize(context.Background(), d, "homeassistant", req)) != apierr.InvalidParams {
		t.Fatal("gleiche Größe")
	}
	req.SizeGiB = 16
	if code(VMDiskResize(context.Background(), d, "homeassistant", req)) != apierr.InvalidParams {
		t.Fatal("verkleinern")
	}
	req = vm.DiskResize{Path: "/srv/vms/disks/fremd.qcow2", SizeGiB: 64}
	if code(VMDiskResize(context.Background(), d, "homeassistant", req)) != apierr.NotFound {
		t.Fatal("fremde Disk")
	}
	if code(VMDiskResize(context.Background(), d, "homeassistant", vm.DiskResize{Path: "/etc/x", SizeGiB: 64})) != apierr.InvalidParams {
		t.Fatal("Pfad")
	}
	if f.Called("qemu-img resize") {
		t.Fatal("kein Resize erwartet")
	}
}
