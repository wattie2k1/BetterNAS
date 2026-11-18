package agent

import (
	"context"
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/vm"
)

var pciInPath = regexp.MustCompile(`[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]`)

// pciOwners liefert zu jedem Gerätenamen eines `find … -printf "%f %l\n"`-Ergebnisses die PCI-Adressen
// im Geräte-Pfad (Brücken und Endgerät) – so erkennt man, woran ein Datenträger oder Netzwerkanschluss hängt.
func pciOwners(out []byte) map[string][]string {
	m := map[string][]string{}
	for _, line := range strings.Split(string(out), "\n") {
		name, target, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		if addrs := pciInPath.FindAllString(target, -1); len(addrs) > 0 {
			m[name] = addrs
		}
	}
	return m
}

func pciKind(class string) string {
	c := strings.TrimPrefix(strings.ToLower(class), "0x")
	switch {
	case strings.HasPrefix(c, "03"):
		return "gpu"
	case strings.HasPrefix(c, "04"):
		return "audio"
	case strings.HasPrefix(c, "0c03"):
		return "usb"
	case strings.HasPrefix(c, "02"):
		return "net"
	case strings.HasPrefix(c, "01"):
		return "storage"
	}
	return "other"
}

// pciPassable meldet, ob die Geräteklasse überhaupt zum Durchreichen gedacht ist
// (Speicher, Netzwerk, Grafik, Multimedia, USB, Funk, Beschleuniger – keine Systemgeräte).
func pciPassable(class string) bool {
	c := strings.TrimPrefix(strings.ToLower(class), "0x")
	if len(c) < 4 {
		return false
	}
	if strings.HasPrefix(c, "0c03") {
		return true
	}
	switch c[:2] {
	case "01", "02", "03", "04", "0b", "0d", "12":
		return true
	}
	return false
}

// VMPCIDevices listet die PCI-Geräte des Hosts mit IOMMU-Gruppe, Treiber und der Auskunft, ob (und warum
// nicht) sie sich an eine VM durchreichen lassen. Geräte, an denen der Betrieb des NAS hängt (Datenträger,
// aktiver Netzwerkanschluss, Konsolen-Grafik) und ihre IOMMU-Gruppe sind gesperrt.
func VMPCIDevices(ctx context.Context, d Deps) (vm.PCIInfo, error) {
	info := vm.PCIInfo{Devices: []vm.PCIDevice{}}
	groups, _ := d.FS.Glob("/sys/kernel/iommu_groups/*")
	info.IOMMU = len(groups) > 0
	paths, err := d.FS.Glob("/sys/bus/pci/devices/*")
	if err != nil {
		return info, err
	}
	sort.Strings(paths)

	// Treiber und IOMMU-Gruppe stehen als Symlinks im sysfs; `readlink -f` liefert je Argument eine Zeile
	// (bei fehlendem Link den Pfad selbst).
	args := []string{"-f"}
	for _, p := range paths {
		args = append(args, p+"/driver", p+"/iommu_group")
	}
	links := map[string]string{}
	if len(paths) > 0 {
		if out, err := run(ctx, d.Runner, nil, "readlink", args...); err == nil {
			lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
			for i, p := range paths {
				if 2*i+1 >= len(lines) {
					break
				}
				// Ein vorhandener Link löst zu /sys/bus/pci/drivers/<name> bzw. /sys/kernel/iommu_groups/<n> auf;
				// fehlt er, bleibt der Pfad unter dem Gerät stehen.
				for j, kind := range []struct{ name, prefix string }{{"driver", "/sys/bus/pci/drivers/"}, {"iommu_group", "/sys/kernel/iommu_groups/"}} {
					if l := strings.TrimSpace(lines[2*i+j]); strings.HasPrefix(l, kind.prefix) {
						links[p+"/"+kind.name] = path.Base(l)
					}
				}
			}
		}
	}
	blockOut, _ := run(ctx, d.Runner, nil, "find", "/sys/block", "-maxdepth", "1", "-type", "l", "-printf", "%f %l\n")
	netOut, _ := run(ctx, d.Runner, nil, "find", "/sys/class/net", "-maxdepth", "1", "-type", "l", "-printf", "%f %l\n")
	hostDisks := map[string][]string{}
	for name, addrs := range pciOwners(blockOut) {
		for _, a := range addrs {
			hostDisks[a] = append(hostDisks[a], name)
		}
	}
	hostNICs := map[string][]string{}
	for name, addrs := range pciOwners(netOut) {
		if sysfsString(d, "/sys/class/net/"+name+"/operstate") != "up" {
			continue
		}
		for _, a := range addrs {
			hostNICs[a] = append(hostNICs[a], name)
		}
	}

	for _, p := range paths {
		addr := path.Base(p)
		class := sysfsString(d, p+"/class")
		if !vm.ValidPCIAddress(addr) || len(class) < 6 || strings.HasPrefix(strings.TrimPrefix(class, "0x"), "06") { // Brücken bleiben außen vor
			continue
		}
		dev := vm.PCIDevice{Address: addr, Kind: pciKind(class), Group: -1, Driver: links[p+"/driver"]}
		dev.ID = strings.TrimPrefix(sysfsString(d, p+"/vendor"), "0x") + ":" + strings.TrimPrefix(sysfsString(d, p+"/device"), "0x")
		dev.Name = dev.ID
		if out, err := run(ctx, d.Runner, nil, "udevadm", "info", "-q", "property", "-p", p); err == nil {
			var vendor, model string
			for _, line := range strings.Split(string(out), "\n") {
				if v, ok := strings.CutPrefix(line, "ID_VENDOR_FROM_DATABASE="); ok {
					vendor = v
				}
				if v, ok := strings.CutPrefix(line, "ID_MODEL_FROM_DATABASE="); ok {
					model = v
				}
			}
			if model != "" {
				dev.Name = strings.TrimSpace(vendor + " " + model)
			}
		}
		if g, err := strconv.Atoi(links[p+"/iommu_group"]); err == nil {
			dev.Group = g
		}
		switch {
		case !info.IOMMU:
			dev.Blocked = "Die IOMMU ist nicht aktiv (im BIOS VT-d/AMD-Vi einschalten und den Kernel mit intel_iommu=on bzw. amd_iommu=on starten)"
		case dev.Group < 0:
			dev.Blocked = "Das Gerät hat keine IOMMU-Gruppe"
		case sysfsString(d, p+"/boot_vga") == "1":
			dev.Blocked = "Grafik der NAS-Konsole"
		case len(hostDisks[addr]) > 0:
			dev.Blocked = "Datenträger des NAS hängen an diesem Gerät (" + strings.Join(sorted(hostDisks[addr]), ", ") + ")"
		case len(hostNICs[addr]) > 0:
			dev.Blocked = "Aktiver Netzwerkanschluss des NAS (" + strings.Join(sorted(hostNICs[addr]), ", ") + ")"
		case !pciPassable(class):
			dev.Blocked = "Systemgerät, nicht zum Durchreichen gedacht"
		}
		switch dev.Kind {
		case "usb":
			dev.Note = "Alle USB-Geräte an diesem Controller stehen dem NAS dann nicht mehr zur Verfügung (auch Tastatur)."
		case "gpu":
			dev.Note = "Grafikkarten brauchen meist UEFI in der VM und ein passendes Gast-System; ohne Monitor an der Karte ist die Anzeige nur über deren Ausgänge sichtbar."
		case "storage":
			dev.Note = "Der Controller samt angeschlossenen Platten gehört danach der VM."
		}
		info.Devices = append(info.Devices, dev)
	}

	// Eine IOMMU-Gruppe lässt sich nur als Ganzes durchreichen: ist ein Mitglied gesperrt, sind es alle.
	blockedBy := map[int]string{}
	for _, dev := range info.Devices {
		if dev.Blocked != "" && dev.Group >= 0 && info.IOMMU {
			if _, ok := blockedBy[dev.Group]; !ok {
				blockedBy[dev.Group] = dev.Address
			}
		}
	}
	for i, dev := range info.Devices {
		if by, ok := blockedBy[dev.Group]; ok && dev.Blocked == "" {
			info.Devices[i].Blocked = "Teilt die IOMMU-Gruppe " + strconv.Itoa(dev.Group) + " mit dem gesperrten Gerät " + by
		}
	}
	return info, nil
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

// checkPCI prüft die gewünschten PCI-Geräte: neue müssen vorhanden und freigegeben sein, und IOMMU-Gruppen
// müssen vollständig zugewiesen werden (VFIO verlangt das).
func checkPCI(ctx context.Context, d Deps, cur, want []string) error {
	info, err := VMPCIDevices(ctx, d)
	if err != nil {
		return err
	}
	byAddr := map[string]vm.PCIDevice{}
	for _, dev := range info.Devices {
		byAddr[dev.Address] = dev
	}
	have := map[string]bool{}
	for _, a := range cur {
		have[a] = true
	}
	wanted := map[string]bool{}
	for _, a := range want {
		wanted[a] = true
	}
	for _, a := range want {
		dev, ok := byAddr[a]
		if !ok {
			if have[a] {
				continue // bereits zugewiesen, aber gerade nicht (mehr) vorhanden
			}
			return apierr.New(apierr.NotFound, "PCI-Gerät nicht gefunden: "+a)
		}
		if dev.Blocked != "" {
			if have[a] {
				continue // schon zugewiesen, inzwischen aber gesperrt: stehen lassen, nicht blockieren
			}
			return apierr.New(apierr.Conflict, "PCI-Gerät "+a+" lässt sich nicht durchreichen: "+dev.Blocked)
		}
		for _, other := range info.Devices {
			if other.Group == dev.Group && dev.Group >= 0 && other.Address != a && !wanted[other.Address] {
				return apierr.New(apierr.Conflict, "PCI-Gerät "+other.Address+" ("+other.Name+") gehört zur selben IOMMU-Gruppe wie "+a+" und muss mit durchgereicht werden")
			}
		}
	}
	return nil
}

func registerVMPCI(s *rpc.Server, d Deps) {
	s.Handle("vm.pci", func(ctx context.Context, _ json.RawMessage) (any, error) { return VMPCIDevices(ctx, d) })
}
