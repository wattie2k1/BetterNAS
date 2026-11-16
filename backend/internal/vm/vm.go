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
	// NetworkRe ist ein libvirt-Netzname.
	NetworkRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,39}$`)
)

// Actions sind die erlaubten Aktionen auf einer VM.
var Actions = map[string]bool{"start": true, "shutdown": true, "destroy": true, "reboot": true, "autostart": true, "noautostart": true}

// DiskPath ist die Disk-Datei einer VM. Der Name muss vorher mit NameRe geprüft sein.
func DiskPath(name string) string { return path.Join(DiskDir, name+".qcow2") }

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
	case !NetworkRe.MatchString(s.Network):
		return invalid("Ungültiger Netzwerkname")
	case s.VCPUs < MinVCPUs || s.VCPUs > MaxVCPUs:
		return invalid("vCPUs: 1 bis 64")
	case s.MemoryMiB < MinMemoryMiB || s.MemoryMiB > MaxMemoryMiB:
		return invalid("Arbeitsspeicher: 256 MiB bis 256 GiB")
	case s.DiskGiB < MinDiskGiB || s.DiskGiB > MaxDiskGiB:
		return invalid("Festplatte: 1 bis 65536 GiB")
	case s.ISO != "" && !ISORe.MatchString(s.ISO):
		return invalid("Ungültiger ISO-Dateiname")
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
}

// Status beschreibt, ob libvirt verfügbar ist.
type Status struct {
	Installed bool `json:"installed"`
	Active    bool `json:"active"`
	KVM       bool `json:"kvm"`
}
