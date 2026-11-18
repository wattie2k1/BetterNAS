package agent

import (
	"context"
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/betternas/betternas/internal/docker"
	"github.com/betternas/betternas/internal/rpc"
)

// Nur Pfade aus unkritischen Zeichen gehen in YAML-Vorschläge.
var devicePathRe = regexp.MustCompile(`^/dev/[A-Za-z0-9_./:+@-]+$`)

var gpuVendors = map[string]string{"0x8086": "Intel", "0x1002": "AMD", "0x10de": "NVIDIA"}

const nvidiaNote = "NVIDIA-Karten brauchen zusätzlich den NVIDIA-Treiber und das NVIDIA Container Toolkit; das Durchreichen des Geräts allein reicht nicht."

// gpuName liefert den Anzeigenamen einer Grafikeinheit aus der udev-Datenbank, sonst nur den Hersteller aus sysfs.
func gpuName(ctx context.Context, d Deps, node string) (name, vendor string) {
	base := "/sys/class/drm/" + node + "/device"
	var v, m string
	if res, err := d.Runner.Run(ctx, nil, "udevadm", "info", "-q", "property", "-p", base); err == nil && res.ExitCode == 0 {
		for _, line := range strings.Split(string(res.Stdout), "\n") {
			if x, ok := strings.CutPrefix(line, "ID_VENDOR_FROM_DATABASE="); ok {
				v = strings.TrimSpace(x)
			}
			if x, ok := strings.CutPrefix(line, "ID_MODEL_FROM_DATABASE="); ok {
				m = strings.TrimSpace(x)
			}
		}
	}
	id := sysfsString(d, base+"/vendor")
	vendor = gpuVendors[id]
	if f := strings.Fields(v); vendor == "" && len(f) > 0 {
		vendor = f[0]
	}
	switch {
	case m != "":
		return vendor + " " + m, vendor
	case vendor != "":
		return vendor + "-Grafik (" + node + ")", vendor
	}
	return "Grafikeinheit (" + node + ")", vendor
}

// ListHostDevices liefert die Geräte, die man an Container durchreichen kann: GPU-Render-Nodes und
// USB-Seriell-Adapter (über ihre stabilen /dev/serial/by-id-Namen), jeweils mit der Gruppe der Gerätedatei.
func ListHostDevices(ctx context.Context, d Deps) ([]docker.HostDevice, error) {
	list := []docker.HostDevice{}
	add := func(kind, p, name, note string) {
		if devicePathRe.MatchString(p) {
			list = append(list, docker.HostDevice{Kind: kind, Path: p, Name: name, Note: note})
		}
	}
	nodes, _ := d.FS.Glob("/dev/dri/renderD*")
	sort.Strings(nodes)
	for _, p := range nodes {
		name, vendor := gpuName(ctx, d, path.Base(p))
		note := ""
		if vendor == "NVIDIA" {
			note = nvidiaNote
		}
		add("gpu", p, name, note)
	}
	serial, _ := d.FS.Glob("/dev/serial/by-id/*")
	sort.Strings(serial)
	for _, p := range serial {
		add("serial", p, strings.TrimPrefix(path.Base(p), "usb-"), "")
	}
	if len(list) == 0 {
		return list, nil
	}
	args := []string{"-L", "-c", "%n %g %G", "--"}
	for _, dev := range list {
		args = append(args, dev.Path)
	}
	// Ein nicht lesbares Gerät fällt nur ohne Gruppeninfo aus; `stat` meldet es auf stderr und liefert die übrigen.
	res, _ := d.Runner.Run(ctx, nil, "stat", args...)
	groups := map[string]docker.HostDevice{}
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		f := strings.Fields(line)
		if len(f) == 3 {
			if gid, err := strconv.Atoi(f[1]); err == nil {
				groups[f[0]] = docker.HostDevice{GID: gid, Group: f[2]}
			}
		}
	}
	out := list[:0]
	for _, dev := range list {
		g, ok := groups[dev.Path]
		if !ok {
			continue // ohne Gruppe lässt sich kein brauchbarer Vorschlag erzeugen
		}
		dev.GID, dev.Group = g.GID, g.Group
		out = append(out, dev)
	}
	return out, nil
}

func registerDockerDevices(s *rpc.Server, d Deps) {
	s.Handle("device.list", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return ListHostDevices(ctx, d)
	})
}
