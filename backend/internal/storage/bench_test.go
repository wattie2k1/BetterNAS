package storage

import "testing"

func benchOf(t *testing.T, e *env, id string) *Bench {
	t.Helper()
	ds, err := e.m.ListDisks(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ds {
		if d.ID == id {
			return d.Bench
		}
	}
	t.Fatalf("%s fehlt", id)
	return nil
}

func TestBenchRunsAndKeepsHistory(t *testing.T) {
	e := newEnv(t)
	if benchOf(t, e, "d1") != nil {
		t.Fatal("vorher kein Benchmark")
	}
	for i := 1; i <= 2; i++ {
		if err := e.m.StartBench(e.ctx, "admin", "d1"); err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool { b := benchOf(t, e, "d1"); return b != nil && !b.Running && len(b.History) == i })
	}
	b := benchOf(t, e, "d1")
	if b.Last == nil || b.Last.AvgMBps != 150 || b.Last.MinMBps != 100 || b.Last.MaxMBps != 200 || len(b.Last.Samples) != 3 {
		t.Fatalf("%+v", b.Last)
	}
	if benchOf(t, e, "d2") != nil {
		t.Fatal("andere Platte unberührt")
	}
}

func TestBenchErrors(t *testing.T) {
	e := newEnv(t)
	if codeOf(e.m.StartBench(e.ctx, "admin", "nope")) != "not_found" {
		t.Error("unbekannte Platte")
	}
	e.agent.benchFail = true
	if err := e.m.StartBench(e.ctx, "admin", "d1"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { b := benchOf(t, e, "d1"); return b != nil && !b.Running && b.Error != "" })
	e.agent.benchFail = false
	if err := e.m.StartBench(e.ctx, "admin", "d1"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { b := benchOf(t, e, "d1"); return b != nil && !b.Running && b.Error == "" && b.Last != nil })
}
