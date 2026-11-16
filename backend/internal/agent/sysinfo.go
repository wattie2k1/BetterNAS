package agent

import (
	"bufio"
	"bytes"
	"runtime"
	"strconv"
	"strings"
)

// Info sind Basisdaten des Systems.
type Info struct {
	Hostname      string `json:"hostname"`
	Kernel        string `json:"kernel"`
	OS            string `json:"os"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
	CPUCount      int    `json:"cpuCount"`
	CPUModel      string `json:"cpuModel"`
	// CPUTotalTicks und CPUIdleTicks sind die kumulierten Zähler aus /proc/stat (Idle inkl. iowait);
	// die Auslastung ergibt sich aus der Differenz zweier Abfragen.
	CPUTotalTicks     uint64     `json:"cpuTotalTicks"`
	CPUIdleTicks      uint64     `json:"cpuIdleTicks"`
	Load              [3]float64 `json:"load"` // 1, 5 und 15 Minuten
	MemTotalBytes     uint64     `json:"memTotalBytes"`
	MemAvailableBytes uint64     `json:"memAvailableBytes"`
}

func (d Deps) readTrim(p string) string {
	b, err := d.FS.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// SystemInfo liest Systemdaten aus /proc und /etc/os-release.
func SystemInfo(d Deps) (Info, error) {
	info := Info{
		Hostname: d.readTrim("/proc/sys/kernel/hostname"),
		Kernel:   d.readTrim("/proc/sys/kernel/osrelease"),
		CPUCount: runtime.NumCPU(),
	}
	if f := strings.Fields(d.readTrim("/proc/uptime")); len(f) > 0 {
		if v, err := strconv.ParseFloat(f[0], 64); err == nil {
			info.UptimeSeconds = int64(v)
		}
	}
	info.CPUTotalTicks, info.CPUIdleTicks = parseCPUStat(d.readTrim("/proc/stat"))
	if f := strings.Fields(d.readTrim("/proc/loadavg")); len(f) >= 3 {
		for i := range info.Load {
			info.Load[i], _ = strconv.ParseFloat(f[i], 64)
		}
	}
	if b, err := d.FS.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "model name" {
				info.CPUModel = strings.TrimSpace(v)
				break
			}
		}
	}
	if mem, err := d.FS.ReadFile("/proc/meminfo"); err == nil {
		sc := bufio.NewScanner(bytes.NewReader(mem))
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) < 2 {
				continue
			}
			kb, _ := strconv.ParseUint(f[1], 10, 64)
			switch f[0] {
			case "MemTotal:":
				info.MemTotalBytes = kb * 1024
			case "MemAvailable:":
				info.MemAvailableBytes = kb * 1024
			}
		}
	}
	if osr, err := d.FS.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(osr), "\n") {
			if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
				info.OS = strings.Trim(v, `"`)
			}
		}
	}
	return info, nil
}

// parseCPUStat summiert die erste Zeile („cpu …“) von /proc/stat. Idle zählt idle + iowait;
// guest-Zeiten sind in user/nice bereits enthalten und werden nicht doppelt gezählt.
func parseCPUStat(stat string) (total, idle uint64) {
	line, _, _ := strings.Cut(stat, "\n")
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return 0, 0
	}
	for i, s := range f[1:] {
		v, _ := strconv.ParseUint(s, 10, 64)
		if i >= 8 { // guest, guest_nice
			break
		}
		total += v
		if i == 3 || i == 4 {
			idle += v
		}
	}
	return total, idle
}
