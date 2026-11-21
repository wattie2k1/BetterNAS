package system

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/agent"
	"github.com/betternas/betternas/internal/db"
)

func TestSamplerHaeltNurDieLetztenMessungen(t *testing.T) {
	s := NewSampler()
	if len(s.History()) != 0 {
		t.Fatal("neuer Sampler muss leer sein")
	}
	for i := 0; i < SampleMax+5; i++ {
		s.add(Sample{At: int64(i)})
	}
	h := s.History()
	if len(h) != SampleMax {
		t.Fatalf("erwartet %d, erhalten %d", SampleMax, len(h))
	}
	if h[0].At != 5 || h[len(h)-1].At != int64(SampleMax+4) {
		t.Fatalf("falsche Reihenfolge: %d..%d", h[0].At, h[len(h)-1].At)
	}
}

func TestTrafficSummiertZuwachsJeTagMonatJahr(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := NewSampler()
	s.SetStore(d)
	ctx := context.Background()
	at := time.Date(2026, 11, 20, 12, 0, 0, 0, time.Local)
	s.now = func() time.Time { return at }
	step := func(rx, tx uint64) {
		if err := s.account(ctx, []agent.NetIface{{Name: "eno1", RxBytes: rx, TxBytes: tx}, {Name: "eno2", RxBytes: 10, TxBytes: 10}}); err != nil {
			t.Fatal(err)
		}
	}
	step(1000, 500) // erste Messung zählt nicht
	step(3000, 900)
	step(100, 950) // Zähler zurückgesprungen: rx zählt nicht, tx schon
	at = at.AddDate(0, 0, 1)
	step(600, 1000)
	at = time.Date(2026, 12, 2, 0, 0, 0, 0, time.Local)
	step(1600, 1000)
	tr, err := s.Traffic(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Days) != 3 || tr.Days[0] != (TrafficEntry{"2026-11-20", 2000, 450}) || tr.Days[1] != (TrafficEntry{"2026-11-21", 500, 50}) || tr.Days[2] != (TrafficEntry{"2026-12-02", 1000, 0}) {
		t.Fatalf("tage: %+v", tr.Days)
	}
	if len(tr.Months) != 2 || tr.Months[0] != (TrafficEntry{"2026-11", 2500, 500}) || tr.Months[1].Key != "2026-12" {
		t.Fatalf("monate: %+v", tr.Months)
	}
	if len(tr.Years) != 1 || tr.Years[0] != (TrafficEntry{"2026", 3500, 500}) {
		t.Fatalf("jahre: %+v", tr.Years)
	}
}
