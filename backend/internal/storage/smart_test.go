package storage

import (
	"testing"
	"time"
)

func TestManagerKeepsLastSmartOnStandby(t *testing.T) {
	e := newEnv(t)
	e.agent.smartBy = map[string]SmartInfo{"/dev/sda": {Passed: true, TemperatureC: 41, PowerOnHours: 100, PendingSectors: 1}}
	find := func() DiskView {
		t.Helper()
		ds, err := e.m.ListDisks(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range ds {
			if d.ID == "d1" {
				return d
			}
		}
		t.Fatal("d1 fehlt")
		return DiskView{}
	}
	if d := find(); d.Smart == nil || d.Smart.TemperatureC != 41 || d.Smart.Standby {
		t.Fatalf("%+v", d.Smart)
	}
	e.agent.smartBy["/dev/sda"] = SmartInfo{Standby: true}
	now := time.Now().Add(6 * time.Minute)
	e.m.now = func() time.Time { return now }
	d := find()
	if d.Smart == nil || !d.Smart.Standby || d.Smart.TemperatureC != 41 || !d.Smart.Passed || d.Smart.PendingSectors != 1 {
		t.Fatalf("alte Werte verloren: %+v", d.Smart)
	}
}
