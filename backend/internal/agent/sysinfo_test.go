package agent

import (
	"testing"

	"github.com/betternas/betternas/internal/sysexec"
)

func TestSysinfoParsesProc(t *testing.T) {
	info, err := SystemInfo(Deps{FS: sysexec.NewRootFS("testdata/root")})
	if err != nil {
		t.Fatal(err)
	}
	if info.MemTotalBytes != 16303472*1024 || info.MemAvailableBytes != 15000000*1024 {
		t.Fatalf("mem %+v", info)
	}
	if info.OS != "Debian GNU/Linux 13 (trixie)" || info.Kernel != "6.12.107+deb13-amd64" || info.Hostname != "betternas" {
		t.Fatalf("%+v", info)
	}
	if info.UptimeSeconds != 12345 || info.CPUCount < 1 {
		t.Fatalf("%+v", info)
	}
	// cpu 4705 150 1120 16250 520 30 45 0 → Summe 22820, idle 16250 + 520
	if info.CPUTotalTicks != 22820 || info.CPUIdleTicks != 16770 {
		t.Fatalf("cpu ticks %+v", info)
	}
	if info.Load != [3]float64{0.52, 0.34, 0.21} || info.CPUModel != "Intel(R) Core(TM) i5-8500T CPU @ 2.10GHz" {
		t.Fatalf("load/model %+v", info)
	}
}

func TestParseCPUStatIgnoresGarbage(t *testing.T) {
	for _, in := range []string{"", "intr 1", "cpu 1 2", "cpu0 1 2 3 4 5"} {
		if total, idle := parseCPUStat(in); total != 0 || idle != 0 {
			t.Errorf("%q: %d %d", in, total, idle)
		}
	}
	if total, idle := parseCPUStat("cpu  1 2 3 4 5 6 7 8 9 10\n"); total != 36 || idle != 9 {
		t.Errorf("guest darf nicht doppelt zählen: %d %d", total, idle)
	}
}
