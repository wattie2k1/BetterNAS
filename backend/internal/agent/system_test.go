package agent

import (
	"context"
	"testing"

	"github.com/betternas/betternas/internal/sysexec"
)

func TestRestartApiRunsSystemdRun(t *testing.T) {
	f := &sysexec.FakeRunner{Responses: map[string]sysexec.Result{}}
	d := Deps{Runner: f, FS: sysexec.NewRootFS(t.TempDir())}
	if err := RestartAPI(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if !f.Called("systemd-run --on-active=2 --unit=betternas-api-restart systemctl restart betternas-api.service") {
		t.Fatalf("%v", f.Calls)
	}
}
