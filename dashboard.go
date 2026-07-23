package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
)

/*
THESIS: Nexus is a calm, persistent remote-computer workspace.
OWN-WORLD: Near-black terminal surfaces, violet focus, cyan live state, thin
dividers, compact host rows, and progressively disclosed operational detail.
STORY: Recognize the right saved computer, act, observe progress, and return to
the same context.
FIRST VIEWPORT: A host-first two-region workspace with one obvious primary
action; fleet, themes, saved-command guidance, and deeper actions stay on demand.
FORM: Daily-driver terminal workspace with information density under control.
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
	actionFleet   dashboardAction = "fleet"
	actionThemes  dashboardAction = "themes"
	actionGuide   dashboardAction = "guide"
)

type dashboardSelection struct {
	Action  dashboardAction
	Host    string
	Command commandConfig
	Args    []string
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

type dashboardCommand struct {
	Label       string
	Description string
	Action      dashboardAction
	Command     commandConfig
}

type probeTickMsg struct{}
type probeTargetMsg reachabilityResult

type metadataRefreshMsg struct {
	Target   string
	Activity hostActivity
	Err      error
}

type transferScanMsg struct {
	Stage transferStage
	Items []string
	Err   error
}

type transferStage string

const (
	transferScanRemoteSource transferStage = "scan-remote-source"
	transferPickRemoteSource transferStage = "pick-remote-source"
	transferScanLocalSource  transferStage = "scan-local-source"
	transferPickLocalSource  transferStage = "pick-local-source"
	transferScanRemoteDest   transferStage = "scan-remote-destination"
	transferPickRemoteDest   transferStage = "pick-remote-destination"
)

type transferFlow struct {
	Action    dashboardAction
	Stage     transferStage
	Host      string
	Items     []string
	Cursor    int
	LocalPath string
	Err       string
}

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
	commandOpen   bool
	commandCursor int
	commandQuery  string
	confirmOpen   bool
	confirmAction dashboardSelection
	transfer      *transferFlow
	helpOpen      bool
	guideOpen     bool
	themeOpen     bool
	themeCursor   int
	themeOriginal theme
	themePreview  bool
	showTopology  bool
	probing       bool
	probeQueue    []string
	probeInitial  []string
	probeTargets  map[string]bool
	probeTotal    int
	probeComplete int
	metadataBusy  map[string]bool
	statePath     string
	indexMode     string
	notice        string
	noticeError   bool
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
		showTopology: false,
		probeTargets: make(map[string]bool),
		metadataBusy: make(map[string]bool),
		indexMode:    "lazy",
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
	if len(model.hosts) > 0 && loadedConfig.Reachability.Enabled != nil && *loadedConfig.Reachability.Enabled {
		model, _ = model.beginProbe(model.allTargets())
	}
	return model
}

func noColorRequested() bool {
	_, disabled := os.LookupEnv("NO_COLOR")
	return disabled || os.Getenv("TERM") == "dumb"
}

func (m dashboardModel) Init() tea.Cmd {
	if len(m.probeInitial) == 0 {
		return nil
	}
	return m.probeCommands(m.probeInitial)
}

func (m dashboardModel) allTargets() []string {
	targets := make([]string, 0, len(m.hosts))
	for i := range m.hosts {
		targets = append(targets, m.hosts[i].Target)
	}
	return targets
}

func (m dashboardModel) beginProbe(targets []string) (dashboardModel, tea.Cmd) {
	if len(targets) == 0 {
		return m, nil
	}
	m.probing = true
	m.probeComplete = 0
	m.probeTotal = len(targets)
	m.probeTargets = make(map[string]bool, len(targets))
	for _, target := range targets {
		m.probeTargets[target] = true
	}
	concurrency := min(max(1, loadedConfig.Reachability.Concurrency), len(targets))
	initial := append([]string(nil), targets[:concurrency]...)
	m.probeInitial = initial
	m.probeQueue = append([]string(nil), targets[concurrency:]...)
	return m, m.probeCommands(initial)
}

func (m dashboardModel) probeCommands(targets []string) tea.Cmd {
	commands := make([]tea.Cmd, 0, len(targets))
	for _, raw := range targets {
		target := raw
		commands = append(commands, func() tea.Msg {
			timeout := time.Duration(loadedConfig.Reachability.TimeoutMS) * time.Millisecond
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			return probeTargetMsg(probeTarget(ctx, target, timeout))
		})
	}
	return tea.Batch(commands...)
}

func (m dashboardModel) metadataCommand(target string) tea.Cmd {
	statePath := m.statePath
	return func() tea.Msg {
		if statePath == "" {
			return metadataRefreshMsg{Target: target, Err: errors.New("metadata cache is unavailable")}
		}
		err := refreshHostMetadata(statePath, target)
		if err != nil {
			return metadataRefreshMsg{Target: target, Err: err}
		}
		state, err := loadState(statePath)
		return metadataRefreshMsg{Target: target, Activity: state.Hosts[target], Err: err}
	}
}

func (m dashboardModel) startMetadataRefresh() (tea.Model, tea.Cmd) {
	target := m.selectedTarget()
	if target == "" || m.metadataBusy[target] {
		return m, nil
	}
	m.metadataBusy[target] = true
	m.notice = "Refreshing system snapshot for " + displayName(m.selectedHost())
	m.noticeError = false
	return m, m.metadataCommand(target)
}

func (m dashboardModel) startTransfer(action dashboardAction) (tea.Model, tea.Cmd) {
	target := m.selectedTarget()
	if target == "" {
		return m, nil
	}
	flow := &transferFlow{Action: action, Host: target}
	if action == actionPull {
		flow.Stage = transferScanRemoteSource
	} else {
		flow.Stage = transferScanLocalSource
	}
	m.transfer = flow
	return m, m.transferScanCommand(flow.Stage)
}

func (m dashboardModel) transferScanCommand(stage transferStage) tea.Cmd {
	target := ""
	if m.transfer != nil {
		target = m.transfer.Host
	}
	return func() tea.Msg {
		switch stage {
		case transferScanLocalSource:
			cwd, err := os.Getwd()
			if err != nil {
				return transferScanMsg{Stage: stage, Err: err}
			}
			entries, err := os.ReadDir(cwd)
			if err != nil {
				return transferScanMsg{Stage: stage, Err: err}
			}
			items := []string{cwd}
			for _, entry := range entries {
				items = append(items, filepath.Join(cwd, entry.Name()))
			}
			return transferScanMsg{Stage: stage, Items: items}
		case transferScanRemoteSource, transferScanRemoteDest:
			action := "pull"
			if stage == transferScanRemoteDest {
				action = "push"
			}
			full := normalizeRemoteIndexMode(m.remoteIndexMode()) == "full"
			items, _, err := getRemotePathsInternal("", target, ".", full, action)
			return transferScanMsg{Stage: stage, Items: items, Err: err}
		default:
			return transferScanMsg{Stage: stage, Err: errors.New("unknown transfer scan stage")}
		}
	}
}

func (m dashboardModel) remoteIndexMode() string {
	return m.indexMode
}

func (m dashboardModel) updateTransfer(key string) (tea.Model, tea.Cmd) {
	if m.transfer == nil {
		return m, nil
	}
	flow := *m.transfer
	switch key {
	case "esc", "q":
		m.transfer = nil
		return m, nil
	case "r":
		if flow.Err != "" {
			flow.Err = ""
			m.transfer = &flow
			return m, m.transferScanCommand(flow.Stage)
		}
	case "up", "k":
		flow.Cursor = max(0, flow.Cursor-1)
	case "down", "j":
		flow.Cursor = min(max(0, len(flow.Items)-1), flow.Cursor+1)
	case "pgup":
		flow.Cursor = max(0, flow.Cursor-8)
	case "pgdown":
		flow.Cursor = min(max(0, len(flow.Items)-1), flow.Cursor+8)
	case "enter":
		if flow.Err != "" || len(flow.Items) == 0 {
			return m, nil
		}
		selected := flow.Items[flow.Cursor]
		switch flow.Stage {
		case transferPickRemoteSource:
			cwd, err := os.Getwd()
			if err != nil {
				flow.Err = sanitizeTerminalText(err.Error())
				break
			}
			m.choice = dashboardSelection{
				Action: actionPull, Host: flow.Host,
				Args: []string{flow.Host, selected, cwd},
			}
			m.done = true
			return m, tea.Quit
		case transferPickLocalSource:
			flow.LocalPath = selected
			flow.Items = nil
			flow.Cursor = 0
			flow.Stage = transferScanRemoteDest
			m.transfer = &flow
			return m, m.transferScanCommand(flow.Stage)
		case transferPickRemoteDest:
			m.choice = dashboardSelection{
				Action: actionPush, Host: flow.Host,
				Args: []string{flow.LocalPath, flow.Host, selected},
			}
			m.done = true
			return m, tea.Quit
		}
	}
	m.transfer = &flow
	return m, nil
}

func (m dashboardModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
		m.height = max(1, msg.Height)
		return m, nil
	case probeTargetMsg:
		result := reachabilityResult(msg)
		for i := range m.hosts {
			if m.hosts[i].Target == result.Target {
				m.hosts[i].Reachability = result
				break
			}
		}
		delete(m.probeTargets, result.Target)
		m.probeComplete++
		if len(m.probeQueue) > 0 {
			next := m.probeQueue[0]
			m.probeQueue = m.probeQueue[1:]
			return m, m.probeCommands([]string{next})
		}
		if m.probeComplete >= m.probeTotal {
			m.probing = false
			m.probeInitial = nil
			delay := time.Duration(loadedConfig.Reachability.CacheSeconds) * time.Second
			return m, tea.Tick(delay, func(time.Time) tea.Msg { return probeTickMsg{} })
		}
		return m, nil
	case probeTickMsg:
		if m.probing || len(m.hosts) == 0 {
			return m, nil
		}
		m, command := m.beginProbe(m.allTargets())
		return m, command
	case metadataRefreshMsg:
		delete(m.metadataBusy, msg.Target)
		if msg.Err != nil {
			m.notice = "Snapshot failed for " + m.displayNameForTarget(msg.Target) + ": " + sanitizeTerminalText(msg.Err.Error())
			m.noticeError = true
			return m, nil
		}
		for i := range m.hosts {
			if m.hosts[i].Target != msg.Target {
				continue
			}
			m.hosts[i].OS = msg.Activity.OS
			m.hosts[i].CPU = msg.Activity.CPU
			m.hosts[i].Memory = msg.Activity.Memory
			m.hosts[i].Disk = msg.Activity.Disk
			m.hosts[i].Tools = append([]string(nil), msg.Activity.Tools...)
			m.hosts[i].Updated = msg.Activity.Updated
			break
		}
		m.notice = "System snapshot refreshed for " + m.displayNameForTarget(msg.Target)
		m.noticeError = false
		return m, nil
	case transferScanMsg:
		if m.transfer == nil || m.transfer.Stage != msg.Stage {
			return m, nil
		}
		flow := *m.transfer
		if msg.Err != nil {
			flow.Err = sanitizeTerminalText(msg.Err.Error())
			m.transfer = &flow
			return m, nil
		}
		flow.Items = append([]string(nil), msg.Items...)
		flow.Cursor = 0
		flow.Err = ""
		switch msg.Stage {
		case transferScanRemoteSource:
			flow.Stage = transferPickRemoteSource
		case transferScanLocalSource:
			flow.Stage = transferPickLocalSource
		case transferScanRemoteDest:
			flow.Stage = transferPickRemoteDest
		}
		m.transfer = &flow
		return m, nil
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
	if m.transfer != nil {
		return m.updateTransfer(key)
	}
	if m.helpOpen {
		switch key {
		case "?", "esc", "q", "enter":
			m.helpOpen = false
		}
		return m, nil
	}
	if m.confirmOpen {
		switch key {
		case "y", "Y", "enter":
			m.choice = m.confirmAction
			m.done = true
			return m, tea.Quit
		case "n", "N", "esc", "q":
			m.confirmOpen = false
			m.confirmAction = dashboardSelection{}
		}
		return m, nil
	}
	if m.guideOpen {
		switch key {
		case "esc", "q", "a":
			m.guideOpen = false
		case "e", "enter":
			return m.choose(actionConfig)
		}
		return m, nil
	}
	if m.showTopology {
		switch key {
		case "esc", "q", "t":
			m.showTopology = false
		}
		return m, nil
	}
	if m.themeOpen {
		names := themeNames()
		switch key {
		case "esc", "q":
			m.theme = m.themeOriginal
			m.themeOpen = false
		case "up", "k", "left", "h":
			m.themeCursor = (m.themeCursor + len(names) - 1) % len(names)
			m.theme = themes[names[m.themeCursor]]
		case "down", "j", "right", "l":
			m.themeCursor = (m.themeCursor + 1) % len(names)
			m.theme = themes[names[m.themeCursor]]
		case "enter":
			m.themeOpen = false
			m.themePreview = m.theme.Name != activeTheme().Name
		case "e":
			return m.choose(actionConfig)
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
			switch command.Action {
			case actionPull, actionPush:
				m.commandOpen = false
				m.commandQuery = ""
				return m.startTransfer(command.Action)
			case actionInfo:
				m.commandOpen = false
				m.commandQuery = ""
				return m.startMetadataRefresh()
			case actionFleet:
				m.commandOpen = false
				m.showTopology = true
				return m, nil
			case actionThemes:
				m.commandOpen = false
				m.openThemePreview()
				return m, nil
			case actionGuide:
				m.commandOpen = false
				m.guideOpen = true
				return m, nil
			case actionConfig:
				return m.choose(actionConfig)
			}
			if command.Action == actionCustom {
				m.commandOpen = false
				m.confirmOpen = true
				m.confirmAction = dashboardSelection{
					Action: actionCustom, Host: m.selectedTarget(), Command: command.Command,
				}
				return m, nil
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
		return m.choose(actionSSH)
	case "p":
		return m.startTransfer(actionPull)
	case "u":
		return m.startTransfer(actionPush)
	case "t":
		m.showTopology = true
	case "r":
		target := m.selectedTarget()
		if target != "" && !m.probing {
			m, command := m.beginProbe([]string{target})
			return m, command
		}
	case "R":
		if !m.probing {
			m, command := m.beginProbe(m.allTargets())
			return m, command
		}
	case "i":
		return m.startMetadataRefresh()
	case "n":
		return m.choose(actionNet)
	case "d":
		return m.choose(actionStorage)
	case "e":
		return m.choose(actionConfig)
	case "a":
		m.guideOpen = true
	}
	return m, nil
}

func (m *dashboardModel) openThemePreview() {
	names := themeNames()
	m.themeOriginal = m.theme
	m.themeCursor = 0
	for index, name := range names {
		if name == m.theme.Name {
			m.themeCursor = index
			break
		}
	}
	m.themeOpen = true
}

func (m *dashboardModel) moveCursor(delta int) {
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

func (m dashboardModel) displayNameForTarget(target string) string {
	for _, host := range m.hosts {
		if host.Target == target {
			return displayName(host)
		}
	}
	return target
}

func (m dashboardModel) availableCommands() []dashboardCommand {
	if m.selectedTarget() == "" {
		return []dashboardCommand{
			{Label: "Saved command guide", Description: "Learn and configure reusable commands", Action: actionGuide},
			{Label: "Theme preview", Description: "Preview Nexus themes", Action: actionThemes},
			{Label: "Edit full config", Description: "Open config.yaml", Action: actionConfig},
		}
	}
	commands := []dashboardCommand{
		{"Connect", "Open SSH session", actionSSH, commandConfig{}},
		{"Pull files", "Download from remote", actionPull, commandConfig{}},
		{"Push files", "Upload to remote", actionPush, commandConfig{}},
		{"Refresh snapshot", "Scan system details in the background", actionInfo, commandConfig{}},
		{"System monitor", "Open the best available monitor", actionTop, commandConfig{}},
		{"Network tools", "Open remote network diagnostics", actionNet, commandConfig{}},
		{"Storage tools", "Inspect disk usage", actionStorage, commandConfig{}},
		{"Saved command guide", "Learn and configure reusable commands", actionGuide, commandConfig{}},
		{"Fleet overview", "Inspect saved peers", actionFleet, commandConfig{}},
		{"Theme preview", "Preview Nexus themes", actionThemes, commandConfig{}},
	}
	for _, command := range commandsForTarget(m.selectedTarget()) {
		commands = append(commands, dashboardCommand{
			Label: command.Name, Description: command.Description, Action: actionCustom, Command: command,
		})
	}
	commands = append(commands, dashboardCommand{"Edit full config", "Open config.yaml", actionConfig, commandConfig{}})
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
		return fitTerminalView(m.helpView(), m.width, m.height)
	}
	if m.transfer != nil {
		return fitTerminalView(m.transferView(), m.width, m.height)
	}
	if m.confirmOpen {
		return fitTerminalView(m.confirmCommandView(), m.width, m.height)
	}
	if m.guideOpen {
		return fitTerminalView(m.savedCommandGuideView(), m.width, m.height)
	}
	if m.showTopology {
		return fitTerminalView(m.fleetView(), m.width, m.height)
	}
	if m.commandOpen {
		return fitTerminalView(m.commandPaletteView(), m.width, m.height)
	}
	if m.themeOpen {
		return fitTerminalView(m.themePreviewView(), m.width, m.height)
	}
	s := m.styles()
	header := m.headerView(s)
	footer := m.footerView(s)
	bodyHeight := max(3, m.height-lipgloss.Height(header)-lipgloss.Height(footer))

	var body string
	switch {
	case m.width >= 72:
		hostWidth := min(52, max(34, m.width*43/100))
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
	panel, selected, selectedMuted, key                        lipgloss.Style
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
			key:           lipgloss.NewStyle().Bold(true),
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
		key: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(t.Focus)).
			Background(lipgloss.Color(t.Elevated)).
			Padding(0, 1),
	}
}

func (m dashboardModel) headerView(s dashboardStyles) string {
	online := 0
	for _, host := range m.hosts {
		if host.Reachability.Status == reachOnline {
			online++
		}
	}
	status := fmt.Sprintf("%d hosts  ·  %d online", len(m.hosts), online)
	if m.probing {
		status += s.muted.Render(fmt.Sprintf("  ·  probing %d/%d", m.probeComplete, m.probeTotal))
	}
	left := s.title.Render("◆ NEXUS") + s.muted.Render("  choose a host")
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(status)-4)
	return s.panel.BorderTop(false).BorderLeft(false).BorderRight(false).
		Width(max(1, m.width-2)).Padding(0, 1).
		Render(left + strings.Repeat(" ", gap) + status)
}

func (m dashboardModel) hostListView(s dashboardStyles, width, height int) string {
	title := s.focus.Render("HOSTS")
	filter := "/ search hosts"
	if m.filtering || m.query != "" {
		filter = "/ " + m.query
		if m.filtering {
			filter += "█"
		}
	}
	lines := []string{title, s.muted.Render(truncateText(filter, width-6)), ""}
	if len(m.hosts) == 0 {
		lines = append(lines,
			s.focus.Render("◇  NO SAVED ENDPOINTS"),
			"",
			s.text.Render("Save an exact SSH destination"),
			s.muted.Render("including its non-default port."),
			"",
			s.key.Render("nexus host add user@host[:port]"),
		)
	} else if len(m.filtered) == 0 {
		lines = append(lines,
			s.focus.Render("No endpoint matches “"+truncateText(m.query, max(4, width-24))+"”."),
			s.muted.Render("Backspace to broaden · esc to clear"),
		)
	} else {
		rows := max(1, (height-6)/2)
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
	refreshing := m.probeTargets[result.Target]
	suffix := ""
	if refreshing && result.Status != reachUnknown {
		suffix = s.muted.Render(" ↻")
	}
	switch result.Status {
	case reachOnline:
		return s.live.Render(fmt.Sprintf("● %dms", max(1, result.Latency.Milliseconds()))) + suffix
	case reachRefused:
		return s.warning.Render("! refused") + suffix
	case reachTimeout:
		return s.failure.Render("× timeout") + suffix
	case reachError:
		return s.failure.Render("× error") + suffix
	default:
		if refreshing {
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
		m.statusText(s, host.Reachability) + s.muted.Render("  ·  used "+relativeTime(host.LastUsed, m.now)),
		"",
	}
	snapshotState := "not scanned · press i"
	if m.metadataBusy[host.Target] {
		snapshotState = "refreshing…"
	}
	if !host.Updated.IsZero() {
		snapshotState = "updated " + relativeTime(host.Updated, m.now)
		if m.metadataBusy[host.Target] {
			snapshotState += " · refreshing…"
		}
	}
	snapshotTitle := "SYSTEM"
	titleGap := max(1, width-lipgloss.Width(snapshotTitle)-lipgloss.Width(snapshotState)-6)
	lines = append(lines, s.focus.Render(snapshotTitle)+strings.Repeat(" ", titleGap)+s.muted.Render(snapshotState))
	lines = append(lines,
		s.text.Render("OS       ")+s.muted.Render(valueOr(host.OS, "unknown")),
		s.text.Render("CPU      ")+s.muted.Render(valueOr(host.CPU, "unknown")),
		s.text.Render("Memory   ")+s.muted.Render(valueOr(host.Memory, "unknown")),
		s.text.Render("Disk     ")+s.muted.Render(valueOr(host.Disk, "unknown")),
	)
	if len(host.Tags) > 0 {
		lines = append(lines, s.text.Render("Tags     ")+s.muted.Render(strings.Join(host.Tags, " · ")))
	}
	tools := strings.Join(host.Tools, " · ")
	if tools != "" {
		lines = append(lines, s.text.Render("Tools    ")+s.muted.Render(tools))
	}
	commands := commandsForTarget(host.Target)
	lines = append(lines,
		"",
		s.focus.Render("SAVED COMMANDS"),
		s.muted.Render("Remote commands · always confirmed"),
	)
	if len(commands) == 0 {
		lines = append(lines,
			s.text.Render("None configured"),
			s.muted.Render("[a] setup guide  ·  example: uptime"),
		)
	} else {
		for _, command := range commands {
			lines = append(lines, s.text.Render(command.Name)+"  "+s.muted.Render(command.Description))
		}
		lines = append(lines, s.muted.Render("[ctrl+k] choose  ·  [a] setup guide"))
	}
	content := strings.Join(lines, "\n")
	return s.panel.BorderLeft(false).BorderTop(false).BorderRight(false).BorderBottom(false).
		Width(max(1, width-1)).Height(max(1, height-1)).Padding(1, 2).Render(content)
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
	hints := strings.Join([]string{
		keyHint(s, "enter", "connect"),
		keyHint(s, "/", "find"),
		keyHint(s, "ctrl+k", "actions"),
		keyHint(s, "?", "help"),
	}, "  ")
	if m.width < 72 {
		hints = strings.Join([]string{
			keyHint(s, "enter", "connect"),
			keyHint(s, "/", "find"),
			keyHint(s, "c", "actions"),
		}, " ")
	}
	right := ""
	if m.themePreview {
		right = "preview: " + m.theme.Name + " · press e to save"
	}
	for target := range m.metadataBusy {
		right = "◌ refreshing " + truncateText(target, 24)
		break
	}
	if right == "" && m.notice != "" {
		if m.noticeError {
			right = s.failure.Render(truncateText(m.notice, max(18, m.width/2)))
		} else {
			right = s.success.Render("✓ " + truncateText(m.notice, max(18, m.width/2)))
		}
	}
	if right == "" && m.probing {
		right = fmt.Sprintf("◌ probing %d/%d", m.probeComplete, m.probeTotal)
	}
	if right == "" {
		right = "ready"
	}
	gap := max(1, m.width-lipgloss.Width(hints)-lipgloss.Width(right)-4)
	return s.panel.BorderBottom(false).BorderLeft(false).BorderRight(false).
		Width(max(1, m.width-2)).Padding(0, 1).
		Render(s.muted.Render(hints) + strings.Repeat(" ", gap) + s.focus.Render(right))
}

func keyHint(s dashboardStyles, key, label string) string {
	return s.key.Render("["+key+"]") + s.muted.Render(" "+label)
}

func (m dashboardModel) commandPaletteView() string {
	s := m.styles()
	commands := m.filteredCommands()
	width := min(max(38, m.width-12), 74)
	search := "/ " + m.commandQuery
	if m.commandQuery == "" {
		search = "/ filter actions"
	} else {
		search += "█"
	}
	context := valueOr(displayName(m.selectedHost()), "Nexus")
	if target := m.selectedTarget(); target != "" {
		context += "  ·  " + target
	}
	lines := []string{s.focus.Render("ACTIONS"), s.muted.Render(truncateText(context, width-6)), s.text.Render(search), ""}
	start := 0
	maxRows := max(1, m.height-10)
	if m.commandCursor >= maxRows {
		start = m.commandCursor - maxRows + 1
	}
	end := min(len(commands), start+maxRows)
	if len(commands) == 0 {
		lines = append(lines,
			s.text.Render("No action matches “"+truncateText(m.commandQuery, max(4, width-28))+"”."),
			s.muted.Render("Backspace to broaden the search."),
		)
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
	lines = append(lines, "", s.muted.Render("[enter] choose   [esc] close   saved commands always confirm"))
	panel := s.panel.Width(width-4).Padding(1, 2).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

func (m dashboardModel) themePreviewView() string {
	s := m.styles()
	names := themeNames()
	width := min(max(46, m.width-16), 72)
	lines := []string{
		s.focus.Render("THEME PREVIEW"),
		s.muted.Render("Live preview across every semantic role"),
		"",
	}
	for index, name := range names {
		t := themes[name]
		swatch := themeSwatch(t, m.plain)
		line := fmt.Sprintf("%-12s %s", name, swatch)
		if index == m.themeCursor {
			line = s.selected.Render("› " + padCell(line, width-6))
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	lines = append(lines,
		"",
		s.live.Render("● online")+"  "+s.warning.Render("! refused")+"  "+s.failure.Render("× unavailable")+"  "+s.focus.Render("◆ focus"),
		"",
		s.muted.Render("[↑↓] preview   [enter] use this session"),
		s.muted.Render("[e] edit config to save   [esc] restore"),
	)
	panel := s.panel.Width(width-4).Padding(1, 2).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, fitTerminalView(panel, m.width, m.height))
}

func (m dashboardModel) fleetView() string {
	s := m.styles()
	width := min(max(42, m.width-16), 78)
	lines := []string{
		s.focus.Render("FLEET"),
		s.muted.Render("Saved endpoints · reachability only, never background authentication"),
		"",
	}
	limit := max(1, m.height-10)
	for index, host := range m.hosts {
		if index >= limit {
			lines = append(lines, s.muted.Render(fmt.Sprintf("… and %d more", len(m.hosts)-index)))
			break
		}
		name := padCell(truncateText(displayName(host), 18), 18)
		target := padCell(truncateText(host.Target, 28), 28)
		lines = append(lines, "  "+s.text.Render(name)+"  "+s.muted.Render(target)+"  "+m.statusText(s, host.Reachability))
	}
	if len(m.hosts) == 0 {
		lines = append(lines, s.muted.Render("No saved endpoints yet."))
	}
	lines = append(lines, "", s.muted.Render("[r] refresh selected from workspace   [esc] back"))
	panel := s.panel.Width(width-4).Padding(1, 2).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, fitTerminalView(panel, m.width, m.height))
}

func (m dashboardModel) transferView() string {
	s := m.styles()
	flow := m.transfer
	if flow == nil {
		return ""
	}
	width := min(max(48, m.width-12), 84)
	title := "PULL · REMOTE SOURCE"
	subtitle := "Scanning remote paths in the background…"
	switch flow.Stage {
	case transferPickRemoteSource:
		subtitle = "Choose what to download"
	case transferScanLocalSource:
		title = "PUSH · LOCAL SOURCE"
		subtitle = "Scanning the current directory in the background…"
	case transferPickLocalSource:
		title = "PUSH · LOCAL SOURCE"
		subtitle = "Choose a local file or directory"
	case transferScanRemoteDest:
		title = "PUSH · REMOTE DESTINATION"
		subtitle = "Scanning remote directories in the background…"
	case transferPickRemoteDest:
		title = "PUSH · REMOTE DESTINATION"
		subtitle = "Choose where to upload"
	}
	lines := []string{
		s.focus.Render(title),
		s.muted.Render(truncateText(flow.Host, width-6)),
		s.text.Render(subtitle),
		"",
	}
	if flow.Err != "" {
		lines = append(lines,
			s.failure.Render("Scan failed: "+truncateText(flow.Err, width-12)),
			s.muted.Render("[r] retry   [esc] cancel"),
		)
	} else if strings.HasPrefix(string(flow.Stage), "scan-") {
		lines = append(lines,
			s.muted.Render("◌ Working without leaving Nexus"),
			"",
			s.muted.Render("[esc] cancel"),
		)
	} else {
		maxRows := max(1, m.height-11)
		start := 0
		if flow.Cursor >= maxRows {
			start = flow.Cursor - maxRows + 1
		}
		end := min(len(flow.Items), start+maxRows)
		if len(flow.Items) == 0 {
			lines = append(lines, s.muted.Render("No paths found."), s.muted.Render("[esc] cancel"))
		}
		for index := start; index < end; index++ {
			label := transferPathLabel(flow.Items[index], flow.Stage)
			line := truncateText(label, width-8)
			if index == flow.Cursor {
				line = s.selected.Render("› " + padCell(line, width-6))
			} else {
				line = "  " + s.text.Render(line)
			}
			lines = append(lines, line)
		}
		lines = append(lines, "", s.muted.Render("[enter] choose   [↑↓] move   [esc] cancel"))
	}
	panel := s.panel.Width(width-4).Padding(1, 2).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, fitTerminalView(panel, m.width, m.height))
}

func transferPathLabel(item string, stage transferStage) string {
	if stage != transferPickLocalSource {
		return item
	}
	cwd, err := os.Getwd()
	if err != nil {
		return item
	}
	if item == cwd {
		return ".  ·  current directory"
	}
	relative, err := filepath.Rel(cwd, item)
	if err != nil {
		return item
	}
	if info, err := os.Stat(item); err == nil && info.IsDir() {
		return relative + "/"
	}
	return relative
}

func (m dashboardModel) savedCommandGuideView() string {
	s := m.styles()
	width := min(max(48, m.width-14), 78)
	lines := []string{
		s.focus.Render("SAVED COMMANDS"),
		s.text.Render("Reusable remote commands that Nexus always shows and confirms before running."),
		"",
		s.focus.Render("ADD ONE"),
		"1  Open the full config with e",
		"2  Add a name, description, and exact command",
		"3  Save, return to Nexus, then choose it from Ctrl+K",
		"",
		s.focus.Render("EXAMPLE · available for every host"),
		s.muted.Render("commands:"),
		s.muted.Render("  - name: uptime"),
		s.muted.Render("    description: Show system uptime"),
		s.muted.Render("    command: uptime"),
		"",
		s.muted.Render("Use host_profiles for one endpoint or tag_commands for a group."),
		s.warning.Render("Nexus never runs a saved command without confirmation."),
		"",
		s.muted.Render("[e/enter] edit full config   [esc] back"),
	}
	panel := s.panel.Width(width-4).Padding(1, 2).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, fitTerminalView(panel, m.width, m.height))
}

func (m dashboardModel) confirmCommandView() string {
	s := m.styles()
	selection := m.confirmAction
	width := min(max(48, m.width-16), 76)
	lines := []string{
		s.warning.Render("CONFIRM SAVED COMMAND"),
		s.muted.Render("Review the exact target and command before Nexus runs it."),
		"",
		s.text.Render("Name     ") + s.focus.Render(valueOr(selection.Command.Name, "unnamed")),
		s.text.Render("Target   ") + s.muted.Render(selection.Host),
		s.text.Render("Command  ") + s.text.Render(selection.Command.Command),
		"",
		s.warning.Render("This command runs on the remote host with your SSH access."),
		"",
		s.text.Render("[y/enter] run") + s.muted.Render("   [n/esc] cancel"),
	}
	panel := s.panel.Width(width-4).Padding(1, 2).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, fitTerminalView(panel, m.width, m.height))
}

func themeSwatch(t theme, plain bool) string {
	roles := []string{t.Focus, t.Live, t.Success, t.Warning, t.Error}
	var blocks []string
	for _, color := range roles {
		if plain {
			blocks = append(blocks, "◆")
			continue
		}
		blocks = append(blocks, lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render("◆"))
	}
	return strings.Join(blocks, " ")
}

func (m dashboardModel) helpView() string {
	s := m.styles()
	if m.height < 22 {
		compact := []string{
			s.focus.Render("NEXUS KEYS"),
			"",
			"enter  connect      /  find",
			"j/k    move         c  actions",
			"p/u    transfer     i  refresh info",
			"r      probe host   t  fleet",
			"a      saved cmds   ?  this help",
			"",
			"● online  ! refused  × unavailable",
			s.muted.Render("esc close"),
		}
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			s.panel.Padding(1, 2).Render(strings.Join(compact, "\n")))
	}
	help := []string{
		s.focus.Render("NEXUS KEYS"),
		"",
		"enter / s     connect with SSH",
		"j/k or ↑/↓   move selection",
		"g/G          first / last host",
		"/            search hosts",
		"ctrl+k / c   command palette",
		"p / u        pull / push",
		"i            refresh snapshot in background",
		"n / d        network / storage tools",
		"r / R        probe selected / all hosts",
		"t            open fleet overview",
		"a            explain and add saved commands",
		"e            edit full YAML configuration",
		"? / esc      close help",
		"",
		"● online   ! refused   × unavailable   ◌ checking",
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
		label := host.Target
		if host.Alias != "" {
			label = host.Alias + "  " + host.Target
		}
		line := prefix + label + "  " + plainReachability(host.Reachability, m.probing)
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

func plainReachability(result reachabilityResult, probing bool) string {
	switch result.Status {
	case reachOnline:
		return fmt.Sprintf("● %dms", max(1, result.Latency.Milliseconds()))
	case reachRefused:
		return "! refused"
	case reachTimeout:
		return "× timeout"
	case reachError:
		return "× error"
	default:
		if probing {
			return "◌ checking"
		}
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
	var resumeTarget string
	var resumeReachability map[string]reachabilityResult
	var notice string
	var noticeError bool
	for {
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
		model.statePath = a.stateFile
		model.indexMode = normalizeRemoteIndexMode(a.remoteIndex)
		model.notice = notice
		model.noticeError = noticeError
		if len(resumeReachability) > 0 {
			model.probing = false
			model.probeInitial = nil
			model.probeQueue = nil
			model.probeTargets = make(map[string]bool)
			model.probeTotal = 0
			model.probeComplete = 0
			for index := range model.hosts {
				if result, ok := resumeReachability[model.hosts[index].Target]; ok {
					model.hosts[index].Reachability = result
				}
			}
		}
		for index := range model.filtered {
			if model.hosts[model.filtered[index]].Target == resumeTarget {
				model.cursor = index
				break
			}
		}
		program := tea.NewProgram(model, tea.WithAltScreen())
		result, err := program.Run()
		if err != nil {
			return fmt.Errorf("dashboard failed: %w", err)
		}
		finalModel, ok := result.(dashboardModel)
		if !ok || finalModel.choice.Action == "" {
			return nil
		}
		resumeReachability = make(map[string]reachabilityResult, len(finalModel.hosts))
		for _, host := range finalModel.hosts {
			resumeReachability[host.Target] = host.Reachability
		}
		resumeTarget = finalModel.choice.Host
		err = a.executeDashboardSelection(finalModel.choice)
		if err != nil {
			notice = actionLabel(finalModel.choice.Action) + " failed: " + sanitizeTerminalText(err.Error())
			noticeError = true
			continue
		}
		notice = actionLabel(finalModel.choice.Action) + " finished"
		noticeError = false
	}
}

func actionLabel(action dashboardAction) string {
	switch action {
	case actionSSH:
		return "SSH session"
	case actionPull:
		return "Pull"
	case actionPush:
		return "Push"
	case actionTop:
		return "System monitor"
	case actionNet:
		return "Network tool"
	case actionInfo:
		return "System snapshot"
	case actionStorage:
		return "Storage tool"
	case actionCustom:
		return "Saved command"
	case actionConfig:
		return "Configuration"
	default:
		return "Action"
	}
}

func (a *app) executeDashboardSelection(selection dashboardSelection) error {
	if selection.Action == actionConfig {
		return a.editConfig()
	}
	if selection.Action == actionCustom {
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
	args := selection.Args
	if len(args) == 0 {
		args = []string{selection.Host}
	}
	return command.RunE(command, args)
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
	remote := remoteShellCommand("sh", command)
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
