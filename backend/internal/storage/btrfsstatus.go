package storage

import (
	"regexp"
	"strconv"
	"strings"
)

// BtrfsErrors sind die Fehlerzähler einer Platte (btrfs device stats).
type BtrfsErrors struct {
	Write      uint64 `json:"write"`
	Read       uint64 `json:"read"`
	Flush      uint64 `json:"flush"`
	Corruption uint64 `json:"corruption"`
	Generation uint64 `json:"generation"`
}

// Total ist die Summe aller Fehlerzähler.
func (e BtrfsErrors) Total() uint64 { return e.Write + e.Read + e.Flush + e.Corruption + e.Generation }

// BtrfsDevice ist ein Mitglied des Dateisystems.
type BtrfsDevice struct {
	DevID     int         `json:"devId"`
	Path      string      `json:"path,omitempty"`   // z. B. /dev/sda1; leer, wenn die Platte fehlt
	DiskID    string      `json:"diskId,omitempty"` // BetterNAS-Kennung (vom Manager ergänzt)
	SizeBytes uint64      `json:"sizeBytes"`
	UsedBytes uint64      `json:"usedBytes"`
	Missing   bool        `json:"missing"`
	Errors    BtrfsErrors `json:"errors"`
}

// BtrfsScrub ist der Stand der letzten oder laufenden Prüfung (btrfs scrub).
type BtrfsScrub struct {
	Running bool `json:"running"`
	// Result: "" (nie geprüft) | running | finished | aborted | interrupted
	Result        string `json:"result,omitempty"`
	Started       string `json:"started,omitempty"` // Zeitangabe wie von btrfs gemeldet
	Duration      string `json:"duration,omitempty"`
	Corrected     uint64 `json:"corrected"`
	Uncorrectable uint64 `json:"uncorrectable"`
	// Errors ist die Summe aus Lese-, Prüfsummen- und Verifikationsfehlern.
	Errors uint64 `json:"errors"`
}

// BtrfsStatus fasst Zustand, Belegung, Platten und Prüfung eines btrfs-Pools zusammen.
type BtrfsStatus struct {
	Mounted bool          `json:"mounted"`
	Devices []BtrfsDevice `json:"devices"`
	// Missing ist die Zahl fehlender Platten (Pool unvollständig).
	Missing int `json:"missing"`
	// Degraded: Platten fehlen.
	Degraded bool `json:"degraded"`
	// DataProfile / MetadataProfile sind die tatsächlich angelegten Profile (z. B. RAID1); nur bei eingehängtem Pool.
	DataProfile     string `json:"dataProfile,omitempty"`
	MetadataProfile string `json:"metadataProfile,omitempty"`
	RawBytes        uint64 `json:"rawBytes"`    // Summe der Plattengrößen
	UsableBytes     uint64 `json:"usableBytes"` // nutzbar nach Abzug der Redundanz
	FreeBytes       uint64 `json:"freeBytes"`   // geschätzt frei
	// Errors ist die Summe aller Fehlerzähler aller Platten.
	Errors uint64      `json:"errors"`
	Scrub  *BtrfsScrub `json:"scrub,omitempty"`
}

var (
	showDevRe   = regexp.MustCompile(`^\s*devid\s+(\d+)\s+size\s+(\d+)\s+used\s+(\d+)\s+path\s+(\S.*?)\s*$`)
	showTotalRe = regexp.MustCompile(`Total devices\s+(\d+)`)
	statsRe     = regexp.MustCompile(`^\[(.+)\]\.(write_io_errs|read_io_errs|flush_io_errs|corruption_errs|generation_errs)\s+(\d+)\s*$`)
	devidKeyRe  = regexp.MustCompile(`^devid:(\d+)$`)
	profileRe   = regexp.MustCompile(`^(Data|Metadata),([A-Za-z0-9]+):`)
	keyValueRe  = regexp.MustCompile(`^\s*([a-z_]+):\s+(\d+)\s*$`)
)

// ParseBtrfsShow liest „btrfs filesystem show --raw <uuid>“: Mitglieder und fehlende Platten.
func ParseBtrfsShow(out string) (devs []BtrfsDevice, missing int) {
	total, someMissing := 0, false
	for _, line := range strings.Split(out, "\n") {
		if m := showTotalRe.FindStringSubmatch(line); m != nil {
			total, _ = strconv.Atoi(m[1])
		}
		if strings.Contains(line, "Some devices missing") {
			someMissing = true
		}
		m := showDevRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		id, _ := strconv.Atoi(m[1])
		size, _ := strconv.ParseUint(m[2], 10, 64)
		used, _ := strconv.ParseUint(m[3], 10, 64)
		d := BtrfsDevice{DevID: id, SizeBytes: size, UsedBytes: used, Path: m[4]}
		if strings.Contains(strings.ToLower(m[4]), "missing") {
			d.Path, d.Missing = "", true
		}
		devs = append(devs, d)
	}
	for _, d := range devs {
		if d.Missing {
			missing++
		}
	}
	// Fehlt eine Platte ganz in der Liste, zählt die Differenz zu „Total devices“.
	if gap := total - len(devs); gap > 0 {
		missing += gap
	} else if missing == 0 && someMissing {
		missing = 1
	}
	return devs, missing
}

// ParseBtrfsStats liest „btrfs device stats“: Fehlerzähler je Platte (Schlüssel: Pfad oder „devid:N“).
func ParseBtrfsStats(out string) map[string]BtrfsErrors {
	res := map[string]BtrfsErrors{}
	for _, line := range strings.Split(out, "\n") {
		m := statsRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		n, _ := strconv.ParseUint(m[3], 10, 64)
		e := res[m[1]]
		switch m[2] {
		case "write_io_errs":
			e.Write = n
		case "read_io_errs":
			e.Read = n
		case "flush_io_errs":
			e.Flush = n
		case "corruption_errs":
			e.Corruption = n
		case "generation_errs":
			e.Generation = n
		}
		res[m[1]] = e
	}
	return res
}

// BtrfsUsage ist die Auswertung von „btrfs filesystem usage -b“.
type BtrfsUsage struct {
	DeviceBytes     uint64
	FreeBytes       uint64
	DataRatio       float64
	DataProfile     string
	MetadataProfile string
}

// ParseBtrfsUsage liest „btrfs filesystem usage -b <mnt>“.
func ParseBtrfsUsage(out string) BtrfsUsage {
	u := BtrfsUsage{DataRatio: 1}
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if m := profileRe.FindStringSubmatch(t); m != nil {
			if m[1] == "Data" && u.DataProfile == "" {
				u.DataProfile = m[2]
			}
			if m[1] == "Metadata" && u.MetadataProfile == "" {
				u.MetadataProfile = m[2]
			}
			continue
		}
		key, val, ok := strings.Cut(t, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(val)
		if len(fields) == 0 {
			continue
		}
		switch key {
		case "Device size":
			u.DeviceBytes, _ = strconv.ParseUint(fields[0], 10, 64)
		case "Free (estimated)":
			u.FreeBytes, _ = strconv.ParseUint(fields[0], 10, 64)
		case "Data ratio":
			if r, err := strconv.ParseFloat(fields[0], 64); err == nil && r > 0 {
				u.DataRatio = r
			}
		}
	}
	return u
}

// ParseBtrfsScrub liest „btrfs scrub status -R <mnt>“. nil: nie geprüft.
func ParseBtrfsScrub(out string) *BtrfsScrub {
	if strings.TrimSpace(out) == "" || strings.Contains(out, "no stats available") {
		return nil
	}
	s := &BtrfsScrub{}
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "Status:"):
			s.Result = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(t, "Status:")))
		case strings.HasPrefix(t, "Scrub started:"):
			s.Started = strings.TrimSpace(strings.TrimPrefix(t, "Scrub started:"))
		case strings.HasPrefix(t, "Duration:"):
			s.Duration = strings.TrimSpace(strings.TrimPrefix(t, "Duration:"))
		}
		if m := keyValueRe.FindStringSubmatch(line); m != nil {
			n, _ := strconv.ParseUint(m[2], 10, 64)
			switch m[1] {
			case "read_errors", "csum_errors", "verify_errors", "super_errors":
				s.Errors += n
			case "uncorrectable_errors":
				s.Uncorrectable = n
			case "corrected_errors":
				s.Corrected = n
			}
		}
	}
	s.Running = s.Result == "running"
	if s.Result == "" {
		return nil
	}
	return s
}

// AssembleBtrfsStatus fügt die Teilergebnisse zusammen (Platten mit Fehlerzählern, Kapazität, Zustand).
func AssembleBtrfsStatus(devs []BtrfsDevice, missing int, stats map[string]BtrfsErrors, usage BtrfsUsage, mounted bool, scrub *BtrfsScrub) BtrfsStatus {
	st := BtrfsStatus{Mounted: mounted, Devices: devs, Missing: missing, Degraded: missing > 0, Scrub: scrub}
	if st.Devices == nil {
		st.Devices = []BtrfsDevice{}
	}
	for i, d := range st.Devices {
		e, ok := stats[d.Path]
		if !ok {
			e = stats["devid:"+strconv.Itoa(d.DevID)]
		}
		st.Devices[i].Errors = e
		st.Errors += e.Total()
		st.RawBytes += d.SizeBytes
	}
	if mounted {
		st.DataProfile, st.MetadataProfile = usage.DataProfile, usage.MetadataProfile
		st.FreeBytes = usage.FreeBytes
		if usage.DeviceBytes > 0 {
			st.UsableBytes = uint64(float64(usage.DeviceBytes) / usage.DataRatio)
		}
	}
	return st
}

// BtrfsDevIDKey erkennt den Schlüssel „devid:N“ aus device stats.
func BtrfsDevIDKey(k string) (int, bool) {
	m := devidKeyRe.FindStringSubmatch(k)
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	return n, true
}
