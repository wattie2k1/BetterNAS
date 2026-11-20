package system

import "testing"

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
