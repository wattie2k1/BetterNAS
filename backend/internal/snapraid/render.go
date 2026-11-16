package snapraid

import (
	"fmt"
	"strings"
)

// Render erzeugt alle Dateien (absoluter Pfad → Inhalt): conf, settings und zwei Timer je Pool.
func Render(s State) (map[string][]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, p := range s.Pools {
		out[ConfPath(p.Pool)] = []byte(renderConf(p))
		out[SettingsPath(p.Pool)] = []byte(fmt.Sprintf("%s\ndelete_threshold=%d\nscrub_percent=%d\nscrub_older_days=%d\n",
			ManagedHeader, p.DeleteThreshold, p.ScrubPercent, p.ScrubOlderDays))
		out[TimerPath("sync", p.Pool)] = []byte(renderTimer("sync", p.Pool, p.SyncSchedule))
		out[TimerPath("scrub", p.Pool)] = []byte(renderTimer("scrub", p.Pool, p.ScrubSchedule))
	}
	return out, nil
}

func renderConf(p PoolConfig) string {
	var b strings.Builder
	b.WriteString(ManagedHeader + "\n")
	for i, id := range p.Parity {
		if i == 0 {
			fmt.Fprintf(&b, "parity %s/snapraid.parity\n", ParityPath(id))
		} else {
			fmt.Fprintf(&b, "%d-parity %s/snapraid.%d-parity\n", i+1, ParityPath(id), i+1)
		}
	}
	// Content-Dateien: Systemplatte plus mindestens zwei Datenplatten (bei nur einer: die erste Paritätsplatte).
	fmt.Fprintf(&b, "content %s\n", ContentPath(p.Pool))
	for _, id := range p.Data[:min(len(p.Data), 3)] {
		fmt.Fprintf(&b, "content %s/.snapraid.content\n", DiskPath(id))
	}
	if len(p.Data) < 2 {
		fmt.Fprintf(&b, "content %s/.snapraid.content\n", ParityPath(p.Parity[0]))
	}
	for _, id := range p.Data {
		fmt.Fprintf(&b, "data %s %s/\n", id, DiskPath(id))
	}
	b.WriteString("exclude /lost+found/\nexclude *.unrecoverable\nexclude /tmp/\n")
	return b.String()
}

func renderTimer(action, pool, schedule string) string {
	what := map[string]string{"sync": "Synchronisierung", "scrub": "Prüfung"}[action]
	return fmt.Sprintf(`%s
[Unit]
Description=BetterNAS SnapRAID %s (%s)

[Timer]
OnCalendar=%s
Persistent=true
RandomizedDelaySec=300
Unit=betternas-snapraid-%s@%s.service

[Install]
WantedBy=timers.target
`, ManagedHeader, what, pool, schedule, action, pool)
}
