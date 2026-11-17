package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
)

var (
	accent    = lipgloss.Color("#8B5CF6")
	accentAlt = lipgloss.Color("#6366F1")
	textCol   = lipgloss.Color("#E6EDF3")
	mutedCol  = lipgloss.Color("#8B949E")
	okCol     = lipgloss.Color("#3FB950")

	logoStyle   = lipgloss.NewStyle().Bold(true).Foreground(accent)
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(textCol)
	stepStyle   = lipgloss.NewStyle().Bold(true).Foreground(accent)
	labelStyle  = lipgloss.NewStyle().Width(34).Foreground(mutedCol)
	focusStyle  = lipgloss.NewStyle().Foreground(accent).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(mutedCol)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#F85149")).Bold(true)
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#D29922"))
	selStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accent)
	keyStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#30363D")).Padding(0, 1)
	cardStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accentAlt).Padding(1, 3)
	buttonStyle = lipgloss.NewStyle().Padding(0, 3).Border(lipgloss.RoundedBorder()).BorderForeground(mutedCol)
	activeBtn   = lipgloss.NewStyle().Padding(0, 3).Border(lipgloss.RoundedBorder()).BorderForeground(accent).Foreground(accent).Bold(true)
)

var stepKeys = map[Step]string{
	StepLanguage: "step.language", StepKeymap: "step.keymap", StepTimezone: "step.timezone",
	StepDisk: "step.disk", StepLayout: "step.layout", StepHostname: "step.hostname",
	StepNetwork: "step.network", StepAdmin: "step.admin", StepSummary: "step.summary",
	StepInstalling: "step.install", StepDone: "step.done", StepFailed: "step.failed",
}

const cardWidth = 78

func (m Model) innerWidth() int {
	w := cardWidth
	if m.width > 0 && m.width-4 < w {
		w = m.width - 4
	}
	if w < 40 {
		w = 40
	}
	return w - 8 // Rahmen und Innenabstand
}

// stepDots zeigt den Fortschritt durch die neun Schritte.
func (m Model) stepDots() string {
	var parts []string
	for i := 0; i <= int(StepSummary); i++ {
		switch {
		case i < int(m.step):
			parts = append(parts, lipgloss.NewStyle().Foreground(okCol).Render("●"))
		case i == int(m.step):
			parts = append(parts, focusStyle.Render("◉"))
		default:
			parts = append(parts, dimStyle.Render("○"))
		}
	}
	return strings.Join(parts, " ")
}

func (m Model) footer() string {
	var parts []string
	for _, p := range strings.Split(m.t("help"), " · ") {
		k, d, ok := strings.Cut(p, ": ")
		if !ok {
			parts = append(parts, dimStyle.Render(p))
			continue
		}
		parts = append(parts, keyStyle.Render(k)+" "+dimStyle.Render(d))
	}
	return strings.Join(parts, " ")
}

func (m Model) View() string {
	var b strings.Builder
	head := m.t(stepKeys[m.step])
	b.WriteString(logoStyle.Render("◆ BetterNAS") + "  " + dimStyle.Render(strings.TrimPrefix(m.t("title"), "BetterNAS ")) + "\n")
	if m.step <= StepSummary {
		b.WriteString(m.stepDots() + "  " + dimStyle.Render(fmt.Sprintf("%d/9", int(m.step)+1)) + "\n")
	} else {
		b.WriteString("\n")
	}
	b.WriteString("\n" + titleStyle.Render(head) + "\n" + dimStyle.Render(strings.Repeat("─", m.innerWidth())) + "\n\n")

	switch m.step {
	case StepSummary:
		b.WriteString(m.summaryView())
	case StepInstalling:
		bar := progress.New(progress.WithGradient("#6366F1", "#A78BFA"), progress.WithWidth(m.innerWidth()-2))
		b.WriteString(bar.ViewAs(float64(m.progress.percent)/100) + "\n\n" + dimStyle.Render(m.progress.step) + "\n")
	case StepDone:
		b.WriteString(lipgloss.NewStyle().Foreground(okCol).Bold(true).Render("✓ ") + fmt.Sprintf(m.t("done.text"), m.answers.Hostname) + "\n")
	case StepFailed:
		msg := ""
		if m.result != nil {
			msg = m.result.Error()
		}
		b.WriteString(errStyle.Render("✗ "+msg) + "\n\n" + fmt.Sprintf(m.t("failed.text"), m.deps.LogPath) + "\n")
	default:
		b.WriteString(m.formView())
	}

	if m.errText != "" {
		b.WriteString("\n" + errStyle.Render("! "+m.errText) + "\n")
	}
	card := cardStyle.Width(m.innerWidth() + 6).Render(strings.TrimRight(b.String(), "\n"))
	out := card
	if m.step <= StepSummary {
		out = lipgloss.JoinVertical(lipgloss.Center, card, m.footer())
	}
	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, out)
	}
	return out + "\n"
}

func (m Model) formView() string {
	var b strings.Builder
	switch m.step {
	case StepDisk:
		if len(m.deps.Targets) == 0 {
			return errStyle.Render(m.t("disk.none")) + "\n"
		}
		b.WriteString(dimStyle.Render(m.t("disk.hint")) + "\n\n")
	case StepAdmin:
		b.WriteString(dimStyle.Render(m.t("admin.hint")) + "\n\n")
	}
	vis := m.visible()
	fs := m.fields[m.step]
	if len(vis) == 1 && fs[vis[0]].kind == kindChoice {
		f := fs[vis[0]]
		for i, o := range f.options {
			note := ""
			if m.step == StepDisk {
				note = m.targetNotes(i)
			}
			if i == f.idx {
				b.WriteString(selStyle.Render(" ▸ "+o+" ") + note + "\n")
			} else {
				b.WriteString("   " + o + " " + note + "\n")
			}
		}
		return b.String()
	}
	for pos, i := range vis {
		f := fs[i]
		label := labelStyle.Render(m.t(f.key))
		var val string
		switch f.kind {
		case kindInput:
			val = f.input.View()
		case kindChoice:
			opt := ""
			if len(f.options) > 0 {
				opt = f.options[f.idx]
				if f.key == "net.mode" {
					opt = map[string]string{"dhcp": m.t("net.dhcp"), "static": m.t("net.static")}[f.values[f.idx]]
				}
			}
			val = focusStyle.Render("◀ ") + opt + focusStyle.Render(" ▶")
		case kindToggle:
			box := dimStyle.Render("▢")
			if f.on {
				box = lipgloss.NewStyle().Foreground(okCol).Bold(true).Render("▣")
			}
			val = box
		}
		if pos == m.focus {
			label = focusStyle.Width(34).Render("▸ " + m.t(f.key))
		}
		b.WriteString(label + " " + val + "\n")
	}
	return b.String()
}

func (m Model) targetNotes(i int) string {
	t := m.deps.Targets[i]
	var notes []string
	if t.HasData {
		notes = append(notes, m.t("disk.hasdata"))
	}
	if t.Warning != "" {
		notes = append(notes, t.Warning)
	}
	if len(notes) == 0 {
		return ""
	}
	return "  " + warnStyle.Render("("+strings.Join(notes, "; ")+")")
}

func (m Model) summaryView() string {
	a := m.answers
	t := m.target()
	var b strings.Builder
	row := func(k, v string) { b.WriteString(labelStyle.Render(m.t(k)) + " " + titleStyle.Render(v) + "\n") }
	row("sum.disk", describeTarget(t))
	parts := "EFI 512 MiB, System "
	if a.CreateAppdata && t.SizeBytes>>30 >= uint64(a.RootSizeGiB)+9 {
		parts += fmt.Sprintf("%d GiB, /srv/appdata: Rest", a.RootSizeGiB)
	} else {
		parts += "gesamte Platte"
	}
	row("sum.parts", parts)
	row("sum.host", a.Hostname+" ("+a.Hostname+".local)")
	if a.Net.DHCP {
		row("sum.net", a.Net.Interface+", "+m.t("net.dhcp"))
	} else {
		row("sum.net", fmt.Sprintf("%s, %s, GW %s, DNS %s", a.Net.Interface, a.Net.Address, a.Net.Gateway, strings.Join(a.Net.DNS, ", ")))
	}
	row("sum.user", a.AdminUser)
	b.WriteString("\n")
	if t.HasData {
		b.WriteString(errStyle.Render(fmt.Sprintf(m.t("sum.wipe"), nonEmpty(t.Model, t.Device), humanSize(t.SizeBytes))) + "\n\n")
	}
	back, inst := buttonStyle, buttonStyle
	if m.sumIdx == 0 {
		back = activeBtn
	} else {
		inst = activeBtn
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, back.Render(m.t("sum.back")), "  ", inst.Render(m.t("sum.install"))) + "\n")
	return b.String()
}
