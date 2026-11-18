package agent

import (
	"context"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/sysexec"
)

func TestListVolumesMarksUnused(t *testing.T) {
	d, f, _ := dockerEnv(t)
	f.Responses["docker volume ls --format {{json .}}"] = sysexec.Result{Stdout: []byte(`{"Name":"data","Driver":"local"}` + "\n" + `{"Name":"old","Driver":"local"}` + "\n")}
	f.Responses["docker volume ls --filter dangling=true"] = sysexec.Result{Stdout: []byte("old\n")}
	list, err := ListVolumes(context.Background(), d)
	if err != nil || len(list) != 2 || list[0].Unused || !list[1].Unused {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestListImagesAndNetworks(t *testing.T) {
	d, f, _ := dockerEnv(t)
	f.Responses["docker image ls"] = sysexec.Result{Stdout: []byte(`{"ID":"aaaaaaaaaaaa","Repository":"nginx","Tag":"stable","Size":"1MB","CreatedSince":"x"}` + "\n")}
	f.Responses["docker network ls"] = sysexec.Result{Stdout: []byte(`{"ID":"111111111111","Name":"bridge","Driver":"bridge","Scope":"local"}` + "\n")}
	if ims, err := ListImages(context.Background(), d); err != nil || len(ims) != 1 || ims[0].Ref != "nginx:stable" {
		t.Fatalf("%+v %v", ims, err)
	}
	if nets, err := ListNetworks(context.Background(), d); err != nil || len(nets) != 1 || !nets[0].Builtin {
		t.Fatalf("%+v %v", nets, err)
	}
}

func TestListImagesAndNetworksMarkUnused(t *testing.T) {
	d, f, _ := dockerEnv(t)
	f.Responses["docker image ls"] = sysexec.Result{Stdout: []byte(`{"ID":"aaaaaaaaaaaa","Repository":"nginx","Tag":"stable","Size":"1MB","CreatedSince":"x"}` + "\n" + `{"ID":"bbbbbbbbbbbb","Repository":"old","Tag":"1","Size":"1MB","CreatedSince":"x"}` + "\n")}
	f.Responses["docker container ls -aq"] = sysexec.Result{Stdout: []byte("c1\n")}
	f.Responses["docker inspect --format {{.Image}} c1"] = sysexec.Result{Stdout: []byte("sha256:aaaaaaaaaaaa1234567890\n")}
	f.Responses["docker network ls"] = sysexec.Result{Stdout: []byte(`{"ID":"111111111111","Name":"bridge","Driver":"bridge","Scope":"local"}` + "\n" + `{"ID":"222222222222","Name":"media_default","Driver":"bridge","Scope":"local"}` + "\n" + `{"ID":"333333333333","Name":"leer","Driver":"bridge","Scope":"local"}` + "\n")}
	f.Responses["docker ps -a --format {{.Networks}}"] = sysexec.Result{Stdout: []byte("media_default,other\n")}
	ims, err := ListImages(context.Background(), d)
	if err != nil || len(ims) != 2 || ims[0].Unused || !ims[1].Unused {
		t.Fatalf("%+v %v", ims, err)
	}
	nets, err := ListNetworks(context.Background(), d)
	if err != nil || len(nets) != 3 {
		t.Fatalf("%+v %v", nets, err)
	}
	for _, n := range nets {
		if want := n.Name == "leer"; n.Unused != want {
			t.Errorf("%s unused=%v", n.Name, n.Unused)
		}
	}
}

func TestRemoveResourceValidationAndArgs(t *testing.T) {
	d, f, _ := dockerEnv(t)
	ctx := context.Background()
	bad := [][2]string{{"image", "--all"}, {"image", "a b"}, {"network", "bridge"}, {"network", "../x"}, {"volume", "../x"}, {"volume", "-v"}, {"container", "abc"}, {"", "x"}}
	for _, b := range bad {
		if code(RemoveResource(ctx, d, b[0], b[1])) != apierr.InvalidParams {
			t.Errorf("%v sollte abgelehnt werden", b)
		}
	}
	if len(f.Calls) != 0 {
		t.Fatalf("ungültige Eingaben dürfen nichts ausführen: %v", f.Calls)
	}
	if err := RemoveResource(ctx, d, "image", "nginx:stable"); err != nil || !f.Called("docker image rm nginx:stable") {
		t.Fatalf("%v %v", err, f.Calls)
	}
	if err := RemoveResource(ctx, d, "volume", "media_data"); err != nil || !f.Called("docker volume rm media_data") {
		t.Fatalf("%v %v", err, f.Calls)
	}
	if err := RemoveResource(ctx, d, "network", "333333333333"); err != nil || !f.Called("docker network rm 333333333333") {
		t.Fatalf("%v %v", err, f.Calls)
	}
}

func TestBuiltinNetworkCannotBeRemoved(t *testing.T) {
	d, f, _ := dockerEnv(t)
	f.Responses["docker network ls"] = sysexec.Result{Stdout: []byte(`{"ID":"222222222222abcdef","Name":"bridge","Driver":"bridge","Scope":"local"}` + "\n")}
	if code(RemoveResource(context.Background(), d, "network", "222222222222")) != apierr.Conflict {
		t.Fatal("bridge darf nicht entfernt werden")
	}
	if f.Called("docker network rm") {
		t.Fatal("rm darf nicht laufen")
	}
}

func TestRemoveResourceReportsDockerRefusal(t *testing.T) {
	d, f, _ := dockerEnv(t)
	f.Responses["docker volume rm"] = sysexec.Result{ExitCode: 1, Stderr: []byte("volume is in use")}
	err := RemoveResource(context.Background(), d, "volume", "data")
	if code(err) != apierr.Conflict || apierr.From(err).Message != "Entfernen nicht möglich: volume is in use" {
		t.Fatalf("%v", err)
	}
}

func TestPruneResources(t *testing.T) {
	d, f, _ := dockerEnv(t)
	ctx := context.Background()
	f.Responses["docker image prune"] = sysexec.Result{Stdout: []byte("Total reclaimed space: 1.5GB\n")}
	res, err := PruneResources(ctx, d, "image", false)
	if err != nil || res.Reclaimed != "1.5GB" || !f.Called("docker image prune -f") {
		t.Fatalf("%+v %v %v", res, err, f.Calls)
	}
	if _, err := PruneResources(ctx, d, "image", true); err != nil || !f.Called("docker image prune -f -a") {
		t.Fatalf("%v %v", err, f.Calls)
	}
	if _, err := PruneResources(ctx, d, "volume", true); err != nil || !f.Called("docker volume prune -f -a") {
		t.Fatalf("%v %v", err, f.Calls)
	}
	if _, err := PruneResources(ctx, d, "network", true); err != nil || f.Called("docker network prune -f -a") || !f.Called("docker network prune -f") {
		t.Fatalf("Netzwerke kennen kein -a: %v %v", err, f.Calls)
	}
	if _, err := PruneResources(ctx, d, "container", false); code(err) != apierr.InvalidParams {
		t.Fatal(err)
	}
}
