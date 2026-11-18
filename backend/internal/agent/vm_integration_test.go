//go:build integration

// Integrationstest für VMs mit echtem libvirt/QEMU. Braucht root, virsh, qemu-img und das NAT-Netz „default“.
package agent

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/apierr"
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

func waitShutoff(t *testing.T, d Deps, name string) {
	t.Helper()
	for i := 0; i < 20; i++ {
		if v, _ := findVM(t, d, name); v.State == "shutoff" {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("%s wird nicht ausgeschaltet", name)
}

func TestVMOps(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("braucht root")
	}
	for _, bin := range []string{"virsh", "qemu-img", "virt-clone"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " ist nicht installiert")
		}
	}
	ctx := context.Background()
	d := Deps{Runner: sysexec.NewRunner(), FS: sysexec.NewRootFS("/")}
	if st, _ := VMStatus(ctx, d); !st.Active {
		t.Skip("libvirt läuft nicht")
	}
	const name, clone = "bnas-it-ops", "bnas-it-ops-kopie"
	cleanup := func() {
		for _, n := range []string{name, clone} {
			_ = VMAction(ctx, d, n, "destroy")
			if v, ok := findVM(t, d, n); ok && v.State != "shutoff" {
				time.Sleep(time.Second)
			}
			_ = VMDelete(ctx, d, n, true)
		}
	}
	t.Cleanup(cleanup)
	cleanup()

	spec := vm.Spec{Name: name, OS: "linux", Firmware: "bios", Network: "default", VCPUs: 1, MemoryMiB: 256, DiskGiB: 1}
	if err := VMCreate(ctx, d, spec, false); err != nil {
		t.Fatal(err)
	}

	// Snapshots
	if err := VMSnapshotCreate(ctx, d, name, "s1"); err != nil {
		t.Fatal(err)
	}
	if code(VMSnapshotCreate(ctx, d, name, "s1")) != "conflict" {
		t.Fatal("doppelter Snapshot-Name")
	}
	list, err := VMSnapshots(ctx, d, name)
	if err != nil || len(list) != 1 || list[0].Name != "s1" || list[0].Created == "" {
		t.Fatalf("%+v %v", list, err)
	}
	if err := VMSnapshotRevert(ctx, d, name, "s1"); err != nil {
		t.Fatal(err)
	}

	// Klonen
	if err := VMClone(ctx, d, name, clone); err != nil {
		t.Fatal(err)
	}
	if v, ok := findVM(t, d, clone); !ok || len(v.Disks) != 1 || v.Disks[0] != vm.DiskPath(clone) {
		t.Fatalf("Klon erwartet: %+v", v)
	}
	if code(VMClone(ctx, d, name, clone)) != "conflict" {
		t.Fatal("Name vergeben")
	}

	// Konsole: VNC-Port nur bei laufender VM
	if code2(VMDisplay(ctx, d, name)) != "conflict" {
		t.Fatal("ausgeschaltete VM hat keine Konsole")
	}
	if err := VMAction(ctx, d, name, "start"); err != nil {
		t.Fatal(err)
	}
	port, err := VMDisplay(ctx, d, name)
	if err != nil || port < 5900 || port > 5999 {
		t.Fatalf("%d %v", port, err)
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	banner := make([]byte, 4)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(conn, banner); err != nil || string(banner) != "RFB " {
		t.Fatalf("VNC-Banner: %q %v", banner, err)
	}
	conn.Close()
	if code(VMClone(ctx, d, name, clone+"2")) != "conflict" {
		t.Fatal("laufende VM darf nicht geklont werden")
	}
	_ = VMAction(ctx, d, name, "destroy")
	waitShutoff(t, d, name)

	// Snapshot löschen, VM samt Metadaten entfernen
	if err := VMSnapshotDelete(ctx, d, name, "s1"); err != nil {
		t.Fatal(err)
	}
	if list, _ = VMSnapshots(ctx, d, name); len(list) != 0 {
		t.Fatalf("%+v", list)
	}
}

// TestVMSnapshotUEFI prüft, ob Snapshots auch bei UEFI-VMs (mit und ohne TPM) funktionieren.
func TestVMSnapshotUEFI(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("braucht root")
	}
	if _, err := exec.LookPath("virsh"); err != nil {
		t.Skip("virsh ist nicht installiert")
	}
	ctx := context.Background()
	d := Deps{Runner: sysexec.NewRunner(), FS: sysexec.NewRootFS("/")}
	if st, _ := VMStatus(ctx, d); !st.Active {
		t.Skip("libvirt läuft nicht")
	}
	for _, tpm := range []bool{false, true} {
		const name = "bnas-it-uefi"
		_ = VMDelete(ctx, d, name, true)
		spec := vm.Spec{Name: name, OS: "linux", Firmware: "uefi", Network: "default", VCPUs: 1, MemoryMiB: 256, DiskGiB: 1, TPM: tpm}
		if err := VMCreate(ctx, d, spec, false); err != nil {
			t.Fatal(err)
		}
		err := VMSnapshotCreate(ctx, d, name, "u1")
		t.Logf("UEFI tpm=%v: Snapshot-Fehler=%v", tpm, err)
		if err == nil {
			err = VMSnapshotRevert(ctx, d, name, "u1")
			t.Logf("UEFI tpm=%v: Revert-Fehler=%v", tpm, err)
		}
		if err := VMDelete(ctx, d, name, true); err != nil {
			t.Fatal(err)
		}
	}
}

// TestVMConfigureLibvirt ändert Ressourcen und Secure Boot einer UEFI-VM gegen echtes libvirt.
func TestVMConfigureLibvirt(t *testing.T) {
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
	const name = "bnas-it-cfg"
	t.Cleanup(func() {
		_ = VMAction(ctx, d, name, "destroy")
		_ = VMDelete(ctx, d, name, true)
	})
	_ = VMAction(ctx, d, name, "destroy")
	_ = VMDelete(ctx, d, name, true)

	// Windows/UEFI ohne TPM: libvirt wählt Secure Boot mit Microsoft-Schlüsseln.
	spec := vm.Spec{Name: name, OS: "windows", Firmware: "uefi", Network: "default", VCPUs: 1, MemoryMiB: 256, DiskGiB: 1}
	if err := VMCreate(ctx, d, spec, false); err != nil {
		t.Fatal(err)
	}
	c, err := VMConfig(ctx, d, name)
	if err != nil || !c.SecureBoot || c.Firmware != "uefi" || c.State != "shutoff" {
		t.Fatalf("Ausgangszustand: %+v %v", c, err)
	}
	u := vm.ConfigUpdate{VCPUs: 2, MemoryMiB: 512, SecureBoot: false, Network: "default", GuestAgent: true}
	if err := VMConfigure(ctx, d, name, u); err != nil {
		t.Fatal(err)
	}
	if c, err = VMConfig(ctx, d, name); err != nil || c.SecureBoot || c.VCPUs != 2 || c.MemoryMiB != 512 || c.Firmware != "uefi" {
		t.Fatalf("nach Änderung: %+v %v", c, err)
	}
	// Neue VMs haben den Guest-Agent-Kanal; er lässt sich aus- und wieder einschalten.
	if !c.GuestAgent {
		t.Fatalf("Guest-Agent-Kanal erwartet: %+v", c)
	}
	u.GuestAgent = false
	if err := VMConfigure(ctx, d, name, u); err != nil {
		t.Fatal(err)
	}
	if c, _ = VMConfig(ctx, d, name); c.GuestAgent {
		t.Fatal("Guest-Agent sollte aus sein")
	}
	u.GuestAgent = true
	if err := VMConfigure(ctx, d, name, u); err != nil {
		t.Fatal(err)
	}
	if c, _ = VMConfig(ctx, d, name); !c.GuestAgent {
		t.Fatal("Guest-Agent sollte an sein")
	}
	// Wieder einschalten, starten, laufende VM ist nicht änderbar.
	u.SecureBoot = true
	if err := VMConfigure(ctx, d, name, u); err != nil {
		t.Fatal(err)
	}
	if c, _ = VMConfig(ctx, d, name); !c.SecureBoot {
		t.Fatalf("Secure Boot erwartet: %+v", c)
	}
	u.SecureBoot = false
	if err := VMConfigure(ctx, d, name, u); err != nil {
		t.Fatal(err)
	}
	if err := VMAction(ctx, d, name, "start"); err != nil {
		t.Fatal(err)
	}
	if code(VMConfigure(ctx, d, name, u)) != "conflict" {
		t.Fatal("laufende VM darf nicht geändert werden")
	}
}

// TestVMTuningLibvirt prüft, dass echtes libvirt die erweiterten Einstellungen annimmt
// (CPU-Typ/Topologie, Hyper-V, Uhr, Laufwerksoptionen, Zusatz-NIC, Boot, MAC, USB).
func TestVMTuningLibvirt(t *testing.T) {
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
	const name = "bnas-it-tune"
	t.Cleanup(func() {
		_ = VMAction(ctx, d, name, "destroy")
		_ = VMDelete(ctx, d, name, true)
	})
	_ = VMAction(ctx, d, name, "destroy")
	_ = VMDelete(ctx, d, name, true)

	spec := vm.Spec{Name: name, OS: "linux", Firmware: "uefi", Network: "default", VCPUs: 2, MemoryMiB: 256, DiskGiB: 1}
	if err := VMCreate(ctx, d, spec, false); err != nil {
		t.Fatal(err)
	}
	c, err := VMConfig(ctx, d, name)
	if err != nil {
		t.Fatal(err)
	}
	on := true
	usb := []vm.USBDevice{{Vendor: "1a86", Product: "7523"}}
	nics := []vm.NIC{{Network: "default", Model: "e1000e"}}
	opts := []vm.DiskOpt{{Path: vm.DiskPath(name), Cache: "none", Discard: "unmap"}}
	u := vm.ConfigUpdate{VCPUs: 4, MemoryMiB: 256, SecureBoot: c.SecureBoot, Network: "default", GuestAgent: true, BootFirst: "hd", MAC: "52:54:00:aa:bb:cc", USB: &usb,
		TuningUpdate: vm.TuningUpdate{CPUMode: "host-model", Topology: &vm.Topology{Sockets: 1, Threads: 2}, Hyperv: &on, Clock: "localtime", ExtraNICs: &nics, DiskOpts: &opts}}
	if err := VMConfigure(ctx, d, name, u); err != nil {
		t.Fatalf("libvirt lehnt die Konfiguration ab: %v", err)
	}
	c, err = VMConfig(ctx, d, name)
	if err != nil {
		t.Fatal(err)
	}
	if c.VCPUs != 4 || c.Sockets != 1 || c.Threads != 2 || c.CPUMode != "host-model" || !c.Hyperv || c.Clock != "localtime" || c.Boot != "hd" || c.MAC != "52:54:00:aa:bb:cc" || len(c.USB) != 1 {
		t.Fatalf("%+v", c)
	}
	if len(c.ExtraNICs) != 1 || c.ExtraNICs[0].Model != "e1000e" || c.ExtraNICs[0].MAC == "" {
		t.Fatalf("Zusatzkarte: %+v", c.ExtraNICs)
	}
	if len(c.DiskOpts) != 1 || c.DiskOpts[0].Cache != "none" || c.DiskOpts[0].Discard != "unmap" {
		t.Fatalf("Laufwerk: %+v", c.DiskOpts)
	}
	// Zurückbauen: alles wieder entfernen, die Zusatzkarte behält vorher ihre MAC.
	off := false
	none, noUSB := []vm.NIC{}, []vm.USBDevice{}
	u2 := vm.ConfigUpdate{VCPUs: 4, MemoryMiB: 256, SecureBoot: c.SecureBoot, Network: "default", GuestAgent: true, USB: &noUSB,
		TuningUpdate: vm.TuningUpdate{Hyperv: &off, ExtraNICs: &none, Clock: "utc"}}
	if err := VMConfigure(ctx, d, name, u2); err != nil {
		t.Fatalf("Zurückbauen: %v", err)
	}
	if c, _ = VMConfig(ctx, d, name); c.Hyperv || len(c.ExtraNICs) != 0 || len(c.USB) != 0 || c.Clock != "utc" {
		t.Fatalf("%+v", c)
	}
}

// TestVMDiskAddRemoveLibvirt hängt eine zweite Festplatte an und wieder ab (echtes libvirt/qemu-img),
// einschließlich Snapshot-Bestätigung.
func TestVMDiskAddRemoveLibvirt(t *testing.T) {
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
	const name = "bnas-it-disk"
	t.Cleanup(func() {
		_ = VMAction(ctx, d, name, "destroy")
		_ = VMDelete(ctx, d, name, true)
	})
	_ = VMAction(ctx, d, name, "destroy")
	_ = VMDelete(ctx, d, name, true)

	spec := vm.Spec{Name: name, OS: "linux", Firmware: "bios", Network: "default", VCPUs: 1, MemoryMiB: 256, DiskGiB: 1}
	if err := VMCreate(ctx, d, spec, false); err != nil {
		t.Fatal(err)
	}
	file, err := VMDiskAdd(ctx, d, name, vm.DiskAdd{SizeGiB: 2})
	if err != nil || file != vm.ExtraDiskPath(name, 2) {
		t.Fatalf("%q %v", file, err)
	}
	disks, err := VMDisks(ctx, d, name)
	if err != nil || len(disks) != 2 || disks[1].Path != file || disks[1].SizeGiB != 2 {
		t.Fatalf("%+v %v", disks, err)
	}
	// Mit Snapshot: ohne Bestätigung Konflikt, mit Bestätigung wird er gelöscht.
	if err := VMSnapshotCreate(ctx, d, name, "s1"); err != nil {
		t.Fatal(err)
	}
	if _, err := VMDiskAdd(ctx, d, name, vm.DiskAdd{SizeGiB: 1}); code(err) != apierr.Conflict {
		t.Fatalf("Konflikt erwartet: %v", err)
	}
	if err := VMDiskRemove(ctx, d, name, vm.DiskRemove{Path: file}); code(err) != apierr.Conflict {
		t.Fatalf("Konflikt erwartet: %v", err)
	}
	if err := VMDiskRemove(ctx, d, name, vm.DiskRemove{Path: file, DeleteFile: true, DeleteSnapshots: true}); err != nil {
		t.Fatal(err)
	}
	if disks, _ = VMDisks(ctx, d, name); len(disks) != 1 {
		t.Fatalf("%+v", disks)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("Datei muss gelöscht sein")
	}
	if snaps, _ := VMSnapshots(ctx, d, name); len(snaps) != 0 {
		t.Fatalf("Snapshots müssen gelöscht sein: %+v", snaps)
	}
	// Die Systemfestplatte lässt sich nicht entfernen.
	if err := VMDiskRemove(ctx, d, name, vm.DiskRemove{Path: vm.DiskPath(name)}); code(err) != apierr.InvalidParams {
		t.Fatalf("%v", err)
	}
}
