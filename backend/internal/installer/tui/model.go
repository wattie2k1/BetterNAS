// Package tui ist die Textoberfläche des BetterNAS-Installers.
package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/betternas/betternas/internal/installer"
)

// Step ist ein Schritt des Installers.
type Step int

const (
	StepLanguage Step = iota
	StepKeymap
	StepTimezone
	StepDisk
	StepLayout
	StepHostname
	StepNetwork
	StepAdmin
	StepSummary
	StepInstalling
	StepDone
	StepFailed
)

// Deps sind die Abhängigkeiten der Oberfläche.
type Deps struct {
	Targets     []installer.Target
	Interfaces  []string
	ApplyKeymap func(keymap string) error
	Timezones   func(string) bool
	Install     func(ctx context.Context, l installer.Layout, a installer.Answers, p installer.Progress) error
	Reboot      func() error
	LogPath     string
}

type fieldKind int

const (
	kindInput fieldKind = iota
	kindChoice
	kindToggle
)

type field struct {
	key     string // Textschlüssel des Labels
	kind    fieldKind
	input   textinput.Model
	options []string // Anzeige
	values  []string // Werte
	idx     int
	on      bool
	hidden  func(m Model) bool
}

func (f field) value() string {
	switch f.kind {
	case kindInput:
		return f.input.Value()
	case kindChoice:
		if len(f.values) == 0 {
			return ""
		}
		return f.values[f.idx]
	}
	return strconv.FormatBool(f.on)
}

type progressMsg struct {
	step    string
	percent int
}
type doneMsg struct{ err error }

// Model ist das Bubble-Tea-Modell des Installers.
type Model struct {
	deps     Deps
	step     Step
	answers  installer.Answers
	fields   map[Step][]field
	focus    int
	errText  string
	sumIdx   int // 0 = Zurück, 1 = Installieren
	progress progressMsg
	result   error
	events   chan tea.Msg
	width    int
	height   int
}

// New erzeugt das Modell.
func New(d Deps) Model {
	m := Model{deps: d, answers: installer.Defaults(), fields: map[Step][]field{}}
	m.fields[StepLanguage] = []field{choice("step.language", []string{"Deutsch", "English"}, installer.Languages)}
	m.fields[StepKeymap] = []field{choice("step.keymap",
		[]string{"Deutsch", "Deutsch (ohne Akzenttasten)", "Schweiz", "Österreich", "US-Englisch"}, installer.Keymaps)}
	m.fields[StepTimezone] = []field{input("step.timezone", m.answers.Timezone, false)}

	var diskOpts, diskVals []string
	for _, t := range d.Targets {
		diskOpts = append(diskOpts, describeTarget(t))
		diskVals = append(diskVals, t.Device)
	}
	m.fields[StepDisk] = []field{choice("step.disk", diskOpts, diskVals)}
	m.fields[StepLayout] = []field{input("layout.root", strconv.Itoa(m.answers.RootSizeGiB), false), toggle("layout.appdata", true)}
	m.fields[StepHostname] = []field{input("hostname.label", m.answers.Hostname, false)}

	isDHCP := func(m Model) bool { return m.fields[StepNetwork][1].idx == 0 }
	addr := input("net.address", "", false)
	addr.hidden = isDHCP
	gw := input("net.gateway", "", false)
	gw.hidden = isDHCP
	dns := input("net.dns", "", false)
	dns.hidden = isDHCP
	m.fields[StepNetwork] = []field{
		choice("net.iface", d.Interfaces, d.Interfaces),
		choice("net.mode", []string{"DHCP", "Statisch"}, []string{"dhcp", "static"}),
		addr, gw, dns,
	}
	m.fields[StepAdmin] = []field{input("admin.user", "", false), input("admin.pass", "", true), input("admin.pass2", "", true)}
	m.focusField(0)
	return m
}

func choice(key string, opts, vals []string) field {
	return field{key: key, kind: kindChoice, options: opts, values: vals}
}

func toggle(key string, on bool) field { return field{key: key, kind: kindToggle, on: on} }

func input(key, value string, secret bool) field {
	ti := textinput.New()
	ti.SetValue(value)
	ti.CharLimit = 128
	ti.Width = 40
	ti.Prompt = ""
	if secret {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '•'
	}
	return field{key: key, kind: kindInput, input: ti}
}

func describeTarget(t installer.Target) string {
	s := fmt.Sprintf("%s  %s  %s", t.Device, nonEmpty(t.Model, "Unbekannt"), humanSize(t.SizeBytes))
	if t.Rotational {
		s += "  HDD"
	} else {
		s += "  SSD"
	}
	return s
}

func nonEmpty(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func humanSize(b uint64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return strings.Replace(fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp]), ".", ",", 1)
}

// Step liefert den aktuellen Schritt (für Tests).
func (m Model) Step() Step { return m.step }

// Answers liefert die bisherigen Antworten (für Tests).
func (m Model) Answers() installer.Answers { return m.answers }

func (m Model) Init() tea.Cmd { return textinput.Blink }

func (m *Model) visible() []int {
	var idx []int
	for i, f := range m.fields[m.step] {
		if f.hidden == nil || !f.hidden(*m) {
			idx = append(idx, i)
		}
	}
	return idx
}

func (m *Model) focusField(pos int) {
	vis := m.visible()
	if len(vis) == 0 {
		return
	}
	if pos < 0 {
		pos = len(vis) - 1
	}
	if pos >= len(vis) {
		pos = 0
	}
	m.focus = pos
	fs := m.fields[m.step]
	for i := range fs {
		if fs[i].kind == kindInput {
			if i == vis[pos] {
				fs[i].input.Focus()
			} else {
				fs[i].input.Blur()
			}
		}
	}
}

func (m *Model) current() *field {
	vis := m.visible()
	if len(vis) == 0 {
		return nil
	}
	return &m.fields[m.step][vis[m.focus]]
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case progressMsg:
		m.progress = msg
		return m, m.wait()
	case doneMsg:
		m.result = msg.err
		if msg.err != nil {
			m.step = StepFailed
		} else {
			m.step = StepDone
		}
		return m, nil
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.step {
	case StepInstalling:
		return m, nil
	case StepDone:
		if k.Type == tea.KeyEnter && m.deps.Reboot != nil {
			_ = m.deps.Reboot()
			return m, tea.Quit
		}
		return m, nil
	case StepFailed:
		if k.Type == tea.KeyEnter {
			m.step = StepSummary
			m.sumIdx = 0
		}
		return m, nil
	case StepSummary:
		switch k.Type {
		case tea.KeyLeft, tea.KeyRight, tea.KeyTab:
			m.sumIdx = 1 - m.sumIdx
		case tea.KeyEsc:
			m.goTo(StepAdmin)
		case tea.KeyEnter:
			if m.sumIdx == 0 {
				m.goTo(StepAdmin)
				return m, nil
			}
			return m.startInstall()
		}
		return m, nil
	}

	f := m.current()
	single := len(m.visible()) == 1
	switch k.Type {
	case tea.KeyEsc:
		if m.step > StepLanguage {
			m.goTo(m.step - 1)
		}
		return m, nil
	case tea.KeyEnter:
		return m.next()
	case tea.KeyTab:
		m.focusField(m.focus + 1)
		return m, nil
	case tea.KeyShiftTab:
		m.focusField(m.focus - 1)
		return m, nil
	case tea.KeyUp, tea.KeyDown:
		if f != nil && f.kind == kindChoice && single {
			f.idx = cycle(f.idx, len(f.values), k.Type == tea.KeyDown)
		} else if k.Type == tea.KeyDown {
			m.focusField(m.focus + 1)
		} else {
			m.focusField(m.focus - 1)
		}
		return m, nil
	case tea.KeyLeft, tea.KeyRight:
		if f != nil && f.kind == kindChoice {
			f.idx = cycle(f.idx, len(f.values), k.Type == tea.KeyRight)
			m.focusField(m.focus)
			return m, nil
		}
	case tea.KeySpace:
		if f != nil && f.kind == kindToggle {
			f.on = !f.on
			return m, nil
		}
	}
	if f != nil && f.kind == kindInput {
		var cmd tea.Cmd
		f.input, cmd = f.input.Update(k)
		m.errText = ""
		return m, cmd
	}
	return m, nil
}

func cycle(i, n int, forward bool) int {
	if n == 0 {
		return 0
	}
	if forward {
		return (i + 1) % n
	}
	return (i - 1 + n) % n
}

func (m *Model) goTo(s Step) {
	m.step = s
	m.errText = ""
	m.focusField(0)
}

func (m Model) field(s Step, i int) field { return m.fields[s][i] }

// next übernimmt die Werte des aktuellen Schritts, prüft sie und geht weiter.
func (m Model) next() (tea.Model, tea.Cmd) {
	a := &m.answers
	var check []string
	switch m.step {
	case StepLanguage:
		a.Language = m.field(StepLanguage, 0).value()
	case StepKeymap:
		a.Keymap = m.field(StepKeymap, 0).value()
		if m.deps.ApplyKeymap != nil {
			if err := m.deps.ApplyKeymap(installer.ConsoleKeymap(a.Keymap)); err != nil {
				m.errText = err.Error()
			}
		}
	case StepTimezone:
		a.Timezone = strings.TrimSpace(m.field(StepTimezone, 0).value())
		check = []string{"timezone"}
	case StepDisk:
		if len(m.deps.Targets) == 0 {
			m.errText = m.t("disk.none")
			return m, nil
		}
		a.TargetDevice = m.field(StepDisk, 0).value()
	case StepLayout:
		n, err := strconv.Atoi(strings.TrimSpace(m.field(StepLayout, 0).value()))
		if err != nil {
			m.errText = m.t("layout.invalid")
			return m, nil
		}
		a.RootSizeGiB = n
		a.CreateAppdata = m.field(StepLayout, 1).on
		if errs := a.ValidateFields(m.deps.Timezones, "rootSizeGiB"); len(errs) > 0 {
			m.errText = errs[0].Message
			return m, nil
		}
		if _, err := installer.PlanLayout(m.target(), *a); err != nil {
			m.errText = err.Error()
			return m, nil
		}
	case StepHostname:
		a.Hostname = strings.TrimSpace(m.field(StepHostname, 0).value())
		check = []string{"hostname"}
	case StepNetwork:
		if len(m.deps.Interfaces) == 0 {
			m.errText = m.t("net.none")
			return m, nil
		}
		a.Net = installer.NetConfig{Interface: m.field(StepNetwork, 0).value(), DHCP: m.field(StepNetwork, 1).value() == "dhcp"}
		if !a.Net.DHCP {
			a.Net.Address = strings.TrimSpace(m.field(StepNetwork, 2).value())
			a.Net.Gateway = strings.TrimSpace(m.field(StepNetwork, 3).value())
			a.Net.DNS = nil
			for _, d := range strings.Split(m.field(StepNetwork, 4).value(), ",") {
				if d = strings.TrimSpace(d); d != "" {
					a.Net.DNS = append(a.Net.DNS, d)
				}
			}
		}
		check = []string{"net"}
	case StepAdmin:
		a.AdminUser = strings.TrimSpace(m.field(StepAdmin, 0).value())
		a.AdminPassword = m.field(StepAdmin, 1).value()
		if a.AdminPassword != m.field(StepAdmin, 2).value() {
			m.errText = m.t("admin.mismatch")
			return m, nil
		}
		check = []string{"adminUser", "adminPassword"}
	}
	if len(check) > 0 {
		if errs := a.ValidateFields(m.deps.Timezones, check...); len(errs) > 0 {
			m.errText = errs[0].Message
			return m, nil
		}
	}
	m.goTo(m.step + 1)
	if m.step == StepSummary {
		m.sumIdx = 0
	}
	return m, nil
}

func (m Model) target() installer.Target {
	for _, t := range m.deps.Targets {
		if t.Device == m.answers.TargetDevice {
			return t
		}
	}
	return installer.Target{}
}

func (m Model) startInstall() (tea.Model, tea.Cmd) {
	l, err := installer.PlanLayout(m.target(), m.answers)
	if err != nil {
		m.errText = err.Error()
		return m, nil
	}
	m.step = StepInstalling
	m.progress = progressMsg{}
	m.events = make(chan tea.Msg, 16)
	ch, a, install := m.events, m.answers, m.deps.Install
	go func() {
		err := install(context.Background(), l, a, func(s string, p int) { ch <- progressMsg{s, p} })
		ch <- doneMsg{err}
	}()
	return m, m.wait()
}

func (m Model) wait() tea.Cmd {
	ch := m.events
	return func() tea.Msg { return <-ch }
}
