package storage

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// IOCounter sind die aufsummierten Zähler eines Blockgeräts seit dem Start (aus /proc/diskstats).
// Die Oberfläche bildet aus zwei Messungen die Raten.
type IOCounter struct {
	ReadBytes  uint64 `json:"readBytes"`
	WriteBytes uint64 `json:"writeBytes"`
	BusyMs     uint64 `json:"busyMs"` // Zeit, in der das Gerät Anfragen bearbeitet hat
}

// IOStats ist eine Messung aller Blockgeräte zu einem Zeitpunkt.
type IOStats struct {
	At      time.Time            `json:"at"`
	Devices map[string]IOCounter `json:"devices"` // Gerätename ohne /dev/, z. B. "sda"
}

const sectorBytes = 512 // /proc/diskstats zählt immer in 512-Byte-Sektoren

// parseDiskstats liest /proc/diskstats; virtuelle Geräte (loop, ram, zram) werden übersprungen.
func parseDiskstats(r io.Reader) map[string]IOCounter {
	out := map[string]IOCounter{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 13 {
			continue
		}
		name := f[2]
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "zram") {
			continue
		}
		rd, e1 := strconv.ParseUint(f[5], 10, 64)
		wr, e2 := strconv.ParseUint(f[9], 10, 64)
		busy, e3 := strconv.ParseUint(f[12], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		out[name] = IOCounter{ReadBytes: rd * sectorBytes, WriteBytes: wr * sectorBytes, BusyMs: busy}
	}
	return out
}

// DiskIO liefert die aktuellen Zähler aller Blockgeräte. Das Lesen weckt keine Platte.
func (m *Manager) DiskIO() (IOStats, error) {
	f, err := os.Open("/proc/diskstats")
	if err != nil {
		return IOStats{}, err
	}
	defer f.Close()
	return IOStats{At: m.now().UTC(), Devices: parseDiskstats(f)}, nil
}
