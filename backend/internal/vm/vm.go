// Package vm enthält Namen, Grenzen, Domain-XML und Parser für die VM-Verwaltung (libvirt/KVM).
package vm

import (
	"path"
	"regexp"

	"github.com/betternas/betternas/internal/apierr"
)

const (
	// DiskDir und ISODir liegen bewusst nicht auf mergerfs (Spec §3).
	DiskDir = "/srv/vms/disks"
	ISODir  = "/srv/vms/iso"

	MinVCPUs, MaxVCPUs         = 1, 64
	MinMemoryMiB, MaxMemoryMiB = 256, 262144
	MinDiskGiB, MaxDiskGiB     = 1, 65536
)

var (
	// NameRe ist der erlaubte VM-Name (zugleich Dateiname der Disk).
	NameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)
	// ISORe erlaubt nur Dateinamen direkt unter ISODir.
	ISORe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}\.iso$`)
	// ImageRe erlaubt fertige Festplatten-Images direkt unter ISODir, die als primäre Disk dienen.
	ImageRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}\.qcow2$`)
	// DomainRe erlaubt auch Namen bestehender, nicht von BetterNAS angelegter VMs (ohne führendes `-`).
	DomainRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	// NetworkRe ist ein libvirt-Netzname.
	NetworkRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,39}$`)
	// InterfaceRe ist eine Netzwerkschnittstelle des Hosts (Linux: höchstens 15 Zeichen).
	InterfaceRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,14}$`)
	// SnapshotRe ist ein Snapshot-Name.
	SnapshotRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)
)

// DirectPrefix kennzeichnet ein LAN-Netz über macvtap auf einer Host-Schnittstelle („direct:eno1“).
const DirectPrefix = "direct:"

// DirectInterface liefert die Host-Schnittstelle eines „direct:“-Netzes.
func DirectInterface(network string) (string, bool) {
	if len(network) > len(DirectPrefix) && network[:len(DirectPrefix)] == DirectPrefix {
		return network[len(DirectPrefix):], true
	}
	return "", false
}

// BridgeNetwork ist das Netz über die optionale Host-Bridge br0 (siehe Netzwerk-Seite).
const BridgeNetwork = "bridge:br0"

// ValidNetwork prüft einen libvirt-Netznamen, ein „direct:<schnittstelle>“-Netz oder die Bridge br0.
func ValidNetwork(n string) bool {
	if n == BridgeNetwork {
		return true
	}
	if ifc, ok := DirectInterface(n); ok {
		return InterfaceRe.MatchString(ifc)
	}
	return NetworkRe.MatchString(n)
}

// Actions sind die erlaubten Aktionen auf einer VM.
var Actions = map[string]bool{"start": true, "shutdown": true, "destroy": true, "reboot": true, "autostart": true, "noautostart": true}

// DiskPath ist die Disk-Datei einer VM. Der Name muss vorher mit NameRe geprüft sein.
func DiskPath(name string) string { return path.Join(DiskDir, name+".qcow2") }

// IsImage meldet, ob die Auswahl ein Festplatten-Image (.qcow2) statt einer Installations-ISO ist.
func (s Spec) IsImage() bool { return ImageRe.MatchString(s.ISO) }

// ISOPath ist der Pfad einer ISO-Datei. Der Dateiname muss vorher mit ISORe geprüft sein.
func ISOPath(file string) string { return path.Join(ISODir, file) }

// Spec beschreibt eine neue VM (Eingabe des Assistenten).
type Spec struct {
	Name      string `json:"name"`
	OS        string `json:"os"`       // linux | windows
	Firmware  string `json:"firmware"` // uefi | bios
	Network   string `json:"network"`
	ISO       string `json:"iso"`
	DriverISO string `json:"driverIso"`
	VCPUs     int    `json:"vcpus"`
	MemoryMiB int    `json:"memoryMiB"`
	DiskGiB   int    `json:"diskGiB"`
	TPM       bool   `json:"tpm"`
}

func invalid(msg string) error { return apierr.New(apierr.InvalidParams, msg) }

// Validate prüft alle Felder gegen die festen Grenzen.
func (s Spec) Validate() error {
	switch {
	case !NameRe.MatchString(s.Name):
		return invalid("Ungültiger Name (erlaubt: Kleinbuchstaben, Ziffern, - und _, bis 40 Zeichen)")
	case s.OS != "linux" && s.OS != "windows":
		return invalid("Ungültiges Betriebssystem (linux oder windows)")
	case s.Firmware != "uefi" && s.Firmware != "bios":
		return invalid("Ungültige Firmware (uefi oder bios)")
	case !ValidNetwork(s.Network):
		return invalid("Ungültiger Netzwerkname")
	case s.VCPUs < MinVCPUs || s.VCPUs > MaxVCPUs:
		return invalid("vCPUs: 1 bis 64")
	case s.MemoryMiB < MinMemoryMiB || s.MemoryMiB > MaxMemoryMiB:
		return invalid("Arbeitsspeicher: 256 MiB bis 256 GiB")
	case s.DiskGiB < MinDiskGiB || s.DiskGiB > MaxDiskGiB:
		return invalid("Festplatte: 1 bis 65536 GiB")
	case s.ISO != "" && !ISORe.MatchString(s.ISO) && !ImageRe.MatchString(s.ISO):
		return invalid("Ungültiger ISO- oder Image-Dateiname")
	case s.DriverISO != "" && !ISORe.MatchString(s.DriverISO):
		return invalid("Ungültiger Dateiname der Treiber-ISO")
	case s.TPM && s.Firmware != "uefi":
		return invalid("TPM erfordert UEFI")
	}
	return nil
}

// VM ist eine virtuelle Maschine in der Liste.
type VM struct {
	Name      string   `json:"name"`
	UUID      string   `json:"uuid"`
	State     string   `json:"state"` // running | shutoff | paused | shuttingdown | crashed | other
	VCPUs     int      `json:"vcpus"`
	MemoryMiB int      `json:"memoryMiB"`
	Autostart bool     `json:"autostart"`
	Disks     []string `json:"disks"`
	// Icon ist die Version des VM-Icons (leer = keins); wird vom Manager ergänzt.
	Icon string `json:"icon,omitempty"`
}

// Status beschreibt, ob libvirt verfügbar ist.
type Status struct {
	Installed bool `json:"installed"`
	Active    bool `json:"active"`
	KVM       bool `json:"kvm"`
}

// Snapshot ist ein Snapshot einer VM.
type Snapshot struct {
	Name    string `json:"name"`
	Created string `json:"created"`
	State   string `json:"state"`
}
