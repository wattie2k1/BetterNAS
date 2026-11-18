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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/docker"
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
		t.Fatalf("Update-Prüfung fehlgeschlagen: %s", sts[0].Error)
	}
	if sts[0].Outdated {
		t.Logf("busybox:stable hat ein Update (%s → %s)", sts[0].Local, sts[0].Remote)
	}
}

// TestDockerRollback prüft Update-Merken und Rollback mit echtem Docker. Das „neue Image“ eines Updates wird
// lokal erzeugt (docker commit), damit kein Registry-Zugriff nötig ist.
func TestDockerRollback(t *testing.T) {
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
	sh := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	const name, ref = "bnasrb", "bnasrb-img:latest"
	_ = DeleteStack(ctx, d, name)
	t.Cleanup(func() {
		_ = DeleteStack(ctx, d, name)
		_ = exec.Command("docker", "rm", "-f", "bnasrb-tmp").Run()
		_ = exec.Command("docker", "rmi", "-f", ref).Run()
	})

	if exec.Command("docker", "image", "inspect", "busybox:stable").Run() != nil {
		sh("pull", "busybox:stable")
	}
	sh("tag", "busybox:stable", ref)
	idA := sh("image", "inspect", "--format", "{{.Id}}", ref)

	compose := "services:\n  app:\n    image: " + ref + "\n    pull_policy: never\n    command: [\"sleep\", \"600\"]\n"
	if err := SaveStack(ctx, d, StackSave{StackFiles: StackFiles{Name: name, Compose: compose}, Create: true}); err != nil {
		t.Fatal(err)
	}
	run := func(action string) {
		t.Helper()
		unit, err := RunStack(ctx, d, name, action)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		waitRun(t, d, unit)
	}
	runningImage := func() string {
		t.Helper()
		cid := sh("ps", "-q", "--filter", "label=com.docker.compose.project="+name)
		return sh("inspect", "--format", "{{.Image}}", cid)
	}

	run("up")
	if got := runningImage(); got != idA {
		t.Fatalf("Image vor dem Update: %s, erwartet %s", got, idA)
	}
	run("update") // merkt idA vor
	// Das „Update“ bringt ein neues Image unter demselben Tag.
	sh("run", "--name", "bnasrb-tmp", "busybox:stable", "true")
	sh("commit", "bnasrb-tmp", ref)
	sh("rm", "bnasrb-tmp")
	idB := sh("image", "inspect", "--format", "{{.Id}}", ref)
	if idA == idB {
		t.Fatal("Testaufbau: Image-ID unverändert")
	}
	if err := StackRollbackCommit(ctx, d, name); err != nil {
		t.Fatal(err)
	}
	if list, _ := ListStacks(ctx, d); len(list) == 0 || list[0].Rollback == "" {
		t.Fatalf("kein Rollback-Stand: %+v", list)
	}
	run("up") // Container läuft jetzt mit dem neuen Image B
	if got := runningImage(); got != idB {
		t.Fatalf("Image nach Update: %s, erwartet %s", got, idB)
	}
	run("rollback")
	if got := runningImage(); got != idA {
		t.Fatalf("Image nach Rollback: %s, erwartet %s", got, idA)
	}
	if err := StackRollbackClear(ctx, d, name); err != nil {
		t.Fatal(err)
	}
	if list, _ := ListStacks(ctx, d); list[0].Rollback != "" {
		t.Fatalf("Rollback-Stand bleibt: %+v", list)
	}
	if out, _ := exec.Command("docker", "image", "ls", "--format", "{{.Repository}}:{{.Tag}}", "betternas-rollback/"+name).Output(); strings.TrimSpace(string(out)) != "" {
		t.Fatalf("Rollback-Tags bleiben: %s", out)
	}
}

// TestDockerFolders prüft die Ordnerauswahl mit echtem realpath/find, auch den Ausbruch per Symlink.
func TestDockerFolders(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("braucht root")
	}
	if st, err := os.Stat("/srv/appdata"); err != nil || !st.IsDir() {
		t.Skip("/srv/appdata fehlt")
	}
	ctx := context.Background()
	d := Deps{Runner: sysexec.NewRunner(), FS: sysexec.NewRootFS("/")}
	base := "/srv/appdata/bnasit-folders"
	_ = os.RemoveAll(base)
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	for _, sub := range []string{"Medien", "backup", ".versteckt", "mit Leerzeichen"} {
		if err := os.MkdirAll(base+"/"+sub, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("/etc", base+"/hinaus"); err != nil {
		t.Fatal(err)
	}

	roots, err := ListFolders(ctx, d, "")
	if err != nil || len(roots.Dirs) == 0 || roots.Dirs[len(roots.Dirs)-1].Path != "/srv/appdata" {
		t.Fatalf("Wurzeln: %+v %v", roots, err)
	}
	l, err := ListFolders(ctx, d, base)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range l.Dirs {
		names = append(names, f.Name)
	}
	// Symlink auf ein Verzeichnis zählt für `find -type d` nicht als Ordner; versteckte fehlen.
	if got := strings.Join(names, "|"); got != "backup|Medien|mit Leerzeichen" || l.Parent != "/srv/appdata" {
		t.Fatalf("%q %+v", got, l)
	}
	if _, err := ListFolders(ctx, d, base+"/hinaus"); err == nil {
		t.Fatal("Symlink nach /etc darf nicht gelistet werden")
	}
	if app, err := ListFolders(ctx, d, "/srv/appdata"); err != nil {
		t.Fatal(err)
	} else {
		for _, f := range app.Dirs {
			if f.Name == "docker" || f.Name == "stacks" {
				t.Fatalf("interner Ordner sichtbar: %s", f.Name)
			}
		}
	}
}

// TestDockerDevices prüft die Geräteerkennung und dass die vorgeschlagenen Einstellungen (--device + --group-add)
// einem Container ohne Root-Rechte den Zugriff auf die GPU geben.
func TestDockerDevices(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("braucht root")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker ist nicht installiert")
	}
	ctx := context.Background()
	d := Deps{Runner: sysexec.NewRunner(), FS: sysexec.NewRootFS("/")}
	if st, _ := DockerStatus(ctx, d); !st.Active {
		t.Skip("Docker-Dienst läuft nicht")
	}
	list, err := ListHostDevices(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	var gpu *docker.HostDevice
	for i := range list {
		if list[i].Kind == "gpu" {
			gpu = &list[i]
			break
		}
	}
	if gpu == nil {
		t.Skip("keine GPU (/dev/dri/renderD*)")
	}
	t.Logf("GPU: %+v", *gpu)
	if gpu.GID == 0 || gpu.Group == "" || gpu.Name == "" {
		t.Fatalf("unvollständig: %+v", *gpu)
	}
	if exec.Command("docker", "image", "inspect", "busybox:stable").Run() != nil {
		if out, err := exec.Command("docker", "pull", "busybox:stable").CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	probe := func(extra ...string) error {
		args := append([]string{"run", "--rm", "--user", "1234:1234", "--device", gpu.Path + ":" + gpu.Path}, extra...)
		args = append(args, "busybox:stable", "sh", "-c", "test -r "+gpu.Path+" && test -w "+gpu.Path)
		return exec.Command("docker", args...).Run()
	}
	if probe() == nil {
		t.Skip("GPU ist ohne Gruppe zugänglich (Gerät ist weltweit lesbar); nichts zu beweisen")
	}
	if err := probe("--group-add", strconv.Itoa(gpu.GID)); err != nil {
		t.Fatalf("mit group_add %d kein Zugriff: %v", gpu.GID, err)
	}
}
