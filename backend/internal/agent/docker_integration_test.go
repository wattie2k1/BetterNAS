//go:build integration

// Integrationstest für Docker-Stacks mit echtem Docker und echtem systemd (systemd-run).
// Braucht root, docker, das Compose-Plugin und Internetzugang für das Image busybox.
package agent

import (
	"context"
	"encoding/json"
	"github.com/betternas/betternas/internal/rpc"
	"os"
	"os/exec"
	"strings"
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

// readUntil sammelt „data“-Ereignisse, bis want vorkommt.
func readUntil(t *testing.T, ch <-chan rpc.Event, want string) string {
	t.Helper()
	var got string
	timeout := time.After(30 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("Strom endete vor %q, bisher: %q", want, got)
			}
			if ev.Event == "data" {
				var b []byte
				_ = json.Unmarshal(ev.Data, &b)
				got += string(b)
				if strings.Contains(got, want) {
					return got
				}
			} else if ev.Event == "error" {
				t.Fatalf("Fehler: %s", ev.Data)
			}
		case <-timeout:
			t.Fatalf("Zeitüberschreitung vor %q, bisher: %q", want, got)
		}
	}
}

func TestDockerOps(t *testing.T) {
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
	d := Deps{Runner: sysexec.NewRunner(), FS: sysexec.NewRootFS("/"), Stream: sysexec.NewStreamer()}
	if st, _ := DockerStatus(ctx, d); !st.Active {
		t.Skip("Docker-Dienst läuft nicht")
	}
	const name = "bnasops"
	_ = DeleteStack(ctx, d, name)
	compose := "services:\n  sleeper:\n    image: busybox:stable\n    command: [\"sh\", \"-c\", \"echo hello-logs; sleep 600\"]\n"
	if err := SaveStack(ctx, d, StackSave{StackFiles: StackFiles{Name: name, Compose: compose}, Create: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = DeleteStack(ctx, d, name) })
	unit, err := RunStack(ctx, d, name, "up")
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, d, unit)

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
		t.Fatal("Container fehlt")
	}

	// Logs
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	in := make(chan rpc.Event)
	out := make(chan rpc.Event, 64)
	go func() {
		_ = ContainerLogs(sctx, d, json.RawMessage(`{"id":"`+id+`","tail":10}`), in, func(ev rpc.Event) error { out <- ev; return nil })
		close(out)
	}()
	readUntil(t, out, "hello-logs")
	close(in)

	// Shell mit echtem PTY
	sin := make(chan rpc.Event, 8)
	sout := make(chan rpc.Event, 64)
	go func() {
		_ = ContainerShell(sctx, d, json.RawMessage(`{"id":"`+id+`","rows":24,"cols":80}`), sin, func(ev rpc.Event) error { sout <- ev; return nil })
		close(sout)
	}()
	time.Sleep(time.Second)
	b, _ := json.Marshal([]byte("echo shell-ok-$((6*7))\n"))
	sin <- rpc.Event{Event: "input", Data: b}
	readUntil(t, sout, "shell-ok-42")
	b, _ = json.Marshal([]byte("exit\n"))
	sin <- rpc.Event{Event: "input", Data: b}
	for ev := range sout {
		if ev.Event == "exit" {
			break
		}
	}

	// Stats, Images, Netzwerke, Volumes, Update-Prüfung
	stats, err := ContainerStatsList(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	var seen bool
	for _, s := range stats {
		if s.ID == id && s.MemLimit > 0 {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("Stats fehlen: %+v", stats)
	}
	images, err := ListImages(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	var busybox bool
	for _, im := range images {
		if im.Ref == "busybox:stable" {
			busybox = true
		}
	}
	if !busybox {
		t.Fatalf("busybox:stable fehlt: %+v", images)
	}
	if nets, err := ListNetworks(ctx, d); err != nil || len(nets) == 0 {
		t.Fatalf("%+v %v", nets, err)
	}
	if _, err := ListVolumes(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := RemoveResource(ctx, d, "network", "bridge"); err == nil {
		t.Fatal("ungültige Netzwerk-ID muss abgelehnt werden")
	}
	imgs, err := StackImages(ctx, d, name)
	if err != nil || len(imgs) != 1 || imgs[0] != "busybox:stable" {
		t.Fatalf("%v %v", imgs, err)
	}
	sts, err := ImageDigests(ctx, d, imgs)
	if err != nil || len(sts) != 1 {
		t.Fatalf("%+v %v", sts, err)
	}
	if sts[0].Error != "" {
		t.Logf("Update-Prüfung nicht möglich (Registry erreichbar?): %s", sts[0].Error)
	} else if sts[0].Outdated {
		t.Logf("busybox:stable hat ein Update (%s → %s)", sts[0].Local, sts[0].Remote)
	}
}
