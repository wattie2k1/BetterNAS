package vm

import "strings"

// LiveUpdate sind Änderungen an einer laufenden (oder pausierten) VM. Fehlt ein Feld (null), bleibt
// der Wert unverändert. Die Werte werden zugleich in die gespeicherte Konfiguration übernommen.
type LiveUpdate struct {
	MemoryMiB *int         `json:"memoryMiB"` // höchstens MaxMemoryMiB
	VCPUs     *int         `json:"vcpus"`     // höchstens MaxVCPUs
	USB       *[]USBDevice `json:"usb"`
	Media     *string      `json:"media"` // "" = Medium auswerfen
}

// Validate prüft alle Felder gegen die festen Grenzen (die VM-spezifischen Maxima prüft der Agent).
func (u LiveUpdate) Validate() error {
	switch {
	case u.MemoryMiB != nil && (*u.MemoryMiB < MinMemoryMiB || *u.MemoryMiB > MaxMemoryMiB):
		return invalid("Arbeitsspeicher: 256 MiB bis 256 GiB")
	case u.VCPUs != nil && (*u.VCPUs < MinVCPUs || *u.VCPUs > MaxVCPUs):
		return invalid("vCPUs: 1 bis 64")
	case u.USB != nil && !validUSB(*u.USB):
		return invalid("Ungültige USB-Geräteliste (höchstens 8 Geräte, keine Doppelten)")
	case u.Media != nil && *u.Media != "" && !ISORe.MatchString(*u.Media):
		return invalid("Ungültiger ISO-Dateiname")
	}
	return nil
}

// Empty meldet, dass nichts zu ändern ist.
func (u LiveUpdate) Empty() bool {
	return u.MemoryMiB == nil && u.VCPUs == nil && u.USB == nil && u.Media == nil
}

// CDROMTarget liefert den Gerätenamen (z. B. „sdb“) des ersten CD-Laufwerks, "" wenn es keins gibt.
func CDROMTarget(xmlStr string) string {
	for _, blk := range reDiskAny.FindAllString(xmlStr, -1) {
		if !strings.Contains(blk, "device='cdrom'") {
			continue
		}
		if m := reDiskTarget.FindStringSubmatch(blk); m != nil {
			return m[1]
		}
	}
	return ""
}

// USBHostdevXML ist das Geräte-XML zum Zuweisen/Entziehen eines USB-Geräts im Betrieb.
func USBHostdevXML(d USBDevice) string { return usbHostdev(d) }

// DiffUSB liefert, welche Geräte hinzukommen und welche wegfallen.
func DiffUSB(cur, want []USBDevice) (add, remove []USBDevice) {
	key := func(d USBDevice) string { return d.Vendor + ":" + d.Product }
	have, wanted := map[string]bool{}, map[string]bool{}
	for _, d := range cur {
		have[key(d)] = true
	}
	for _, d := range want {
		wanted[key(d)] = true
		if !have[key(d)] {
			add = append(add, d)
		}
	}
	for _, d := range cur {
		if !wanted[key(d)] {
			remove = append(remove, d)
		}
	}
	return add, remove
}
