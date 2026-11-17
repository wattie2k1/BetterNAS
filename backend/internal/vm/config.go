package vm

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
)

// NvramDir ist das Verzeichnis, aus dem der Agent UEFI-Variablen-Dateien entfernen darf.
const guestAgentName = "org.qemu.guest_agent.0"

// GuestAgentChannel ist das Gerät, über das libvirt mit dem Guest-Agent in der VM spricht.
const GuestAgentChannel = "    <channel type='unix'>\n      <target type='virtio' name='" + guestAgentName + "'/>\n    </channel>\n"

const NvramDir = "/var/lib/libvirt/qemu/nvram/"

// Config ist die änderbare Konfiguration einer VM (aus der inaktiven Domain-XML gelesen).
type Config struct {
	Name       string   `json:"name"`
	State      string   `json:"state"`
	VCPUs      int      `json:"vcpus"`
	MemoryMiB  int      `json:"memoryMiB"`
	Autostart  bool     `json:"autostart"`
	Firmware   string   `json:"firmware"` // uefi | bios
	SecureBoot bool     `json:"secureBoot"`
	TPM        bool     `json:"tpm"`
	GuestAgent bool     `json:"guestAgent"` // Kanal für den QEMU-Guest-Agent (Herunterfahren, IP-Anzeige)
	Network    string   `json:"network"`    // „default“, „direct:eno1“ oder „bridge:br0“
	HasCDROM   bool     `json:"hasCdrom"`   // mindestens ein CD-Laufwerk
	Media      string   `json:"media"`      // Dateiname der eingelegten ISO ("" = leer)
	Disks      []string `json:"disks"`
}

// ConfigUpdate sind die änderbaren Werte; die VM muss dafür ausgeschaltet sein.
type ConfigUpdate struct {
	VCPUs      int    `json:"vcpus"`
	MemoryMiB  int    `json:"memoryMiB"`
	SecureBoot bool   `json:"secureBoot"`
	Network    string `json:"network"`
	Media      string `json:"media"` // "" = Laufwerk leer
	GuestAgent bool   `json:"guestAgent"`
}

// Validate prüft alle Felder gegen die festen Grenzen.
func (u ConfigUpdate) Validate() error {
	switch {
	case u.VCPUs < MinVCPUs || u.VCPUs > MaxVCPUs:
		return invalid("vCPUs: 1 bis 64")
	case u.MemoryMiB < MinMemoryMiB || u.MemoryMiB > MaxMemoryMiB:
		return invalid("Arbeitsspeicher: 256 MiB bis 256 GiB")
	case !ValidNetwork(u.Network):
		return invalid("Ungültiger Netzwerkname")
	case u.Media != "" && !ISORe.MatchString(u.Media):
		return invalid("Ungültiger ISO-Dateiname")
	}
	return nil
}

type domXML struct {
	Name   string `xml:"name"`
	Memory struct {
		Unit string `xml:"unit,attr"`
		V    int    `xml:",chardata"`
	} `xml:"memory"`
	VCPU int `xml:"vcpu"`
	OS   struct {
		Firmware string `xml:"firmware,attr"`
		FW       struct {
			Features []struct {
				Name    string `xml:"name,attr"`
				Enabled string `xml:"enabled,attr"`
			} `xml:"feature"`
		} `xml:"firmware"`
		Loader struct {
			Type   string `xml:"type,attr"`
			Secure string `xml:"secure,attr"`
		} `xml:"loader"`
	} `xml:"os"`
	Devices struct {
		Disks []struct {
			Device string `xml:"device,attr"`
			Source struct {
				File string `xml:"file,attr"`
			} `xml:"source"`
		} `xml:"disk"`
		Interfaces []struct {
			Type   string `xml:"type,attr"`
			Source struct {
				Network string `xml:"network,attr"`
				Bridge  string `xml:"bridge,attr"`
				Dev     string `xml:"dev,attr"`
			} `xml:"source"`
		} `xml:"interface"`
		TPM      *struct{} `xml:"tpm"`
		Channels []struct {
			Target struct {
				Name string `xml:"name,attr"`
			} `xml:"target"`
		} `xml:"channel"`
	} `xml:"devices"`
}

var mibPerUnit = map[string]float64{"": 1.0 / 1024, "b": 1.0 / 1048576, "bytes": 1.0 / 1048576, "KiB": 1.0 / 1024, "k": 1.0 / 1024, "KB": 1000.0 / 1048576, "MiB": 1, "M": 1, "MB": 1e6 / 1048576, "GiB": 1024, "G": 1024, "GB": 1e9 / 1048576}

// ParseConfig liest die Konfiguration aus einer Domain-XML (`virsh dumpxml --inactive`).
func ParseConfig(xmlData []byte) (Config, error) {
	var d domXML
	if err := xml.NewDecoder(bytes.NewReader(xmlData)).Decode(&d); err != nil {
		return Config{}, err
	}
	c := Config{Name: d.Name, VCPUs: d.VCPU, Firmware: "bios", Disks: []string{}}
	c.MemoryMiB = int(float64(d.Memory.V)*mibPerUnit[d.Memory.Unit] + 0.5)
	if d.OS.Firmware == "efi" || d.OS.Loader.Type == "pflash" {
		c.Firmware = "uefi"
	}
	c.SecureBoot = d.OS.Loader.Secure == "yes"
	for _, f := range d.OS.FW.Features {
		if f.Name == "secure-boot" {
			c.SecureBoot = f.Enabled == "yes"
		}
	}
	c.TPM = d.Devices.TPM != nil
	for _, ch := range d.Devices.Channels {
		if ch.Target.Name == guestAgentName {
			c.GuestAgent = true
		}
	}
	for _, disk := range d.Devices.Disks {
		switch disk.Device {
		case "disk":
			if disk.Source.File != "" {
				c.Disks = append(c.Disks, disk.Source.File)
			}
		case "cdrom":
			if !c.HasCDROM {
				c.HasCDROM = true
				if f := disk.Source.File; strings.HasPrefix(f, ISODir+"/") {
					c.Media = strings.TrimPrefix(f, ISODir+"/")
				}
			}
		}
	}
	if len(d.Devices.Interfaces) > 0 {
		i := d.Devices.Interfaces[0]
		switch {
		case i.Type == "direct":
			c.Network = DirectPrefix + i.Source.Dev
		case i.Type == "bridge" && i.Source.Bridge == "br0":
			c.Network = BridgeNetwork
		case i.Type == "network":
			c.Network = i.Source.Network
		}
	}
	return c, nil
}

var (
	reMemory     = regexp.MustCompile(`<memory\b[^>]*>\d+</memory>`)
	reCurMemory  = regexp.MustCompile(`<currentMemory\b[^>]*>\d+</currentMemory>`)
	reVCPU       = regexp.MustCompile(`<vcpu\b([^>]*)>\d+</vcpu>`)
	reFirmware   = regexp.MustCompile(`(?s)[ \t]*<firmware>.*?</firmware>\n?`)
	reLoader     = regexp.MustCompile(`[ \t]*(?:<loader\b[^>]*/>|<loader\b[^>]*>[^<]*</loader>)\n?`)
	reNvram      = regexp.MustCompile(`[ \t]*(?:<nvram\b[^>]*/>|<nvram\b[^>]*>[^<]*</nvram>)\n?`)
	reNvramPath  = regexp.MustCompile(`<nvram\b[^>]*>([^<]+)</nvram>`)
	reInterface  = regexp.MustCompile(`(?s)<interface\b.*?</interface>`)
	reIfaceType  = regexp.MustCompile(`<interface type='[^']*'`)
	reSource     = regexp.MustCompile(`<source\b[^>]*/>`)
	reCDROM      = regexp.MustCompile(`(?s)<disk\b[^>]*device='cdrom'[^>]*>.*?</disk>`)
	reCDSource   = regexp.MustCompile(`[ \t]*<source\b[^>]*/>\n?`)
	reCDDriver   = regexp.MustCompile(`<driver\b[^>]*/>`)
	reOSFirmware = regexp.MustCompile(`<os firmware='efi'>`)
	reChannel    = regexp.MustCompile(`(?s)[ \t]*<channel\b.*?</channel>\n?`)
)

func secureBootFirmware(on bool) string {
	v := "no"
	if on {
		v = "yes"
	}
	return fmt.Sprintf("\n    <firmware>\n      <feature enabled='%[1]s' name='enrolled-keys'/>\n      <feature enabled='%[1]s' name='secure-boot'/>\n    </firmware>", v)
}

// ApplyConfig ändert die Domain-XML. Es liefert die neue XML und – wenn sich Secure Boot ändert –
// den Pfad der alten UEFI-Variablen-Datei, die nach dem Definieren entfernt werden muss
// (sonst bliebe der alte Schlüsselstand erhalten).
func ApplyConfig(xmlStr string, u ConfigUpdate) (string, string, error) {
	if err := u.Validate(); err != nil {
		return "", "", err
	}
	cur, err := ParseConfig([]byte(xmlStr))
	if err != nil {
		return "", "", err
	}
	out := xmlStr
	nvram := ""

	out = reMemory.ReplaceAllString(out, fmt.Sprintf("<memory unit='KiB'>%d</memory>", u.MemoryMiB*1024))
	out = reCurMemory.ReplaceAllString(out, fmt.Sprintf("<currentMemory unit='KiB'>%d</currentMemory>", u.MemoryMiB*1024))
	out = reVCPU.ReplaceAllString(out, fmt.Sprintf("<vcpu${1}>%d</vcpu>", u.VCPUs))

	if u.SecureBoot != cur.SecureBoot {
		if cur.Firmware != "uefi" || !reOSFirmware.MatchString(out) {
			return "", "", invalid("Secure Boot lässt sich bei dieser VM nicht ändern (nur UEFI mit automatischer Firmware-Auswahl)")
		}
		if m := reNvramPath.FindStringSubmatch(out); m != nil && strings.HasPrefix(m[1], NvramDir) && !strings.Contains(m[1], "..") {
			nvram = m[1]
		}
		out = reFirmware.ReplaceAllString(out, "")
		out = reLoader.ReplaceAllString(out, "")
		out = reNvram.ReplaceAllString(out, "")
		out = strings.Replace(out, "</type>", "</type>"+secureBootFirmware(u.SecureBoot), 1)
	}

	if u.Network != cur.Network {
		var typ, src string
		if ifc, ok := DirectInterface(u.Network); ok {
			typ, src = "direct", fmt.Sprintf("<source dev='%s' mode='bridge'/>", esc(ifc))
		} else if u.Network == BridgeNetwork {
			typ, src = "bridge", "<source bridge='br0'/>"
		} else {
			typ, src = "network", fmt.Sprintf("<source network='%s'/>", esc(u.Network))
		}
		if !reInterface.MatchString(out) {
			return "", "", invalid("Die VM hat keine Netzwerkschnittstelle")
		}
		done := false
		out = reInterface.ReplaceAllStringFunc(out, func(blk string) string {
			if done {
				return blk
			}
			done = true
			blk = reIfaceType.ReplaceAllString(blk, "<interface type='"+typ+"'")
			return reSource.ReplaceAllLiteralString(blk, src)
		})
	}

	if u.Media != cur.Media || (u.Media != "" && !cur.HasCDROM) {
		switch {
		case cur.HasCDROM:
			done := false
			out = reCDROM.ReplaceAllStringFunc(out, func(blk string) string {
				if done {
					return blk
				}
				done = true
				blk = reCDSource.ReplaceAllString(blk, "")
				if u.Media == "" {
					return blk
				}
				src := fmt.Sprintf("\n      <source file='%s'/>", esc(ISOPath(u.Media)))
				if reCDDriver.MatchString(blk) {
					return reCDDriver.ReplaceAllStringFunc(blk, func(d string) string { return d + src })
				}
				return strings.Replace(blk, ">", ">"+src, 1)
			})
		case u.Media != "":
			if strings.Contains(out, "dev='sdb'") {
				return "", "", invalid("Das Laufwerk sdb ist bereits belegt")
			}
			out = strings.Replace(out, "  </devices>", cdrom("sdb", ISOPath(u.Media))+"  </devices>", 1)
		}
	}
	if u.GuestAgent != cur.GuestAgent {
		if u.GuestAgent {
			out = strings.Replace(out, "  </devices>", GuestAgentChannel+"  </devices>", 1)
		} else {
			out = reChannel.ReplaceAllStringFunc(out, func(blk string) string {
				if strings.Contains(blk, guestAgentName) {
					return ""
				}
				return blk
			})
		}
	}
	return out, nvram, nil
}
