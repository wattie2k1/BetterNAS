package system

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/betternas/betternas/internal/agent"
)

// SampleInterval ist der Takt der Messungen; das Dashboard fragt im selben Takt ab.
const SampleInterval = 5 * time.Second

// SampleMax ist die Zahl der gehaltenen Messungen (bei 5 s Takt 5 Minuten).
const SampleMax = 60

// Sample ist eine Messung der Zähler, aus denen das Dashboard Auslastung und Datenraten errechnet.
type Sample struct {
	At                int64            `json:"at"` // Unix-Millisekunden
	CPUTotalTicks     uint64           `json:"cpuTotalTicks"`
	CPUIdleTicks      uint64           `json:"cpuIdleTicks"`
	MemTotalBytes     uint64           `json:"memTotalBytes"`
	MemAvailableBytes uint64           `json:"memAvailableBytes"`
	Net               []agent.NetIface `json:"net"`
}

// Caller ist der Teil des RPC-Clients, den der Sampler braucht.
type Caller interface {
	Call(ctx context.Context, method string, params, result any) error
}

// Sampler misst im Hintergrund regelmäßig die Systemzähler, damit das Dashboard den Verlauf
// schon beim Öffnen anzeigen kann und nicht erst nach den ersten eigenen Messungen.
type Sampler struct {
	mu      sync.Mutex
	samples []Sample
	db      *sql.DB
	now     func() time.Time
	prevNet map[string][2]uint64
}

func NewSampler() *Sampler { return &Sampler{} }

// History liefert die gehaltenen Messungen, älteste zuerst.
func (s *Sampler) History() []Sample {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Sample(nil), s.samples...)
}

func (s *Sampler) add(x Sample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = append(s.samples, x)
	if len(s.samples) > SampleMax {
		s.samples = append([]Sample(nil), s.samples[len(s.samples)-SampleMax:]...)
	}
}

// Run misst bis ctx endet; Fehler einzelner Messungen werden übersprungen.
func (s *Sampler) Run(ctx context.Context, c Caller) {
	t := time.NewTicker(SampleInterval)
	defer t.Stop()
	for {
		var info agent.Info
		cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		err := c.Call(cctx, "system.info", nil, &info)
		cancel()
		if err == nil {
			if aerr := s.account(ctx, info.Net); aerr != nil {
				slog.Warn("datenmenge speichern", "err", aerr)
			}
			s.add(Sample{At: time.Now().UnixMilli(), CPUTotalTicks: info.CPUTotalTicks, CPUIdleTicks: info.CPUIdleTicks,
				MemTotalBytes: info.MemTotalBytes, MemAvailableBytes: info.MemAvailableBytes, Net: info.Net})
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
