package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/sysexec"
)

func TestReadLogWhitelistAndLimit(t *testing.T) {
	d, f, _ := sysEnv(t)
	f.Responses["journalctl -u betternas-api.service"] = sysexec.Result{Stdout: []byte("zeile\n")}
	txt, err := ReadLog(context.Background(), d, "api", 99999)
	if err != nil || txt != "zeile\n" {
		t.Fatalf("%q %v", txt, err)
	}
	if !f.Called("journalctl -u betternas-api.service -o short-iso -n 2000 --no-pager") {
		t.Fatalf("Zeilenzahl nicht begrenzt: %v", f.Calls)
	}
	if _, err := ReadLog(context.Background(), d, "../../etc/passwd", 10); err == nil {
		t.Fatal("unbekannte Quelle muss abgelehnt werden")
	}
}

func TestCollectSupport(t *testing.T) {
	d, f, _ := sysEnv(t)
	f.Responses["uname -a"] = sysexec.Result{Stdout: []byte("Linux nas\n")}
	f.Responses["journalctl -u docker.service"] = sysexec.Result{Stdout: []byte("docker läuft\n")}
	out := CollectSupport(context.Background(), d)
	if out["system/uname.txt"] != "Linux nas\n" || out["logs/docker.log"] != "docker läuft\n" {
		t.Fatalf("%v", out)
	}
	for _, k := range []string{"system/lsblk.txt", "logs/api.log", "logs/agent.log"} {
		if _, ok := out[k]; !ok {
			t.Errorf("%s fehlt", k)
		}
	}
	for k := range out {
		if strings.Contains(k, "shadow") || strings.Contains(k, "settings") {
			t.Errorf("verbotene Datei %s", k)
		}
	}
}
