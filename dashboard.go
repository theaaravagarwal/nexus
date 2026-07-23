package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

type dashboardAction string

const (
	actionSSH     dashboardAction = "ssh"
	actionPull    dashboardAction = "pull"
	actionPush    dashboardAction = "push"
	actionTop     dashboardAction = "top"
	actionNet     dashboardAction = "net"
	actionInfo    dashboardAction = "info"
	actionStorage dashboardAction = "storage"
)

type dashboardSelection struct {
	Action dashboardAction
	Host   string
}

type dashboardModel struct {
	hosts     []string
	filtered  []string
	cursor    int
	query     string
	filtering bool
	width     int
	height    int
	choice    dashboardSelection
	done      bool
}

func newDashboardModel(hosts []string) dashboardModel {
	safe := dedupeKeepOrder(hosts)
	return dashboardModel{
		hosts:    safe,
		filtered: append([]string(nil), safe...),
		width:    84,
		height:   24,
	}
}

func (m dashboardModel) Init() tea.Cmd { return nil }

func (m dashboardModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		if m.filtering {
			switch msg.String() {
			case "esc", "enter":
				m.filtering = false
			case "backspace":
				runes := []rune(m.query)
				if len(runes) > 0 {
					m.query = string(runes[:len(runes)-1])
					m.applyFilter()
				}
			case "ctrl+c":
				m.done = true
				return m, tea.Quit
			default:
				if msg.Type == tea.KeyRunes {
					m.query += string(msg.Runes)
					m.applyFilter()
				}
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.done = true
			return m, tea.Quit
		case "/", "f":
			m.filtering = true
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor+1 < len(m.filtered) {
				m.cursor++
			}
		case "enter", "s":
			return m.choose(actionSSH)
		case "p":
			return m.choose(actionPull)
		case "u":
			return m.choose(actionPush)
		case "t":
			return m.choose(actionTop)
		case "n":
			return m.choose(actionNet)
		case "i":
			return m.choose(actionInfo)
		case "d":
			return m.choose(actionStorage)
		}
	}
	return m, nil
}

func (m dashboardModel) choose(action dashboardAction) (tea.Model, tea.Cmd) {
	if len(m.filtered) == 0 {
		return m, nil
	}
	m.choice = dashboardSelection{Action: action, Host: m.filtered[m.cursor]}
	m.done = true
	return m, tea.Quit
}

func (m *dashboardModel) applyFilter() {
	needle := strings.ToLower(strings.TrimSpace(m.query))
	m.filtered = m.filtered[:0]
	for _, host := range m.hosts {
		if needle == "" || strings.Contains(strings.ToLower(host), needle) {
			m.filtered = append(m.filtered, host)
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
}

func (m dashboardModel) View() string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	selected := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("63"))
	panel := lipgloss.NewStyle().Padding(1, 2)
	if m.width >= 60 {
		panel = panel.Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63"))
	}

	var body strings.Builder
	body.WriteString(title.Render("NEXUS"))
	body.WriteString("  ")
	body.WriteString(muted.Render("remote workspace"))
	body.WriteString("\n\n")

	if len(m.hosts) == 0 {
		body.WriteString(accent.Render("No saved hosts yet."))
		body.WriteString("\n")
		body.WriteString(muted.Render("Add one with: nexus host add user@host[:port]"))
	} else {
		filterLabel := "Filter: "
		if m.filtering {
			filterLabel = accent.Render("Filter: ")
		}
		body.WriteString(filterLabel + m.query)
		if m.filtering {
			body.WriteString(accent.Render("█"))
		}
		body.WriteString("\n\n")

		maxRows := max(3, m.height-12)
		start := 0
		if m.cursor >= maxRows {
			start = m.cursor - maxRows + 1
		}
		end := min(len(m.filtered), start+maxRows)
		for i := start; i < end; i++ {
			line := "  " + truncateText(m.filtered[i], max(20, m.width-12))
			if i == m.cursor {
				line = selected.Render("› " + truncateText(m.filtered[i], max(20, m.width-12)))
			}
			body.WriteString(line + "\n")
		}
		if len(m.filtered) == 0 {
			body.WriteString(muted.Render("No matching hosts.") + "\n")
		}
	}

	body.WriteString("\n")
	body.WriteString(accent.Render("enter") + " ssh  ")
	body.WriteString(accent.Render("p") + " pull  ")
	body.WriteString(accent.Render("u") + " push  ")
	body.WriteString(accent.Render("t") + " top  ")
	body.WriteString(accent.Render("i") + " info\n")
	body.WriteString(accent.Render("n") + " net  ")
	body.WriteString(accent.Render("d") + " disk  ")
	body.WriteString(accent.Render("/") + " filter  ")
	body.WriteString(accent.Render("q") + " quit")

	rendered := panel.Render(body.String())
	if m.width > 0 {
		rendered = lipgloss.PlaceHorizontal(m.width, lipgloss.Center, rendered)
	}
	return rendered + "\n"
}

func truncateText(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func isInteractiveTerminal() bool {
	in, inErr := os.Stdin.Stat()
	out, outErr := os.Stdout.Stat()
	return inErr == nil && outErr == nil &&
		in.Mode()&os.ModeCharDevice != 0 &&
		out.Mode()&os.ModeCharDevice != 0
}

func (a *app) runDashboard() error {
	hosts, err := a.readHosts()
	if err != nil {
		return err
	}
	model := newDashboardModel(hosts)
	program := tea.NewProgram(model, tea.WithAltScreen())
	result, err := program.Run()
	if err != nil {
		return fmt.Errorf("dashboard failed: %w", err)
	}
	finalModel, ok := result.(dashboardModel)
	if !ok || finalModel.choice.Action == "" {
		return nil
	}
	return a.executeDashboardSelection(finalModel.choice)
}

func (a *app) executeDashboardSelection(selection dashboardSelection) error {
	var command *cobra.Command
	switch selection.Action {
	case actionSSH:
		command = a.newSSHCmd()
	case actionPull:
		command = a.newPullCmd()
	case actionPush:
		command = a.newPushCmd()
	case actionTop:
		command = a.newTopCmd()
	case actionNet:
		command = a.newNetCmd()
	case actionInfo:
		command = a.newInfoCmd()
	case actionStorage:
		command = a.newStorageCmd()
	default:
		return errors.New("unknown dashboard action")
	}
	return command.RunE(command, []string{selection.Host})
}
