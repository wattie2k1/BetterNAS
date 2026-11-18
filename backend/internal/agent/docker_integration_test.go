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

// TestDockerBackupRollback prüft Update mit Datensicherung und Rollback mit Daten gegen echtes Docker:
// Nach dem Rollback müssen Bind-Ordner und benanntes Volume wieder den Stand vor dem Update haben.
func TestDockerBackupRollback(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("braucht root")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker ist nicht installiert")
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		t.Skip("braucht systemd")
	}
	if st, err := os.Stat("/srv/appdata"); err != nil || !st.IsDir() {
		t.Skip("/srv/appdata fehlt")
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
	const name, ref = "bnasbk", "bnasbk-img:latest"
	conf := "/srv/appdata/bnasbk-data/conf"
	_ = DeleteStack(ctx, d, name)
	t.Cleanup(func() {
		_ = DeleteStack(ctx, d, name)
		_ = exec.Command("docker", "rm", "-f", "bnasbk-tmp").Run()
		_ = exec.Command("docker", "volume", "rm", "-f", "bnasbk_vol").Run()
		_ = exec.Command("docker", "rmi", "-f", ref).Run()
		_ = os.RemoveAll("/srv/appdata/bnasbk-data")
	})
	if exec.Command("docker", "image", "inspect", "busybox:stable").Run() != nil {
		sh("pull", "busybox:stable")
	}
	sh("tag", "busybox:stable", ref)
	idA := sh("image", "inspect", "--format", "{{.Id}}", ref)
	if err := os.MkdirAll(conf, 0o755); err != nil {
		t.Fatal(err)
	}

	compose := "services:\n  app:\n    image: " + ref + "\n    pull_policy: never\n    command: [\"sleep\", \"600\"]\n" +
		"    volumes:\n      - " + conf + ":/conf\n      - vol:/data\n      - /srv/pools:/pools:ro\nvolumes:\n  vol:\n    name: bnasbk_vol\n"
	if err := SaveStack(ctx, d, StackSave{StackFiles: StackFiles{Name: name, Compose: compose}, Create: true}); err != nil {
		t.Fatal(err)
	}
	run := func(action string, opts docker.RunOptions) {
		t.Helper()
		unit, err := RunStackOpts(ctx, d, name, action, opts)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		waitRun(t, d, unit)
	}
	exec1 := func(script string) string {
		t.Helper()
		return sh("exec", name+"-app-1", "sh", "-c", script)
	}
	readConf := func(f string) string {
		b, err := os.ReadFile(conf + "/" + f)
		if err != nil {
			return "<fehlt>"
		}
		return strings.TrimSpace(string(b))
	}

	run("up", docker.RunOptions{})
	if err := os.WriteFile(conf+"/state", []byte("v1\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	exec1("echo v1 > /data/state && mkdir -p /data/sub && echo deep > /data/sub/f")

	plan, err := StackBackupPlan(ctx, d, name)
	if err != nil || !plan.Possible || len(plan.Sources) != 2 {
		t.Fatalf("Plan: %+v %v", plan, err)
	}
	for _, s := range plan.Sources {
		if strings.HasPrefix(s.Path, "/srv/pools") {
			t.Fatalf("Pool darf nicht gesichert werden: %+v", s)
		}
	}
	t.Logf("Plan: %+v", plan)

	run("update", docker.RunOptions{Backup: true})
	if got := exec1("cat /data/state"); got != "v1" {
		t.Fatalf("Stack läuft nach dem Update nicht mit den Daten: %q", got)
	}
	// Das „Update“ bringt ein neues Image, und die Anwendung verändert ihre Daten (z. B. Migration).
	sh("run", "--name", "bnasbk-tmp", "busybox:stable", "true")
	sh("commit", "bnasbk-tmp", ref)
	sh("rm", "bnasbk-tmp")
	idB := sh("image", "inspect", "--format", "{{.Id}}", ref)
	if err := StackRollbackCommit(ctx, d, name); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conf+"/state", []byte("v2\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conf+"/neu", []byte("nach dem Update\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	exec1("echo v2 > /data/state && rm -rf /data/sub && echo extra > /data/extra")
	run("up", docker.RunOptions{}) // Container mit Image B
	if got := runningImageOf(t, name); got != idB {
		t.Fatalf("Image nach Update: %s, erwartet %s", got, idB)
	}
	list, _ := ListStacks(ctx, d)
	if len(list) == 0 || list[0].Rollback == "" || !list[0].RollbackData {
		t.Fatalf("Rollback-Stand ohne Daten: %+v", list)
	}

	// Rollback ohne Daten lässt den aktuellen Datenstand in Ruhe …
	run("rollback", docker.RunOptions{})
	if readConf("state") != "v2" || readConf("neu") == "<fehlt>" {
		t.Fatalf("Rollback ohne Daten hat Daten verändert: state=%q neu=%q", readConf("state"), readConf("neu"))
	}
	// … ein erneutes Update mit Sicherung und Rollback mit Daten stellt den Stand davor wieder her.
	// (Der erste Rollback hat den Stand verbraucht; wir bauen ihn für den Test neu auf.)
	sh("tag", "busybox:stable", ref)
	run("update", docker.RunOptions{Backup: true}) // sichert v2
	sh("run", "--name", "bnasbk-tmp", "busybox:stable", "true")
	sh("commit", "bnasbk-tmp", ref)
	sh("rm", "bnasbk-tmp")
	if err := StackRollbackCommit(ctx, d, name); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conf+"/state", []byte("v3\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	exec1("echo v3 > /data/state && echo noch-mehr > /data/mehr")
	run("rollback", docker.RunOptions{RestoreData: true})

	if readConf("state") != "v2" {
		t.Fatalf("Bind-Ordner nicht zurückgesetzt: %q", readConf("state"))
	}
	if got := exec1("cat /data/state; ls /data | tr '\\n' ' '"); !strings.HasPrefix(got, "v2") || strings.Contains(got, "mehr") {
		t.Fatalf("Volume nicht zurückgesetzt: %q", got)
	}
	if got := exec1("cat /data/extra"); got != "extra" {
		t.Fatalf("Datei aus der Sicherung fehlt: %q", got)
	}
	if st, err := os.Stat(conf + "/state"); err != nil || st.Mode().Perm() != 0o640 {
		t.Fatalf("Rechte nicht erhalten: %v %v", st, err)
	}
	if got := runningImageOf(t, name); got != idA {
		t.Fatalf("Image nach Rollback: %s, erwartet %s", got, idA)
	}
	// Nach einem erfolgreichen Rollback räumt der Manager den Stand samt Sicherung auf.
	if err := StackRollbackClear(ctx, d, name); err != nil {
		t.Fatal(err)
	}
	if list, _ := ListStacks(ctx, d); len(list) == 0 || list[0].Name != name || list[0].Rollback != "" {
		t.Fatalf("Rollback-Stand bleibt: %+v", list)
	}
	if left, _ := os.ReadDir("/srv/appdata/backups/stacks/" + name); len(left) != 0 {
		t.Fatalf("Sicherung bleibt liegen: %v", left)
	}
}

func runningImageOf(t *testing.T, project string) string {
	t.Helper()
	cid, err := exec.Command("docker", "ps", "-q", "--filter", "label=com.docker.compose.project="+project).Output()
	if err != nil || strings.TrimSpace(string(cid)) == "" {
		t.Fatalf("kein laufender Container: %v", err)
	}
	out, err := exec.Command("docker", "inspect", "--format", "{{.Image}}", strings.TrimSpace(string(cid))).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// TestDockerCreateFolder prüft das Anlegen von Ordnern mit echtem realpath/mkdir (App-Daten und Pool).
func TestDockerCreateFolder(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("braucht root")
	}
	if st, err := os.Stat("/srv/appdata"); err != nil || !st.IsDir() {
		t.Skip("/srv/appdata fehlt")
	}
	ctx := context.Background()
	d := Deps{Runner: sysexec.NewRunner(), FS: sysexec.NewRootFS("/")}
	base := "/srv/appdata/bnasit-create"
	_ = os.RemoveAll(base)
	t.Cleanup(func() { _ = os.RemoveAll(base) })

	f, err := CreateFolder(ctx, d, "/srv/appdata", "bnasit-create")
	if err != nil || f.Path != base {
		t.Fatalf("%+v %v", f, err)
	}
	st, err := os.Stat(base)
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0o755 {
		t.Fatalf("%v %v", st, err)
	}
	if _, err := CreateFolder(ctx, d, base, "Medien-1.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(base + "/Medien-1.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateFolder(ctx, d, base, "Medien-1.0"); err == nil {
		t.Fatal("Doppelter Ordner darf nicht angelegt werden")
	}
	// Ein Link aus dem Bereich heraus führt nicht zu Ordnern außerhalb.
	if err := os.Symlink("/etc", base+"/hinaus"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateFolder(ctx, d, base+"/hinaus", "boese"); err == nil {
		t.Fatal("Ordner über Symlink nach /etc angelegt")
	}
	if _, err := os.Stat("/etc/boese"); err == nil {
		t.Fatal("/etc/boese existiert")
	}
	// BetterNAS-Bereiche und die Pool-Übersicht sind tabu.
	for _, tc := range [][2]string{{"/srv/appdata", "stacks"}, {"/srv/appdata/stacks", "x"}, {"/srv/appdata/docker", "x"}, {"/srv/pools", "x"}} {
		if _, err := CreateFolder(ctx, d, tc[0], tc[1]); err == nil {
			t.Errorf("%v wurde akzeptiert", tc)
		}
	}
	// In einem echten Pool (falls vorhanden) geht es, und der Ordner erscheint in der Liste.
	if pools, _ := os.ReadDir("/srv/pools"); len(pools) > 0 {
		pool := "/srv/pools/" + pools[0].Name()
		if _, err := CreateFolder(ctx, d, pool, "bnasit-pool-test"); err != nil {
			t.Fatalf("im Pool %s: %v", pool, err)
		}
		t.Cleanup(func() { _ = os.Remove(pool + "/bnasit-pool-test") })
		l, err := ListFolders(ctx, d, pool)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, x := range l.Dirs {
			found = found || x.Name == "bnasit-pool-test"
			if x.Name == "lost+found" {
				t.Fatal("lost+found sichtbar")
			}
		}
		if !found {
			t.Fatalf("neuer Ordner fehlt in der Liste: %+v", l)
		}
	}
}

// TestDockerProxy prüft den Reverse-Proxy mit echtem Caddy: Schreiben, Prüfen, Übernehmen, Starten, Neuladen,
// Abruf über die Domain (Caddys interne CA für .local) und Ablehnung einer kaputten Konfiguration.
func TestDockerProxy(t *testing.T) {
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
	if d.FS.Exists(composePath(docker.ProxyStack)) {
		t.Skip("es gibt schon einen Stack " + docker.ProxyStack)
	}
	sh := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	t.Cleanup(func() {
		if unit, err := RunStack(ctx, d, docker.ProxyStack, "down"); err == nil {
			for i := 0; i < 60; i++ {
				if st, _ := StackRunStatus(ctx, d, unit); st.State != "running" {
					break
				}
				time.Sleep(time.Second)
			}
		}
		_ = DeleteStack(ctx, d, docker.ProxyStack)
		_ = exec.Command("docker", "rm", "-f", "bnasprx-a", "bnasprx-b").Run()
		_ = os.RemoveAll(docker.ProxyDir)
	})

	// Zwei kleine Webserver als Ziele.
	for name, port := range map[string]string{"bnasprx-a": "18081", "bnasprx-b": "18082"} {
		sh("run", "-d", "--name", name, "-p", port+":80", "busybox:stable", "sh", "-c", "mkdir -p /www && echo antwort-von-"+name+" > /www/index.html && httpd -f -p 80 -h /www")
	}
	cfg := docker.ProxyConfig{Enabled: true, HTTPPort: 18080, HTTPSPort: 18443, Sites: []docker.ProxySite{
		{Domain: "a.nas.local", Port: 18081, Public: true},
		{Domain: "lan.nas.local", Port: 18081}, // nur Heimnetz; der Test kommt aus einem privaten Netz
	}}
	apply := func(c docker.ProxyConfig) {
		t.Helper()
		if err := ProxyWrite(ctx, d, c); err != nil {
			t.Fatal(err)
		}
		runStack := func(action string) {
			unit, err := RunStack(ctx, d, docker.ProxyStack, action)
			if err != nil {
				t.Fatalf("%s: %v", action, err)
			}
			waitRun(t, d, unit)
		}
		runStack("pull")
		if err := ProxyValidate(ctx, d); err != nil {
			t.Fatal(err)
		}
		if err := ProxyCommit(ctx, d); err != nil {
			t.Fatal(err)
		}
		runStack("up")
		var err error
		for i := 0; i < 10; i++ {
			if err = ProxyReload(ctx, d); err == nil {
				break
			}
			time.Sleep(2 * time.Second)
		}
		if err != nil {
			t.Fatalf("Reload: %v", err)
		}
	}
	fetch := func(domain, port string) string {
		var last string
		for i := 0; i < 30; i++ {
			out, _ := exec.Command("curl", "-sk", "-m", "5", "--resolve", domain+":"+port+":127.0.0.1", "https://"+domain+":"+port+"/").CombinedOutput()
			last = strings.TrimSpace(string(out))
			if last != "" {
				return last
			}
			time.Sleep(time.Second)
		}
		return last
	}

	apply(cfg)
	if got := fetch("a.nas.local", "18443"); got != "antwort-von-bnasprx-a" {
		t.Fatalf("a.nas.local: %q", got)
	}
	if got := fetch("lan.nas.local", "18443"); got != "antwort-von-bnasprx-a" {
		t.Fatalf("lan.nas.local aus dem Heimnetz: %q", got)
	}
	// Eine zweite Seite per Neuladen (ohne den Container neu zu erstellen).
	idBefore := sh("ps", "-q", "--filter", "name="+docker.ProxyContainer)
	cfg.Sites = append(cfg.Sites, docker.ProxySite{Domain: "b.nas.local", Port: 18082, Public: true})
	apply(cfg)
	if got := fetch("b.nas.local", "18443"); got != "antwort-von-bnasprx-b" {
		t.Fatalf("b.nas.local nach Neuladen: %q", got)
	}
	if after := sh("ps", "-q", "--filter", "name="+docker.ProxyContainer); after != idBefore {
		t.Fatalf("Container wurde bei reiner Seitenänderung neu erstellt: %s → %s", idBefore, after)
	}
	if got := fetch("a.nas.local", "18443"); got != "antwort-von-bnasprx-a" {
		t.Fatalf("a.nas.local nach Neuladen: %q", got)
	}

	// Eine kaputte Konfiguration lehnt Caddy ab, die laufende bleibt.
	if err := d.FS.WriteFile(proxyNew, []byte("a.nas.local {\n\tunbekannte_direktive 1\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ProxyValidate(ctx, d)
	if err == nil || !strings.Contains(err.Error(), "Caddy lehnt") {
		t.Fatalf("kaputte Konfiguration akzeptiert: %v", err)
	}
	t.Logf("Caddy-Meldung: %v", err)
	if d.FS.Exists(proxyNew) {
		t.Fatal("abgelehnte Datei bleibt liegen")
	}
	if got := fetch("a.nas.local", "18443"); got != "antwort-von-bnasprx-a" {
		t.Fatalf("Proxy nach abgelehnter Konfiguration: %q", got)
	}
}
