package vm

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func cdrom(dev, file string) string {
	return fmt.Sprintf(`    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='%s'/>
      <target dev='%s' bus='sata'/>
      <readonly/>
    </disk>
`, esc(file), dev)
}

// BuildXML erzeugt die Domain-XML. Alle Werte stammen aus einer validierten Spec und werden escaped.
func BuildXML(s Spec, kvm bool) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	domType := "qemu"
	if kvm {
		domType = "kvm"
	}
	win := s.OS == "windows"
	diskBus, nicModel := "virtio", "virtio"
	if win && s.DriverISO == "" {
		diskBus, nicModel = "sata", "e1000e"
	}
	diskDev := "vda"
	if diskBus == "sata" {
		diskDev = "sda"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<domain type='%s'>\n  <name>%s</name>\n", domType, esc(s.Name))
	fmt.Fprintf(&b, "  <memory unit='MiB'>%d</memory>\n  <vcpu>%d</vcpu>\n", s.MemoryMiB, s.VCPUs)
	if s.Firmware == "uefi" {
		b.WriteString("  <os firmware='efi'>\n")
		if !win {
			// Ohne Angabe wählt libvirt OVMF mit Secure Boot und Microsoft-Schlüsseln; Linux-Installer
			// und fertige Images (Home Assistant OS) sind nicht signiert und scheiterten mit „Access Denied“.
			b.WriteString("    <firmware>\n      <feature enabled='no' name='enrolled-keys'/>\n      <feature enabled='no' name='secure-boot'/>\n    </firmware>\n")
		}
	} else {
		b.WriteString("  <os>\n")
	}
	b.WriteString("    <type arch='x86_64' machine='q35'>hvm</type>\n    <boot dev='cdrom'/>\n    <boot dev='hd'/>\n  </os>\n")
	b.WriteString("  <features>\n    <acpi/>\n    <apic/>\n")
	if win {
		b.WriteString("    <hyperv mode='custom'>\n      <relaxed state='on'/>\n      <vapic state='on'/>\n      <spinlocks state='on' retries='8191'/>\n    </hyperv>\n")
	}
	b.WriteString("  </features>\n")
	if kvm {
		b.WriteString("  <cpu mode='host-passthrough'/>\n")
	} else {
		b.WriteString("  <cpu mode='qemu64'/>\n")
	}
	clock := "utc"
	if win {
		clock = "localtime"
	}
	fmt.Fprintf(&b, "  <clock offset='%s'/>\n", clock)
	b.WriteString("  <on_poweroff>destroy</on_poweroff>\n  <on_reboot>restart</on_reboot>\n  <on_crash>destroy</on_crash>\n  <devices>\n")
	fmt.Fprintf(&b, "    <disk type='file' device='disk'>\n      <driver name='qemu' type='qcow2'/>\n      <source file='%s'/>\n      <target dev='%s' bus='%s'/>\n    </disk>\n",
		esc(DiskPath(s.Name)), diskDev, diskBus)
	if s.ISO != "" && !s.IsImage() {
		b.WriteString(cdrom("sdb", ISOPath(s.ISO)))
	}
	if s.DriverISO != "" {
		b.WriteString(cdrom("sdc", ISOPath(s.DriverISO)))
	}
	if s.Network == BridgeNetwork {
		fmt.Fprintf(&b, "    <interface type='bridge'>\n      <source bridge='br0'/>\n      <model type='%s'/>\n    </interface>\n", nicModel)
	} else if ifc, ok := DirectInterface(s.Network); ok {
		fmt.Fprintf(&b, "    <interface type='direct'>\n      <source dev='%s' mode='bridge'/>\n      <model type='%s'/>\n    </interface>\n", esc(ifc), nicModel)
	} else {
		fmt.Fprintf(&b, "    <interface type='network'>\n      <source network='%s'/>\n      <model type='%s'/>\n    </interface>\n", esc(s.Network), nicModel)
	}
	b.WriteString("    <graphics type='vnc' port='-1' autoport='yes' listen='127.0.0.1'>\n      <listen type='address' address='127.0.0.1'/>\n    </graphics>\n")
	b.WriteString("    <video>\n      <model type='virtio'/>\n    </video>\n    <input type='tablet' bus='usb'/>\n")
	if s.TPM {
		b.WriteString("    <tpm model='tpm-crb'>\n      <backend type='emulator' version='2.0'/>\n    </tpm>\n")
	}
	b.WriteString("  </devices>\n</domain>\n")
	return b.String(), nil
}
