package storage

import (
	"strings"
	"testing"
)

const diskstatsSample = `   7       0 loop0 10 0 20 0 0 0 0 0 0 0 0
   8       0 sda 100 0 2000 50 40 0 800 30 0 120 80
   8       1 sda1 90 0 1800 45 40 0 800 30 0 100 75
 259       0 nvme0n1 5 0 16 1 7 0 32 2 0 9 3
 252       0 zram0 1 0 8 0 0 0 0 0 0 0 0
kaputt
`

func TestParseDiskstats(t *testing.T) {
	got := parseDiskstats(strings.NewReader(diskstatsSample))
	if _, ok := got["loop0"]; ok {
		t.Error("loop wird übersprungen")
	}
	if _, ok := got["zram0"]; ok {
		t.Error("zram wird übersprungen")
	}
	sda := got["sda"]
	if sda.ReadBytes != 2000*512 || sda.WriteBytes != 800*512 || sda.BusyMs != 120 {
		t.Fatalf("%+v", sda)
	}
	if got["nvme0n1"].ReadBytes != 16*512 {
		t.Fatalf("%+v", got["nvme0n1"])
	}
	if len(got) != 3 { // sda, sda1, nvme0n1
		t.Fatalf("%v", got)
	}
}
