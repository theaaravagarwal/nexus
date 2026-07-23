package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
)

/*
THESIS: Nexus is a remote-computer workbench, not a flat status table.
OWN-WORLD: Near-black terminal surfaces, violet focus, cyan live state, thin
dividers, full-width host rows, a compact rail, dossiers, and a workspace map.
STORY: Find the right saved computer, understand its state, then connect or act.
FIRST VIEWPORT: Navigation at left, hosts in the center, selected-host context
at right, with the map and command palette available without leaving the TUI.
FORM: Constellation workbench, the approved rich first-design direction.
*/

type dashboardAction string

const (
	actionSSH     dashboardAction = "ssh"
	actionPull    dashboardAction = "pull"
	actionPush    dashboardAction = "push"
	actionTop     dashboardAction = "top"
	actionNet     dashboardAction = "net"
	actionInfo    dashboardAction = "info"
	actionStorage dashboardAction = "storage"
	actionCustom  dashboardAction = "custom"
	actionConfig  dashboardAction = "config"
)

type dashboardSelection struct {
	Action  dashboardAction
	Host    string
	Command commandConfig
}

type dashboardHost struct {
	Target       string
	Alias        string
	Tags         []string
	OS           string
	CPU          string
	Memory       string
	Disk         string
	Tools        []string
	Updated      time.Time
	LastUsed     time.Time
	Reachability reachabilityResult
}

type dashboardFocus int

const (
	focusNavigation dashboardFocus = iota
	focusHosts
	focusDetails
)

type dashboardCommand struct {
	Label       string
	Description string
	Action      dashboardAction
	Command     commandConfig
}

type probeBatchMsg []reachabilityResult
type probeTickMsg struct{}

type dashboardModel struct {
	hosts         []dashboardHost
	filtered      []int
	cursor        int
	query         string
	filtering     bool
	width         int
	height        int
	choice        dashboardSelection
	done          bool
	focus         dashboardFocus
	navCursor     int
	commandOpen   bool
	commandCursor int
	commandQuery  string
	helpOpen      bool
	showTopology  bool
	probing       bool
	plain         bool
	theme         theme
	now           time.Time
}

func newDashboardModel(hosts []string) dashboardModel {
	return newDashboardModelWithState(hosts, nexusState{Hosts: map[string]hostActivity{}}, time.Now())
}

func newDashboardModelWithState(hosts []string, state nexusState, now time.Time) dashboardModel {
	safe := dedupeKeepOrder(hosts)
	model := dashboardModel{
		width:        100,
		height:       30,
		focus:        focusHosts,
		showTopology: true,
		plain:        noColorRequested(),
		theme:        activeTheme(),
		now:          now,
	}
	for _, target := range safe {
		profile := profileForTarget(target)
		activity := state.Hosts[target]
		osName := profile.OS
		if activity.OS != "" {
			osName = activity.OS
		}
		model.hosts = append(model.hosts, dashboardHost{
			Target:   target,
			Alias:    profile.Alias,
			Tags:     profile.Tags,
			OS:       osName,
			CPU:      activity.CPU,
			Memory:   activity.Memory,
			Disk:     activity.Disk,
			Tools:    activity.Tools,
			Updated:  activity.Updated,
			LastUsed: activity.LastUsed,
			Reachability: reachabilityResult{
				Target: target,
				Status: reachUnknown,
			},
		})
	}
	model.applyFilter()
	return model
}

func noColorRequested() bool {
	_, disabled := os.LookupEnv("NO_COLOR")
	return disabled || os.Getenv("TERM") == "dumb"
}

func (m dashboardModel) Init() tea.Cmd {
	if len(m.hosts) == 0 || loadedConfig.Reachability.Enabled == nil || !*loadedConfig.Reachability.Enabled {
		return nil
	}
	m.probing = true
	return m.probeCommand()
}

func (m dashboardModel) probeCommand() tea.Cmd {
	targets := make([]string, len(m.hosts))
	for i := range m.hosts {
		targets[i] = m.hosts[i].Target
	}
	timeout := time.Duration(loadedConfig.Reachability.TimeoutMS) * time.Millisecond
	concurrency := loadedConfig.Reachability.Concurrency
	batches := (len(targets) + max(1, concurrency) - 1) / max(1, concurrency)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout*time.Duration(batches+1))
		defer cancel()
		return probeBatchMsg(probeTargets(ctx, targets, timeout, concurrency))
	}
}

func (m dashboardModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
		m.height = max(1, msg.Height)
		return m, nil
	case probeBatchMsg:
		byTarget := make(map[string]reachabilityResult, len(msg))
		for _, result := range msg {
			byTarget[result.Target] = result
		}
		for i := range m.hosts {
			if result, ok := byTarget[m.hosts[i].Target]; ok {
				m.hosts[i].Reachability = result
			}
		}
		m.probing = false
		delay := time.Duration(loadedConfig.Reachability.CacheSeconds) * time.Second
		return m, tea.Tick(delay, func(time.Time) tea.Msg { return probeTickMsg{} })
	case probeTickMsg:
		if m.probing || len(m.hosts) == 0 {
			return m, nil
		}
		m.probing = true
		return m, m.probeCommand()
	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m dashboardModel) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		m.done = true
		return m, tea.Quit
	}
	if m.helpOpen {
		switch key {
		case "?", "esc", "q", "enter":
			m.helpOpen = false
		}
		return m, nil
	}
	if m.commandOpen {
		commands := m.filteredCommands()
		switch key {
		case "esc", "ctrl+k":
			m.commandOpen = false
			m.commandQuery = ""
		case "backspace":
			runes := []rune(m.commandQuery)
			if len(runes) > 0 {
				m.commandQuery = string(runes[:len(runes)-1])
				m.commandCursor = 0
			}
		case "up", "ctrl+p":
			m.commandCursor = max(0, m.commandCursor-1)
		case "down", "ctrl+n":
			m.commandCursor = min(max(0, len(commands)-1), m.commandCursor+1)
		case "enter":
			if len(commands) == 0 {
				return m, nil
			}
			command := commands[m.commandCursor]
			if command.Action == actionConfig {
				return m.choose(actionConfig)
			}
			if command.Action == actionCustom {
				m.choice = dashboardSelection{Action: actionCustom, Host: m.selectedTarget(), Command: command.Command}
				m.done = true
				return m, tea.Quit
			}
			return m.choose(command.Action)
		default:
			if msg.Type == tea.KeyRunes {
				m.commandQuery += sanitizeTerminalText(string(msg.Runes))
				m.commandCursor = 0
			}
		}
		return m, nil
	}
	if m.filtering {
		switch key {
		case "esc":
			m.filtering = false
			m.query = ""
			m.applyFilter()
		case "enter":
			m.filtering = false
			return m.choose(actionSSH)
		case "backspace":
			runes := []rune(m.query)
			if len(runes) > 0 {
				m.query = string(runes[:len(runes)-1])
				m.applyFilter()
			}
		default:
			if msg.Type == tea.KeyRunes {
				m.query += sanitizeTerminalText(string(msg.Runes))
				m.applyFilter()
			}
		}
		return m, nil
	}

	switch key {
	case "q", "esc":
		m.done = true
		return m, tea.Quit
	case "?":
		m.helpOpen = true
	case "/", "f":
		m.filtering = true
	case "ctrl+k", "c":
		m.commandOpen = true
		m.commandCursor = 0
		m.commandQuery = ""
	case "tab":
		m.focus = (m.focus + 1) % 3
	case "shift+tab":
		m.focus = (m.focus + 2) % 3
	case "left", "h":
		m.focus = max(focusNavigation, m.focus-1)
	case "right", "l":
		m.focus = min(focusDetails, m.focus+1)
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "pgup":
		m.moveCursor(-max(1, m.visibleHostRows()))
	case "pgdown":
		m.moveCursor(max(1, m.visibleHostRows()))
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = max(0, len(m.filtered)-1)
	case "enter", "s":
		if m.focus == focusNavigation {
			return m.activateNavigation()
		}
		return m.choose(actionSSH)
	case "p":
		return m.choose(actionPull)
	case "u":
		return m.choose(actionPush)
	case "t":
		m.showTopology = !m.showTopology
	case "r":
		if !m.probing {
			m.probing = true
			return m, m.probeCommand()
		}
	case "i":
		return m.choose(actionInfo)
	case "n":
		return m.choose(actionNet)
	case "d":
		return m.choose(actionStorage)
	case "e":
		return m.choose(actionConfig)
	}
	return m, nil
}

func (m dashboardModel) activateNavigation() (tea.Model, tea.Cmd) {
	switch m.navCursor {
	case 0:
		m.focus = focusHosts
	case 1:
		m.showTopology = true
		m.focus = focusDetails
	case 2:
		m.commandOpen = true
		m.commandCursor = 0
		m.commandQuery = ""
	case 3:
		m.query = ""
		m.applyFilter()
		m.focus = focusHosts
	case 4:
		return m.choose(actionConfig)
	}
	return m, nil
}

func (m *dashboardModel) moveCursor(delta int) {
	if m.focus == focusNavigation {
		m.navCursor = min(4, max(0, m.navCursor+delta))
		return
	}
	if len(m.filtered) == 0 {
		m.cursor = 0
		return
	}
	m.cursor = min(len(m.filtered)-1, max(0, m.cursor+delta))
}

func (m dashboardModel) choose(action dashboardAction) (tea.Model, tea.Cmd) {
	if action != actionConfig && len(m.filtered) == 0 {
		return m, nil
	}
	m.choice = dashboardSelection{Action: action, Host: m.selectedTarget()}
	m.done = true
	return m, tea.Quit
}

func (m *dashboardModel) applyFilter() {
	needle := strings.ToLower(strings.TrimSpace(m.query))
	selected := m.selectedTarget()
	m.filtered = m.filtered[:0]
	for i, host := range m.hosts {
		haystack := strings.ToLower(strings.Join([]string{
			host.Alias, host.Target, host.OS, strings.Join(host.Tags, " "),
		}, " "))
		if needle == "" || strings.Contains(haystack, needle) {
			m.filtered = append(m.filtered, i)
		}
	}
	if selected != "" {
		for i, index := range m.filtered {
			if m.hosts[index].Target == selected {
				m.cursor = i
				return
			}
		}
	}
	m.cursor = min(m.cursor, max(0, len(m.filtered)-1))
}

func (m dashboardModel) selectedTarget() string {
	if len(m.filtered) == 0 || m.cursor < 0 || m.cursor >= len(m.filtered) {
		return ""
	}
	return m.hosts[m.filtered[m.cursor]].Target
}

func (m dashboardModel) selectedHost() dashboardHost {
	if len(m.filtered) == 0 || m.cursor < 0 || m.cursor >= len(m.filtered) {
		return dashboardHost{}
	}
	return m.hosts[m.filtered[m.cursor]]
}

func (m dashboardModel) availableCommands() []dashboardCommand {
	if m.selectedTarget() == "" {
		return []dashboardCommand{{Label: "Edit configuration", Description: "Open config.yaml", Action: actionConfig}}
	}
	commands := []dashboardCommand{
		{"Connect", "Open SSH session", actionSSH, commandConfig{}},
		{"Pull files", "Download from remote", actionPull, commandConfig{}},
		{"Push files", "Upload to remote", actionPush, commandConfig{}},
		{"System info", "Refresh authenticated system details", actionInfo, commandConfig{}},
		{"System monitor", "Open the best available monitor", actionTop, commandConfig{}},
		{"Network tools", "Open remote network diagnostics", actionNet, commandConfig{}},
		{"Storage tools", "Inspect disk usage", actionStorage, commandConfig{}},
	}
	for _, command := range commandsForTarget(m.selectedTarget()) {
		commands = append(commands, dashboardCommand{
			Label: command.Name, Description: command.Description, Action: actionCustom, Command: command,
		})
	}
	commands = append(commands, dashboardCommand{"Edit configuration", "Open config.yaml", actionConfig, commandConfig{}})
	return commands
}

func (m dashboardModel) filteredCommands() []dashboardCommand {
	commands := m.availableCommands()
	needle := strings.ToLower(strings.TrimSpace(m.commandQuery))
	if needle == "" {
		return commands
	}
	out := make([]dashboardCommand, 0, len(commands))
	for _, command := range commands {
		haystack := strings.ToLower(command.Label + " " + command.Description)
		if strings.Contains(haystack, needle) {
			out = append(out, command)
		}
	}
	return out
}

func (m dashboardModel) View() string {
	if m.width < 30 || m.height < 10 {
		return m.tinyView()
	}
	if m.height < 16 {
		return m.shortView()
	}
	if m.helpOpen {
		return m.helpView()
	}
	if m.commandOpen {
		return m.commandPaletteView()
	}
	s := m.styles()
	header := m.headerView(s)
	footer := m.footerView(s)
	bodyHeight := max(3, m.height-lipgloss.Height(header)-lipgloss.Height(footer))

	var body string
	switch {
	case m.width >= 110 && bodyHeight >= 20:
		railWidth := 18
		hostWidth := min(48, max(38, (m.width-railWidth)*46/100))
		detailWidth := max(34, m.width-railWidth-hostWidth)
		lowerHeight := min(10, max(7, bodyHeight/3))
		upperHeight := max(10, bodyHeight-lowerHeight)
		rail := fitTerminalView(m.navigationView(s, railWidth, upperHeight), railWidth, upperHeight)
		hosts := fitTerminalView(m.hostListView(s, hostWidth, upperHeight), hostWidth, upperHeight)
		details := fitTerminalView(m.detailView(s, detailWidth, upperHeight), detailWidth, upperHeight)
		upper := lipgloss.JoinHorizontal(lipgloss.Top, rail, hosts, details)
		lower := fitTerminalView(m.workspaceBottomView(s, m.width, lowerHeight), m.width, lowerHeight)
		body = lipgloss.JoinVertical(lipgloss.Left, upper, lower)
	case m.width >= 72:
		hostWidth := max(34, m.width*48/100)
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			fitTerminalView(m.hostListView(s, hostWidth, bodyHeight), hostWidth, bodyHeight),
			fitTerminalView(m.detailView(s, m.width-hostWidth, bodyHeight), m.width-hostWidth, bodyHeight),
		)
	default:
		body = m.compactView(s, m.width, bodyHeight)
	}
	body = fitTerminalView(body, m.width, bodyHeight)
	rendered := fitTerminalView(
		lipgloss.JoinVertical(lipgloss.Left, header, body, footer),
		m.width,
		m.height,
	)
	if !m.plain && m.theme.Background != "" {
		rendered = lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Text)).
			Background(lipgloss.Color(m.theme.Background)).
			Width(m.width).
			Height(m.height).
			Render(rendered)
	}
	return rendered
}

type dashboardStyles struct {
	title, text, muted, focus, live, success, warning, failure lipgloss.Style
	panel, selected, selectedMuted                             lipgloss.Style
}

func (m dashboardModel) styles() dashboardStyles {
	if m.plain {
		return dashboardStyles{
			title:         lipgloss.NewStyle().Bold(true),
			text:          lipgloss.NewStyle(),
			muted:         lipgloss.NewStyle().Faint(true),
			focus:         lipgloss.NewStyle().Bold(true),
			live:          lipgloss.NewStyle(),
			success:       lipgloss.NewStyle(),
			warning:       lipgloss.NewStyle(),
			failure:       lipgloss.NewStyle(),
			panel:         lipgloss.NewStyle().Border(lipgloss.NormalBorder()),
			selected:      lipgloss.NewStyle().Bold(true).Reverse(true),
			selectedMuted: lipgloss.NewStyle().Reverse(true),
		}
	}
	t := m.theme
	panel := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color(t.Border))
	if t.Surface != "" {
		panel = panel.Background(lipgloss.Color(t.Surface))
	}
	selected := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(t.Text))
	selectedMuted := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Muted))
	if t.Elevated != "" {
		selected = selected.Background(lipgloss.Color(t.Elevated))
		selectedMuted = selectedMuted.Background(lipgloss.Color(t.Elevated))
	}
	return dashboardStyles{
		title:         lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(t.Focus)),
		text:          lipgloss.NewStyle().Foreground(lipgloss.Color(t.Text)),
		muted:         lipgloss.NewStyle().Foreground(lipgloss.Color(t.Muted)),
		focus:         lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(t.Focus)),
		live:          lipgloss.NewStyle().Foreground(lipgloss.Color(t.Live)),
		success:       lipgloss.NewStyle().Foreground(lipgloss.Color(t.Success)),
		warning:       lipgloss.NewStyle().Foreground(lipgloss.Color(t.Warning)),
		failure:       lipgloss.NewStyle().Foreground(lipgloss.Color(t.Error)),
		panel:         panel,
		selected:      selected,
		selectedMuted: selectedMuted,
	}
}

func (m dashboardModel) headerView(s dashboardStyles) string {
	status := fmt.Sprintf("%d hosts", len(m.hosts))
	online := 0
	for _, host := range m.hosts {
		if host.Reachability.Status == reachOnline {
			online++
		}
	}
	if online > 0 {
		status += s.live.Render(fmt.Sprintf(" · %d reachable", online))
	}
	left := s.title.Render("◆ NEXUS") + s.muted.Render("  remote workspace")
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(status)-4)
	return s.panel.BorderTop(false).BorderLeft(false).BorderRight(false).
		Width(max(1, m.width-2)).Padding(0, 1).
		Render(left + strings.Repeat(" ", gap) + status)
}

func (m dashboardModel) navigationView(s dashboardStyles, width, height int) string {
	items := []string{"⌂ Hosts", "↔ Connections", "›_ Commands", "◷ History", "◈ Themes"}
	var rows []string
	for i, item := range items {
		line := "  " + item
		if i == m.navCursor && m.focus == focusNavigation {
			line = s.selected.Render("› " + item + strings.Repeat(" ", max(0, width-lipgloss.Width(item)-5)))
		} else if i == 0 {
			line = s.focus.Render("› " + item)
		} else {
			line = s.muted.Render(line)
		}
		rows = append(rows, line)
	}
	rows = append(rows, "", s.muted.Render("STATUS"))
	rows = append(rows,
		s.live.Render("● reachable"),
		s.warning.Render("● refused"),
		s.failure.Render("● timeout/error"),
		s.muted.Render("○ checking"),
	)
	content := strings.Join(rows, "\n")
	return s.panel.BorderLeft(false).BorderTop(false).BorderBottom(false).
		Width(max(1, width-1)).Height(max(1, height-1)).Padding(1, 1).Render(content)
}

func (m dashboardModel) hostListView(s dashboardStyles, width, height int) string {
	title := s.focus.Render("HOSTS")
	if m.focus != focusHosts {
		title = s.muted.Render("HOSTS")
	}
	filter := "/ search hosts"
	if m.filtering || m.query != "" {
		filter = "/ " + m.query
		if m.filtering {
			filter += "█"
		}
	}
	lines := []string{title, s.muted.Render(truncateText(filter, width-6)), ""}
	if len(m.hosts) == 0 {
		lines = append(lines, s.focus.Render("No saved hosts yet."))
		lines = append(lines, s.muted.Render("Run: nexus host add user@host[:port]"))
	} else if len(m.filtered) == 0 {
		lines = append(lines, s.muted.Render("No matching hosts."))
	} else {
		rows := max(1, (height-7)/3)
		start := 0
		if m.cursor >= rows {
			start = m.cursor - rows + 1
		}
		end := min(len(m.filtered), start+rows)
		for position := start; position < end; position++ {
			host := m.hosts[m.filtered[position]]
			lines = append(lines, m.hostRow(s, host, width-4, position == m.cursor))
		}
		lines = append(lines, s.muted.Render(fmt.Sprintf("%d–%d / %d", start+1, end, len(m.filtered))))
	}
	content := strings.Join(lines, "\n")
	return s.panel.BorderLeft(false).BorderTop(false).BorderBottom(false).
		Width(max(1, width-1)).Height(max(1, height-1)).Padding(1, 1).Render(content)
}

func (m dashboardModel) hostRow(s dashboardStyles, host dashboardHost, width int, selected bool) string {
	name := host.Alias
	if name == "" {
		target, _ := parseConnectionTarget(host.Target)
		name = target.Host
	}
	status := m.statusText(s, host.Reachability)
	firstGap := max(1, width-lipgloss.Width(name)-lipgloss.Width(status)-2)
	first := "  " + truncateText(name, max(8, width-lipgloss.Width(status)-3)) + strings.Repeat(" ", firstGap) + status
	target := truncateText(host.Target, max(10, width-15))
	last := relativeTime(host.LastUsed, m.now)
	secondGap := max(1, width-lipgloss.Width(target)-lipgloss.Width(last)-2)
	second := "  " + target + strings.Repeat(" ", secondGap) + last
	meta := ""
	if host.OS != "" {
		meta = host.OS
	}
	if len(host.Tags) > 0 {
		if meta != "" {
			meta += " · "
		}
		meta += strings.Join(host.Tags, ", ")
	}
	if meta != "" {
		second += "\n  " + truncateText(meta, width-2)
	}
	if selected {
		first = s.selected.Render("› " + padCell(strings.TrimPrefix(first, "  "), width-2))
		secondLines := strings.Split(second, "\n")
		for i := range secondLines {
			secondLines[i] = s.selectedMuted.Render(padCell(secondLines[i], width))
		}
		second = strings.Join(secondLines, "\n")
	} else {
		first = s.text.Render(first)
		second = s.muted.Render(second)
	}
	return first + "\n" + second
}

func (m dashboardModel) statusText(s dashboardStyles, result reachabilityResult) string {
	switch result.Status {
	case reachOnline:
		return s.live.Render(fmt.Sprintf("● %dms", max(1, result.Latency.Milliseconds())))
	case reachRefused:
		return s.warning.Render("● refused")
	case reachTimeout:
		return s.failure.Render("● timeout")
	case reachError:
		return s.failure.Render("● error")
	default:
		if m.probing {
			return s.muted.Render("◌ checking")
		}
		return s.muted.Render("○ unknown")
	}
}

func (m dashboardModel) detailView(s dashboardStyles, width, height int) string {
	host := m.selectedHost()
	if host.Target == "" {
		return s.panel.BorderLeft(false).BorderTop(false).BorderRight(false).BorderBottom(false).
			Width(max(1, width-1)).Height(max(1, height-1)).Padding(1, 2).
			Render(s.muted.Render("Select a host to inspect it."))
	}
	name := host.Alias
	if name == "" {
		spec, _ := parseConnectionTarget(host.Target)
		name = spec.Host
	}
	lines := []string{
		s.title.Render(name),
		s.muted.Render(host.Target),
		m.statusText(s, host.Reachability),
		"",
		s.focus.Render("IDENTITY"),
	}
	if host.OS != "" {
		lines = append(lines, s.text.Render("OS       ")+s.muted.Render(host.OS))
	} else {
		lines = append(lines, s.text.Render("OS       ")+s.muted.Render("refresh info with i"))
	}
	lines = append(lines,
		s.text.Render("CPU      ")+s.muted.Render(valueOr(host.CPU, "unknown")),
		s.text.Render("Memory   ")+s.muted.Render(valueOr(host.Memory, "unknown")),
		s.text.Render("Disk     ")+s.muted.Render(valueOr(host.Disk, "unknown")),
		s.text.Render("Last used ")+s.muted.Render(relativeTime(host.LastUsed, m.now)),
		s.text.Render("Updated   ")+s.muted.Render(relativeTime(host.Updated, m.now)),
		s.text.Render("Tags      ")+s.muted.Render(valueOr(strings.Join(host.Tags, ", "), "none")),
		"",
		s.focus.Render("TOOLS"),
		s.muted.Render(valueOr(strings.Join(host.Tools, " · "), "unknown — refresh info with i")),
	)
	commands := commandsForTarget(host.Target)
	lines = append(lines, "", s.focus.Render("CUSTOM COMMANDS"))
	if len(commands) == 0 {
		lines = append(lines, s.muted.Render("Configure commands in nexus config"))
	} else {
		for _, command := range commands {
			lines = append(lines, s.text.Render(command.Name)+"  "+s.muted.Render(command.Description))
		}
	}
	content := strings.Join(lines, "\n")
	return s.panel.BorderLeft(false).BorderTop(false).BorderRight(false).BorderBottom(false).
		Width(max(1, width-1)).Height(max(1, height-1)).Padding(1, 2).Render(content)
}

func (m dashboardModel) workspaceBottomView(s dashboardStyles, width, height int) string {
	if !m.showTopology {
		content := []string{
			s.focus.Render("QUICK ACTIONS"),
			s.text.Render("enter") + s.muted.Render(" connect    "),
			s.text.Render("c") + s.muted.Render(" commands    "),
			s.text.Render("p / u") + s.muted.Render(" pull / push    "),
			s.text.Render("i") + s.muted.Render(" refresh system info    "),
			s.text.Render("t") + s.muted.Render(" show workspace map"),
		}
		return s.panel.BorderLeft(false).BorderRight(false).BorderBottom(false).
			Width(max(1, width-2)).Height(max(1, height-1)).Padding(0, 2).
			Render(strings.Join(content, ""))
	}
	mapWidth := max(52, width*64/100)
	actionWidth := max(28, width-mapWidth)
	mapContent := s.focus.Render("WORKSPACE MAP") + "\n" + m.topologyView(s, mapWidth-6)
	mapPanel := s.panel.BorderLeft(false).BorderBottom(false).
		Width(max(1, mapWidth-1)).Height(max(1, height-1)).Padding(0, 2).Render(mapContent)
	host := m.selectedHost()
	actions := []string{
		s.focus.Render("QUICK ACTIONS"),
		s.text.Render("enter") + s.muted.Render(" connect"),
		s.text.Render("c") + s.muted.Render(" commands"),
		s.text.Render("p / u") + s.muted.Render(" pull / push"),
		s.text.Render("i") + s.muted.Render(" refresh system info"),
	}
	if len(commandsForTarget(host.Target)) > 0 {
		actions = append(actions, s.text.Render("custom")+"  "+s.muted.Render("available in palette"))
	}
	actionPanel := s.panel.BorderLeft(false).BorderRight(false).BorderBottom(false).
		Width(max(1, actionWidth-1)).Height(max(1, height-1)).Padding(0, 2).Render(strings.Join(actions, "\n"))
	return lipgloss.JoinHorizontal(lipgloss.Top, mapPanel, actionPanel)
}

func (m dashboardModel) topologyView(s dashboardStyles, width int) string {
	host := m.selectedHost()
	center := truncateText(displayName(host), 12)
	lines := []string{
		s.muted.Render("       ┌──────────────┐"),
		s.live.Render(fmt.Sprintf("       │ %-12s │", center)),
		s.muted.Render("       └──────┬───────┘"),
		s.muted.Render("  ┌───────────┼───────────┐"),
	}
	var names []string
	for _, candidate := range m.hosts {
		if candidate.Target == host.Target {
			continue
		}
		names = append(names, truncateText(displayName(candidate), 10))
		if len(names) == 3 {
			break
		}
	}
	for len(names) < 3 {
		names = append(names, "·")
	}
	lines = append(lines, fmt.Sprintf("  %-10s  %-10s  %-10s", names[0], names[1], names[2]))
	return strings.Join(lines, "\n")
}

func (m dashboardModel) compactView(s dashboardStyles, width, height int) string {
	listHeight := max(5, height-5)
	list := m.hostListView(s, width, listHeight)
	host := m.selectedHost()
	summary := "enter connect  / search  c commands  ? help"
	if host.Target != "" && height >= 12 {
		summary = s.focus.Render(displayName(host)) + "  " + m.statusText(s, host.Reachability) + "\n" +
			s.muted.Render(truncateText(host.Target+"  "+valueOr(host.OS, "system info not cached"), width-4))
	}
	return lipgloss.JoinVertical(lipgloss.Left, list, s.panel.BorderBottom(false).BorderLeft(false).BorderRight(false).
		Width(max(1, width-2)).Padding(0, 1).Render(summary))
}

func (m dashboardModel) footerView(s dashboardStyles) string {
	hints := "enter connect   / search   ctrl+k commands   tab focus   ? help   q quit"
	if m.width < 72 {
		hints = "enter connect   / search   c commands   ? help"
	}
	right := "theme: " + m.theme.Name
	if m.probing {
		right = "checking saved ports…"
	}
	gap := max(1, m.width-lipgloss.Width(hints)-lipgloss.Width(right)-4)
	return s.panel.BorderBottom(false).BorderLeft(false).BorderRight(false).
		Width(max(1, m.width-2)).Padding(0, 1).
		Render(s.muted.Render(hints) + strings.Repeat(" ", gap) + s.focus.Render(right))
}

func (m dashboardModel) commandPaletteView() string {
	s := m.styles()
	commands := m.filteredCommands()
	width := min(max(38, m.width-12), 74)
	height := min(max(8, len(commands)+5), max(8, m.height-4))
	search := "/ " + m.commandQuery
	if m.commandQuery == "" {
		search = "/ filter actions"
	} else {
		search += "█"
	}
	lines := []string{s.focus.Render("COMMAND PALETTE"), s.muted.Render("actions for " + valueOr(displayName(m.selectedHost()), "Nexus")), s.text.Render(search), ""}
	start := 0
	maxRows := max(1, height-5)
	if m.commandCursor >= maxRows {
		start = m.commandCursor - maxRows + 1
	}
	end := min(len(commands), start+maxRows)
	if len(commands) == 0 {
		lines = append(lines, s.muted.Render("No matching actions."))
	}
	for i := start; i < end; i++ {
		command := commands[i]
		line := fmt.Sprintf("%-20s %s", truncateText(command.Label, 19), truncateText(command.Description, width-25))
		if i == m.commandCursor {
			line = s.selected.Render("› " + padCell(line, width-4))
		} else {
			line = "  " + s.text.Render(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", s.muted.Render("enter run   esc close"))
	panel := s.panel.Width(width-4).Height(height-2).Padding(1, 2).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

func (m dashboardModel) helpView() string {
	s := m.styles()
	help := []string{
		s.focus.Render("NEXUS KEYS"),
		"",
		"enter / s     connect with SSH",
		"j/k or ↑/↓   move selection",
		"g/G          first / last host",
		"/            search hosts",
		"tab or h/l   move workspace focus",
		"ctrl+k / c   command palette",
		"p / u        pull / push",
		"i / n / d    info / network / storage",
		"r            refresh saved-port reachability",
		"t            toggle workspace map",
		"e            edit YAML configuration",
		"? / esc      close help",
	}
	panel := s.panel.Padding(1, 2).Render(strings.Join(help, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

func (m dashboardModel) tinyView() string {
	if m.selectedTarget() == "" {
		return "NEXUS — add a host with nexus host add user@host[:port]\n"
	}
	return truncateText("NEXUS › "+m.selectedTarget()+" — enter connect · q quit", max(1, m.width)) + "\n"
}

func (m dashboardModel) shortView() string {
	s := m.styles()
	lines := []string{s.title.Render(truncateText("◆ NEXUS  / search · enter connect", m.width))}
	rows := max(1, m.height-2)
	start := 0
	if m.cursor >= rows {
		start = m.cursor - rows + 1
	}
	end := min(len(m.filtered), start+rows)
	for position := start; position < end; position++ {
		host := m.hosts[m.filtered[position]]
		prefix := "  "
		if position == m.cursor {
			prefix = "› "
		}
		line := prefix + displayName(host) + "  " + host.Target + "  " + plainReachability(host.Reachability)
		lines = append(lines, s.text.Render(truncateText(line, m.width)))
	}
	for len(lines) < m.height-1 {
		lines = append(lines, "")
	}
	lines = append(lines, truncateText("c commands · ? help · q quit", m.width))
	return strings.Join(lines[:m.height], "\n")
}

func fitTerminalView(view string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for index, line := range lines {
		lines[index] = ansi.Truncate(line, width, "")
	}
	return strings.Join(lines, "\n")
}

func plainReachability(result reachabilityResult) string {
	switch result.Status {
	case reachOnline:
		return fmt.Sprintf("● %dms", max(1, result.Latency.Milliseconds()))
	case reachRefused:
		return "● refused"
	case reachTimeout:
		return "● timeout"
	case reachError:
		return "● error"
	default:
		return "○ unknown"
	}
}

func (m dashboardModel) visibleHostRows() int {
	return max(1, (m.height-10)/3)
}

func displayName(host dashboardHost) string {
	if host.Alias != "" {
		return host.Alias
	}
	spec, err := parseConnectionTarget(host.Target)
	if err == nil {
		return spec.Host
	}
	return host.Target
}

func relativeTime(value, now time.Time) string {
	if value.IsZero() {
		return "never"
	}
	delta := now.Sub(value)
	if delta < 0 {
		delta = 0
	}
	switch {
	case delta < time.Minute:
		return "just now"
	case delta < time.Hour:
		return fmt.Sprintf("%dm ago", int(delta.Minutes()))
	case delta < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(delta.Hours()))
	case delta < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(delta.Hours()/24))
	default:
		return value.Format("Jan 2")
	}
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func padCell(value string, width int) string {
	value = truncateText(value, width)
	return value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
}

func truncateText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = sanitizeTerminalText(value)
	if lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	var b strings.Builder
	for _, r := range value {
		candidate := b.String() + string(r)
		if lipgloss.Width(candidate) > width-1 {
			break
		}
		b.WriteRune(r)
	}
	return b.String() + "…"
}

func isInteractiveTerminal() bool {
	in, inErr := os.Stdin.Stat()
	out, outErr := os.Stdout.Stat()
	return inErr == nil && outErr == nil &&
		in.Mode()&os.ModeCharDevice != 0 &&
		out.Mode()&os.ModeCharDevice != 0 &&
		os.Getenv("TERM") != "dumb"
}

func (a *app) runDashboard() error {
	hosts, err := a.readHosts()
	if err != nil {
		return err
	}
	state, err := loadState(a.stateFile)
	if err != nil {
		return err
	}
	now := time.Now()
	hosts = sortHostsByFrecency(hosts, state, now)
	model := newDashboardModelWithState(hosts, state, now)
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
	if selection.Action == actionConfig {
		return a.editConfig()
	}
	if selection.Action == actionCustom {
		if err := confirmRemoteCommand(selection.Host, selection.Command); err != nil {
			if errors.Is(err, errCancelled) {
				return nil
			}
			return err
		}
		err := runConfiguredRemoteCommand(selection.Host, selection.Command.Command)
		if err == nil {
			if stateErr := a.recordSuccess(selection.Host); stateErr != nil {
				logVerbose("failed to record host activity: %v", stateErr)
			}
		}
		return err
	}
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

func confirmRemoteCommand(target string, command commandConfig) error {
	fmt.Printf("Remote command: %s\nTarget: %s\nCommand: %s\n", command.Name, target, command.Command)
	fmt.Print("Run this command? [y/N] ")
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, os.ErrClosed) {
		return err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		return errCancelled
	}
	return nil
}

func runConfiguredRemoteCommand(target, command string) error {
	if command = sanitizeCommandText(command); command == "" {
		return errors.New("configured command is empty or contains unsafe control characters")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	remote := "sh -lc " + shellQuote(command)
	cmd, err := buildSSHCommand(ctx, target, true, remote)
	if err != nil {
		return err
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (a *app) newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run [command] [user@host[:port]]",
		Short: "Run a configured remote command after confirmation",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := sanitizeLabel(args[0])
			hostArg := ""
			if len(args) == 2 {
				hostArg = args[1]
			}
			target, err := a.resolveHostForTransfer(hostArg)
			if errors.Is(err, errCancelled) {
				return nil
			}
			if err != nil {
				return err
			}
			var selected commandConfig
			for _, candidate := range commandsForTarget(target) {
				if candidate.Name == name {
					selected = candidate
					break
				}
			}
			if selected.Name == "" {
				return fmt.Errorf("configured command %q is not available for %s", name, target)
			}
			if err := confirmRemoteCommand(target, selected); err != nil {
				if errors.Is(err, errCancelled) {
					return nil
				}
				return err
			}
			if err := runConfiguredRemoteCommand(target, selected.Command); err != nil {
				return err
			}
			if err := a.recordSuccess(target); err != nil {
				logVerbose("failed to record host activity: %v", err)
			}
			return nil
		},
	}
}
