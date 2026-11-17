package vm

import (
	"bytes"
	"encoding/xml"
	"regexp"
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

var snapshotLine = regexp.MustCompile(`^\s*(\S+)\s+(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d [+-]\d{4})\s+(.+?)\s*$`)

// ParseSnapshots liest die Tabelle von `virsh snapshot-list <name>`.
func ParseSnapshots(out []byte) []Snapshot {
	list := []Snapshot{}
	for _, line := range strings.Split(string(out), "\n") {
		if m := snapshotLine.FindStringSubmatch(line); m != nil {
			list = append(list, Snapshot{Name: m[1], Created: m[2], State: m[3]})
		}
	}
	return list
}

var displayRe = regexp.MustCompile(`^vnc://[^:]+:(\d+)$`)

// ParseDisplayPort liefert den TCP-Port aus `virsh domdisplay` („vnc://127.0.0.1:0“ → 5900).
func ParseDisplayPort(out []byte) (int, bool) {
	m := displayRe.FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 0 || n > 99 {
		return 0, false
	}
	return 5900 + n, true
}

// DomIface ist eine Netzwerkschnittstelle einer VM laut `virsh domiflist`.
type DomIface struct {
	Type   string
	Source string
}

// ParseDomIfList liest die Ausgabe von `virsh domiflist` (Spalten: Interface Type Source Model MAC).
func ParseDomIfList(out []byte) []DomIface {
	var list []DomIface
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] == "Interface" || strings.HasPrefix(f[0], "---") {
			continue
		}
		list = append(list, DomIface{Type: f[1], Source: f[2]})
	}
	return list
}
