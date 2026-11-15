package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/betternas/betternas/internal/installer"
)

type harness struct {
	m         Model
	keymaps   []string
	installed bool
}

func newHarness(targets []installer.Target) *harness {
	h := &harness{}
	h.m = New(Deps{
		Targets:     targets,
		Interfaces:  []string{"enp1s0"},
		ApplyKeymap: func(k string) error { h.keymaps = append(h.keymaps, k); return nil },
		Timezones:   func(tz string) bool { return tz == "Europe/Berlin" },
		Install: func(context.Context, installer.Layout, installer.Answers, installer.Progress) error {
			h.installed = true
			return nil
		},
	})
	return h
}

func defaultTargets() []installer.Target {
	return []installer.Target{
		{Device: "/dev/nvme0n1", Model: "Samsung SSD", SizeBytes: 500_000_000_000},
		{Device: "/dev/sda", Model: "WDC WD40EFRX", SizeBytes: 4_000_000_000_000, Rotational: true, HasData: true, Warning: "HDD"},
	}
}

func (h *harness) send(msgs ...tea.Msg) {
	for _, msg := range msgs {
		m, _ := h.m.Update(msg)
		h.m = m.(Model)
	}
}

func key(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func (h *harness) typeText(s string) {
	for _, r := range s {
		h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func (h *harness) clear() {
	for i := 0; i < 64; i++ {
		h.send(key(tea.KeyBackspace))
	}
}

func enter() tea.KeyMsg { return key(tea.KeyEnter) }

// toAdmin führt mit Standardwerten bis zum Administrator-Schritt; Tastatur „Schweiz“, zweite Platte.
func (h *harness) toAdmin() {
	h.send(enter())                                     // Sprache
	h.send(key(tea.KeyDown), key(tea.KeyDown), enter()) // Tastatur: ch
	h.send(enter())                                     // Zeitzone
	h.send(key(tea.KeyDown), enter())                   // Platte: /dev/sda
	h.send(enter())                                     // Aufteilung
	h.send(enter())                                     // Hostname
	h.send(enter())                                     // Netzwerk
}

func TestKeymapAppliedBeforeAdminStep(t *testing.T) {
	h := newHarness(defaultTargets())
	h.toAdmin()
	if h.m.Step() != StepAdmin {
		t.Fatalf("step %d", h.m.Step())
	}
	if len(h.keymaps) != 1 || h.keymaps[0] != "sg" {
		t.Fatalf("keymaps %v", h.keymaps)
	}
	if h.m.Answers().TargetDevice != "/dev/sda" {
		t.Fatal(h.m.Answers().TargetDevice)
	}
}

func TestCannotAdvanceWithInvalidHostname(t *testing.T) {
	h := newHarness(defaultTargets())
	for i := 0; i < 5; i++ {
		h.send(enter())
	}
	if h.m.Step() != StepHostname {
		t.Fatalf("step %d", h.m.Step())
	}
	h.clear()
	h.typeText("Bet ter")
	h.send(enter())
	if h.m.Step() != StepHostname || !strings.Contains(h.m.View(), "Kleinbuchstaben") {
		t.Fatalf("step %d view:\n%s", h.m.Step(), h.m.View())
	}
}

func (h *harness) fillAdmin(user, pw, pw2 string) {
	h.typeText(user)
	h.send(key(tea.KeyTab))
	h.typeText(pw)
	h.send(key(tea.KeyTab))
	h.typeText(pw2)
	h.send(enter())
}

func TestSummaryDefaultsToBack(t *testing.T) {
	h := newHarness(defaultTargets())
	h.toAdmin()
	h.fillAdmin("anna", "sicheres-pw", "sicheres-pw")
	if h.m.Step() != StepSummary {
		t.Fatalf("step %d: %s", h.m.Step(), h.m.View())
	}
	h.send(enter())
	if h.m.Step() != StepAdmin || h.installed {
		t.Fatalf("enter on summary must go back, step %d installed %v", h.m.Step(), h.installed)
	}
}

func TestSummaryInstallStarts(t *testing.T) {
	h := newHarness(defaultTargets())
	h.toAdmin()
	h.fillAdmin("anna", "sicheres-pw", "sicheres-pw")
	h.send(key(tea.KeyRight))
	m, cmd := h.m.Update(enter())
	h.m = m.(Model)
	if h.m.Step() != StepInstalling || cmd == nil {
		t.Fatal("install not started")
	}
	for h.m.Step() == StepInstalling {
		h.send(cmd())
	}
	if !h.installed || h.m.Step() != StepDone {
		t.Fatalf("step %d", h.m.Step())
	}
}

func TestSummaryShowsWipeWarning(t *testing.T) {
	h := newHarness(defaultTargets())
	h.toAdmin()
	h.fillAdmin("anna", "sicheres-pw", "sicheres-pw")
	if !strings.Contains(h.m.View(), "werden gelöscht") {
		t.Fatal(h.m.View())
	}
}

func TestPasswordMasked(t *testing.T) {
	h := newHarness(defaultTargets())
	h.toAdmin()
	h.typeText("anna")
	h.send(key(tea.KeyTab))
	h.typeText("Supergeheim9")
	if strings.Contains(h.m.View(), "Supergeheim9") {
		t.Fatal("password visible")
	}
}

func TestPasswordMismatch(t *testing.T) {
	h := newHarness(defaultTargets())
	h.toAdmin()
	h.fillAdmin("anna", "sicheres-pw", "anderes-pw")
	if h.m.Step() != StepAdmin || !strings.Contains(h.m.View(), "stimmen nicht") {
		t.Fatal(h.m.View())
	}
}

func TestEscGoesBack(t *testing.T) {
	h := newHarness(defaultTargets())
	h.send(enter(), key(tea.KeyDown), enter())
	if h.m.Step() != StepTimezone {
		t.Fatalf("step %d", h.m.Step())
	}
	h.send(key(tea.KeyEsc))
	if h.m.Step() != StepKeymap || h.m.fields[StepKeymap][0].idx != 1 {
		t.Fatalf("step %d idx %d", h.m.Step(), h.m.fields[StepKeymap][0].idx)
	}
}

func TestNoTargets(t *testing.T) {
	h := newHarness(nil)
	h.send(enter(), enter(), enter(), enter())
	if h.m.Step() != StepDisk || !strings.Contains(h.m.View(), "Kein geeigneter") {
		t.Fatal(h.m.View())
	}
}

func TestStaticNetworkValidation(t *testing.T) {
	h := newHarness(defaultTargets())
	for i := 0; i < 6; i++ {
		h.send(enter())
	}
	if h.m.Step() != StepNetwork {
		t.Fatalf("step %d", h.m.Step())
	}
	h.send(key(tea.KeyTab), key(tea.KeyRight)) // Modus: statisch
	h.send(key(tea.KeyTab))
	h.typeText("192.168.1.20/24")
	h.send(key(tea.KeyTab))
	h.typeText("10.0.0.1")
	h.send(key(tea.KeyTab))
	h.typeText("192.168.1.1")
	h.send(enter())
	if h.m.Step() != StepNetwork || !strings.Contains(h.m.View(), "Gateway") {
		t.Fatal(h.m.View())
	}
}
