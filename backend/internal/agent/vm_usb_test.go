package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeSys(t *testing.T, root, dir string, files map[string]string) {
	t.Helper()
	full := filepath.Join(root, "sys/bus/usb/devices", dir)
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	for n, c := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(full, n)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(full, n), []byte(c+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVMUSBDevices(t *testing.T) {
	d, _, root := vmEnv(t)
	writeSys(t, root, "usb1", map[string]string{"idVendor": "1d6b", "idProduct": "0002", "bDeviceClass": "09"})
	writeSys(t, root, "1-1", map[string]string{"idVendor": "1A86", "idProduct": "7523", "manufacturer": "QinHeng", "product": "USB Serial", "bDeviceClass": "ff"})
	writeSys(t, root, "1-2", map[string]string{"idVendor": "0b95", "idProduct": "1790", "bDeviceClass": "09"}) // Hub
	writeSys(t, root, "1-3", map[string]string{"idVendor": "174c", "idProduct": "55aa", "product": "ASM Platte", "bDeviceClass": "00", "1-3:1.0/bInterfaceClass": "08"})
	list, err := VMUSBDevices(context.Background(), d)
	if err != nil || len(list) != 2 {
		t.Fatalf("%+v %v", list, err)
	}
	if list[0].Name != "ASM Platte" || !list[0].Storage || list[1].Vendor != "1a86" || list[1].Name != "QinHeng USB Serial" || list[1].Storage {
		t.Fatalf("%+v", list)
	}
}
