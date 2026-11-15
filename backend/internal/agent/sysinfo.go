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
	Hostname          string `json:"hostname"`
	Kernel            string `json:"kernel"`
	OS                string `json:"os"`
	UptimeSeconds     int64  `json:"uptimeSeconds"`
	CPUCount          int    `json:"cpuCount"`
	MemTotalBytes     uint64 `json:"memTotalBytes"`
	MemAvailableBytes uint64 `json:"memAvailableBytes"`
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
