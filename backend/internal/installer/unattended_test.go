package installer

import (
	"context"
	"testing"

	"github.com/betternas/betternas/internal/sysexec"
)

func TestLoadAnswersFillsDefaults(t *testing.T) {
	a, err := LoadAnswers([]byte(`{"targetDevice":"/dev/vda","confirmWipe":"/dev/vda","adminUser":"ittest","adminPassword":"ittest-pw"}`))
	if err != nil {
		t.Fatal(err)
	}
	if a.Hostname != "betternas" || a.RootSizeGiB != 32 || !a.Net.DHCP || a.Language != "de" {
		t.Fatalf("%+v", a)
	}
}

func TestLoadAnswersRejectsUnknownField(t *testing.T) {
	if _, err := LoadAnswers([]byte(`{"hostnmae":"x"}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestCheckUnattendedRequiresConfirm(t *testing.T) {
	targets := []Target{{Device: "/dev/vda"}}
	a := testAnswers()
	a.TargetDevice = "/dev/vda"
	if err := CheckUnattended(a, targets, tzOK); err == nil {
		t.Fatal("missing confirmWipe accepted")
	}
	a.ConfirmWipe = "/dev/vdb"
	if err := CheckUnattended(a, targets, tzOK); err == nil {
		t.Fatal("wrong confirmWipe accepted")
	}
	a.ConfirmWipe = "/dev/vda"
	if err := CheckUnattended(a, targets, tzOK); err != nil {
		t.Fatal(err)
	}
	a.TargetDevice, a.ConfirmWipe = "/dev/sdc", "/dev/sdc" // z. B. das Live-Medium
	if err := CheckUnattended(a, targets, tzOK); err == nil {
		t.Fatal("target outside of allowed targets accepted")
	}
}

func TestFindNoDisk(t *testing.T) {
	f := &sysexec.FakeRunner{Responses: map[string]sysexec.Result{"blkid -L BNASCFG": {ExitCode: 2}}}
	_, found, err := Find(context.Background(), f, sysexec.NewRootFS(t.TempDir()))
	if err != nil || found {
		t.Fatalf("found=%v err=%v", found, err)
	}
}

func TestFindReadsAnswers(t *testing.T) {
	root := t.TempDir()
	fs := sysexec.NewRootFS(root)
	_ = fs.WriteFile("/run/bnascfg/answers.json", []byte(`{"hostname":"x"}`), 0o644)
	f := &sysexec.FakeRunner{Responses: map[string]sysexec.Result{"blkid -L BNASCFG": {Stdout: []byte("/dev/vdb\n")}}}
	data, found, err := Find(context.Background(), f, fs)
	if err != nil || !found || string(data) != `{"hostname":"x"}` {
		t.Fatalf("%s %v %v", data, found, err)
	}
	if !f.Called("mount -o ro /dev/vdb /run/bnascfg") {
		t.Fatal(f.Calls)
	}
}
