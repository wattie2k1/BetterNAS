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
}
