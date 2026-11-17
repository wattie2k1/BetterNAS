package snapraid

import (
	"github.com/betternas/betternas/internal/apierr"
	"os"
	"strings"
	"testing"
)

func TestSettingsDefaultsAndValidation(t *testing.T) {
	s := Settings{}.WithDefaults()
	if s.SyncSchedule != "*-*-* 03:00:00" || s.ScrubSchedule != "Sun *-*-* 05:00:00" ||
		s.ScrubPercent != 8 || s.ScrubOlderDays != 10 || s.DeleteThreshold != 500 {
		t.Fatalf("%+v", s)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := []Settings{
		{SyncSchedule: "daily; rm -rf /"},
		{ScrubSchedule: "Sun\n[Service]"},
		{ScrubPercent: 101},
		{ScrubOlderDays: 3651},
		{DeleteThreshold: 1000001},
	}
	for i, b := range bad {
		b = b.WithDefaults()
		// WithDefaults ersetzt 0 durch Standard; ungültige Werte bleiben erhalten
		if err := b.Validate(); err == nil {
			t.Errorf("case %d accepted: %+v", i, b)
		}
	}
	if err := (Settings{ScrubPercent: 0, ScrubOlderDays: 0, DeleteThreshold: 0, SyncSchedule: "daily", ScrubSchedule: "weekly"}).Validate(); err == nil {
		t.Error("scrub percent 0 accepted by Validate")
	}
}

func TestPoolConfigValidate(t *testing.T) {
	ok := PoolConfig{Pool: "media", Data: []string{"d1", "d2"}, Parity: []string{"p1"}}.WithDefaults()
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]PoolConfig{
		"keine Parität":    {Pool: "media", Data: []string{"d1"}},
		"drei Parität":     {Pool: "media", Data: []string{"d1"}, Parity: []string{"p1", "p2", "p3"}},
		"keine Daten":      {Pool: "media", Parity: []string{"p1"}},
		"doppelte ID":      {Pool: "media", Data: []string{"d1", "p1"}, Parity: []string{"p1"}},
		"Poolname":         {Pool: "../x", Data: []string{"d1"}, Parity: []string{"p1"}},
		"Datenträger-Name": {Pool: "media", Data: []string{"d 1"}, Parity: []string{"p1"}},
	}
	for name, c := range cases {
		if err := c.WithDefaults().Validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if err := (State{Pools: []PoolConfig{ok, ok}}).Validate(); err == nil {
		t.Error("duplicate pool accepted")
	}
}

func TestParseDiff(t *testing.T) {
	b, err := os.ReadFile("testdata/diff.txt")
	if err != nil {
		t.Fatal(err)
	}
	d := ParseDiff(b)
	if d.Equal != 12 || d.Added != 3 || d.Removed != 1 || d.Updated != 2 || d.Moved != 0 || d.Copied != 0 {
		t.Fatalf("%+v", d)
	}
	if d.Unprotected() != 6 {
		t.Fatal(d.Unprotected())
	}
	if (ParseDiff([]byte("Nothing to do\n"))) != (Diff{}) {
		t.Fatal("garbage parsed")
	}
}

func TestParseSettingsRoundtrip(t *testing.T) {
	files, err := Render(State{Pools: []PoolConfig{PoolConfig{Pool: "media", Data: []string{"d1"}, Parity: []string{"p1"},
		Settings: Settings{ScrubPercent: 12, ScrubOlderDays: 7, DeleteThreshold: 42}.WithDefaults()}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseSettings(files[SettingsPath("media")])
	if err != nil {
		t.Fatal(err)
	}
	if s.ScrubPercent != 12 || s.ScrubOlderDays != 7 || s.DeleteThreshold != 42 {
		t.Fatalf("%+v", s)
	}
	if _, err := ParseSettings([]byte("delete_threshold=abc\n")); err == nil {
		t.Fatal("garbage accepted")
	}
	if !strings.HasPrefix(string(files[SettingsPath("media")]), ManagedHeader+"\n") {
		t.Fatal("no header")
	}
}

func TestPoolConfigNames(t *testing.T) {
	base := func(names map[string]string) PoolConfig {
		return PoolConfig{Pool: "media", Data: []string{"d1", "new2"}, Parity: []string{"p1"}, Names: names}.WithDefaults()
	}
	if got := base(map[string]string{"new2": "old2"}).Name("new2"); got != "old2" {
		t.Errorf("Name: %q", got)
	}
	if got := base(nil).Name("d1"); got != "d1" {
		t.Errorf("Name ohne Alias: %q", got)
	}
	if err := base(map[string]string{"new2": "old2"}).Validate(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]map[string]string{
		"Alias = ID einer anderen Platte": {"new2": "d1"},
		"Alias ungültig":                  {"new2": "../x"},
		"Alias leer":                      {"new2": ""},
		"Schlüssel ist keine Datenplatte": {"p1": "old"},
		"Schlüssel unbekannt":             {"zzz": "old"},
	}
	for name, n := range bad {
		if err := base(n).Validate(); err == nil || apierr.From(err).Code != apierr.InvalidParams {
			t.Errorf("%s: erwartet invalid_params, bekam %v", name, err)
		}
	}
	two := PoolConfig{Pool: "media", Data: []string{"a", "b"}, Parity: []string{"p1"}, Names: map[string]string{"a": "x", "b": "x"}}.WithDefaults()
	if err := two.Validate(); err == nil {
		t.Error("doppelter Alias akzeptiert")
	}
}
