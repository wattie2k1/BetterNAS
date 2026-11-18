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

type fakePCI struct {
	class, vendor, device, driver, group, bootVGA, model string
}

// pciEnv baut ein sysfs mit typischen Geräten eines NAS (Intel-Board) und lässt den FakeRunner darauf antworten.
func pciEnv(t *testing.T, devs map[string]fakePCI, iommu bool) (Deps, *sysexec.FakeRunner) {
	t.Helper()
	d, f, root := vmEnv(t)
	for addr, p := range devs {
		dir := filepath.Join(root, "sys/bus/pci/devices", addr)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		files := map[string]string{"class": p.class, "vendor": "0x" + p.vendor, "device": "0x" + p.device}
		if p.bootVGA != "" {
			files["boot_vga"] = p.bootVGA
		}
		for n, c := range files {
			if err := os.WriteFile(filepath.Join(dir, n), []byte(c+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if iommu {
		if err := os.MkdirAll(filepath.Join(root, "sys/kernel/iommu_groups/0"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "sys/class/net/eno1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sys/class/net/eno1/operstate"), []byte("up\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.Hook = func(call string) (sysexec.Result, bool) {
		switch {
		case strings.HasPrefix(call, "readlink -f "):
			var out []string
			for _, a := range strings.Fields(call)[2:] {
				addr, kind := filepath.Base(filepath.Dir(a)), filepath.Base(a)
				p := devs[addr]
				switch {
				case kind == "driver" && p.driver != "":
					out = append(out, "/sys/bus/pci/drivers/"+p.driver)
				case kind == "iommu_group" && iommu && p.group != "":
					out = append(out, "/sys/kernel/iommu_groups/"+p.group)
				default:
					out = append(out, "/sys/devices/pci0000:00/"+addr+"/"+kind)
				}
			}
			return sysexec.Result{Stdout: []byte(strings.Join(out, "\n") + "\n")}, true
		case strings.HasPrefix(call, "find /sys/block"):
			return sysexec.Result{Stdout: []byte("sdb ../devices/pci0000:00/0000:00:14.0/usb4/4-6/block/sdb\nsda ../devices/pci0000:00/0000:00:1f.2/ata1/host0/block/sda\nzram0 ../devices/virtual/block/zram0\n")}, true
		case strings.HasPrefix(call, "find /sys/class/net"):
			return sysexec.Result{Stdout: []byte("eno1 ../../devices/pci0000:00/0000:00:19.0/net/eno1\nlo ../../devices/virtual/net/lo\n")}, true
		case strings.HasPrefix(call, "udevadm info"):
			addr := filepath.Base(strings.Fields(call)[len(strings.Fields(call))-1])
			if m := devs[addr].model; m != "" {
				return sysexec.Result{Stdout: []byte("ID_VENDOR_FROM_DATABASE=Intel Corporation\nID_MODEL_FROM_DATABASE=" + m + "\n")}, true
			}
			return sysexec.Result{}, true
		}
		return sysexec.Result{}, false
	}
	return d, f
}

func nasDevices() map[string]fakePCI {
	return map[string]fakePCI{
		"0000:00:02.0": {class: "0x030000", vendor: "8086", device: "0412", driver: "i915", group: "0", bootVGA: "1", model: "Integrated Graphics"},
		"0000:00:03.0": {class: "0x040300", vendor: "8086", device: "0c0c", driver: "snd_hda_intel", group: "2", model: "HD Audio Controller"},
		"0000:00:14.0": {class: "0x0c0330", vendor: "8086", device: "8c31", driver: "xhci_hcd", group: "3", model: "xHCI"},
		"0000:00:16.0": {class: "0x078000", vendor: "8086", device: "8c3a", driver: "mei_me", group: "4"},
		"0000:00:19.0": {class: "0x020000", vendor: "8086", device: "153a", driver: "e1000e", group: "5"},
		"0000:00:1b.0": {class: "0x040300", vendor: "8086", device: "8c20", driver: "snd_hda_intel", group: "7", model: "High Definition Audio Controller"},
		"0000:00:1f.0": {class: "0x060100", vendor: "8086", device: "8c4e", driver: "lpc_ich", group: "9"},
		"0000:00:1f.2": {class: "0x010601", vendor: "8086", device: "8c02", driver: "ahci", group: "9"},
		"0000:00:1f.3": {class: "0x0c0500", vendor: "8086", device: "8c22", driver: "i801_smbus", group: "9"},
		"0000:00:1f.5": {class: "0x040300", vendor: "8086", device: "8c23", driver: "snd_hda_intel", group: "9", model: "Audio in der SATA-Gruppe"},
		"0000:01:00.0": {class: "0x030000", vendor: "10de", device: "1b80", driver: "nouveau", group: "10", model: "GP104 [GeForce GTX 1080]"},
		"0000:01:00.1": {class: "0x040300", vendor: "10de", device: "10f0", driver: "snd_hda_intel", group: "10", model: "GP104 High Definition Audio"},
	}
}

func byAddr(info vm.PCIInfo) map[string]vm.PCIDevice {
	m := map[string]vm.PCIDevice{}
	for _, d := range info.Devices {
		m[d.Address] = d
	}
	return m
}

func TestVMPCIDevicesClassification(t *testing.T) {
	d, _ := pciEnv(t, nasDevices(), true)
	info, err := VMPCIDevices(context.Background(), d)
	if err != nil || !info.IOMMU {
		t.Fatalf("%+v %v", info, err)
	}
	m := byAddr(info)
	if _, ok := m["0000:00:1f.0"]; ok {
		t.Fatal("Brücken gehören nicht in die Liste")
	}
	blocked := map[string]string{
		"0000:00:02.0": "Konsole",
		"0000:00:14.0": "sdb",       // Datenträger hängt am USB-Controller
		"0000:00:16.0": "System",    // Management-Engine
		"0000:00:19.0": "eno1",      // aktiver Netzwerkanschluss
		"0000:00:1f.2": "sda",       // SATA mit Systemplatte
		"0000:00:1f.3": "System",    // SMBus
		"0000:00:1f.5": "Teilt die", // an sich erlaubtes Audio, aber in der Gruppe des SATA-Controllers
	}
	for a, want := range blocked {
		if !strings.Contains(m[a].Blocked, want) {
			t.Errorf("%s: %q enthält nicht %q", a, m[a].Blocked, want)
		}
	}
	for _, a := range []string{"0000:00:03.0", "0000:00:1b.0", "0000:01:00.0", "0000:01:00.1"} {
		if m[a].Blocked != "" {
			t.Errorf("%s sollte erlaubt sein: %q", a, m[a].Blocked)
		}
	}
	if g := m["0000:00:1b.0"]; g.Group != 7 || g.Driver != "snd_hda_intel" || g.Kind != "audio" || g.ID != "8086:8c20" || g.Name != "Intel Corporation High Definition Audio Controller" {
		t.Fatalf("%+v", g)
	}
	if m["0000:01:00.0"].Kind != "gpu" || m["0000:01:00.0"].Note == "" || m["0000:00:14.0"].Note == "" {
		t.Fatal("Hinweise fehlen")
	}
}

func TestVMPCIDevicesWithoutIOMMU(t *testing.T) {
	d, _ := pciEnv(t, nasDevices(), false)
	info, _ := VMPCIDevices(context.Background(), d)
	if info.IOMMU || len(info.Devices) == 0 {
		t.Fatalf("%+v", info)
	}
	for _, dev := range info.Devices {
		if !strings.Contains(dev.Blocked, "IOMMU") {
			t.Fatalf("%s: %q", dev.Address, dev.Blocked)
		}
	}
}

func TestCheckPCI(t *testing.T) {
	ctx := context.Background()
	d, _ := pciEnv(t, nasDevices(), true)
	// Erlaubt
	if err := checkPCI(ctx, d, nil, []string{"0000:00:1b.0"}); err != nil {
		t.Fatal(err)
	}
	// Gesperrt, unbekannt
	err := checkPCI(ctx, d, nil, []string{"0000:00:1f.2"})
	if code(err) != apierr.Conflict || !strings.Contains(err.Error(), "sda") {
		t.Fatalf("%v", err)
	}
	if code(checkPCI(ctx, d, nil, []string{"0000:09:00.0"})) != apierr.NotFound {
		t.Fatal("unbekannt")
	}
	// Gruppe unvollständig: GPU ohne ihr Audio
	err = checkPCI(ctx, d, nil, []string{"0000:01:00.0"})
	if code(err) != apierr.Conflict || !strings.Contains(err.Error(), "0000:01:00.1") {
		t.Fatalf("%v", err)
	}
	if err := checkPCI(ctx, d, nil, []string{"0000:01:00.0", "0000:01:00.1"}); err != nil {
		t.Fatal(err)
	}
	// Bereits zugewiesene, inzwischen gesperrte oder fehlende Geräte bleiben stehen (kein Zwang zum Entfernen).
	if err := checkPCI(ctx, d, []string{"0000:00:1f.2", "0000:09:00.0"}, []string{"0000:00:1f.2", "0000:09:00.0"}); err != nil {
		t.Fatal(err)
	}
}

func TestVMConfigurePCI(t *testing.T) {
	ctx := context.Background()
	d, f, _ := configEnv(t, "shut off")
	// configEnv hat eigenes sysfs-freies FS; ein zweites Env mit Geräten bauen und Antworten übernehmen
	dp, fp := pciEnv(t, nasDevices(), true)
	for k, v := range f.Responses {
		fp.Responses[k] = v
	}
	_ = d
	pci := []string{"0000:00:1b.0"}
	u := vm.ConfigUpdate{VCPUs: 2, MemoryMiB: 4096, SecureBoot: true, Network: "default", TuningUpdate: vm.TuningUpdate{PCI: &pci}}
	if err := VMConfigure(ctx, dp, "homeassistant", u); err != nil {
		t.Fatal(err)
	}
	i := fp.Index(v + "define /dev/stdin")
	if i < 0 {
		t.Fatal(fp.Calls)
	}
	x := string(fp.Stdins[i])
	if !strings.Contains(x, "type='pci' managed='yes'") || !strings.Contains(x, "domain='0x0000' bus='0x00' slot='0x1b' function='0x0'") {
		t.Fatal(x)
	}
	// Gesperrtes Gerät: nichts wird definiert.
	dp2, fp2 := pciEnv(t, nasDevices(), true)
	for k, v := range f.Responses {
		fp2.Responses[k] = v
	}
	bad := []string{"0000:00:19.0"}
	u.PCI = &bad
	if code(VMConfigure(ctx, dp2, "homeassistant", u)) != apierr.Conflict || fp2.Called(v+"define") {
		t.Fatal("gesperrtes Gerät muss abgelehnt werden")
	}
}
