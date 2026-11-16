package docker

import (
	"strings"
	"testing"
)

func TestStackNameValidation(t *testing.T) {
	for _, ok := range []string{"jellyfin", "a_b-1", "0abc"} {
		if !StackNameRe.MatchString(ok) {
			t.Errorf("%q sollte gültig sein", ok)
		}
	}
	for _, bad := range []string{"", "../x", "A", "a b", "-a", "a/b", strings.Repeat("a", 41)} {
		if StackNameRe.MatchString(bad) {
			t.Errorf("%q sollte ungültig sein", bad)
		}
	}
}

func TestContainerIDAndUnitValidation(t *testing.T) {
	if !ContainerIDRe.MatchString("0123456789ab") || ContainerIDRe.MatchString("../x") || ContainerIDRe.MatchString("--all") || ContainerIDRe.MatchString("abc") {
		t.Fatal("Container-ID-Prüfung falsch")
	}
	if !RunUnitRe.MatchString("betternas-stack-media-1700000000") || RunUnitRe.MatchString("sshd") || RunUnitRe.MatchString("betternas-stack-../x-1") {
		t.Fatal("Unit-Prüfung falsch")
	}
}

func TestStackDir(t *testing.T) {
	if got := StackDir("media"); got != "/srv/appdata/stacks/media" {
		t.Fatal(got)
	}
}

func TestParseContainers(t *testing.T) {
	out := []byte(`{"ID":"aaaaaaaaaaaabbbbbbbbbbbb","Names":"web","Image":"nginx","State":"running","Status":"Up 2 hours","Ports":"0.0.0.0:8080->80/tcp, :::8080->80/tcp","Labels":"a=b,com.docker.compose.project=media","CreatedAt":"2026-11-16 10:00:00 +0100 CET"}
{"ID":"cccccccccccc","Names":"alone","Image":"busybox","State":"exited","Status":"Exited (0) 1 hour ago","Ports":"","Labels":"","CreatedAt":"x"}
`)
	got, err := ParseContainers(out)
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	if got[0].Name != "alone" || got[0].Stack != "" || len(got[0].Ports) != 0 {
		t.Errorf("%+v", got[0])
	}
	w := got[1]
	if w.ID != "aaaaaaaaaaaa" || w.Stack != "media" || len(w.Ports) != 1 || w.Ports[0] != "0.0.0.0:8080->80/tcp" {
		t.Errorf("%+v", w)
	}
}

func TestParseComposeLs(t *testing.T) {
	got, err := ParseComposeLs([]byte(`[{"Name":"media","Status":"running(2), exited(1)","ConfigFiles":"/x"},{"Name":"off","Status":"exited(3)"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if got["media"].Running != 2 || got["media"].Total != 3 || got["off"].Running != 0 || got["off"].Total != 3 {
		t.Errorf("%+v", got)
	}
	if empty, err := ParseComposeLs([]byte("")); err != nil || len(empty) != 0 {
		t.Fatal(empty, err)
	}
}
