//go:build integration

// Integrationstest für VMs mit echtem libvirt/QEMU. Braucht root, virsh, qemu-img und das NAT-Netz „default“.
package agent

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/sysexec"
	"github.com/betternas/betternas/internal/vm"
)

func findVM(t *testing.T, d Deps, name string) (vm.VM, bool) {
	t.Helper()
	list, err := ListVMs(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range list {
		if v.Name == name {
			return v, true
		}
	}
	return vm.VM{}, false
}

func TestVMLifecycle(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("braucht root")
	}
	for _, bin := range []string{"virsh", "qemu-img"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " ist nicht installiert")
		}
	}
	ctx := context.Background()
	d := Deps{Runner: sysexec.NewRunner(), FS: sysexec.NewRootFS("/")}
	if st, _ := VMStatus(ctx, d); !st.Active {
		t.Skip("libvirt läuft nicht")
	}
	if nets, _ := VMNetworks(ctx, d); len(nets) == 0 {
		t.Skip("kein aktives libvirt-Netz")
	}
	const name = "bnas-it-vm"
	t.Cleanup(func() { // Reste in jedem Fall entfernen
		_ = VMAction(ctx, d, name, "destroy")
		_ = VMDelete(ctx, d, name, true)
	})
	_ = VMAction(ctx, d, name, "destroy")
	_ = VMDelete(ctx, d, name, true)

	spec := vm.Spec{Name: name, OS: "linux", Firmware: "bios", Network: "default", VCPUs: 1, MemoryMiB: 256, DiskGiB: 1}
	if err := VMCreate(ctx, d, spec, true); err != nil {
		t.Fatal(err)
	}
	if code(VMCreate(ctx, d, spec, false)) != "conflict" {
		t.Fatal("zweites Anlegen muss abgelehnt werden")
	}
	v, ok := findVM(t, d, name)
	if !ok || v.State != "running" || len(v.Disks) != 1 || v.Disks[0] != vm.DiskPath(name) {
		t.Fatalf("laufende VM erwartet: %+v", v)
	}
	if code(VMDelete(ctx, d, name, true)) != "conflict" {
		t.Fatal("laufende VM darf nicht gelöscht werden")
	}
	if err := VMAction(ctx, d, name, "autostart"); err != nil {
		t.Fatal(err)
	}
	if v, _ = findVM(t, d, name); !v.Autostart {
		t.Fatal("Autostart erwartet")
	}
	if err := VMAction(ctx, d, name, "noautostart"); err != nil {
		t.Fatal(err)
	}
	if err := VMAction(ctx, d, name, "destroy"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if v, _ = findVM(t, d, name); v.State == "shutoff" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if v.State != "shutoff" {
		t.Fatalf("ausgeschaltet erwartet: %+v", v)
	}
	if err := VMDelete(ctx, d, name, true); err != nil {
		t.Fatal(err)
	}
	if _, ok := findVM(t, d, name); ok {
		t.Fatal("VM muss weg sein")
	}
	if _, err := os.Stat(vm.DiskPath(name)); !os.IsNotExist(err) {
		t.Fatal("Disk muss gelöscht sein")
	}
}
