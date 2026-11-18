package agent

import (
	"context"
	"encoding/json"
	"path"
	"sort"
	"strings"

	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/vm"
)

func sysfsString(d Deps, p string) string {
	b, err := d.FS.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// VMUSBDevices liefert die am Host angeschlossenen USB-Geräte (ohne Hubs und Root-Hubs) aus sysfs.
func VMUSBDevices(_ context.Context, d Deps) ([]vm.USBDevice, error) {
	list := []vm.USBDevice{}
	dirs, err := d.FS.Glob("/sys/bus/usb/devices/*/idVendor")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, f := range dirs {
		dir := path.Dir(f)
		vendor := strings.ToLower(sysfsString(d, dir+"/idVendor"))
		product := strings.ToLower(sysfsString(d, dir+"/idProduct"))
		if len(vendor) != 4 || len(product) != 4 {
			continue
		}
		if sysfsString(d, dir+"/bDeviceClass") == "09" || vendor == "1d6b" { // Hubs und Linux-Root-Hubs
			continue
		}
		if seen[vendor+":"+product] {
			continue
		}
		seen[vendor+":"+product] = true
		name := strings.TrimSpace(sysfsString(d, dir+"/manufacturer") + " " + sysfsString(d, dir+"/product"))
		if name == "" {
			name = vendor + ":" + product
		}
		storage := false
		if ifs, err := d.FS.Glob(dir + "/*/bInterfaceClass"); err == nil {
			for _, i := range ifs {
				if sysfsString(d, i) == "08" {
					storage = true
				}
			}
		}
		list = append(list, vm.USBDevice{Vendor: vendor, Product: product, Name: name, Storage: storage})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

func registerVMUSB(s *rpc.Server, d Deps) {
	s.Handle("vm.usb", func(ctx context.Context, _ json.RawMessage) (any, error) { return VMUSBDevices(ctx, d) })
}
