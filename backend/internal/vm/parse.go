package vm

import (
	"bytes"
	"encoding/xml"
	"strconv"
	"strings"
)

// ParseDomInfo liest die Ausgabe von `virsh dominfo <name>`.
func ParseDomInfo(out []byte) (VM, error) {
	v := VM{Disks: []string{}, State: "other"}
	for _, line := range strings.Split(string(out), "\n") {
		k, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch strings.TrimSpace(k) {
		case "Name":
			v.Name = val
		case "UUID":
			v.UUID = val
		case "State":
			v.State = normalizeState(val)
		case "CPU(s)":
			v.VCPUs, _ = strconv.Atoi(val)
		case "Max memory":
			kib, _ := strconv.Atoi(strings.Fields(val + " ")[0])
			v.MemoryMiB = kib / 1024
		case "Autostart":
			v.Autostart = val == "enable"
		}
	}
	return v, nil
}

func normalizeState(s string) string {
	switch s {
	case "running", "idle":
		return "running"
	case "shut off":
		return "shutoff"
	case "paused":
		return "paused"
	case "in shutdown":
		return "shuttingdown"
	case "crashed":
		return "crashed"
	}
	return "other"
}

// ParseDisks liefert die Dateien aller `<disk device='disk'>` einer Domain-XML (CD-ROMs ausgenommen).
func ParseDisks(xmlData []byte) ([]string, error) {
	var dom struct {
		Disks []struct {
			Device string `xml:"device,attr"`
			Source struct {
				File string `xml:"file,attr"`
			} `xml:"source"`
		} `xml:"devices>disk"`
	}
	if err := xml.NewDecoder(bytes.NewReader(xmlData)).Decode(&dom); err != nil {
		return nil, err
	}
	files := []string{}
	for _, d := range dom.Disks {
		if d.Device == "disk" && d.Source.File != "" {
			files = append(files, d.Source.File)
		}
	}
	return files, nil
}

// ParseNetworks liest `virsh net-list --name`.
func ParseNetworks(out []byte) ([]string, error) {
	list := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			list = append(list, line)
		}
	}
	return list, nil
}
