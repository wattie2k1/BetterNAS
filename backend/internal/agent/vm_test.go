package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/sysexec"
	"github.com/betternas/betternas/internal/vm"
)

const v = "virsh -c qemu:///system "

func vmEnv(t *testing.T) (Deps, *sysexec.FakeRunner, string) {
	t.Helper()
	root := t.TempDir()
	f := &sysexec.FakeRunner{Responses: map[string]sysexec.Result{
		v + "net-list --name": {Stdout: []byte("default\n")},
		v + "dominfo":         {ExitCode: 1, Stderr: []byte("error: failed to get domain")},
	}}
	return Deps{Runner: f, FS: sysexec.NewRootFS(root)}, f, root
}

func okVMSpec() vm.Spec {
	return vm.Spec{Name: "debian", OS: "linux", Firmware: "uefi", Network: "default", VCPUs: 2, MemoryMiB: 4096, DiskGiB: 40}
}

func writeFile(t *testing.T, root, p string) {
	t.Helper()
	full := filepath.Join(root, p)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestVMStatus(t *testing.T) {
	d, f, root := vmEnv(t)
	st, _ := VMStatus(context.Background(), d)
	if !st.Installed || !st.Active || st.KVM {
		t.Fatalf("%+v", st)
	}
	writeFile(t, root, "/dev/kvm")
	if st, _ = VMStatus(context.Background(), d); !st.KVM {
		t.Fatal("KVM erwartet")
	}
	f.Responses[v+"list --name"] = sysexec.Result{ExitCode: 1}
	if st, _ = VMStatus(context.Background(), d); st.Active {
		t.Fatal("libvirt nicht aktiv erwartet")
	}
	f.Responses["virsh --version"] = sysexec.Result{ExitCode: 127}
	if st, _ = VMStatus(context.Background(), d); st.Installed {
		t.Fatal("nicht installiert erwartet")
	}
}

func TestVMListEmptyWhenLibvirtDown(t *testing.T) {
	d, f, _ := vmEnv(t)
	f.Responses[v+"list --all --name"] = sysexec.Result{ExitCode: 1}
	list, err := ListVMs(context.Background(), d)
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("%v %v", list, err)
	}
}

func TestVMList(t *testing.T) {
	d, f, _ := vmEnv(t)
	f.Responses[v+"list --all --name"] = sysexec.Result{Stdout: []byte("debian\n\n-bad\n")}
	f.Responses[v+"dominfo debian"] = sysexec.Result{Stdout: []byte("Name: debian\nState: running\nCPU(s): 2\nMax memory: 2097152 KiB\nAutostart: enable\n")}
	f.Responses[v+"dumpxml --inactive debian"] = sysexec.Result{Stdout: []byte("<domain><devices><disk device='disk'><source file='/srv/vms/disks/debian.qcow2'/></disk></devices></domain>")}
	list, _ := ListVMs(context.Background(), d)
	if len(list) != 1 || list[0].Name != "debian" || list[0].State != "running" || list[0].MemoryMiB != 2048 || !list[0].Autostart || len(list[0].Disks) != 1 {
		t.Fatalf("%+v", list)
	}
}

func TestVMListUptime(t *testing.T) {
	d, f, root := vmEnv(t)
	f.Responses[v+"list --all --name"] = sysexec.Result{Stdout: []byte("debian\n")}
	f.Responses[v+"dominfo debian"] = sysexec.Result{Stdout: []byte("Name: debian\nState: running\n")}
	f.Responses["ps -o etimes= -p 4242"] = sysexec.Result{Stdout: []byte("  7200\n")}
	full := filepath.Join(root, "/run/libvirt/qemu/debian.pid")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("4242\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	list, _ := ListVMs(context.Background(), d)
	if len(list) != 1 || list[0].UptimeSeconds != 7200 {
		t.Fatalf("%+v", list)
	}
}

func TestCreateBuildsDiskAndDomain(t *testing.T) {
	d, f, _ := vmEnv(t)
	if err := VMCreate(context.Background(), d, okVMSpec(), true); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, f, "qemu-img create -f qcow2 /srv/vms/disks/debian.qcow2 40G", v+"define /dev/stdin", v+"start debian")
	i := f.Index(v + "define")
	if !strings.Contains(string(f.Stdins[i]), "<name>debian</name>") {
		t.Fatal("XML nicht per stdin übergeben")
	}
}

func TestCreateRefusesExisting(t *testing.T) {
	d, f, root := vmEnv(t)
	f.Responses[v+"dominfo debian"] = sysexec.Result{Stdout: []byte("Name: debian\nState: running\n")}
	if code(VMCreate(context.Background(), d, okVMSpec(), false)) != apierr.Conflict {
		t.Fatal("bestehende VM")
	}
	delete(f.Responses, v+"dominfo debian")
	writeFile(t, root, "/srv/vms/disks/debian.qcow2")
	if code(VMCreate(context.Background(), d, okVMSpec(), false)) != apierr.Conflict {
		t.Fatal("bestehende Disk")
	}
	if f.Called("qemu-img") {
		t.Fatal("nichts darf überschrieben werden")
	}
}

func TestCreateCleansUpDisk(t *testing.T) {
	d, f, root := vmEnv(t)
	f.Hook = func(call string) (sysexec.Result, bool) {
		if strings.HasPrefix(call, "qemu-img create") {
			writeFile(t, root, "/srv/vms/disks/debian.qcow2")
		}
		return sysexec.Result{}, false
	}
	f.Responses[v+"define"] = sysexec.Result{ExitCode: 1, Stderr: []byte("boom")}
	if err := VMCreate(context.Background(), d, okVMSpec(), false); err == nil {
		t.Fatal("Fehler erwartet")
	}
	if d.FS.Exists("/srv/vms/disks/debian.qcow2") {
		t.Fatal("Disk muss wieder entfernt werden")
	}
}

func TestCreateRequiresExistingISOAndNetwork(t *testing.T) {
	d, f, root := vmEnv(t)
	s := okVMSpec()
	s.ISO = "d.iso"
	if code(VMCreate(context.Background(), d, s, false)) != apierr.NotFound {
		t.Fatal("ISO fehlt")
	}
	writeFile(t, root, "/srv/vms/iso/d.iso")
	s.Network = "other"
	if code(VMCreate(context.Background(), d, s, false)) != apierr.InvalidParams {
		t.Fatal("Netz inaktiv")
	}
	if f.Called("qemu-img") {
		t.Fatal("nichts angelegt")
	}
}

func TestActionWhitelist(t *testing.T) {
	d, f, _ := vmEnv(t)
	ctx := context.Background()
	if code(VMAction(ctx, d, "debian", "undefine")) != apierr.InvalidParams || code(VMAction(ctx, d, "--x", "start")) != apierr.InvalidParams {
		t.Fatal("ungültig muss abgelehnt werden")
	}
	if len(f.Calls) != 0 {
		t.Fatalf("nichts darf laufen: %v", f.Calls)
	}
	for action, want := range map[string]string{"start": v + "start debian", "destroy": v + "destroy debian", "autostart": v + "autostart debian", "noautostart": v + "autostart --disable debian"} {
		if err := VMAction(ctx, d, "debian", action); err != nil || !f.Called(want) {
			t.Errorf("%s: %v", action, err)
		}
	}
}

func deleteEnv(t *testing.T, state string) (Deps, *sysexec.FakeRunner, string) {
	d, f, root := vmEnv(t)
	f.Responses[v+"dominfo debian"] = sysexec.Result{Stdout: []byte("Name: debian\nState: " + state + "\n")}
	f.Responses[v+"dumpxml --inactive debian"] = sysexec.Result{Stdout: []byte(
		"<domain><devices><disk device='disk'><source file='/srv/vms/disks/debian.qcow2'/></disk><disk device='disk'><source file='/srv/vms/disks/../../etc/keep'/></disk><disk device='cdrom'><source file='/srv/vms/iso/d.iso'/></disk></devices></domain>")}
	writeFile(t, root, "/srv/vms/disks/debian.qcow2")
	writeFile(t, root, "/srv/vms/iso/d.iso")
	writeFile(t, root, "/etc/keep")
	return d, f, root
}

func TestDeleteRefusesRunning(t *testing.T) {
	d, f, _ := deleteEnv(t, "running")
	if code(VMDelete(context.Background(), d, "debian", true)) != apierr.Conflict {
		t.Fatal("conflict erwartet")
	}
	if f.Called(v+"undefine") || !d.FS.Exists("/srv/vms/disks/debian.qcow2") {
		t.Fatal("nichts darf entfernt werden")
	}
}

func TestDeleteOnlyOwnDisks(t *testing.T) {
	d, f, _ := deleteEnv(t, "shut off")
	if err := VMDelete(context.Background(), d, "debian", true); err != nil {
		t.Fatal(err)
	}
	if !f.Called(v + "undefine debian --nvram --tpm") {
		t.Fatal("undefine fehlt")
	}
	if d.FS.Exists("/srv/vms/disks/debian.qcow2") {
		t.Fatal("eigene Disk muss weg")
	}
	if !d.FS.Exists("/etc/keep") || !d.FS.Exists("/srv/vms/iso/d.iso") {
		t.Fatal("fremde Dateien und ISOs müssen bleiben")
	}
}

func TestDeleteKeepsDisksByDefault(t *testing.T) {
	d, _, _ := deleteEnv(t, "shut off")
	if err := VMDelete(context.Background(), d, "debian", false); err != nil || !d.FS.Exists("/srv/vms/disks/debian.qcow2") {
		t.Fatal("Disk muss bleiben", err)
	}
}

func TestDeleteNotFound(t *testing.T) {
	d, _, _ := vmEnv(t)
	if code(VMDelete(context.Background(), d, "nix", false)) != apierr.NotFound {
		t.Fatal("not_found erwartet")
	}
}

func TestVMISOs(t *testing.T) {
	d, _, root := vmEnv(t)
	writeFile(t, root, "/srv/vms/iso/b.iso")
	writeFile(t, root, "/srv/vms/iso/a.iso")
	writeFile(t, root, "/srv/vms/iso/x.img")
	got, _ := VMISOs(context.Background(), d)
	if len(got) != 2 || got[0] != "a.iso" || got[1] != "b.iso" {
		t.Fatalf("%v", got)
	}
}
