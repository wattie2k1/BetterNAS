package jobs

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/events"
)

func newManager(t *testing.T) (*Manager, *events.Hub) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	return NewManager(d, hub), hub
}

func waitDone(t *testing.T, m *Manager, id string) Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		j, err := m.Get(context.Background(), id)
		if err == nil && (j.Status == StatusSucceeded || j.Status == StatusFailed) {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return Job{}
}

func TestJobSucceeds(t *testing.T) {
	m, _ := newManager(t)
	id := m.Start("pool.create", "media", "anna", func(_ context.Context, l Logger) error {
		l.Printf("a")
		l.SetProgress(50)
		return nil
	})
	j := waitDone(t, m, id)
	if j.Status != StatusSucceeded || j.Progress != 100 || !strings.Contains(j.Log, "a") || j.FinishedAt == nil || j.StartedBy != "anna" {
		t.Fatalf("%+v", j)
	}
	list, _ := m.List(context.Background(), 10)
	if len(list) != 1 || list[0].ID != id {
		t.Fatalf("%+v", list)
	}
}

func TestJobFails(t *testing.T) {
	m, _ := newManager(t)
	id := m.Start("disk.format", "sdb", "anna", func(context.Context, Logger) error {
		return apierr.New(apierr.PoolBusy, "Pool wird verwendet")
	})
	j := waitDone(t, m, id)
	if j.Status != StatusFailed || !strings.Contains(j.Log, "Pool wird verwendet") {
		t.Fatalf("%+v", j)
	}
}

func TestJobInternalErrorLogged(t *testing.T) {
	m, _ := newManager(t)
	id := m.Start("x", "y", "anna", func(context.Context, Logger) error { return errors.New("mkfs kaputt") })
	if j := waitDone(t, m, id); !strings.Contains(j.Log, "mkfs kaputt") {
		t.Fatalf("%+v", j)
	}
}

func TestJobPublishesUpdates(t *testing.T) {
	m, hub := newManager(t)
	ch, cancel := hub.Subscribe()
	defer cancel()
	release := make(chan struct{})
	id := m.Start("x", "y", "anna", func(context.Context, Logger) error { <-release; return nil })
	close(release)
	seen := map[Status]bool{}
	timeout := time.After(3 * time.Second)
	for !seen[StatusSucceeded] {
		select {
		case msg := <-ch:
			if msg.Type == "job.updated" {
				j := msg.Data.(Job)
				if j.ID == id {
					seen[j.Status] = true
				}
			}
		case <-timeout:
			t.Fatalf("seen %v", seen)
		}
	}
	if !seen[StatusRunning] {
		t.Fatalf("running not published: %v", seen)
	}
}

func TestMarkInterrupted(t *testing.T) {
	m, _ := newManager(t)
	block := make(chan struct{})
	defer close(block)
	id := m.Start("x", "y", "anna", func(context.Context, Logger) error { <-block; return nil })
	time.Sleep(50 * time.Millisecond)
	if err := m.MarkInterrupted(context.Background()); err != nil {
		t.Fatal(err)
	}
	j, _ := m.Get(context.Background(), id)
	if j.Status != StatusFailed || !strings.Contains(j.Log, "Durch Neustart unterbrochen") {
		t.Fatalf("%+v", j)
	}
}

func TestGetUnknown(t *testing.T) {
	m, _ := newManager(t)
	if _, err := m.Get(context.Background(), "nope"); apierr.From(err).Code != apierr.NotFound {
		t.Fatal(err)
	}
}
