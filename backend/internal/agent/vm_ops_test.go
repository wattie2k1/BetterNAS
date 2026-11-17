package agent

import (
	"context"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/sysexec"
)

const snapTable = " Name   Creation Time   State\n------\n s1   2026-11-17 10:00:00 +0100   shutoff\n"

func TestVMSnapshots(t *testing.T) {
	d, f, _ := vmEnv(t)
	ctx := context.Background()
	f.Responses[v+"snapshot-list debian"] = sysexec.Result{Stdout: []byte(snapTable)}
	list, err := VMSnapshots(ctx, d, "debian")
	if err != nil || len(list) != 1 || list[0].Name != "s1" {
		t.Fatalf("%v %v", list, err)
	}
	if code(VMSnapshotCreate(ctx, d, "debian", "s1")) != apierr.Conflict {
		t.Fatal("doppelter Name")
	}
	if err := VMSnapshotCreate(ctx, d, "debian", "s2"); err != nil {
		t.Fatal(err)
	}
	if !f.Called(v + "snapshot-create-as debian s2") {
		t.Fatal(f.Calls)
	}
	if err := VMSnapshotRevert(ctx, d, "debian", "s1"); err != nil || !f.Called(v+"snapshot-revert debian s1") {
		t.Fatal(err, f.Calls)
	}
	if err := VMSnapshotDelete(ctx, d, "debian", "s1"); err != nil || !f.Called(v+"snapshot-delete debian s1") {
		t.Fatal(err, f.Calls)
	}
}

func TestVMSnapshotRejectsBadNames(t *testing.T) {
	d, f, _ := vmEnv(t)
	ctx := context.Background()
	for _, snap := range []string{"", "-x", "a b", "a;b", "../x", "--all"} {
		if code(VMSnapshotCreate(ctx, d, "debian", snap)) != apierr.InvalidParams || code(VMSnapshotRevert(ctx, d, "debian", snap)) != apierr.InvalidParams {
			t.Errorf("%q", snap)
		}
	}
	if code(VMSnapshotDelete(ctx, d, "-bad", "s")) != apierr.InvalidParams || f.Called("virsh") {
		t.Fatal("nichts darf ausgeführt werden")
	}
}

func cloneEnv(t *testing.T, state string) (Deps, *sysexec.FakeRunner, string) {
	d, f, root := vmEnv(t)
	f.Responses[v+"dominfo debian"] = sysexec.Result{Stdout: []byte("Name: debian\nState: " + state + "\n")}
	f.Responses[v+"dumpxml --inactive debian"] = sysexec.Result{Stdout: []byte("<domain><devices><disk device='disk'><source file='/srv/vms/disks/debian.qcow2'/></disk></devices></domain>")}
	f.Responses[v+"dominfo debian2"] = sysexec.Result{ExitCode: 1} // Präfix-Treffer von „debian“ vermeiden
	writeFile(t, root, "/srv/vms/disks/debian.qcow2")
	return d, f, root
}

func TestVMClone(t *testing.T) {
	d, f, _ := cloneEnv(t, "shut off")
	if err := VMClone(context.Background(), d, "debian", "debian2"); err != nil {
		t.Fatal(err)
	}
	if !f.Called("virt-clone --connect qemu:///system --original debian --name debian2 --file /srv/vms/disks/debian2.qcow2") {
		t.Fatal(f.Calls)
	}
}

func TestVMCloneRefusals(t *testing.T) {
	ctx := context.Background()
	d, f, _ := cloneEnv(t, "running")
	if code(VMClone(ctx, d, "debian", "debian2")) != apierr.Conflict {
		t.Fatal("laufende VM")
	}
	d, f, root := cloneEnv(t, "shut off")
	f.Responses[v+"dominfo debian2"] = sysexec.Result{Stdout: []byte("Name: debian2\nState: shut off\n")}
	if code(VMClone(ctx, d, "debian", "debian2")) != apierr.Conflict {
		t.Fatal("Name vergeben")
	}
	f.Responses[v+"dominfo debian2"] = sysexec.Result{ExitCode: 1}
	writeFile(t, root, "/srv/vms/disks/debian2.qcow2")
	if code(VMClone(ctx, d, "debian", "debian2")) != apierr.Conflict {
		t.Fatal("Disk vorhanden")
	}
	d, f, _ = cloneEnv(t, "shut off")
	f.Responses[v+"dumpxml --inactive debian"] = sysexec.Result{Stdout: []byte("<domain><devices><disk device='disk'><source file='/etc/passwd'/></disk></devices></domain>")}
	if code(VMClone(ctx, d, "debian", "debian2")) != apierr.Conflict {
		t.Fatal("fremde Disk")
	}
	for _, n := range []string{"", "A", "a b", "../x", "-x"} {
		if code(VMClone(ctx, d, "debian", n)) != apierr.InvalidParams {
			t.Errorf("%q", n)
		}
	}
	if f.Called("virt-clone") {
		t.Fatal("nichts darf geklont werden")
	}
}

func TestVMCloneCleansUp(t *testing.T) {
	d, f, root := cloneEnv(t, "shut off")
	f.Hook = func(call string) (sysexec.Result, bool) {
		if call[:10] == "virt-clone" {
			writeFile(t, root, "/srv/vms/disks/debian2.qcow2")
			return sysexec.Result{ExitCode: 1, Stderr: []byte("boom")}, true
		}
		return sysexec.Result{}, false
	}
	if err := VMClone(context.Background(), d, "debian", "debian2"); err == nil {
		t.Fatal("Fehler erwartet")
	}
	if d.FS.Exists("/srv/vms/disks/debian2.qcow2") {
		t.Fatal("halbfertige Disk muss entfernt werden")
	}
}

func TestVMDisplay(t *testing.T) {
	d, f, _ := vmEnv(t)
	ctx := context.Background()
	f.Responses[v+"dominfo debian"] = sysexec.Result{Stdout: []byte("Name: debian\nState: running\n")}
	f.Responses[v+"domdisplay debian"] = sysexec.Result{Stdout: []byte("vnc://127.0.0.1:3\n")}
	if p, err := VMDisplay(ctx, d, "debian"); err != nil || p != 5903 {
		t.Fatal(p, err)
	}
	f.Responses[v+"dominfo debian"] = sysexec.Result{Stdout: []byte("Name: debian\nState: shut off\n")}
	if code2(VMDisplay(ctx, d, "debian")) != apierr.Conflict {
		t.Fatal("ausgeschaltet")
	}
}

func code2(_ int, err error) string { return code(err) }

func TestVMISODelete(t *testing.T) {
	d, f, root := vmEnv(t)
	ctx := context.Background()
	writeFile(t, root, "/srv/vms/iso/d.iso")
	f.Responses[v+"list --all --name"] = sysexec.Result{Stdout: []byte("debian\n")}
	f.Responses[v+"dumpxml --inactive debian"] = sysexec.Result{Stdout: []byte("<disk device='cdrom'><source file='/srv/vms/iso/d.iso'/></disk>")}
	if code(VMISODelete(ctx, d, "d.iso")) != apierr.Conflict || !d.FS.Exists("/srv/vms/iso/d.iso") {
		t.Fatal("verwendete ISO darf nicht gelöscht werden")
	}
	f.Responses[v+"dumpxml --inactive debian"] = sysexec.Result{Stdout: []byte("<domain/>")}
	if err := VMISODelete(ctx, d, "d.iso"); err != nil || d.FS.Exists("/srv/vms/iso/d.iso") {
		t.Fatal(err)
	}
	for _, n := range []string{"../x.iso", "a/b.iso", "x.txt", ".iso"} {
		if code(VMISODelete(ctx, d, n)) != apierr.InvalidParams {
			t.Errorf("%q", n)
		}
	}
	if code(VMISODelete(ctx, d, "none.iso")) != apierr.NotFound {
		t.Fatal("fehlende Datei")
	}
}

func TestVMInterfacesAndDirectNetwork(t *testing.T) {
	d, _, root := vmEnv(t)
	writeFile(t, root, "/sys/class/net/eno1/device/x")
	writeFile(t, root, "/sys/class/net/wlan0/device/x")
	writeFile(t, root, "/sys/class/net/wlan0/wireless/x")
	writeFile(t, root, "/sys/class/net/virbr0/x")
	ifs, _ := VMInterfaces(context.Background(), d)
	if len(ifs) != 1 || ifs[0] != "eno1" {
		t.Fatalf("%v", ifs)
	}
	spec := okVMSpec()
	spec.Network = "direct:eno1"
	if err := VMCreate(context.Background(), d, spec, false); err != nil {
		t.Fatal(err)
	}
	spec.Name, spec.Network = "x2", "direct:eth9"
	if code(VMCreate(context.Background(), d, spec, false)) != apierr.InvalidParams {
		t.Fatal("unbekannte Schnittstelle")
	}
}
