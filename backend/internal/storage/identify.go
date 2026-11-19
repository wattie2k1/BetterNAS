package storage

import (
	"context"
	"log/slog"
	"time"

	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
)

// StartIdentify lässt die Aktivitäts-LED einer Platte eine Weile blinken (Lese-Impulse mit Pausen), damit man sie im Gehäuse findet.
// Der erste Impuls läuft sofort und meldet Fehler; ist die Platte schon dabei, passiert nichts.
func (m *Manager) StartIdentify(ctx context.Context, actor, id string) error {
	d, err := m.liveDisk(ctx, id)
	if err != nil {
		return err
	}
	m.tmu.Lock()
	_, busy := m.identifying[id]
	m.tmu.Unlock()
	if busy {
		return nil
	}
	err = m.agent.Call(ctx, "disk.identify", map[string]string{"device": d.Device}, nil)
	result := "ok"
	if err != nil {
		result = "error"
	}
	_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: "disk.identify", Target: id, Result: result})
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithTimeout(context.Background(), m.identifyFor)
	m.tmu.Lock()
	m.identifying[id] = cancel
	m.tmu.Unlock()
	m.hub.Publish(events.Message{Type: "disks.changed"})
	go func() {
		defer func() {
			cancel()
			m.tmu.Lock()
			delete(m.identifying, id)
			m.tmu.Unlock()
			m.hub.Publish(events.Message{Type: "disks.changed"})
		}()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-time.After(m.identifyGap):
			}
			if err := m.agent.Call(runCtx, "disk.identify", map[string]string{"device": d.Device}, nil); err != nil && runCtx.Err() == nil {
				slog.Warn("platte identifizieren", "disk", id, "err", err)
				return
			}
		}
	}()
	return nil
}

// StopIdentify beendet das Blinken vorzeitig.
func (m *Manager) StopIdentify(id string) {
	m.tmu.Lock()
	cancel := m.identifying[id]
	m.tmu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *Manager) isIdentifying(id string) bool {
	m.tmu.Lock()
	defer m.tmu.Unlock()
	_, ok := m.identifying[id]
	return ok
}
