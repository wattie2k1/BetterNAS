// Package jobs führt langlaufende Aktionen im Hintergrund aus und hält ihren Zustand fest.
package jobs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/events"
)

// Status eines Jobs.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

// Job ist ein Hintergrundauftrag.
type Job struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Target     string     `json:"target"`
	Status     Status     `json:"status"`
	Progress   int        `json:"progress"`
	Log        string     `json:"log"`
	StartedBy  string     `json:"startedBy"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// Logger nimmt Protokollzeilen und Fortschritt eines Jobs entgegen.
type Logger interface {
	Printf(format string, a ...any)
	SetProgress(p int)
}

// Manager startet und speichert Jobs.
type Manager struct {
	db  *sql.DB
	hub *events.Hub
	mu  sync.Mutex // serialisiert Schreibzugriffe auf laufende Jobs
}

// NewManager erzeugt einen Manager.
func NewManager(d *sql.DB, hub *events.Hub) *Manager { return &Manager{db: d, hub: hub} }

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

const tsFormat = time.RFC3339Nano

type jobLogger struct {
	m   *Manager
	job *Job
}

func (l *jobLogger) Printf(format string, a ...any) {
	l.m.mu.Lock()
	l.job.Log += fmt.Sprintf(format, a...) + "\n"
	l.m.mu.Unlock()
	l.m.save(l.job)
}

func (l *jobLogger) SetProgress(p int) {
	l.m.mu.Lock()
	l.job.Progress = max(0, min(p, 100))
	l.m.mu.Unlock()
	l.m.save(l.job)
}

func (m *Manager) save(j *Job) {
	m.mu.Lock()
	snap := *j
	m.mu.Unlock()
	var fin any
	if snap.FinishedAt != nil {
		fin = snap.FinishedAt.UTC().Format(tsFormat)
	}
	_, err := m.db.Exec(`INSERT INTO jobs(id, kind, target, status, progress, log, started_by, started_at, finished_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status, progress=excluded.progress, log=excluded.log, finished_at=excluded.finished_at`,
		snap.ID, snap.Kind, snap.Target, string(snap.Status), snap.Progress, snap.Log, snap.StartedBy,
		snap.StartedAt.UTC().Format(tsFormat), fin)
	if err != nil {
		slog.Error("jobs: speichern fehlgeschlagen", "id", snap.ID, "err", err)
	}
	m.hub.Publish(events.Message{Type: "job.updated", Data: snap})
}

// Start führt fn in einer Goroutine aus und liefert die Job-ID.
func (m *Manager) Start(kind, target, actor string, fn func(ctx context.Context, log Logger) error) string {
	j := &Job{ID: newID(), Kind: kind, Target: target, Status: StatusRunning, StartedBy: actor, StartedAt: time.Now()}
	m.save(j)
	go func() {
		l := &jobLogger{m: m, job: j}
		err := fn(context.Background(), l)
		now := time.Now()
		m.mu.Lock()
		j.FinishedAt = &now
		if err != nil {
			j.Status = StatusFailed
			var ae *apierr.Error
			if errors.As(err, &ae) {
				j.Log += "Fehler: " + ae.Message + "\n"
			} else {
				j.Log += "Fehler: " + err.Error() + "\n"
			}
		} else {
			j.Status = StatusSucceeded
			j.Progress = 100
		}
		m.mu.Unlock()
		m.save(j)
	}()
	return j.ID
}

func scanJob(sc interface{ Scan(...any) error }) (Job, error) {
	var j Job
	var status, started string
	var fin sql.NullString
	if err := sc.Scan(&j.ID, &j.Kind, &j.Target, &status, &j.Progress, &j.Log, &j.StartedBy, &started, &fin); err != nil {
		return Job{}, err
	}
	j.Status = Status(status)
	j.StartedAt, _ = time.Parse(tsFormat, started)
	if fin.Valid {
		t, _ := time.Parse(tsFormat, fin.String)
		j.FinishedAt = &t
	}
	return j, nil
}

const cols = `id, kind, target, status, progress, log, started_by, started_at, finished_at`

// Get liefert einen Job.
func (m *Manager) Get(ctx context.Context, id string) (Job, error) {
	j, err := scanJob(m.db.QueryRowContext(ctx, `SELECT `+cols+` FROM jobs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, apierr.New(apierr.NotFound, "Auftrag nicht gefunden")
	}
	return j, err
}

// List liefert die neuesten Jobs zuerst.
func (m *Manager) List(ctx context.Context, limit int) ([]Job, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT `+cols+` FROM jobs ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// MarkInterrupted markiert beim Start alle noch laufenden Jobs als fehlgeschlagen.
func (m *Manager) MarkInterrupted(ctx context.Context) error {
	now := time.Now().UTC().Format(tsFormat)
	_, err := m.db.ExecContext(ctx, `UPDATE jobs SET status = ?, finished_at = ?, log = log || ?
		WHERE status IN (?, ?)`, string(StatusFailed), now, "Durch Neustart unterbrochen\n",
		string(StatusQueued), string(StatusRunning))
	return err
}
