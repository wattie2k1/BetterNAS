package storage

import (
	"context"
	"log/slog"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
)

// Erase ist der Zustand des Löschvorgangs einer Platte (nur im Speicher; nach einem Neustart des Servers weg).
type Erase struct {
	Running      bool   `json:"running"`
	Method       string `json:"method"` // zero | discard
	WrittenBytes uint64 `json:"writtenBytes"`
	TotalBytes   uint64 `json:"totalBytes"`
	Result       string `json:"result,omitempty"` // done | failed | aborted
	Error        string `json:"error,omitempty"`
}

// EraseRequest ist die Bestätigung des Nutzers.
type EraseRequest struct {
	Method        string `json:"method"` // zero | discard
	ConfirmSerial string `json:"confirmSerial"`
	ConfirmToken  string `json:"confirmToken"`
}

type agentErase struct {
	Known        bool   `json:"known"`
	Running      bool   `json:"running"`
	WrittenBytes uint64 `json:"writtenBytes"`
	TotalBytes   uint64 `json:"totalBytes"`
	Result       string `json:"result"`
	Error        string `json:"error"`
}

func (m *Manager) eraseView(id string) *Erase {
	m.tmu.Lock()
	defer m.tmu.Unlock()
	if e, ok := m.erases[id]; ok {
		c := *e
		return &c
	}
	return nil
}

// PreviewErase zeigt, was beim Löschen verloren geht, und gibt ein Bestätigungstoken aus. Nur für Platten, die BetterNAS nicht benutzt.
func (m *Manager) PreviewErase(ctx context.Context, id string) (FormatPreview, error) {
	return m.PreviewFormat(ctx, id)
}

// StartErase überschreibt eine Platte vollständig (zero) oder verwirft alle Blöcke (discard, nur SSD). Unwiderruflich.
func (m *Manager) StartErase(ctx context.Context, actor, id string, req EraseRequest) (err error) {
	if req.Method != "zero" && req.Method != "discard" {
		return apierr.New(apierr.InvalidParams, "Methode muss zero oder discard sein")
	}
	m.tmu.Lock()
	tok, ok := m.tokens[req.ConfirmToken]
	m.tmu.Unlock()
	if !ok || tok.diskID != id || m.now().After(tok.expires) {
		return apierr.New(apierr.Conflict, "Die Bestätigung ist abgelaufen. Bitte die Vorschau erneut öffnen.")
	}
	d, err := m.liveDisk(ctx, id)
	if err != nil {
		return err
	}
	if d.Device != tok.device || d.SizeBytes != tok.size {
		return apierr.New(apierr.Conflict, "Der Datenträger hat sich seit der Vorschau verändert. Bitte erneut prüfen.")
	}
	if req.ConfirmSerial != confirmValue(d) {
		return apierr.New(apierr.InvalidParams, "Die eingegebene Seriennummer stimmt nicht überein")
	}
	if err := m.checkUnassigned(ctx, d); err != nil {
		return err
	}
	if req.Method == "discard" && d.Rotational {
		return apierr.New(apierr.InvalidParams, "Verwerfen (TRIM) gibt es nur für SSDs; für Festplatten bitte Überschreiben wählen.")
	}
	m.tmu.Lock()
	for _, e := range m.erases {
		if e.Running {
			m.tmu.Unlock()
			return apierr.New(apierr.Conflict, "Es läuft bereits ein Löschvorgang.")
		}
	}
	m.tmu.Unlock()
	defer func() {
		result := "ok"
		if err != nil {
			result = "error"
		}
		_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: "disk.erase", Target: id, Result: result, Details: map[string]any{"device": d.Device, "method": req.Method}})
	}()
	if err = m.agent.Call(ctx, "disk.erase.start", map[string]any{"device": d.Device, "method": req.Method, "sizeBytes": d.SizeBytes}, nil); err != nil {
		return err
	}
	m.tmu.Lock()
	delete(m.tokens, req.ConfirmToken)
	m.erases[id] = &Erase{Running: true, Method: req.Method, TotalBytes: d.SizeBytes}
	m.tmu.Unlock()
	m.hub.Publish(events.Message{Type: "disks.changed"})
	go m.watchErase(id, d.Device, actor)
	return nil
}

func (m *Manager) watchErase(id, device, actor string) {
	set := func(f func(e *Erase)) {
		m.tmu.Lock()
		if e := m.erases[id]; e != nil {
			f(e)
		}
		m.tmu.Unlock()
		m.hub.Publish(events.Message{Type: "disks.changed"})
	}
	fails := 0
	for {
		time.Sleep(m.erasePoll)
		var s agentErase
		if err := m.agent.Call(context.Background(), "disk.erase.status", map[string]string{"device": device}, &s); err != nil {
			if fails++; fails < 5 {
				continue
			}
			set(func(e *Erase) { e.Running, e.Result, e.Error = false, "failed", "Der Agent war nicht erreichbar." })
			return
		}
		fails = 0
		switch {
		case !s.Known:
			set(func(e *Erase) {
				e.Running, e.Result, e.Error = false, "failed", "Der Vorgang wurde unterbrochen (der Agent wurde neu gestartet). Die Platte ist teilweise überschrieben."
			})
			return
		case s.Running:
			set(func(e *Erase) { e.WrittenBytes = s.WrittenBytes })
		default:
			set(func(e *Erase) { e.Running, e.WrittenBytes, e.Result, e.Error = false, s.WrittenBytes, s.Result, s.Error })
			slog.Info("platte gelöscht", "disk", id, "result", s.Result)
			_ = audit.Record(context.Background(), m.db, audit.Entry{Actor: actor, Action: "disk.erase.finished", Target: id, Result: s.Result})
			return
		}
	}
}

// AbortErase bricht einen laufenden Löschvorgang ab; die Platte bleibt teilweise überschrieben.
func (m *Manager) AbortErase(ctx context.Context, actor, id string) error {
	d, err := m.liveDisk(ctx, id)
	if err != nil {
		return err
	}
	if e := m.eraseView(id); e == nil || !e.Running {
		return apierr.New(apierr.Conflict, "Auf dieser Platte läuft kein Löschvorgang.")
	}
	_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: "disk.erase.abort", Target: id, Result: "ok"})
	return m.agent.Call(ctx, "disk.erase.abort", map[string]string{"device": d.Device}, nil)
}
