package vm

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
)

// HypervXML sind die Hyper-V-Optimierungen für Windows-Gäste (Features-Block).
const HypervXML = "    <hyperv mode='custom'>\n      <relaxed state='on'/>\n      <vapic state='on'/>\n      <spinlocks state='on' retries='8191'/>\n    </hyperv>\n"

const MaxExtraNICs = 4

// NIC ist eine (zusätzliche) Netzwerkkarte. Bei bestehenden Karten bleibt das Modell unveränderlich.
type NIC struct {
	Network string `json:"network"`
	Model   string `json:"model"` // virtio | e1000e | rtl8139
	MAC     string `json:"mac"`   // leer = beim Anlegen automatisch
}

// DiskOpt sind die Laufwerksoptionen einer Festplatte.
type DiskOpt struct {
	Path    string `json:"path"`
	Cache   string `json:"cache"`   // default | none | writeback | writethrough
	Discard string `json:"discard"` // ignore | unmap (TRIM)
}

// Topology ist die CPU-Topologie; Kerne ergeben sich aus vCPUs / (Sockets × Threads).
type Topology struct {
	Sockets int `json:"sockets"`
	Threads int `json:"threads"`
}

// Tuning sind die erweiterten Einstellungen (Teil von Config).
type Tuning struct {
	CPUMode   string    `json:"cpuMode"` // host-passthrough | host-model | other
	Sockets   int       `json:"sockets"` // 0 = keine Topologie festgelegt
	Threads   int       `json:"threads"`
	Hyperv    bool      `json:"hyperv"`
	Clock     string    `json:"clock"` // utc | localtime | other
	ExtraNICs []NIC     `json:"extraNics"`
	DiskOpts  []DiskOpt `json:"diskOpts"`
}

// TuningUpdate sind die erweiterten Werte; fehlt ein Feld, bleibt der Wert unverändert.
type TuningUpdate struct {
	CPUMode   string     `json:"cpuMode"`
	Topology  *Topology  `json:"topology"`
	Hyperv    *bool      `json:"hyperv"`
	Clock     string     `json:"clock"`
	ExtraNICs *[]NIC     `json:"extraNics"`
	DiskOpts  *[]DiskOpt `json:"diskOpts"`
}

var nicModels = map[string]bool{"virtio": true, "e1000e": true, "rtl8139": true}
var diskCaches = map[string]bool{"default": true, "none": true, "writeback": true, "writethrough": true}

func (u ConfigUpdate) validateTuning() error {
	switch {
	case u.CPUMode != "" && u.CPUMode != "host-passthrough" && u.CPUMode != "host-model":
		return invalid("Ungültiger CPU-Modus")
	case u.Clock != "" && u.Clock != "utc" && u.Clock != "localtime":
		return invalid("Ungültige Uhr (utc oder localtime)")
	}
	if t := u.Topology; t != nil {
		if t.Sockets < 1 || t.Sockets > MaxVCPUs || t.Threads < 1 || t.Threads > 8 {
			return invalid("Ungültige CPU-Topologie")
		}
		if u.VCPUs%(t.Sockets*t.Threads) != 0 {
			return invalid("Die vCPUs müssen durch Sockets × Threads teilbar sein")
		}
	}
	if u.ExtraNICs != nil {
		if len(*u.ExtraNICs) > MaxExtraNICs {
			return invalid("Höchstens 4 zusätzliche Netzwerkkarten")
		}
		seen := map[string]bool{}
		for _, n := range *u.ExtraNICs {
			if !ValidNetwork(n.Network) {
				return invalid("Ungültiger Netzwerkname")
			}
			if n.Model != "" && !nicModels[n.Model] {
				return invalid("Ungültiges Netzwerkkarten-Modell")
			}
			m := strings.ToLower(n.MAC)
			if m != "" && (!validMAC(m) || seen[m] || m == strings.ToLower(u.MAC)) {
				return invalid("Ungültige oder doppelte MAC-Adresse bei einer Netzwerkkarte")
			}
			if m != "" {
				seen[m] = true
			}
		}
	}
	if u.DiskOpts != nil {
		for _, d := range *u.DiskOpts {
			if d.Path == "" {
				return invalid("Ungültiger Festplattenpfad")
			}
			if !diskCaches[d.Cache] || (d.Discard != "ignore" && d.Discard != "unmap") {
				return invalid("Ungültige Laufwerksoption (Cache: default, none, writeback, writethrough; TRIM: ignore, unmap)")
			}
		}
	}
	return nil
}

type tuningXML struct {
	CPU struct {
		Mode     string `xml:"mode,attr"`
		Topology *struct {
			Sockets int `xml:"sockets,attr"`
			Threads int `xml:"threads,attr"`
		} `xml:"topology"`
	} `xml:"cpu"`
	Features struct {
		Hyperv *struct{} `xml:"hyperv"`
	} `xml:"features"`
	Clock struct {
		Offset string `xml:"offset,attr"`
	} `xml:"clock"`
	Devices struct {
		Disks []struct {
			Device string `xml:"device,attr"`
			Source struct {
				File string `xml:"file,attr"`
			} `xml:"source"`
			Driver struct {
				Cache   string `xml:"cache,attr"`
				Discard string `xml:"discard,attr"`
			} `xml:"driver"`
		} `xml:"disk"`
		Interfaces []struct {
			Type string `xml:"type,attr"`
			MAC  struct {
				Address string `xml:"address,attr"`
			} `xml:"mac"`
			Source struct {
				Network string `xml:"network,attr"`
				Bridge  string `xml:"bridge,attr"`
				Dev     string `xml:"dev,attr"`
			} `xml:"source"`
			Model struct {
				Type string `xml:"type,attr"`
			} `xml:"model"`
		} `xml:"interface"`
	} `xml:"devices"`
}

func parseTuning(data []byte) Tuning {
	t := Tuning{CPUMode: "other", Clock: "other", ExtraNICs: []NIC{}, DiskOpts: []DiskOpt{}}
	var x tuningXML
	if err := xml.NewDecoder(bytes.NewReader(data)).Decode(&x); err != nil {
		return t
	}
	if x.CPU.Mode == "host-passthrough" || x.CPU.Mode == "host-model" {
		t.CPUMode = x.CPU.Mode
	}
	if x.CPU.Topology != nil {
		t.Sockets, t.Threads = max(x.CPU.Topology.Sockets, 1), max(x.CPU.Topology.Threads, 1)
	}
	t.Hyperv = x.Features.Hyperv != nil
	if x.Clock.Offset == "utc" || x.Clock.Offset == "localtime" {
		t.Clock = x.Clock.Offset
	}
	for _, d := range x.Devices.Disks {
		if d.Device != "disk" || d.Source.File == "" {
			continue
		}
		o := DiskOpt{Path: d.Source.File, Cache: d.Driver.Cache, Discard: d.Driver.Discard}
		if o.Cache == "" {
			o.Cache = "default"
		}
		if o.Discard != "unmap" {
			o.Discard = "ignore"
		}
		t.DiskOpts = append(t.DiskOpts, o)
	}
	for i, n := range x.Devices.Interfaces {
		if i == 0 {
			continue
		}
		nic := NIC{Model: n.Model.Type, MAC: strings.ToLower(n.MAC.Address)}
		switch {
		case n.Type == "direct":
			nic.Network = DirectPrefix + n.Source.Dev
		case n.Type == "bridge" && n.Source.Bridge == "br0":
			nic.Network = BridgeNetwork
		case n.Type == "network":
			nic.Network = n.Source.Network
		}
		t.ExtraNICs = append(t.ExtraNICs, nic)
	}
	return t
}

var (
	reCPU        = regexp.MustCompile(`(?s)<cpu\b([^>]*?)(?:/>|>(.*?)</cpu>)`)
	reTopology   = regexp.MustCompile(`[ \t]*<topology\b[^>]*/>\n?`)
	reHyperv     = regexp.MustCompile(`(?s)[ \t]*<hyperv\b.*?</hyperv>\n?`)
	reClock      = regexp.MustCompile(`<clock offset='[^']*'`)
	reDiskDevice = regexp.MustCompile(`(?s)<disk\b[^>]*device='disk'[^>]*>.*?</disk>`)
	reDiskSource = regexp.MustCompile(`<source file='([^']*)'`)
	reDriver     = regexp.MustCompile(`<driver\b[^>]*/>`)
	reDrvAttr    = regexp.MustCompile(` (?:cache|discard)='[^']*'`)
	reIfaceFull  = regexp.MustCompile(`(?s)[ \t]*<interface\b.*?</interface>\n?`)
	reIfaceMAC   = regexp.MustCompile(`<mac address='([^']*)'/>`)
	reMigratable = regexp.MustCompile(` (?:migratable|check)='[^']*'`)
	reCPUMode    = regexp.MustCompile(`mode='[^']*'`)
)

// ifaceSource liefert Typ und <source>-Element für ein Netz.
func ifaceSource(network string) (string, string) {
	if ifc, ok := DirectInterface(network); ok {
		return "direct", fmt.Sprintf("<source dev='%s' mode='bridge'/>", esc(ifc))
	}
	if network == BridgeNetwork {
		return "bridge", "<source bridge='br0'/>"
	}
	return "network", fmt.Sprintf("<source network='%s'/>", esc(network))
}

func setIfaceNetwork(blk, network string) string {
	typ, src := ifaceSource(network)
	blk = reIfaceType.ReplaceAllString(blk, "<interface type='"+typ+"'")
	return reSource.ReplaceAllLiteralString(blk, src)
}

func (c Config) topologyAfter(u ConfigUpdate) (*Topology, error) {
	switch {
	case u.Topology != nil:
		return u.Topology, nil
	case c.Sockets > 0 && u.VCPUs != c.VCPUs:
		if u.VCPUs%(c.Sockets*c.Threads) != 0 {
			return nil, invalid("Die neue vCPU-Zahl passt nicht zur CPU-Topologie (Sockets × Threads) – Topologie mit anpassen")
		}
		return &Topology{Sockets: c.Sockets, Threads: c.Threads}, nil
	}
	return nil, nil
}

// applyTuning wendet die erweiterten Einstellungen auf die XML an (nach den Grundeinstellungen).
func applyTuning(out string, cur Config, u ConfigUpdate) (string, error) {
	topo, err := cur.topologyAfter(u)
	if err != nil {
		return "", err
	}
	modeChange := u.CPUMode != "" && u.CPUMode != cur.CPUMode
	if modeChange && cur.CPUMode == "other" {
		return "", invalid("Der CPU-Typ dieser VM lässt sich hier nicht ändern")
	}
	if modeChange || topo != nil {
		m := reCPU.FindStringSubmatchIndex(out)
		if m == nil {
			return "", invalid("Die VM hat keine CPU-Angabe")
		}
		attrs, body := out[m[2]:m[3]], ""
		if m[4] >= 0 {
			body = out[m[4]:m[5]]
		}
		if modeChange {
			attrs = reCPUMode.ReplaceAllLiteralString(attrs, "mode='"+u.CPUMode+"'")
			if u.CPUMode == "host-model" {
				attrs = reMigratable.ReplaceAllString(attrs, "")
			}
		}
		if topo != nil {
			body = reTopology.ReplaceAllString(body, "")
			body = fmt.Sprintf("\n    <topology sockets='%d' dies='1' cores='%d' threads='%d'/>", topo.Sockets, u.VCPUs/(topo.Sockets*topo.Threads), topo.Threads) + strings.TrimRight(body, " \t")
			if !strings.HasSuffix(body, "\n  ") {
				body += "\n  "
			}
		}
		repl := "<cpu" + attrs + "/>"
		if strings.TrimSpace(body) != "" {
			repl = "<cpu" + attrs + ">" + body + "</cpu>"
		}
		out = out[:m[0]] + repl + out[m[1]:]
	}

	if u.Hyperv != nil && *u.Hyperv != cur.Hyperv {
		if *u.Hyperv {
			if !strings.Contains(out, "  </features>") {
				return "", invalid("Die VM hat keinen Features-Abschnitt")
			}
			out = strings.Replace(out, "  </features>", HypervXML+"  </features>", 1)
		} else {
			out = reHyperv.ReplaceAllString(out, "")
		}
	}
	if u.Clock != "" && u.Clock != cur.Clock {
		if !reClock.MatchString(out) {
			return "", invalid("Die Uhr dieser VM lässt sich nicht ändern")
		}
		out = reClock.ReplaceAllLiteralString(out, "<clock offset='"+u.Clock+"'")
	}

	if u.DiskOpts != nil {
		want := map[string]DiskOpt{}
		for _, d := range *u.DiskOpts {
			want[d.Path] = d
		}
		have := map[string]bool{}
		for _, d := range cur.DiskOpts {
			have[d.Path] = true
		}
		for p := range want {
			if !have[p] {
				return "", invalid("Die Festplatte gehört nicht zu dieser VM")
			}
		}
		out = reDiskDevice.ReplaceAllStringFunc(out, func(blk string) string {
			sm := reDiskSource.FindStringSubmatch(blk)
			if sm == nil {
				return blk
			}
			o, ok := want[sm[1]]
			if !ok {
				return blk
			}
			return reDriver.ReplaceAllStringFunc(blk, func(drv string) string {
				drv = reDrvAttr.ReplaceAllString(drv, "")
				extra := ""
				if o.Cache != "default" {
					extra += " cache='" + o.Cache + "'"
				}
				if o.Discard == "unmap" {
					extra += " discard='unmap'"
				}
				return strings.TrimSuffix(drv, "/>") + extra + "/>"
			})
		})
	}

	if u.ExtraNICs != nil {
		want := map[string]NIC{}
		var fresh []NIC
		for _, n := range *u.ExtraNICs {
			n.MAC = strings.ToLower(n.MAC)
			exists := false
			for _, c := range cur.ExtraNICs {
				if n.MAC != "" && c.MAC == n.MAC {
					exists = true
				}
			}
			if exists {
				want[n.MAC] = n
			} else {
				fresh = append(fresh, n)
			}
		}
		idx := 0
		out = reIfaceFull.ReplaceAllStringFunc(out, func(blk string) string {
			idx++
			if idx == 1 {
				return blk
			}
			sm := reIfaceMAC.FindStringSubmatch(blk)
			if sm == nil {
				return ""
			}
			n, ok := want[strings.ToLower(sm[1])]
			if !ok {
				return ""
			}
			for _, c := range cur.ExtraNICs {
				if c.MAC == n.MAC && c.Network != n.Network {
					blk = setIfaceNetwork(blk, n.Network)
				}
			}
			return blk
		})
		var b strings.Builder
		for _, n := range fresh {
			model := n.Model
			if model == "" {
				model = "virtio"
			}
			typ, src := ifaceSource(n.Network)
			fmt.Fprintf(&b, "    <interface type='%s'>\n", typ)
			if n.MAC != "" {
				fmt.Fprintf(&b, "      <mac address='%s'/>\n", n.MAC)
			}
			fmt.Fprintf(&b, "      %s\n      <model type='%s'/>\n    </interface>\n", src, model)
		}
		if b.Len() > 0 {
			out = strings.Replace(out, "  </devices>", b.String()+"  </devices>", 1)
		}
	}
	return out, nil
}
