package storage

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
)

// DiskNote sind die eigenen Angaben des Nutzers zu einer Platte (z. B. „Schacht 3“, Kaufdatum, Garantie).
type DiskNote struct {
	Name          string `json:"name"`
	Notes         string `json:"notes"`
	Purchased     string `json:"purchased"`     // YYYY-MM-DD oder leer
	WarrantyUntil string `json:"warrantyUntil"` // YYYY-MM-DD oder leer
}

const (
	maxNoteName  = 60
	maxNoteNotes = 1000
	dateLayout   = "2006-01-02"
)

func (n DiskNote) empty() bool { return n == DiskNote{} }

func (n DiskNote) normalize() (DiskNote, error) {
	n.Name, n.Notes = strings.TrimSpace(n.Name), strings.TrimSpace(n.Notes)
	n.Purchased, n.WarrantyUntil = strings.TrimSpace(n.Purchased), strings.TrimSpace(n.WarrantyUntil)
	if utf8.RuneCountInString(n.Name) > maxNoteName {
		return n, apierr.New(apierr.InvalidParams, "Die Bezeichnung darf höchstens 60 Zeichen lang sein.")
	}
	if utf8.RuneCountInString(n.Notes) > maxNoteNotes {
		return n, apierr.New(apierr.InvalidParams, "Die Notiz darf höchstens 1000 Zeichen lang sein.")
	}
	for _, d := range []string{n.Purchased, n.WarrantyUntil} {
		if d == "" {
			continue
		}
		if _, err := time.Parse(dateLayout, d); err != nil {
			return n, apierr.New(apierr.InvalidParams, "Datumsangaben müssen das Format JJJJ-MM-TT haben.")
		}
	}
	return n, nil
}

func (m *Manager) loadNotes(ctx context.Context) (map[string]DiskNote, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT disk_id, name, notes, purchased, warranty_until FROM disk_notes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]DiskNote{}
	for rows.Next() {
		var id string
		var n DiskNote
		if err := rows.Scan(&id, &n.Name, &n.Notes, &n.Purchased, &n.WarrantyUntil); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// SetNote speichert die Angaben zu einer erkannten oder bekannten (fehlenden) Platte; leere Angaben löschen sie.
func (m *Manager) SetNote(ctx context.Context, actor, id string, n DiskNote) (err error) {
	n, err = n.normalize()
	if err != nil {
		return err
	}
	if _, lerr := m.liveDisk(ctx, id); lerr != nil {
		known, kerr := m.loadDisks(ctx)
		if kerr != nil {
			return kerr
		}
		if _, ok := known[id]; !ok {
			return lerr
		}
	}
	defer func() {
		result := "ok"
		if err != nil {
			result = "error"
		}
		_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: "disk.note", Target: id, Result: result})
	}()
	if n.empty() {
		_, err = m.db.ExecContext(ctx, `DELETE FROM disk_notes WHERE disk_id = ?`, id)
	} else {
		_, err = m.db.ExecContext(ctx, `INSERT INTO disk_notes(disk_id, name, notes, purchased, warranty_until) VALUES (?,?,?,?,?)
			ON CONFLICT(disk_id) DO UPDATE SET name = excluded.name, notes = excluded.notes, purchased = excluded.purchased, warranty_until = excluded.warranty_until`,
			id, n.Name, n.Notes, n.Purchased, n.WarrantyUntil)
	}
	if err != nil {
		return err
	}
	m.hub.Publish(events.Message{Type: "disks.changed"})
	return nil
}
