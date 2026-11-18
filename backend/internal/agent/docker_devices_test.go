package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/sysexec"
)

func deviceEnv(t *testing.T, files []string, props map[string]string, stat string) (Deps, *sysexec.FakeRunner) {
	t.Helper()
	d, f, _ := dockerEnv(t)
	for _, p := range files {
		if err := d.FS.MkdirAll(p[:strings.LastIndex(p, "/")], 0o755); err != nil {
			t.Fatal(err)
		}
		if err := d.FS.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f.Hook = func(call string) (sysexec.Result, bool) {
		if node, ok := strings.CutPrefix(call, "udevadm info -q property -p /sys/class/drm/"); ok {
			node = strings.TrimSuffix(node, "/device")
			if p, ok := props[node]; ok {
				return sysexec.Result{Stdout: []byte(p)}, true
			}
			return sysexec.Result{ExitCode: 1}, true
		}
		if strings.HasPrefix(call, "stat -L -c %n %g %G -- ") {
			return sysexec.Result{Stdout: []byte(stat)}, true
		}
		return sysexec.Result{}, false
	}
	return d, f
}

func TestHostDevicesGPUAndSerial(t *testing.T) {
	d, _ := deviceEnv(t,
		[]string{"/dev/dri/card0", "/dev/dri/renderD128", "/dev/serial/by-id/usb-ITEAD_SONOFF_Zigbee_3.0_USB_Dongle_Plus-if00-port0"},
		map[string]string{"renderD128": "ID_VENDOR_FROM_DATABASE=Intel Corporation\nID_MODEL_FROM_DATABASE=HD Graphics 4600\n"},
		"/dev/dri/renderD128 991 render\n/dev/serial/by-id/usb-ITEAD_SONOFF_Zigbee_3.0_USB_Dongle_Plus-if00-port0 20 dialout\n")
	list, err := ListHostDevices(context.Background(), d)
	if err != nil || len(list) != 2 {
		t.Fatalf("%+v %v", list, err)
	}
	g, s := list[0], list[1]
	if g.Kind != "gpu" || g.Path != "/dev/dri/renderD128" || g.GID != 991 || g.Group != "render" || !strings.Contains(g.Name, "HD Graphics 4600") || g.Note != "" {
		t.Errorf("GPU: %+v", g)
	}
	if s.Kind != "serial" || !strings.HasPrefix(s.Path, "/dev/serial/by-id/usb-ITEAD") || s.GID != 20 || s.Group != "dialout" || !strings.Contains(s.Name, "Zigbee") {
		t.Errorf("Seriell: %+v", s)
	}
}

func TestHostDevicesNvidiaGetsNote(t *testing.T) {
	d, _ := deviceEnv(t, []string{"/dev/dri/renderD129"},
		map[string]string{"renderD129": "ID_VENDOR_FROM_DATABASE=NVIDIA Corporation\nID_MODEL_FROM_DATABASE=GP104 [GeForce GTX 1070]\n"}, "/dev/dri/renderD129 991 render\n")
	list, _ := ListHostDevices(context.Background(), d)
	if len(list) != 1 || !strings.Contains(list[0].Note, "NVIDIA") {
		t.Fatalf("%+v", list)
	}
}

func TestHostDevicesNameFallbackWithoutUdev(t *testing.T) {
	d, _ := deviceEnv(t, []string{"/dev/dri/renderD128"}, nil, "/dev/dri/renderD128 991 render\n")
	_ = d.FS.MkdirAll("/sys/class/drm/renderD128/device", 0o755)
	_ = d.FS.WriteFile("/sys/class/drm/renderD128/device/vendor", []byte("0x1002\n"), 0o644)
	list, _ := ListHostDevices(context.Background(), d)
	if len(list) != 1 || !strings.Contains(list[0].Name, "AMD") {
		t.Fatalf("%+v", list)
	}
}

func TestHostDevicesNoneAndUnsafeNames(t *testing.T) {
	d, f := deviceEnv(t, []string{"/dev/serial/by-id/usb-evil name;rm", "/dev/serial/by-id/usb-ok-if00"}, nil, "/dev/serial/by-id/usb-ok-if00 20 dialout\n")
	list, err := ListHostDevices(context.Background(), d)
	if err != nil || len(list) != 1 || !strings.HasSuffix(list[0].Path, "usb-ok-if00") {
		t.Fatalf("%+v %v", list, err)
	}
	d2, f2 := deviceEnv(t, nil, nil, "")
	if l, err := ListHostDevices(context.Background(), d2); err != nil || len(l) != 0 || l == nil {
		t.Fatalf("%+v %v", l, err)
	}
	for _, c := range f2.Calls {
		if strings.HasPrefix(c, "stat") {
			t.Error("ohne Geräte darf stat nicht laufen")
		}
	}
	_ = f
}

func TestHostDevicesUnknownGPUDoesNotPanic(t *testing.T) {
	d, _ := deviceEnv(t, []string{"/dev/dri/renderD128"}, nil, "/dev/dri/renderD128 44 video\n")
	list, err := ListHostDevices(context.Background(), d)
	if err != nil || len(list) != 1 || list[0].Name != "Grafikeinheit (renderD128)" {
		t.Fatalf("%+v %v", list, err)
	}
}
