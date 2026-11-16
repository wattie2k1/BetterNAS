package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/sysexec"
)

func TestStackImagesDedupesAndSorts(t *testing.T) {
	d, f, _ := dockerEnv(t)
	saveStack(t, d, "media")
	f.Responses["docker compose -p media"] = sysexec.Result{Stdout: []byte("nginx:stable\nghcr.io/x/y:1\nnginx:stable\n-bad\n\n")}
	got, err := StackImages(context.Background(), d, "media")
	if err != nil || strings.Join(got, ",") != "ghcr.io/x/y:1,nginx:stable" {
		t.Fatalf("%v %v", got, err)
	}
	if !f.Called("docker compose -p media --project-directory /srv/appdata/stacks/media -f /srv/appdata/stacks/media/compose.yaml config --images") {
		t.Fatalf("%v", f.Calls)
	}
}

func TestStackImagesErrors(t *testing.T) {
	d, f, _ := dockerEnv(t)
	if _, err := StackImages(context.Background(), d, "../x"); code(err) != apierr.InvalidParams {
		t.Fatal(err)
	}
	if _, err := StackImages(context.Background(), d, "nope"); code(err) != apierr.NotFound {
		t.Fatal(err)
	}
	saveStack(t, d, "media")
	f.Responses["docker compose -p media"] = sysexec.Result{ExitCode: 1, Stderr: []byte("kaputt")}
	if _, err := StackImages(context.Background(), d, "media"); code(err) != apierr.InvalidParams {
		t.Fatal(err)
	}
}

func digestEnv(f *sysexec.FakeRunner, ref, repoDigests, remote string, remoteExit int) {
	f.Responses["docker image inspect --format {{json .RepoDigests}} "+ref] = sysexec.Result{Stdout: []byte(repoDigests)}
	body := ""
	if remote != "" {
		body = `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","digest":"` + strings.TrimSpace(remote) + `","size":1}`
	}
	f.Responses["docker buildx imagetools inspect "+ref+" --format {{json .Manifest}}"] = sysexec.Result{Stdout: []byte(body), ExitCode: remoteExit}
}

func TestImageDigests(t *testing.T) {
	d, f, _ := dockerEnv(t)
	digestEnv(f, "a:1", `["docker.io/library/a@sha256:old"]`, "sha256:new\n", 0)
	digestEnv(f, "b:1", `["docker.io/library/b@sha256:same"]`, "sha256:same\n", 0)
	digestEnv(f, "c:1", `["docker.io/library/c@sha256:x"]`, "", 1)
	digestEnv(f, "d:1", `[]`, "sha256:y", 0)
	f.Responses["docker image inspect --format {{json .RepoDigests}} e:1"] = sysexec.Result{ExitCode: 1}
	got, err := ImageDigests(context.Background(), d, []string{"a:1", "b:1", "c:1", "d:1", "e:1"})
	if err != nil || len(got) != 5 {
		t.Fatalf("%v %v", got, err)
	}
	if !got[0].Outdated || got[0].Local != "sha256:old" || got[0].Remote != "sha256:new" {
		t.Errorf("a: %+v", got[0])
	}
	if got[1].Outdated || got[1].Error != "" {
		t.Errorf("b: %+v", got[1])
	}
	for i, name := range []string{"c", "d", "e"} {
		if got[2+i].Outdated || got[2+i].Error == "" {
			t.Errorf("%s: %+v (Fehler darf nie als veraltet gelten)", name, got[2+i])
		}
	}
}

func TestImageDigestsValidation(t *testing.T) {
	d, f, _ := dockerEnv(t)
	for _, bad := range []string{"--all", "a b", "", "x;rm"} {
		if _, err := ImageDigests(context.Background(), d, []string{"ok:1", bad}); code(err) != apierr.InvalidParams {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if len(f.Calls) != 0 {
		t.Fatalf("bei ungültiger Eingabe darf nichts laufen: %v", f.Calls)
	}
	if _, err := ImageDigests(context.Background(), d, make([]string, 101)); code(err) != apierr.InvalidParams {
		t.Fatal("Limit")
	}
}
