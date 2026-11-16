//go:build integration

// Integrationstest für Docker-Stacks mit echtem Docker und echtem systemd (systemd-run).
// Braucht root, docker, das Compose-Plugin und Internetzugang für das Image busybox.
package agent

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/sysexec"
)

func waitRun(t *testing.T, d Deps, unit string) {
	t.Helper()
	for i := 0; i < 180; i++ {
		st, err := StackRunStatus(context.Background(), d, unit)
		if err != nil {
			t.Fatal(err)
		}
		switch st.State {
		case "succeeded":
			return
		case "failed":
			t.Fatalf("Lauf %s fehlgeschlagen:\n%s", unit, st.Log)
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("Lauf %s: Zeitüberschreitung", unit)
}

func TestDockerLifecycle(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("braucht root")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker ist nicht installiert")
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		t.Skip("braucht systemd")
	}
	ctx := context.Background()
	d := Deps{Runner: sysexec.NewRunner(), FS: sysexec.NewRootFS("/")}
	if st, _ := DockerStatus(ctx, d); !st.Active {
		t.Skip("Docker-Dienst läuft nicht")
	}
	const name = "bnasit"
	_ = DeleteStack(ctx, d, name) // Reste eines früheren Laufs

	compose := "services:\n  sleeper:\n    image: busybox:stable\n    command: [\"sleep\", \"600\"]\n"
	if err := SaveStack(ctx, d, StackSave{StackFiles: StackFiles{Name: name, Compose: compose, Env: "A=1\n"}, Create: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = DeleteStack(ctx, d, name) })

	if chk, err := ValidateStack(ctx, d, name); err != nil || !chk.OK {
		t.Fatalf("gültige Datei: %+v %v", chk, err)
	}
	if err := SaveStack(ctx, d, StackSave{StackFiles: StackFiles{Name: name, Compose: "services:\n  sleeper:\n    image: [kaputt\n"}}); err != nil {
		t.Fatal(err)
	}
	chk, err := ValidateStack(ctx, d, name)
	if err != nil || chk.OK || chk.Message == "" {
		t.Fatalf("ungültige Datei: %+v %v", chk, err)
	}
	if _, err := RunStack(ctx, d, name, "up"); code(err) != "invalid_params" {
		t.Fatalf("up mit ungültiger Datei muss abgelehnt werden: %v", err)
	}
	if err := SaveStack(ctx, d, StackSave{StackFiles: StackFiles{Name: name, Compose: compose}}); err != nil {
		t.Fatal(err)
	}

	unit, err := RunStack(ctx, d, name, "up")
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, d, unit)

	list, err := ListStacks(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, s := range list {
		if s.Name == name && s.Running == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("Stack läuft nicht laut Liste: %+v", list)
	}
	containers, err := ListContainers(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, c := range containers {
		if c.Stack == name {
			id = c.ID
		}
	}
	if id == "" {
		t.Fatalf("Container des Stacks fehlt: %+v", containers)
	}
	if err := ContainerAction(ctx, d, id, "stop"); err != nil {
		t.Fatal(err)
	}

	unit, err = RunStack(ctx, d, name, "down")
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, d, unit)
	if err := DeleteStack(ctx, d, name); err != nil {
		t.Fatal(err)
	}
	if _, err := GetStack(ctx, d, name); code(err) != "not_found" {
		t.Fatalf("Stack sollte weg sein: %v", err)
	}
}
