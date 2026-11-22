package monitor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/docker"
	"github.com/betternas/betternas/internal/storage"
)

type fakeDocker struct {
	status docker.Status
	list   []docker.Container
	err    error
}

func (f *fakeDocker) Status(context.Context) (docker.Status, error) { return f.status, nil }
func (f *fakeDocker) ListContainers(context.Context) ([]docker.Container, error) {
	return f.list, f.err
}

var up = docker.Status{Installed: true, Active: true}

func monWithDocker(t *testing.T, fd *fakeDocker) (*Monitor, *fakeNotifier) {
	t.Helper()
	m, n, _ := newMon(t, &fakeSource{disks: []storage.DiskView{healthyDisk("a")}})
	m.SetDocker(fd)
	return m, n
}

func TestDockerFindingsUnhealthyAndRestartLoop(t *testing.T) {
	fd := &fakeDocker{status: up, list: []docker.Container{
		{Name: "web", Stack: "media", State: "running", Health: "unhealthy", Status: "Up 1 hour (unhealthy)"},
		{Name: "db", State: "restarting", Status: "Restarting (1) 3 seconds ago"},
		{Name: "ok", State: "running", Health: "healthy"},
		{Name: "starting", State: "running", Health: "starting"},
		{Name: "stopped", State: "exited", Status: "Exited (137) 1 hour ago"}, // bewusst gestoppt
		{Name: "job", State: "exited", Status: "Exited (0) 1 hour ago"},
	}}
	m, _ := monWithDocker(t, fd)
	fs, err := m.Findings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := keys(fs)
	if len(got) != 2 || got["container-unhealthy:web"] != "warn" || got["container-restarting:db"] != "crit" {
		t.Fatalf("%v", got)
	}
}

func TestDockerFindingNeedsTwoConsecutiveChecks(t *testing.T) {
	fd := &fakeDocker{status: up, list: []docker.Container{{Name: "web", Stack: "media", State: "running", Health: "unhealthy"}}}
	m, n := monWithDocker(t, fd)
	ctx := context.Background()
	_ = m.Check(ctx)
	if n.count() != 0 {
		t.Fatalf("beim ersten Mal darf noch nichts gemeldet werden: %+v", n.sent)
	}
	_ = m.Check(ctx)
	_ = m.Check(ctx)
	if n.count() != 1 || !strings.Contains(n.sent[0].Title, "web") || !strings.Contains(n.sent[0].Body, "media") {
		t.Fatalf("%+v", n.sent)
	}
	fd.list = []docker.Container{{Name: "web", State: "running", Health: "healthy"}}
	_ = m.Check(ctx)
	if n.count() != 2 || !strings.HasPrefix(n.sent[1].Title, "Behoben:") {
		t.Fatalf("%+v", n.sent)
	}
}

func TestDockerBlipDoesNotAlert(t *testing.T) {
	bad := []docker.Container{{Name: "web", State: "restarting", Status: "Restarting (1) 1 second ago"}}
	good := []docker.Container{{Name: "web", State: "running"}}
	fd := &fakeDocker{status: up, list: bad}
	m, n := monWithDocker(t, fd)
	ctx := context.Background()
	for _, l := range [][]docker.Container{bad, good, bad, good} {
		fd.list = l
		_ = m.Check(ctx)
	}
	if n.count() != 0 {
		t.Fatalf("Zacken dürfen nicht melden: %+v", n.sent)
	}
}

func TestDockerUnavailableKeepsAlertsAndStaysQuiet(t *testing.T) {
	fd := &fakeDocker{status: up, list: []docker.Container{{Name: "web", State: "running", Health: "unhealthy"}}}
	m, n := monWithDocker(t, fd)
	ctx := context.Background()
	_, _ = m.Findings(ctx)
	_ = m.Check(ctx)
	_ = m.Check(ctx)
	if n.count() != 1 {
		t.Fatalf("%+v", n.sent)
	}
	// Docker nicht aktiv: kein Fehler, kein „Behoben“, Alarm bleibt bestehen.
	fd.status = docker.Status{Installed: true}
	if err := m.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if a, _ := m.Active(ctx); len(a) != 1 || n.count() != 1 {
		t.Fatalf("%+v %+v", a, n.sent)
	}
	// Docker aktiv, aber Liste nicht lesbar: Fehler wird gemeldet, Alarm bleibt.
	fd.status, fd.err = up, errors.New("boom")
	if err := m.Check(ctx); err == nil {
		t.Fatal("Fehler erwartet")
	}
	if a, _ := m.Active(ctx); len(a) != 1 || n.count() != 1 {
		t.Fatalf("%+v %+v", a, n.sent)
	}
}

func TestNoDockerSourceNoFindings(t *testing.T) {
	m, _, _ := newMon(t, &fakeSource{disks: []storage.DiskView{healthyDisk("a")}})
	if fs, err := m.Findings(context.Background()); err != nil || len(fs) != 0 {
		t.Fatalf("%v %v", fs, err)
	}
}

// Läuft Docker, ist aber noch nicht eingerichtet (docker_not_configured), ist das kein Fehler des Monitors.
func TestDockerNotConfiguredIsNoError(t *testing.T) {
	fd := &fakeDocker{status: up, err: apierr.New(apierr.DockerNotConfigured, "Docker ist noch nicht eingerichtet.")}
	m, n := monWithDocker(t, fd)
	if _, err := m.Findings(context.Background()); err != nil {
		t.Fatalf("Findings: %v", err)
	}
	if err := m.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if n.count() != 0 {
		t.Fatalf("es darf nichts gemeldet werden: %+v", n.sent)
	}
}
