package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/sysexec"
)

func dockerEnv(t *testing.T) (Deps, *sysexec.FakeRunner, string) {
	t.Helper()
	root := t.TempDir()
	f := &sysexec.FakeRunner{Responses: map[string]sysexec.Result{}}
	nowUnix = func() int64 { return 1700000000 }
	t.Cleanup(func() { nowUnix = defaultNowUnix })
	return Deps{Runner: f, FS: sysexec.NewRootFS(root)}, f, root
}

func saveStack(t *testing.T, d Deps, name string) {
	t.Helper()
	if err := SaveStack(context.Background(), d, StackSave{StackFiles: StackFiles{Name: name, Compose: "services: {}\n", Env: "A=1\n"}, Create: true}); err != nil {
		t.Fatal(err)
	}
}

func TestStackNameValidationInAgent(t *testing.T) {
	d, _, _ := dockerEnv(t)
	ctx := context.Background()
	for _, bad := range []string{"../x", "A", "a b", "", "a/b"} {
		if code(SaveStack(ctx, d, StackSave{StackFiles: StackFiles{Name: bad, Compose: "x: 1"}})) != apierr.InvalidParams {
			t.Errorf("Save %q", bad)
		}
		if _, err := GetStack(ctx, d, bad); code(err) != apierr.InvalidParams {
			t.Errorf("Get %q", bad)
		}
		if _, err := RunStack(ctx, d, bad, "up"); code(err) != apierr.InvalidParams {
			t.Errorf("Run %q", bad)
		}
		if code(DeleteStack(ctx, d, bad)) != apierr.InvalidParams {
			t.Errorf("Delete %q", bad)
		}
	}
}

func TestSaveAndGetStackWithMode0600(t *testing.T) {
	d, _, root := dockerEnv(t)
	saveStack(t, d, "media")
	sf, err := GetStack(context.Background(), d, "media")
	if err != nil || sf.Compose != "services: {}\n" || sf.Env != "A=1\n" {
		t.Fatalf("%+v %v", sf, err)
	}
	for _, f := range []string{"compose.yaml", ".env"} {
		st, err := os.Stat(filepath.Join(root, "srv/appdata/stacks/media", f))
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v %v", f, st, err)
		}
	}
	st, _ := os.Stat(filepath.Join(root, "srv/appdata/stacks/media"))
	if st.Mode().Perm() != 0o700 {
		t.Errorf("Verzeichnis: %v", st.Mode().Perm())
	}
}

func TestSaveStackConflictAndLimits(t *testing.T) {
	d, _, _ := dockerEnv(t)
	ctx := context.Background()
	saveStack(t, d, "media")
	if code(SaveStack(ctx, d, StackSave{StackFiles: StackFiles{Name: "media", Compose: "x: 1"}, Create: true})) != apierr.Conflict {
		t.Error("Doppelter Name muss Konflikt sein")
	}
	big := strings.Repeat("a", 256<<10+1)
	if code(SaveStack(ctx, d, StackSave{StackFiles: StackFiles{Name: "big", Compose: big}})) != apierr.InvalidParams {
		t.Error("Zu große Datei")
	}
	if code(SaveStack(ctx, d, StackSave{StackFiles: StackFiles{Name: "empty", Compose: "  \n"}})) != apierr.InvalidParams {
		t.Error("Leere Datei")
	}
	// Leeres .env entfernt die Datei.
	if err := SaveStack(ctx, d, StackSave{StackFiles: StackFiles{Name: "media", Compose: "x: 1"}}); err != nil {
		t.Fatal(err)
	}
	if sf, _ := GetStack(ctx, d, "media"); sf.Env != "" {
		t.Error(".env sollte weg sein")
	}
}

func TestValidateStackReportsComposeError(t *testing.T) {
	d, f, _ := dockerEnv(t)
	saveStack(t, d, "media")
	f.Responses["docker compose -p media"] = sysexec.Result{ExitCode: 1, Stderr: []byte("services.web.image must be a string\n")}
	chk, err := ValidateStack(context.Background(), d, "media")
	if err != nil || chk.OK || !strings.Contains(chk.Message, "must be a string") {
		t.Fatalf("%+v %v", chk, err)
	}
	if !f.Called("docker compose -p media --project-directory /srv/appdata/stacks/media -f /srv/appdata/stacks/media/compose.yaml config -q") {
		t.Fatalf("%v", f.Calls)
	}
}

func TestListStacksIgnoresDirsWithoutCompose(t *testing.T) {
	d, f, root := dockerEnv(t)
	saveStack(t, d, "media")
	_ = os.MkdirAll(filepath.Join(root, "srv/appdata/stacks/leer"), 0o700)
	f.Responses["docker compose ls"] = sysexec.Result{Stdout: []byte(`[{"Name":"media","Status":"running(2), exited(1)"}]`)}
	list, err := ListStacks(context.Background(), d)
	if err != nil || len(list) != 1 || list[0].Name != "media" || list[0].Running != 2 || list[0].Total != 3 {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestDeleteStackOnlyOwnDir(t *testing.T) {
	d, f, root := dockerEnv(t)
	saveStack(t, d, "media")
	saveStack(t, d, "other")
	if err := DeleteStack(context.Background(), d, "media"); err != nil {
		t.Fatal(err)
	}
	if !f.Called("docker compose -p media --project-directory /srv/appdata/stacks/media -f /srv/appdata/stacks/media/compose.yaml down --remove-orphans") {
		t.Fatalf("%v", f.Calls)
	}
	if _, err := os.Stat(filepath.Join(root, "srv/appdata/stacks/media")); !os.IsNotExist(err) {
		t.Error("Verzeichnis sollte weg sein")
	}
	if _, err := os.Stat(filepath.Join(root, "srv/appdata/stacks/other/compose.yaml")); err != nil {
		t.Error("anderer Stack darf nicht betroffen sein")
	}
	if code(DeleteStack(context.Background(), d, "media")) != apierr.NotFound {
		t.Error("zweites Löschen")
	}
}

func TestRunStackBuildsSystemdRun(t *testing.T) {
	d, f, _ := dockerEnv(t)
	saveStack(t, d, "media")
	unit, err := RunStack(context.Background(), d, "media", "up")
	if err != nil || unit != "betternas-stack-media-1700000000" {
		t.Fatalf("%q %v", unit, err)
	}
	want := "systemd-run --unit=betternas-stack-media-1700000000 --no-block --description=BetterNAS Stack media (up) --property=RemainAfterExit=yes docker compose -p media --project-directory /srv/appdata/stacks/media -f /srv/appdata/stacks/media/compose.yaml up -d --remove-orphans"
	if !f.Called(want) {
		t.Fatalf("%v", f.Calls)
	}
	assertOrder(t, f, "systemctl list-units", "docker compose -p media", "systemd-run")
}

func TestRunStackUpdateChainsPullAndUp(t *testing.T) {
	d, f, _ := dockerEnv(t)
	saveStack(t, d, "media")
	if _, err := RunStack(context.Background(), d, "media", "update"); err != nil {
		t.Fatal(err)
	}
	i := f.Index("systemd-run")
	if i < 0 || !strings.Contains(f.Calls[i], "sh -c 'docker' 'compose' '-p' 'media'") || !strings.Contains(f.Calls[i], "'pull' && 'docker'") || !strings.Contains(f.Calls[i], "'up' '-d' '--remove-orphans'") {
		t.Fatalf("%v", f.Calls)
	}
}

func TestRunStackRefusesWhenRunning(t *testing.T) {
	d, f, _ := dockerEnv(t)
	saveStack(t, d, "media")
	f.Responses["systemctl list-units"] = sysexec.Result{Stdout: []byte("betternas-stack-media-1699999999.service loaded active running BetterNAS Stack\n")}
	if _, err := RunStack(context.Background(), d, "media", "up"); code(err) != apierr.Conflict {
		t.Fatalf("%v", err)
	}
	if f.Called("systemd-run") {
		t.Fatal("kein zweiter Lauf")
	}
}

func TestRunStackIgnoresRunUnitOfPrefixStack(t *testing.T) {
	d, f, _ := dockerEnv(t)
	saveStack(t, d, "a")
	f.Responses["systemctl list-units"] = sysexec.Result{Stdout: []byte("betternas-stack-a-b-1699999999.service loaded active running x\n")}
	if _, err := RunStack(context.Background(), d, "a", "stop"); err != nil {
		t.Fatalf("Stack a-b darf Stack a nicht blockieren: %v", err)
	}
}

func TestDeployRefusesInvalidCompose(t *testing.T) {
	d, f, _ := dockerEnv(t)
	saveStack(t, d, "media")
	f.Responses["docker compose -p media"] = sysexec.Result{ExitCode: 1, Stderr: []byte("kaputt")}
	if _, err := RunStack(context.Background(), d, "media", "up"); code(err) != apierr.InvalidParams {
		t.Fatalf("%v", err)
	}
	if f.Called("systemd-run") {
		t.Fatal("ungültige Datei darf nicht deployt werden")
	}
	// Entwurf bleibt erhalten.
	if sf, err := GetStack(context.Background(), d, "media"); err != nil || sf.Compose == "" {
		t.Fatal("Entwurf verloren")
	}
	// Stopp ist trotzdem möglich (keine Validierung nötig).
	f.Responses = map[string]sysexec.Result{}
	if _, err := RunStack(context.Background(), d, "media", "down"); err != nil {
		t.Fatal(err)
	}
}

func TestRunStackRejectsUnknownAction(t *testing.T) {
	d, _, _ := dockerEnv(t)
	saveStack(t, d, "media")
	if _, err := RunStack(context.Background(), d, "media", "exec"); code(err) != apierr.InvalidParams {
		t.Fatalf("%v", err)
	}
	if _, err := RunStack(context.Background(), d, "nope", "up"); code(err) != apierr.NotFound {
		t.Fatalf("%v", err)
	}
}

func TestStackRunStatus(t *testing.T) {
	d, f, _ := dockerEnv(t)
	ctx := context.Background()
	if _, err := StackRunStatus(ctx, d, "sshd"); code(err) != apierr.InvalidParams {
		t.Fatal(err)
	}
	f.Responses["systemctl show"] = sysexec.Result{Stdout: []byte("ActiveState=failed\nSubState=failed\nResult=exit-code\n")}
	f.Responses["journalctl"] = sysexec.Result{Stdout: []byte("Pulling…\n")}
	st, err := StackRunStatus(ctx, d, "betternas-stack-media-1700000000")
	if err != nil || st.State != "failed" || st.Log != "Pulling…" {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestContainerActionValidation(t *testing.T) {
	d, f, _ := dockerEnv(t)
	ctx := context.Background()
	if code(ContainerAction(ctx, d, "../x", "stop")) != apierr.InvalidParams || code(ContainerAction(ctx, d, "--all", "stop")) != apierr.InvalidParams {
		t.Error("ID")
	}
	if code(ContainerAction(ctx, d, "0123456789ab", "exec")) != apierr.InvalidParams {
		t.Error("Aktion")
	}
	if err := ContainerAction(ctx, d, "0123456789ab", "remove"); err != nil || !f.Called("docker rm -f 0123456789ab") {
		t.Fatalf("%v %v", err, f.Calls)
	}
	if err := ContainerAction(ctx, d, "0123456789ab", "restart"); err != nil || !f.Called("docker restart 0123456789ab") {
		t.Fatalf("%v %v", err, f.Calls)
	}
}

func TestListContainers(t *testing.T) {
	d, f, _ := dockerEnv(t)
	f.Responses["docker ps"] = sysexec.Result{Stdout: []byte(`{"ID":"aaaaaaaaaaaa","Names":"web","Image":"nginx","State":"running","Status":"Up","Ports":"","Labels":"com.docker.compose.project=media","CreatedAt":"x"}` + "\n")}
	list, err := ListContainers(context.Background(), d)
	if err != nil || len(list) != 1 || list[0].Stack != "media" {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestDockerStatus(t *testing.T) {
	d, f, _ := dockerEnv(t)
	f.Responses["docker --version"] = sysexec.Result{ExitCode: 127}
	if st, _ := DockerStatus(context.Background(), d); st.Installed {
		t.Fatal("nicht installiert")
	}
	f.Responses["docker --version"] = sysexec.Result{Stdout: []byte("Docker version 26")}
	f.Responses["systemctl is-active docker.service"] = sysexec.Result{Stdout: []byte("active\n")}
	f.Responses["docker version"] = sysexec.Result{Stdout: []byte("26.1.5\n")}
	st, _ := DockerStatus(context.Background(), d)
	if !st.Installed || !st.Active || st.Version != "26.1.5" {
		t.Fatalf("%+v", st)
	}
}
