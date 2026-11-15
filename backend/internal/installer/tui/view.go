package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#1F6FEB")).Padding(0, 2)
	stepStyle   = lipgloss.NewStyle().Bold(true).MarginTop(1)
	labelStyle  = lipgloss.NewStyle().Width(28)
	focusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#58A6FF")).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#8B949E"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#F85149")).Bold(true)
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#D29922"))
	buttonStyle = lipgloss.NewStyle().Padding(0, 2).Border(lipgloss.RoundedBorder())
	activeBtn   = buttonStyle.BorderForeground(lipgloss.Color("#58A6FF")).Foreground(lipgloss.Color("#58A6FF")).Bold(true)
)

var stepKeys = map[Step]string{
	StepLanguage: "step.language", StepKeymap: "step.keymap", StepTimezone: "step.timezone",
	StepDisk: "step.disk", StepLayout: "step.layout", StepHostname: "step.hostname",
	StepNetwork: "step.network", StepAdmin: "step.admin", StepSummary: "step.summary",
	StepInstalling: "step.install", StepDone: "step.done", StepFailed: "step.failed",
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.t("title")) + "\n")
	head := m.t(stepKeys[m.step])
	if m.step <= StepSummary {
		head = fmt.Sprintf("%d/9  %s", int(m.step)+1, head)
	}
	b.WriteString(stepStyle.Render(head) + "\n\n")

	switch m.step {
	case StepSummary:
		b.WriteString(m.summaryView())
	case StepInstalling:
		bar := progress.New(progress.WithDefaultGradient(), progress.WithWidth(50))
		b.WriteString(bar.ViewAs(float64(m.progress.percent)/100) + "\n\n" + m.progress.step + "\n")
	case StepDone:
		b.WriteString(fmt.Sprintf(m.t("done.text"), m.answers.Hostname) + "\n")
	case StepFailed:
		msg := ""
		if m.result != nil {
			msg = m.result.Error()
		}
		b.WriteString(errStyle.Render(msg) + "\n\n" + fmt.Sprintf(m.t("failed.text"), m.deps.LogPath) + "\n")
	default:
		b.WriteString(m.formView())
	}

	if m.errText != "" {
		b.WriteString("\n" + errStyle.Render(m.errText) + "\n")
	}
	if m.step <= StepSummary {
		b.WriteString("\n" + dimStyle.Render(m.t("help")) + "\n")
	}
	return b.String()
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
			line := "  " + o
			if m.step == StepDisk {
				line += m.targetNotes(i)
			}
			if i == f.idx {
				line = focusStyle.Render("▸ " + strings.TrimPrefix(line, "  "))
			}
			b.WriteString(line + "\n")
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
			val = "◀ " + opt + " ▶"
		case kindToggle:
			box := "[ ]"
			if f.on {
				box = "[x]"
			}
			val = box
		}
		if pos == m.focus {
			label = focusStyle.Render(labelStyle.Render("▸ " + m.t(f.key)))
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
	row := func(k, v string) { b.WriteString(labelStyle.Render(m.t(k)) + " " + v + "\n") }
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
