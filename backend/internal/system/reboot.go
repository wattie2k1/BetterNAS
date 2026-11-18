package system

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// RebootStatus sagt, ob der Server neu gestartet werden muss (z. B. nach einem Kernel-Update).
type RebootStatus struct {
	Required        bool     `json:"required"`
	Reason          string   `json:"reason,omitempty"`
	RunningKernel   string   `json:"runningKernel,omitempty"`
	InstalledKernel string   `json:"installedKernel,omitempty"`
	Packages        []string `json:"packages"`
}

// RebootChecker erkennt einen nötigen Neustart. Die Pfade sind austauschbar (Tests).
type RebootChecker struct {
	// Running liefert die Version des laufenden Kernels.
	Running func() (string, error)
	// ModulesDir enthält je installiertem Kernel ein Verzeichnis (/lib/modules).
	ModulesDir string
	// FlagFile ist die Markierung von Debian/Ubuntu (/run/reboot-required); FlagFile+".pkgs" nennt die Pakete.
	FlagFile string
}

// NewRebootChecker liefert den Prüfer für das laufende System.
func NewRebootChecker() RebootChecker {
	return RebootChecker{
		Running: func() (string, error) {
			b, err := os.ReadFile("/proc/sys/kernel/osrelease")
			return strings.TrimSpace(string(b)), err
		},
		ModulesDir: "/lib/modules",
		FlagFile:   "/run/reboot-required",
	}
}

var digits = regexp.MustCompile(`\d+`)

// kernelKey zerlegt „6.12.43+deb13-amd64“ in alle Zahlen und die Variante (der Name mit Zahlen als „#“),
// damit z. B. „-rt-amd64“ nicht mit „-amd64“ verglichen wird.
func kernelKey(v string) (nums []int, flavor string) {
	locs := digits.FindAllStringIndex(v, -1)
	for _, l := range locs {
		n, _ := strconv.Atoi(v[l[0]:l[1]])
		nums = append(nums, n)
	}
	return nums, digits.ReplaceAllString(v, "#")
}

func numsLess(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// Check prüft die Markierungsdatei und vergleicht den laufenden mit dem neuesten installierten Kernel
// (nur derselben Variante). Ist nichts lesbar, wird kein Neustart gemeldet.
func (c RebootChecker) Check() RebootStatus {
	st := RebootStatus{Packages: []string{}}
	if c.Running != nil {
		st.RunningKernel, _ = c.Running()
	}
	if st.RunningKernel != "" && c.ModulesDir != "" {
		if entries, err := os.ReadDir(c.ModulesDir); err == nil {
			runNums, runFlavor := kernelKey(st.RunningKernel)
			var newer []string
			runningPresent := false
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				if e.Name() == st.RunningKernel {
					runningPresent = true
					continue
				}
				n, f := kernelKey(e.Name())
				if f == runFlavor && numsLess(runNums, n) {
					newer = append(newer, e.Name())
				}
			}
			sort.Slice(newer, func(i, j int) bool {
				a, _ := kernelKey(newer[i])
				b, _ := kernelKey(newer[j])
				return numsLess(a, b)
			})
			switch {
			case len(newer) > 0:
				st.Required = true
				st.InstalledKernel = newer[len(newer)-1]
				st.Reason = "Ein neuer Kernel (" + st.InstalledKernel + ") ist installiert, es läuft noch " + st.RunningKernel + "."
			case !runningPresent && len(entries) > 0:
				st.Required = true
				st.Reason = "Der laufende Kernel (" + st.RunningKernel + ") wurde durch ein Update ersetzt."
			}
		}
	}
	if c.FlagFile != "" {
		if _, err := os.Stat(c.FlagFile); err == nil {
			st.Required = true
			if st.Reason == "" {
				st.Reason = "Ein Update verlangt einen Neustart."
			}
			if b, err := os.ReadFile(c.FlagFile + ".pkgs"); err == nil {
				seen := map[string]bool{}
				for _, p := range strings.Fields(string(b)) {
					if !seen[p] {
						seen[p] = true
						st.Packages = append(st.Packages, p)
					}
				}
			}
		}
	}
	return st
}
