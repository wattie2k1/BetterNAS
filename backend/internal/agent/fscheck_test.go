package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/sysexec"
)

func TestFsCheckCommandIsReadOnly(t *testing.T) {
	cases := []struct{ fs, mp, tool, arg string }{
		{"ext4", "", "e2fsck", "-n"},
		{"ext4", "/srv/disks/a", "e2fsck", "-n"},
		{"xfs", "", "xfs_repair", "-n"},
		{"xfs", "/srv/disks/a", "xfs_scrub", "-n"},
		{"btrfs", "", "btrfs", "--readonly"},
		{"btrfs", "/srv/disks/a", "btrfs", "scrub"},
	}
	for _, c := range cases {
		tool, args, err := fsCheckCommand(c.fs, "/dev/sdx1", c.mp)
		if err != nil || tool != c.tool || !strings.Contains(strings.Join(args, " "), c.arg) {
			t.Errorf("%s/%q: %s %v %v", c.fs, c.mp, tool, args, err)
		}
	}
	if _, _, err := fsCheckCommand("ntfs", "/dev/x", ""); code(err) != "invalid_params" {
		t.Error("ntfs nicht unterstützt")
	}
}

func TestFsCheckResultMapping(t *testing.T) {
	cases := []struct {
		fs     string
		online bool
		code   int
		want   string
	}{
		{"ext4", false, 0, fsOK}, {"ext4", false, 4, fsErrors}, {"ext4", false, 8, fsFailed},
		{"ext4", true, 1, fsFailed}, // 1 = korrigiert: mit -n nicht möglich, also ungewöhnlich
		{"xfs", false, 1, fsErrors}, {"xfs", false, 2, fsFailed},
		{"xfs", true, 1, fsErrors}, {"btrfs", true, 3, fsErrors}, {"btrfs", false, 1, fsFailed}, {"btrfs", true, 127, fsFailed},
	}
	for _, c := range cases {
		if got := fsCheckResult(c.fs, c.online, c.code); got != c.want {
			t.Errorf("%s online=%v code=%d: %s statt %s", c.fs, c.online, c.code, got, c.want)
		}
	}
}

func TestFsCheckStartRunsAndReports(t *testing.T) {
	d, f, _ := storageEnv(t)
	disks, err := ListDisks(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	var dev, part string
	for _, x := range disks {
		if len(x.Partitions) == 1 && x.Partitions[0].FSType == "ext4" && !x.System {
			dev, part = x.Device, x.Partitions[0].Device
		}
	}
	if dev == "" {
		t.Skip("keine passende Platte in der Vorlage")
	}
	f.Responses["e2fsck -f -n "+part] = sysexec.Result{Stdout: []byte("clean"), ExitCode: 0}
	if err := FsCheckStart(context.Background(), d, dev); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if s := FsCheckStatus(dev); s.Known && !s.Running {
			if s.Result != fsOK || !strings.Contains(s.Output, "clean") {
				t.Fatalf("%+v", s)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("Prüfung wurde nicht fertig")
}

func TestFsCheckStartErrors(t *testing.T) {
	d, _, _ := storageEnv(t)
	if code(FsCheckStart(context.Background(), d, "/dev/zzz")) != "not_found" {
		t.Error("unbekannte Platte")
	}
	if s := FsCheckStatus("/dev/zzz"); s.Known {
		t.Error("unbekannt")
	}
}
