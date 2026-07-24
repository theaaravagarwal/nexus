package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	actionSSH       dashboardAction = "ssh"
	actionPull      dashboardAction = "pull"
	actionPush      dashboardAction = "push"
	actionTop       dashboardAction = "top"
	actionNet       dashboardAction = "net"
	actionInfo      dashboardAction = "info"
	actionStorage   dashboardAction = "storage"
	actionCopyKey   dashboardAction = "copy-key"
	actionCustom    dashboardAction = "custom"
	actionConfig    dashboardAction = "config"
	actionFleet     dashboardAction = "fleet"
	actionThemes    dashboardAction = "themes"
	actionWorkspace dashboardAction = "workspace"
	actionProbe     dashboardAction = "probe"
	actionProbeAll  dashboardAction = "probe-all"
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
	GPUs         []string
	Memory       string
	Disk         string
	Disks        []diskUsage
	Tools        []string
	Score        float64
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
	Target      string
	OperationID uint64
	Activity    hostActivity
	Err         error
}

type transferScanMsg struct {
	Stage transferStage
	Items []string
	Err   error
}

type themeSaveMsg struct {
	Name string
	Err  error
}

type configuredCommandMsg struct {
	OperationID uint64
	Output      string
	Err         error
}

type actionUsageMsg struct {
	Err error
}

type operationPersistMsg struct {
	Err error
}

type workspaceSaveMsg struct {
	Name string
	Err  error
}

type dashboardOperation struct {
	operationSummary
	Output string
}

type activityEvent struct {
	Label      string
	Host       string
	Status     string
	Summary    string
	Output     string
	FinishedAt time.Time
	Duration   time.Duration
}

type configuredCommandResult struct {
	Action  dashboardAction
	Host    string
	Command commandConfig
	Output  string
	Err     string
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
	hosts              []dashboardHost
	filtered           []int
	cursor             int
	query              string
	filtering          bool
	width              int
	height             int
	choice             dashboardSelection
	done               bool
	commandOpen        bool
	commandFiltering   bool
	commandCursor      int
	commandQuery       string
	confirmOpen        bool
	confirmAction      dashboardSelection
	commandResult      *configuredCommandResult
	commandRunning     bool
	commandOffset      int
	actionUses         map[string]int
	transfer           *transferFlow
	helpOpen           bool
	themeOpen          bool
	themeCursor        int
	themeOriginal      theme
	themePreview       bool
	themeSaving        bool
	workspaceOpen      bool
	workspaceCursor    int
	workspaceOriginal  string
	workspaceSaving    bool
	workspace          string
	showTopology       bool
	probing            bool
	probeQueue         []string
	probeInitial       []string
	probeTargets       map[string]bool
	probeTotal         int
	probeComplete      int
	metadataBusy       map[string]bool
	statePath          string
	configPath         string
	indexMode          string
	notice             string
	noticeError        bool
	plain              bool
	theme              theme
	now                time.Time
	operation          *dashboardOperation
	operationPersisted bool
	operationID        uint64
	operationProbeID   uint64
	telemetry          map[string]hostTelemetry
	telemetryTarget    string
	telemetryGen       uint64
	telemetryFlight    uint64
	telemetryFocused   bool
	activities         []activityEvent
}

func newDashboardModel(hosts []string) dashboardModel {
	return newDashboardModelWithState(hosts, nexusState{Hosts: map[string]hostActivity{}}, time.Now())
}

func newDashboardModelWithState(hosts []string, state nexusState, now time.Time) dashboardModel {
	safe := dedupeKeepOrder(hosts)
	model := dashboardModel{
		width:            100,
		height:           30,
		showTopology:     false,
		probeTargets:     make(map[string]bool),
		metadataBusy:     make(map[string]bool),
		actionUses:       make(map[string]int, len(state.Actions)),
		indexMode:        "lazy",
		plain:            noColorRequested(),
		theme:            activeTheme(),
		workspace:        normalizeWorkspaceMode(loadedConfig.UI.Workspace),
		now:              now,
		telemetry:        make(map[string]hostTelemetry),
		telemetryGen:     1,
		telemetryFocused: true,
	}
	for action, count := range state.Actions {
		model.actionUses[action] = count
	}
	if state.LatestOperation != nil {
		snapshot := *state.LatestOperation
		model.operation = &dashboardOperation{operationSummary: snapshot}
		model.operationPersisted = true
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
			GPUs:     append([]string(nil), activity.GPUs...),
			Memory:   normalizeLegacyCapacityText(activity.Memory),
			Disk:     normalizeLegacyCapacityText(activity.Disk),
			Disks:    append([]diskUsage(nil), activity.Disks...),
			Tools:    activity.Tools,
			Score:    activity.Score,
			Updated:  activity.Updated,
			LastUsed: activity.LastUsed,
			Reachability: reachabilityResult{
				Target: target,
				Status: reachUnknown,
			},
		})
	}
	model.applyFilter()
	model.telemetryTarget = model.selectedTarget()
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
	commands := []tea.Cmd{telemetryTick(time.Second, m.telemetryGen)}
	if len(m.probeInitial) > 0 {
		commands = append(commands, m.probeCommands(m.probeInitial))
	}
	return tea.Batch(commands...)
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

func (m dashboardModel) metadataCommand(target string, operationID uint64) tea.Cmd {
	statePath := m.statePath
	return func() tea.Msg {
		if statePath == "" {
			return metadataRefreshMsg{Target: target, OperationID: operationID, Err: errors.New("metadata cache is unavailable")}
		}
		err := refreshHostMetadata(statePath, target)
		if err != nil {
			return metadataRefreshMsg{Target: target, OperationID: operationID, Err: err}
		}
		state, err := loadState(statePath)
		return metadataRefreshMsg{Target: target, OperationID: operationID, Activity: state.Hosts[target], Err: err}
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
	m.startOperation(actionInfo, "Refresh info", target)
	return m, m.metadataCommand(target, m.operationID)
}

func (m *dashboardModel) startOperation(action dashboardAction, label, host string) {
	m.operationID++
	m.operationPersisted = false
	m.operation = &dashboardOperation{operationSummary: operationSummary{
		Action:    string(action),
		Label:     label,
		Host:      host,
		Status:    "running",
		StartedAt: time.Now(),
	}}
}

func (m *dashboardModel) finishOperation(status, summary, output string) tea.Cmd {
	if m.operation == nil {
		return nil
	}
	now := time.Now()
	m.operation.Status = status
	m.operation.Summary = sanitizeTerminalText(summary)
	m.operation.FinishedAt = now
	m.operation.Duration = now.Sub(m.operation.StartedAt)
	m.operation.Output = sanitizeCommandOutput(output)
	m.activities = appendActivity(m.activities, activityEvent{
		Label: m.operation.Label, Host: m.operation.Host, Status: status,
		Summary: m.operation.Summary, Output: m.operation.Output,
		FinishedAt: now, Duration: m.operation.Duration,
	})
	if m.statePath == "" {
		return nil
	}
	snapshot := m.operation.operationSummary
	path := m.statePath
	return func() tea.Msg {
		return operationPersistMsg{Err: recordLatestOperation(path, snapshot)}
	}
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
	case "esc":
		m.transfer = nil
		return m, nil
	case "r":
		if flow.Err != "" {
			flow.Err = ""
			m.transfer = &flow
			return m, m.transferScanCommand(flow.Stage)
		}
	case "k":
		flow.Cursor = max(0, flow.Cursor-1)
	case "j":
		flow.Cursor = min(max(0, len(flow.Items)-1), flow.Cursor+1)
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
	case tea.FocusMsg:
		m.telemetryFocused = true
		return m, telemetryTick(100*time.Millisecond, m.telemetryGen)
	case tea.BlurMsg:
		m.telemetryFocused = false
		return m, nil
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
			m.sortHostsByAvailabilityKeeping(m.selectedTarget())
			var operationCmd tea.Cmd
			if m.operationProbeID != 0 && m.operationProbeID == m.operationID {
				online := 0
				for _, host := range m.hosts {
					if m.operation != nil && m.operation.Action == string(actionProbe) && host.Target != m.operation.Host {
						continue
					}
					if host.Reachability.Status == reachOnline {
						online++
					}
				}
				operationCmd = m.finishOperation(
					"success",
					fmt.Sprintf("%d of %d online", online, m.probeTotal),
					"",
				)
			}
			m.operationProbeID = 0
			delay := time.Duration(loadedConfig.Reachability.CacheSeconds) * time.Second
			return m, tea.Batch(
				operationCmd,
				tea.Tick(delay, func(time.Time) tea.Msg { return probeTickMsg{} }),
				telemetryTick(100*time.Millisecond, m.telemetryGen),
			)
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
			if msg.OperationID == m.operationID {
				return m, m.finishOperation("error", "Snapshot failed", msg.Err.Error())
			}
			return m, nil
		}
		for i := range m.hosts {
			if m.hosts[i].Target != msg.Target {
				continue
			}
			m.hosts[i].OS = msg.Activity.OS
			m.hosts[i].CPU = msg.Activity.CPU
			m.hosts[i].GPUs = append([]string(nil), msg.Activity.GPUs...)
			m.hosts[i].Memory = msg.Activity.Memory
			m.hosts[i].Disk = msg.Activity.Disk
			m.hosts[i].Disks = append([]diskUsage(nil), msg.Activity.Disks...)
			m.hosts[i].Tools = append([]string(nil), msg.Activity.Tools...)
			m.hosts[i].Updated = msg.Activity.Updated
			break
		}
		m.notice = "System snapshot refreshed for " + m.displayNameForTarget(msg.Target)
		m.noticeError = false
		usedAt := time.Now()
		if m.statePath != "" {
			if err := recordHostSuccess(m.statePath, msg.Target, usedAt); err != nil {
				logVerbose("failed to record host activity: %v", err)
			}
		}
		m.markHostUsed(msg.Target, usedAt)
		if msg.OperationID == m.operationID {
			return m, m.finishOperation("success", "System details updated", "")
		}
		return m, nil
	case themeSaveMsg:
		m.themeSaving = false
		if msg.Err != nil {
			m.notice = "Theme save failed: " + sanitizeTerminalText(msg.Err.Error())
			m.noticeError = true
			return m, nil
		}
		loadedConfig.UI.Theme = msg.Name
		m.theme = activeTheme()
		m.themeOriginal = m.theme
		m.themeOpen = false
		m.themePreview = false
		m.notice = "Theme saved as default: " + msg.Name
		m.noticeError = false
		return m, nil
	case workspaceSaveMsg:
		m.workspaceSaving = false
		if msg.Err != nil {
			m.notice = "Workspace save failed: " + sanitizeTerminalText(msg.Err.Error())
			m.noticeError = true
			return m, nil
		}
		loadedConfig.UI.Workspace = msg.Name
		m.workspace = msg.Name
		m.workspaceOriginal = msg.Name
		m.workspaceOpen = false
		m.notice = "Workspace saved as default: " + msg.Name
		m.noticeError = false
		return m, nil
	case configuredCommandMsg:
		m.commandRunning = false
		if m.commandResult == nil {
			return m, nil
		}
		m.commandResult.Output = msg.Output
		if msg.Err != nil {
			m.commandResult.Err = sanitizeTerminalText(msg.Err.Error())
			if msg.OperationID == m.operationID {
				return m, m.finishOperation("error", "Command failed", msg.Output+"\n"+msg.Err.Error())
			}
			return m, nil
		} else {
			usedAt := time.Now()
			if m.statePath != "" {
				if err := recordHostSuccess(m.statePath, m.commandResult.Host, usedAt); err != nil {
					logVerbose("failed to record host activity: %v", err)
				}
			}
			m.markHostUsed(m.commandResult.Host, usedAt)
		}
		if msg.OperationID == m.operationID {
			return m, m.finishOperation("success", "Command finished", msg.Output)
		}
		return m, nil
	case operationPersistMsg:
		if msg.Err != nil {
			logVerbose("failed to record latest operation: %v", msg.Err)
		}
		return m, nil
	case telemetryTickMsg:
		if msg.Generation != m.telemetryGen {
			return m, nil
		}
		target := m.selectedTarget()
		if target != m.telemetryTarget {
			m.telemetryTarget = target
			m.telemetryGen++
			return m, telemetryTick(100*time.Millisecond, m.telemetryGen)
		}
		if target == "" || !m.telemetryFocused || m.telemetryPaused() {
			return m, telemetryTick(telemetryInterval, m.telemetryGen)
		}
		if m.telemetryFlight != 0 {
			return m, telemetryTick(time.Second, m.telemetryGen)
		}
		host := m.selectedHost()
		if host.Reachability.Status != reachOnline {
			return m, telemetryTick(telemetryInterval, m.telemetryGen)
		}
		entry := m.telemetry[target]
		if wait := time.Until(entry.NextAttempt); wait > 0 {
			return m, telemetryTick(min(wait, telemetryMaxBackoff), m.telemetryGen)
		}
		m.telemetryFlight = m.telemetryGen
		return m, telemetryCommand(target, m.telemetryGen)
	case telemetryResultMsg:
		if msg.Generation == m.telemetryFlight {
			m.telemetryFlight = 0
		}
		if msg.Generation != m.telemetryGen || msg.Target != m.selectedTarget() {
			return m, telemetryTick(100*time.Millisecond, m.telemetryGen)
		}
		entry := m.telemetry[msg.Target]
		if msg.Err != nil {
			entry.Failures++
			entry.LastErr = sanitizeTerminalText(msg.Err.Error())
			delay := telemetryBackoff(entry.Failures, m.telemetryGen)
			entry.NextAttempt = time.Now().Add(delay)
			m.telemetry[msg.Target] = entry
			return m, telemetryTick(delay, m.telemetryGen)
		}
		entry.Failures = 0
		entry.LastErr = ""
		entry.NextAttempt = time.Time{}
		entry.History = appendTelemetry(entry.History, msg.Sample)
		entry.Current = entry.History[len(entry.History)-1]
		m.telemetry[msg.Target] = entry
		return m, telemetryTick(telemetryInterval, m.telemetryGen)
	case actionUsageMsg:
		if msg.Err != nil {
			logVerbose("failed to record action usage: %v", msg.Err)
		}
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
	if m.commandResult != nil {
		switch key {
		case "esc":
			m.commandResult = nil
			m.commandOffset = 0
		case "k":
			if !m.commandRunning {
				m.commandOffset = max(0, m.commandOffset-1)
			}
		case "j":
			if !m.commandRunning {
				lines := strings.Split(m.commandResult.Output, "\n")
				m.commandOffset = min(max(0, len(lines)-1), m.commandOffset+1)
			}
		case "r":
			if !m.commandRunning {
				m.commandRunning = true
				m.commandOffset = 0
				m.commandResult.Output = ""
				m.commandResult.Err = ""
				m.startOperation(m.commandResult.Action, m.commandResult.Command.Name, m.commandResult.Host)
				if m.commandResult.Action == actionStorage {
					return m, m.storageCommandCmd(m.commandResult.Host, m.operationID)
				}
				return m, m.configuredCommandCmd(m.commandResult.Host, m.commandResult.Command.Command, m.operationID)
			}
		}
		return m, nil
	}
	if m.helpOpen {
		switch key {
		case "esc":
			m.helpOpen = false
		}
		return m, nil
	}
	if m.confirmOpen {
		switch key {
		case "y":
			if m.confirmAction.Action == actionCopyKey {
				usageCmd := m.recordActionUsage(actionCopyKey, commandConfig{})
				m.choice = m.confirmAction
				m.done = true
				return m, tea.Sequence(usageCmd, tea.Quit)
			}
			usageCmd := m.recordActionUsage(actionCustom, m.confirmAction.Command)
			if !m.confirmAction.Command.Interactive {
				selection := m.confirmAction
				m.confirmOpen = false
				m.confirmAction = dashboardSelection{}
				m.commandRunning = true
				m.commandOffset = 0
				m.commandResult = &configuredCommandResult{
					Action: actionCustom, Host: selection.Host, Command: selection.Command,
				}
				m.startOperation(actionCustom, selection.Command.Name, selection.Host)
				return m, tea.Batch(usageCmd, m.configuredCommandCmd(selection.Host, selection.Command.Command, m.operationID))
			}
			m.choice = m.confirmAction
			m.done = true
			return m, tea.Sequence(usageCmd, tea.Quit)
		case "esc":
			m.confirmOpen = false
			m.confirmAction = dashboardSelection{}
		}
		return m, nil
	}
	if m.showTopology {
		switch key {
		case "esc":
			m.showTopology = false
		}
		return m, nil
	}
	if m.themeOpen {
		names := themeNames()
		switch key {
		case "esc":
			m.theme = m.themeOriginal
			m.themeOpen = false
		case "k":
			m.themeCursor = (m.themeCursor + len(names) - 1) % len(names)
			m.theme = themes[names[m.themeCursor]]
		case "j":
			m.themeCursor = (m.themeCursor + 1) % len(names)
			m.theme = themes[names[m.themeCursor]]
		case "enter":
			m.themeOpen = false
			m.themePreview = m.theme.Name != activeTheme().Name
			m.notice = "Using theme for this session: " + m.theme.Name
			m.noticeError = false
		case "s":
			if !m.themeSaving {
				m.themeSaving = true
				name := names[m.themeCursor]
				return m, m.saveThemeCommand(name)
			}
		}
		return m, nil
	}
	if m.workspaceOpen {
		names := workspaceModes()
		switch key {
		case "esc":
			m.workspace = m.workspaceOriginal
			m.workspaceOpen = false
		case "k":
			m.workspaceCursor = (m.workspaceCursor + len(names) - 1) % len(names)
			m.workspace = names[m.workspaceCursor]
		case "j":
			m.workspaceCursor = (m.workspaceCursor + 1) % len(names)
			m.workspace = names[m.workspaceCursor]
		case "enter":
			m.workspaceOpen = false
			m.notice = "Using workspace for this session: " + m.workspace
			m.noticeError = false
		case "s":
			if !m.workspaceSaving {
				m.workspaceSaving = true
				name := names[m.workspaceCursor]
				return m, m.saveWorkspaceCommand(name)
			}
		}
		return m, nil
	}
	if m.commandOpen {
		commands := m.filteredCommands()
		switch key {
		case "esc":
			m.commandOpen = false
			m.commandFiltering = false
			m.commandQuery = ""
		case "backspace":
			runes := []rune(m.commandQuery)
			if m.commandFiltering && len(runes) > 0 {
				m.commandQuery = string(runes[:len(runes)-1])
				m.commandCursor = 0
			}
		case "/":
			if !m.commandFiltering {
				m.commandFiltering = true
				m.commandQuery = ""
				m.commandCursor = 0
			}
		case " ":
			if m.commandFiltering {
				m.commandQuery += " "
				m.commandCursor = 0
			}
		case "k":
			if m.commandFiltering {
				m.commandQuery += "k"
				m.commandCursor = 0
			} else {
				m.commandCursor = max(0, m.commandCursor-1)
			}
		case "j":
			if m.commandFiltering {
				m.commandQuery += "j"
				m.commandCursor = 0
			} else {
				m.commandCursor = min(max(0, len(commands)-1), m.commandCursor+1)
			}
		case "enter":
			if len(commands) == 0 {
				return m, nil
			}
			command := commands[m.commandCursor]
			var usageCmd tea.Cmd
			if command.Action != actionCustom {
				usageCmd = m.recordActionUsage(command.Action, command.Command)
			}
			switch command.Action {
			case actionPull, actionPush:
				m.commandOpen = false
				m.commandQuery = ""
				updated, commandCmd := m.startTransfer(command.Action)
				return updated, tea.Batch(usageCmd, commandCmd)
			case actionInfo:
				m.commandOpen = false
				m.commandFiltering = false
				m.commandQuery = ""
				updated, commandCmd := m.startMetadataRefresh()
				return updated, tea.Batch(usageCmd, commandCmd)
			case actionProbe:
				m.commandOpen = false
				m.commandFiltering = false
				m.commandQuery = ""
				target := m.selectedTarget()
				if target != "" && !m.probing {
					m.startOperation(actionProbe, "Check host", target)
					m.operationProbeID = m.operationID
					m, command := m.beginProbe([]string{target})
					return m, tea.Batch(usageCmd, command)
				}
				return m, usageCmd
			case actionProbeAll:
				m.commandOpen = false
				m.commandFiltering = false
				m.commandQuery = ""
				if !m.probing {
					m.startOperation(actionProbeAll, "Check all hosts", "")
					m.operationProbeID = m.operationID
					m, command := m.beginProbe(m.allTargets())
					return m, tea.Batch(usageCmd, command)
				}
				return m, usageCmd
			case actionFleet:
				m.commandOpen = false
				m.commandFiltering = false
				m.showTopology = true
				return m, usageCmd
			case actionThemes:
				m.commandOpen = false
				m.commandFiltering = false
				m.openThemePreview()
				return m, usageCmd
			case actionWorkspace:
				m.commandOpen = false
				m.commandFiltering = false
				m.openWorkspacePreview()
				return m, usageCmd
			case actionConfig:
				updated, commandCmd := m.choose(actionConfig)
				return updated, tea.Sequence(usageCmd, commandCmd)
			case actionStorage:
				m.commandOpen = false
				m.commandFiltering = false
				m.commandQuery = ""
				m.commandRunning = true
				m.commandOffset = 0
				m.commandResult = &configuredCommandResult{
					Action: actionStorage,
					Host:   m.selectedTarget(),
					Command: commandConfig{
						Name: "Storage", Description: "All mounted filesystems",
					},
				}
				m.startOperation(actionStorage, "Storage", m.selectedTarget())
				return m, tea.Batch(usageCmd, m.storageCommandCmd(m.selectedTarget(), m.operationID))
			case actionCopyKey:
				m.commandOpen = false
				m.commandFiltering = false
				m.commandQuery = ""
				args, err := buildSSHCopyIDArgs(m.selectedTarget())
				if err != nil {
					m.notice = "Copy key unavailable: " + sanitizeTerminalText(err.Error())
					m.noticeError = true
					return m, nil
				}
				m.confirmOpen = true
				m.confirmAction = dashboardSelection{
					Action: actionCopyKey,
					Host:   m.selectedTarget(),
					Command: commandConfig{
						Name:        "Copy SSH key",
						Description: "Set up passwordless SSH",
						Command:     formatCommand("ssh-copy-id", args),
						Interactive: true,
					},
				}
				return m, nil
			}
			if command.Action == actionCustom {
				m.commandOpen = false
				m.confirmOpen = true
				m.confirmAction = dashboardSelection{
					Action: actionCustom, Host: m.selectedTarget(), Command: command.Command,
				}
				return m, nil
			}
			updated, commandCmd := m.choose(command.Action)
			return updated, tea.Sequence(usageCmd, commandCmd)
		default:
			if m.commandFiltering && msg.Type == tea.KeyRunes {
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
	case "q":
		m.done = true
		return m, tea.Quit
	case "h":
		m.helpOpen = true
	case "/":
		m.filtering = true
	case "a":
		m.commandOpen = true
		m.commandFiltering = false
		m.commandCursor = 0
		m.commandQuery = ""
	case "k":
		before := m.selectedTarget()
		m.moveCursor(-1)
		if before != m.selectedTarget() {
			m.resetTelemetryTarget()
			return m, telemetryTick(100*time.Millisecond, m.telemetryGen)
		}
	case "j":
		before := m.selectedTarget()
		m.moveCursor(1)
		if before != m.selectedTarget() {
			m.resetTelemetryTarget()
			return m, telemetryTick(100*time.Millisecond, m.telemetryGen)
		}
	case "enter":
		return m.choose(actionSSH)
	}
	return m, nil
}

func (m dashboardModel) telemetryPaused() bool {
	return m.helpOpen || m.commandOpen || m.themeOpen || m.workspaceOpen || m.confirmOpen ||
		m.transfer != nil || m.commandResult != nil || m.showTopology
}

func (m *dashboardModel) resetTelemetryTarget() {
	m.telemetryTarget = m.selectedTarget()
	m.telemetryGen++
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

func workspaceModes() []string {
	return []string{"workbench", "console", "fleet"}
}

func (m *dashboardModel) openWorkspacePreview() {
	names := workspaceModes()
	m.workspaceOriginal = m.workspace
	m.workspaceCursor = 0
	for index, name := range names {
		if name == m.workspace {
			m.workspaceCursor = index
			break
		}
	}
	m.workspaceOpen = true
}

func (m dashboardModel) saveThemeCommand(name string) tea.Cmd {
	configPath := m.configPath
	return func() tea.Msg {
		return themeSaveMsg{Name: name, Err: saveThemeToConfig(configPath, name)}
	}
}

func (m dashboardModel) saveWorkspaceCommand(name string) tea.Cmd {
	configPath := m.configPath
	return func() tea.Msg {
		return workspaceSaveMsg{Name: name, Err: saveWorkspaceToConfig(configPath, name)}
	}
}

func (m dashboardModel) configuredCommandCmd(host, command string, operationID uint64) tea.Cmd {
	return func() tea.Msg {
		output, err := runConfiguredRemoteCommandCaptured(host, command)
		return configuredCommandMsg{OperationID: operationID, Output: output, Err: err}
	}
}

const storageInventoryScript = `LC_ALL=C df -Pk 2>/dev/null | awk 'NR > 1 && NF >= 6 && $2 ~ /^[0-9]+$/ && $2 > 0 { mount=$6; for (field=7; field<=NF; field++) mount=mount " " $field; printf "DISK=%s\t%s\t%.0f\t%.0f\t%.0f\n", $1, mount, $3 * 1024, $4 * 1024, $2 * 1024 }'`

func (m dashboardModel) storageCommandCmd(host string, operationID uint64) tea.Cmd {
	return func() tea.Msg {
		output, err := captureRemoteStorage(host)
		return configuredCommandMsg{OperationID: operationID, Output: output, Err: err}
	}
}

func captureRemoteStorage(target string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command, err := buildSSHCommand(ctx, target, false, remoteShellCommand("sh", storageInventoryScript))
	if err != nil {
		return "", err
	}
	output := &cappedCommandOutput{limit: maxMetadataBytes}
	command.Stdout = output
	command.Stderr = output
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return output.String(), fmt.Errorf("storage scan timed out: %w", ctx.Err())
		}
		return output.String(), fmt.Errorf("storage scan failed: %w", err)
	}
	snapshot := parseMetadata(output.builder.String())
	if len(snapshot.Disks) == 0 {
		return "", errors.New("storage scan returned no mounted filesystems")
	}
	return renderStorageInventory(snapshot.Disks), nil
}

func renderStorageInventory(disks []diskUsage) string {
	lines := []string{"MOUNT  USED / TOTAL  AVAILABLE  USE  FILESYSTEM"}
	for _, disk := range disks {
		percent := 0.0
		if disk.TotalBytes > 0 {
			percent = float64(disk.UsedBytes) * 100 / float64(disk.TotalBytes)
		}
		lines = append(lines, fmt.Sprintf(
			"%s  %s / %s  %s free  %.0f%%  %s",
			disk.Mountpoint,
			valueOr(formatDecimalBytes(disk.UsedBytes), "0 GB"),
			valueOr(formatDecimalBytes(disk.TotalBytes), "0 GB"),
			valueOr(formatDecimalBytes(disk.AvailableBytes), "0 GB"),
			percent,
			disk.Filesystem,
		))
	}
	return strings.Join(lines, "\n")
}

func dashboardActionUsageKey(action dashboardAction, command commandConfig) string {
	if action == actionCustom {
		return "custom:" + strings.ToLower(strings.TrimSpace(command.Name))
	}
	return string(action)
}

func (m *dashboardModel) recordActionUsage(action dashboardAction, command commandConfig) tea.Cmd {
	key := dashboardActionUsageKey(action, command)
	if key == "" {
		return nil
	}
	if m.actionUses == nil {
		m.actionUses = map[string]int{}
	}
	m.actionUses[key]++
	statePath := m.statePath
	if statePath == "" {
		return nil
	}
	return func() tea.Msg {
		return actionUsageMsg{Err: recordActionUse(statePath, key)}
	}
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
	m.applyFilterKeeping(m.selectedTarget())
}

func (m *dashboardModel) applyFilterKeeping(selected string) {
	needle := strings.ToLower(strings.TrimSpace(m.query))
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

func (m *dashboardModel) sortHostsByAvailabilityKeeping(selected string) {
	sort.SliceStable(m.hosts, func(left, right int) bool {
		leftOnline := m.hosts[left].Reachability.Status == reachOnline
		rightOnline := m.hosts[right].Reachability.Status == reachOnline
		if leftOnline != rightOnline {
			return leftOnline
		}
		leftUsage := frecency(hostActivity{
			Score: m.hosts[left].Score, LastUsed: m.hosts[left].LastUsed,
		}, m.now)
		rightUsage := frecency(hostActivity{
			Score: m.hosts[right].Score, LastUsed: m.hosts[right].LastUsed,
		}, m.now)
		return leftUsage > rightUsage
	})
	m.applyFilterKeeping(selected)
}

func (m *dashboardModel) markHostUsed(target string, usedAt time.Time) {
	selected := m.selectedTarget()
	for index := range m.hosts {
		if m.hosts[index].Target != target {
			continue
		}
		m.hosts[index].Score++
		if m.hosts[index].Score < 1 {
			m.hosts[index].Score = 1
		}
		m.hosts[index].LastUsed = usedAt
		break
	}
	m.now = usedAt
	m.sortHostsByAvailabilityKeeping(selected)
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
		commands := []dashboardCommand{
			{Label: "Themes", Description: "Preview or save a theme", Action: actionThemes},
			{Label: "Workspace", Description: "Choose the dashboard layout", Action: actionWorkspace},
			{Label: "Config", Description: "Commands, profiles, and themes", Action: actionConfig},
		}
		m.sortCommandsByUsage(commands)
		return commands
	}
	commands := []dashboardCommand{
		{"Connect", "Open SSH session", actionSSH, commandConfig{}},
		{"Copy key", "Set up passwordless SSH", actionCopyKey, commandConfig{}},
		{"Pull", "Download from remote", actionPull, commandConfig{}},
		{"Push", "Upload to remote", actionPush, commandConfig{}},
		{"Refresh info", "Update system details", actionInfo, commandConfig{}},
		{"Check host", "Refresh this connection", actionProbe, commandConfig{}},
		{"Check all", "Refresh every connection", actionProbeAll, commandConfig{}},
		{"Monitor", "Open system monitor", actionTop, commandConfig{}},
		{"Network", "Open network diagnostics", actionNet, commandConfig{}},
		{"Storage", "Inspect disk usage", actionStorage, commandConfig{}},
		{"Fleet", "Inspect saved hosts", actionFleet, commandConfig{}},
		{"Themes", "Preview or save a theme", actionThemes, commandConfig{}},
		{"Workspace", "Choose the dashboard layout", actionWorkspace, commandConfig{}},
	}
	for _, command := range commandsForTarget(m.selectedTarget()) {
		commands = append(commands, dashboardCommand{
			Label: command.Name, Description: command.Description, Action: actionCustom, Command: command,
		})
	}
	commands = append(commands, dashboardCommand{"Config", "Commands, profiles, and themes", actionConfig, commandConfig{}})
	m.sortCommandsByUsage(commands)
	return commands
}

func (m dashboardModel) sortCommandsByUsage(commands []dashboardCommand) {
	pinOrder := make(map[string]int, len(loadedConfig.UI.PinnedActions))
	for index, pin := range loadedConfig.UI.PinnedActions {
		pinOrder[pin] = index
	}
	sort.SliceStable(commands, func(left, right int) bool {
		leftPin, leftPinned := pinOrder[dashboardCommandPinKey(commands[left])]
		rightPin, rightPinned := pinOrder[dashboardCommandPinKey(commands[right])]
		if leftPinned != rightPinned {
			return leftPinned
		}
		if leftPinned && leftPin != rightPin {
			return leftPin < rightPin
		}
		leftUses := m.actionUses[dashboardActionUsageKey(commands[left].Action, commands[left].Command)]
		rightUses := m.actionUses[dashboardActionUsageKey(commands[right].Action, commands[right].Command)]
		return leftUses > rightUses
	})
}

func dashboardCommandPinKey(command dashboardCommand) string {
	if command.Action == actionCustom {
		id := command.Command.ID
		if id == "" {
			id = sanitizeCommandID(command.Command.Name)
		}
		return "command:" + id
	}
	return string(command.Action)
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
		return m.finishView(m.tinyView())
	}
	if m.height < 16 {
		return m.finishView(m.shortView())
	}
	if m.helpOpen {
		return m.finishView(m.helpView())
	}
	if m.commandResult != nil {
		return m.finishView(m.commandResultView())
	}
	if m.transfer != nil {
		return m.finishView(m.transferView())
	}
	if m.confirmOpen {
		return m.finishView(m.confirmCommandView())
	}
	if m.showTopology {
		return m.finishView(m.fleetView())
	}
	if m.commandOpen {
		return m.finishView(m.commandPaletteView())
	}
	if m.themeOpen {
		return m.finishView(m.themePreviewView())
	}
	if m.workspaceOpen {
		return m.finishView(m.workspacePreviewView())
	}
	s := m.styles()
	header := m.headerView(s)
	footer := m.footerView(s)
	bodyHeight := max(3, m.height-lipgloss.Height(header)-lipgloss.Height(footer))

	var body string
	switch {
	case m.width >= 150 && bodyHeight >= 24:
		body = m.ultraWideView(s, m.width, bodyHeight)
	case m.width >= 96:
		hostWidth := min(48, max(38, m.width*38/100))
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			fitTerminalView(m.hostListView(s, hostWidth, bodyHeight), hostWidth, bodyHeight),
			fitTerminalView(m.detailView(s, m.width-hostWidth, bodyHeight), m.width-hostWidth, bodyHeight),
		)
	case m.width >= 72 && bodyHeight >= 18:
		hostHeight := max(10, bodyHeight*3/5)
		detailHeight := bodyHeight - hostHeight
		body = lipgloss.JoinVertical(lipgloss.Left,
			fitTerminalView(m.hostListView(s, m.width, hostHeight), m.width, hostHeight),
			fitTerminalView(m.compactDetailView(s, m.width, detailHeight), m.width, detailHeight),
		)
	default:
		body = m.compactView(s, m.width, bodyHeight)
	}
	body = fitTerminalView(body, m.width, bodyHeight)
	return m.finishView(
		lipgloss.JoinVertical(lipgloss.Left, header, body, footer),
	)
}

func (m dashboardModel) finishView(view string) string {
	rendered := fitTerminalView(view, m.width, m.height)
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

func (m dashboardModel) ultraWideView(s dashboardStyles, width, height int) string {
	switch normalizeWorkspaceMode(m.workspace) {
	case "console":
		return m.consoleWorkspaceView(s, width, height)
	case "fleet":
		return m.fleetWorkspaceView(s, width, height)
	default:
		return m.workbenchWorkspaceView(s, width, height)
	}
}

func (m dashboardModel) workspaceColumns(s dashboardStyles, width, height int) string {
	contentWidth := width
	hostWidth := min(42, max(38, contentWidth*24/100))
	actionWidth := min(38, max(32, contentWidth*20/100))
	detailWidth := max(56, contentWidth-hostWidth-actionWidth)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		fitTerminalView(m.hostListView(s, hostWidth, height), hostWidth, height),
		fitTerminalView(m.detailView(s, detailWidth, height), detailWidth, height),
		fitTerminalView(m.actionRailView(s, actionWidth, height), actionWidth, height),
	)
}

func (m dashboardModel) workbenchWorkspaceView(s dashboardStyles, width, height int) string {
	topHeight := min(44, max(22, height*3/5))
	topHeight = min(topHeight, max(18, height-12))
	if host := m.selectedHost(); len(host.Disks) > 6 {
		topHeight = min(max(18, height-12), max(topHeight, 24+min(12, len(host.Disks)-6)))
	}
	deckHeight := max(10, height-topHeight)
	pulseWidth := max(64, width*3/5)
	activityWidth := width - pulseWidth
	top := m.workspaceColumns(s, width, topHeight)
	deckBody := lipgloss.JoinHorizontal(lipgloss.Top,
		fitTerminalView(m.telemetryView(s, pulseWidth, deckHeight), pulseWidth, deckHeight),
		fitTerminalView(m.activityView(s, activityWidth, deckHeight), activityWidth, deckHeight),
	)
	deck := m.frameDeck(deckBody, width, deckHeight)
	content := lipgloss.JoinVertical(lipgloss.Left, top, deck)
	return lipgloss.NewStyle().Width(width).Height(height).Render(content)
}

func (m dashboardModel) consoleWorkspaceView(s dashboardStyles, width, height int) string {
	topHeight := min(24, max(18, height/3))
	deckHeight := max(10, height-topHeight)
	outputWidth := max(72, width*2/3)
	top := m.workspaceColumns(s, width, topHeight)
	deckBody := lipgloss.JoinHorizontal(lipgloss.Top,
		fitTerminalView(m.consoleOutputView(s, outputWidth, deckHeight), outputWidth, deckHeight),
		fitTerminalView(m.activityView(s, width-outputWidth, deckHeight), width-outputWidth, deckHeight),
	)
	deck := m.frameDeck(deckBody, width, deckHeight)
	return lipgloss.NewStyle().Width(width).Height(height).
		Render(lipgloss.JoinVertical(lipgloss.Left, top, deck))
}

func (m dashboardModel) fleetWorkspaceView(s dashboardStyles, width, height int) string {
	topHeight := min(22, max(18, height/3))
	top := m.workspaceColumns(s, width, topHeight)
	fleetHeight := max(10, height-topHeight)
	fleet := m.frameDeck(m.fleetDeckView(s, width, fleetHeight), width, fleetHeight)
	return lipgloss.NewStyle().Width(width).Height(height).
		Render(lipgloss.JoinVertical(lipgloss.Left, top, fleet))
}

func (m dashboardModel) frameDeck(content string, width, height int) string {
	dividerStyle := lipgloss.NewStyle()
	if !m.plain {
		dividerStyle = dividerStyle.Foreground(lipgloss.Color(m.theme.Border))
		if m.theme.Surface != "" {
			dividerStyle = dividerStyle.Background(lipgloss.Color(m.theme.Surface))
		}
	}
	divider := dividerStyle.Render(strings.Repeat("─", max(1, width)))
	return lipgloss.JoinVertical(lipgloss.Left,
		divider,
		fitTerminalView(content, width, max(1, height-1)),
	)
}

func (m dashboardModel) actionRailView(s dashboardStyles, width, height int) string {
	lines := []string{
		s.focus.Render("PINNED & FREQUENT"),
		s.muted.Render("Your order, then usage"),
		"",
	}
	commands := m.availableCommands()
	pins := make(map[string]struct{}, len(loadedConfig.UI.PinnedActions))
	for _, pin := range loadedConfig.UI.PinnedActions {
		pins[pin] = struct{}{}
	}
	limit := min(len(commands), min(8, max(4, (height-8)/2)))
	for _, command := range commands[:limit] {
		marker := "  "
		if _, pinned := pins[dashboardCommandPinKey(command)]; pinned {
			marker = "◆ "
		}
		lines = append(lines,
			s.text.Render(marker+truncateText(command.Label, max(1, width-7))),
			s.muted.Render("  "+truncateText(command.Description, max(1, width-7))),
		)
	}
	if hidden := len(commands) - limit; hidden > 0 {
		lines = append(lines, s.muted.Render(fmt.Sprintf("  +%d more", hidden)))
	}
	lines = append(lines, "", s.key.Render("[a]")+" "+s.muted.Render("open actions"))
	return s.panel.BorderTop(false).BorderRight(false).BorderBottom(false).
		Width(max(1, width-1)).Height(max(1, height-1)).Padding(1, 1).
		Render(strings.Join(lines, "\n"))
}

func (m dashboardModel) telemetryView(s dashboardStyles, width, height int) string {
	host := m.selectedHost()
	entry := m.telemetry[host.Target]
	lines := []string{s.focus.Render("HOST PULSE")}
	if host.Target == "" {
		lines = append(lines, s.muted.Render("Select a host to begin sampling."))
	} else if entry.Current.CollectedAt.IsZero() {
		status := "Waiting for an online sample"
		if entry.Failures > 0 {
			status = fmt.Sprintf("Unavailable · retrying with backoff (%d)", entry.Failures)
		}
		lines = append(lines,
			s.text.Render(displayName(host))+"  "+m.statusText(s, host.Reachability),
			s.muted.Render(status),
		)
	} else {
		sample := entry.Current
		updated := relativeTime(sample.CollectedAt, time.Now())
		lines = append(lines,
			s.text.Render(displayName(host))+"  "+m.statusText(s, host.Reachability)+
				s.muted.Render("  ·  sampled "+updated),
			"",
			s.text.Render("Uptime   ")+s.muted.Render(formatUptime(sample.Uptime)),
			s.text.Render("Load     ")+s.muted.Render(fmt.Sprintf("%.2f across %d cores", sample.LoadOne, sample.CPUCores)),
			s.text.Render("Memory   ")+s.muted.Render(capacityUsage(sample.MemoryUsed, sample.MemoryTotal)),
			s.text.Render("Network  ")+s.live.Render("↓ "+formatByteRate(sample.NetworkRXRate))+
				s.muted.Render("  ")+s.focus.Render("↑ "+formatByteRate(sample.NetworkTXRate)),
		)
		if len(sample.GPUs) > 0 {
			lines = append(lines, "", s.focus.Render("GPU"))
			for _, gpu := range sample.GPUs[:min(4, len(sample.GPUs))] {
				detail := fmt.Sprintf("%d%%", gpu.Utilization)
				if gpu.Temperature > 0 {
					detail += fmt.Sprintf(" · %d°C", gpu.Temperature)
				}
				if gpu.MemoryTotal > 0 {
					detail += " · " + capacityUsage(gpu.MemoryUsed, gpu.MemoryTotal) + " VRAM"
				}
				lines = append(lines, s.text.Render(truncateText(gpu.Name, max(12, width/2)))+
					s.muted.Render("  "+detail))
			}
		}
	}
	if len(host.Disks) > 0 && len(lines) < height-5 {
		disks := append([]diskUsage(nil), host.Disks...)
		sort.SliceStable(disks, func(left, right int) bool {
			return diskPercent(disks[left]) > diskPercent(disks[right])
		})
		lines = append(lines, "", s.focus.Render("STORAGE PRESSURE"))
		for _, disk := range disks[:min(4, len(disks))] {
			lines = append(lines, s.text.Render(padCell(truncateText(disk.Mountpoint, 16), 16))+
				s.muted.Render(fmt.Sprintf(" %3.0f%%  %s / %s",
					diskPercent(disk),
					formatDecimalBytes(disk.UsedBytes),
					formatDecimalBytes(disk.TotalBytes),
				)))
		}
	}
	return s.panel.BorderLeft(false).BorderRight(false).BorderTop(false).BorderBottom(false).
		Width(max(1, width-1)).Height(max(1, height-1)).Padding(1, 2).
		Render(strings.Join(lines, "\n"))
}

func (m dashboardModel) activityView(s dashboardStyles, width, height int) string {
	lines := []string{s.focus.Render("ACTIVITY"), s.muted.Render("This session · newest first"), ""}
	running := m.operation != nil && m.operation.Status == "running"
	if running {
		lines = append(lines,
			s.warning.Render("◌ "+truncateText(m.operation.Label+" · "+m.operation.Host, max(1, width-6))),
			s.muted.Render("  running "+compactDuration(time.Since(m.operation.StartedAt))),
		)
	}
	if len(m.activities) == 0 && !running {
		if m.operation != nil && m.operationPersisted {
			lines = append(lines,
				s.muted.Render("Previous session"),
				s.text.Render("  "+truncateText(m.operation.Label+" · "+m.operation.Host, max(1, width-7))),
				s.muted.Render("  "+truncateText(m.operation.Summary, max(1, width-7))),
			)
		} else {
			lines = append(lines,
				s.muted.Render("No operations yet."),
				s.muted.Render("Run an action and its result will stay here."),
			)
		}
	} else if len(m.activities) > 0 {
		for index := len(m.activities) - 1; index >= 0; index-- {
			event := m.activities[index]
			icon := "✓"
			status := s.success
			if event.Status == "error" {
				icon, status = "×", s.failure
			}
			subject := event.Label
			if event.Host != "" {
				subject += " · " + event.Host
			}
			lines = append(lines,
				status.Render(icon+" "+truncateText(subject, max(1, width-6))),
				s.muted.Render("  "+truncateText(valueOr(event.Summary, compactDuration(event.Duration)), max(1, width-7))),
			)
			if len(lines) >= height-5 {
				break
			}
		}
	}
	if m.operation != nil && m.operation.Output != "" && len(lines) < height-4 {
		lines = append(lines, "", s.focus.Render("LATEST OUTPUT"))
		for _, line := range tailNonEmptyLines(m.operation.Output, max(1, height-len(lines)-3)) {
			lines = append(lines, s.text.Render(truncateText(line, max(1, width-5))))
		}
	}
	return s.panel.BorderRight(false).BorderTop(false).BorderBottom(false).
		Width(max(1, width-1)).Height(max(1, height-1)).Padding(1, 2).
		Render(strings.Join(lines, "\n"))
}

func (m dashboardModel) consoleOutputView(s dashboardStyles, width, height int) string {
	lines := []string{s.focus.Render("OPERATIONS CONSOLE")}
	if m.operation == nil {
		lines = append(lines, s.muted.Render("Run an action to keep its bounded remote output here."))
	} else {
		status := s.success
		icon := "✓"
		switch m.operation.Status {
		case "running":
			status, icon = s.warning, "◌"
		case "error":
			status, icon = s.failure, "×"
		}
		lines = append(lines,
			status.Render(icon+" "+m.operation.Label)+s.muted.Render("  ·  "+m.operation.Host),
			s.muted.Render(m.operation.Summary),
			"",
		)
		output := m.operation.Output
		if output == "" {
			output = "(no captured output)"
		}
		for _, line := range tailNonEmptyLines(output, max(1, height-len(lines)-3)) {
			lines = append(lines, s.text.Render(truncateText(line, max(1, width-6))))
		}
	}
	return s.panel.BorderLeft(false).BorderRight(false).BorderTop(false).BorderBottom(false).
		Width(max(1, width-1)).Height(max(1, height-1)).Padding(1, 2).
		Render(strings.Join(lines, "\n"))
}

func (m dashboardModel) fleetDeckView(s dashboardStyles, width, height int) string {
	lines := []string{
		s.focus.Render("FLEET WORKSPACE"),
		s.muted.Render("Cached inventory with explicit freshness · selected-host telemetry stays local to the workbench"),
		"",
	}
	nameWidth := min(28, max(16, width/6))
	for _, host := range m.hosts {
		gpu := "GPU unknown"
		if len(host.GPUs) > 0 {
			gpu = host.GPUs[0]
			if len(host.GPUs) > 1 {
				gpu += fmt.Sprintf(" +%d", len(host.GPUs)-1)
			}
		}
		freshness := "not scanned"
		if !host.Updated.IsZero() {
			freshness = relativeTime(host.Updated, time.Now())
		}
		line := padCell(truncateText(displayName(host), nameWidth), nameWidth) + "  " +
			padCell(plainReachability(host.Reachability, m.probeTargets[host.Target]), 12) + "  " +
			padCell(truncateText(valueOr(host.OS, "OS unknown"), 24), 24) + "  " +
			padCell(truncateText(valueOr(host.Memory, "RAM unknown"), 12), 12) + "  " +
			truncateText(gpu, max(12, width-nameWidth-62)) + "  " + freshness
		lines = append(lines, s.text.Render(truncateText(line, max(1, width-6))))
		if len(lines) >= height-3 {
			break
		}
	}
	return s.panel.BorderLeft(false).BorderRight(false).BorderTop(false).BorderBottom(false).
		Width(max(1, width-1)).Height(max(1, height-1)).Padding(1, 2).
		Render(strings.Join(lines, "\n"))
}

func diskPercent(disk diskUsage) float64 {
	if disk.TotalBytes == 0 {
		return 0
	}
	return float64(disk.UsedBytes) * 100 / float64(disk.TotalBytes)
}

func capacityUsage(used, total uint64) string {
	if total == 0 {
		return "unknown"
	}
	return fmt.Sprintf("%s / %s (%.0f%%)",
		formatDecimalBytes(used), formatDecimalBytes(total),
		float64(used)*100/float64(total),
	)
}

func formatByteRate(value float64) string {
	switch {
	case value >= 1_000_000_000:
		return fmt.Sprintf("%.1f GB/s", value/1_000_000_000)
	case value >= 1_000_000:
		return fmt.Sprintf("%.1f MB/s", value/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.1f KB/s", value/1_000)
	default:
		return fmt.Sprintf("%.0f B/s", value)
	}
}

func formatUptime(duration time.Duration) string {
	if duration < time.Hour {
		return compactDuration(duration)
	}
	days := int(duration.Hours()) / 24
	hours := int(duration.Hours()) % 24
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	return fmt.Sprintf("%dh %dm", hours, int(duration.Minutes())%60)
}

func compactDuration(duration time.Duration) string {
	if duration < time.Second {
		return fmt.Sprintf("%dms", max(1, duration.Milliseconds()))
	}
	if duration < time.Minute {
		return fmt.Sprintf("%.1fs", duration.Seconds())
	}
	return fmt.Sprintf("%dm%02ds", int(duration.Minutes()), int(duration.Seconds())%60)
}

func tailNonEmptyLines(output string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	raw := strings.Split(sanitizeCommandOutput(output), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}

func appendActivity(events []activityEvent, event activityEvent) []activityEvent {
	event.Label = sanitizeTerminalText(event.Label)
	event.Host = sanitizeTerminalText(event.Host)
	event.Summary = sanitizeTerminalText(event.Summary)
	event.Output = sanitizeCommandOutput(event.Output)
	events = append(events, event)
	if len(events) > 5 {
		events = append([]activityEvent(nil), events[len(events)-5:]...)
	}
	return events
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
	semantic := func(color string) lipgloss.Style {
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		if t.Surface != "" {
			style = style.Background(lipgloss.Color(t.Surface))
		}
		return style
	}
	return dashboardStyles{
		title:         semantic(t.Focus).Bold(true),
		text:          semantic(t.Text),
		muted:         semantic(t.Muted),
		focus:         semantic(t.Focus).Bold(true),
		live:          semantic(t.Live),
		success:       semantic(t.Success),
		warning:       semantic(t.Warning),
		failure:       semantic(t.Error),
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
	if m.width < 60 {
		left = s.title.Render("◆ NEXUS")
		status = fmt.Sprintf("%d saved · %d up", len(m.hosts), online)
		if m.probing {
			status = fmt.Sprintf("probing %d/%d", m.probeComplete, m.probeTotal)
		}
	}
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(status)-4)
	return s.panel.BorderTop(false).BorderLeft(false).BorderRight(false).
		Width(max(1, m.width-2)).Padding(0, 1).
		Render(left + strings.Repeat(" ", gap) + status)
}

func (m dashboardModel) hostListView(s dashboardStyles, width, height int) string {
	titleText := "HOSTS"
	if width == m.width && m.width < 72 {
		titleText += " · j/k move"
	}
	title := s.focus.Render(titleText)
	filter := "[/] find · online first · frequent within status"
	if m.filtering || m.query != "" {
		filter = "Find: " + m.query
		if m.filtering {
			filter += "█  ·  [enter] connect  [esc] clear"
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
	panel := s.panel.BorderLeft(false).BorderTop(false).BorderBottom(false)
	if width == m.width {
		panel = panel.BorderRight(false)
	}
	return panel.Width(max(1, width-1)).Height(max(1, height-1)).Padding(1, 1).Render(content)
}

func (m dashboardModel) hostRow(s dashboardStyles, host dashboardHost, width int, selected bool) string {
	name := host.Alias
	if name == "" {
		target, _ := parseConnectionTarget(host.Target)
		name = target.Host
	}
	status := m.statusText(s, host.Reachability)
	firstName := truncateText(name, max(8, width-lipgloss.Width(status)-3))
	firstGap := max(1, width-lipgloss.Width(firstName)-lipgloss.Width(status)-2)
	first := "  " + firstName + strings.Repeat(" ", firstGap) + status
	target := truncateText(host.Target, max(10, width-15))
	last := relativeTime(host.LastUsed, m.now)
	secondGap := max(1, width-lipgloss.Width(target)-lipgloss.Width(last)-2)
	second := "  " + target + strings.Repeat(" ", secondGap) + last
	if selected {
		selectedStatus := plainReachability(host.Reachability, m.probeTargets[host.Target])
		if m.probeTargets[host.Target] && host.Reachability.Status != reachUnknown {
			selectedStatus += " ↻"
		}
		selectedName := truncateText(name, max(8, width-lipgloss.Width(selectedStatus)-3))
		selectedGap := max(1, width-lipgloss.Width(selectedName)-lipgloss.Width(selectedStatus)-2)
		selectedLine := selectedName + strings.Repeat(" ", selectedGap) + selectedStatus
		first = s.selected.Render("› " + padCell(selectedLine, width-2))
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
	}
	if len(host.Tags) > 0 {
		lines = append(lines, s.muted.Render("tags  "+strings.Join(host.Tags, " · ")))
	}
	lines = append(lines, "")
	snapshotState := "not scanned · use Actions"
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
	)
	if len(host.GPUs) == 0 {
		lines = append(lines, s.text.Render("GPU      ")+s.muted.Render("unknown"))
	} else {
		gpuLimit := min(2, len(host.GPUs))
		for index := 0; index < gpuLimit; index++ {
			label := "         "
			if index == 0 {
				label = "GPU      "
			}
			lines = append(lines, s.text.Render(label)+s.muted.Render(host.GPUs[index]))
		}
		if hidden := len(host.GPUs) - gpuLimit; hidden > 0 {
			lines = append(lines, s.muted.Render(fmt.Sprintf("         +%d more GPUs", hidden)))
		}
	}
	lines = append(lines, s.text.Render("Memory   ")+s.muted.Render(valueOr(host.Memory, "unknown")), "")

	lines = append(lines, s.focus.Render("STORAGE"))
	if len(host.Disks) == 0 {
		lines = append(lines, s.muted.Render(valueOr(host.Disk, "Not scanned · choose Storage in Actions")))
	} else {
		diskLimit := min(len(host.Disks), max(1, height-len(lines)-7))
		for _, disk := range host.Disks[:diskLimit] {
			percent := 0.0
			if disk.TotalBytes > 0 {
				percent = float64(disk.UsedBytes) * 100 / float64(disk.TotalBytes)
			}
			mount := padCell(truncateText(disk.Mountpoint, 12), 12)
			line := fmt.Sprintf("%s  %s / %s  %.0f%%",
				mount,
				valueOr(formatDecimalBytes(disk.UsedBytes), "0 GB"),
				valueOr(formatDecimalBytes(disk.TotalBytes), "0 GB"),
				percent,
			)
			lines = append(lines, s.text.Render(truncateText(line, max(1, width-6))))
		}
		if hidden := len(host.Disks) - diskLimit; hidden > 0 {
			lines = append(lines, s.muted.Render(fmt.Sprintf("+%d more · Actions → Storage shows all", hidden)))
		}
	}
	lines = append(lines, "")

	tools := strings.Join(host.Tools, " · ")
	commands := commandsForTarget(host.Target)
	lines = append(lines, s.focus.Render("TOOLS & COMMANDS"))
	if tools != "" {
		lines = append(lines, s.text.Render("Tools    ")+s.muted.Render(tools))
	}
	if len(commands) == 0 {
		lines = append(lines, s.muted.Render("No saved commands · configure from Actions"))
	} else {
		commandLimit := min(2, len(commands))
		for _, command := range commands[:commandLimit] {
			line := command.Name + "  " + command.Description
			lines = append(lines, s.text.Render(truncateText(line, max(1, width-6))))
		}
		if hidden := len(commands) - commandLimit; hidden > 0 {
			lines = append(lines, s.muted.Render(fmt.Sprintf("+%d more commands", hidden)))
		}
		lines = append(lines, s.muted.Render("[a] choose an action"))
	}
	content := strings.Join(lines, "\n")
	return s.panel.BorderLeft(false).BorderTop(false).BorderRight(false).BorderBottom(false).
		Width(max(1, width-1)).Height(max(1, height-1)).Padding(1, 2).Render(content)
}

func (m dashboardModel) compactDetailView(s dashboardStyles, width, height int) string {
	host := m.selectedHost()
	if host.Target == "" {
		return ""
	}
	name := displayName(host)
	gpu := "GPU unknown"
	if len(host.GPUs) > 0 {
		gpu = host.GPUs[0]
		if len(host.GPUs) > 1 {
			gpu += fmt.Sprintf(" +%d", len(host.GPUs)-1)
		}
	}
	storage := valueOr(host.Disk, "storage not scanned")
	if len(host.Disks) > 0 {
		storage = fmt.Sprintf("%d mounted filesystems", len(host.Disks))
	}
	lines := []string{
		s.title.Render(name) + s.muted.Render("  "+host.Target),
		m.statusText(s, host.Reachability) + s.muted.Render("  ·  used "+relativeTime(host.LastUsed, m.now)),
		s.text.Render("System  ") + s.muted.Render(strings.Join([]string{
			valueOr(host.OS, "OS unknown"), valueOr(host.Memory, "RAM unknown"),
		}, " · ")),
		s.text.Render("GPU     ") + s.muted.Render(gpu),
		s.text.Render("Storage ") + s.muted.Render(storage),
	}
	content := strings.Join(lines, "\n")
	return s.panel.BorderLeft(false).BorderRight(false).BorderBottom(false).
		Width(max(1, width-1)).Height(max(1, height-1)).Padding(0, 1).Render(content)
}

func (m dashboardModel) compactView(s dashboardStyles, width, height int) string {
	return m.hostListView(s, width, height)
}

func (m dashboardModel) footerView(s dashboardStyles) string {
	hints := strings.Join([]string{
		keyHint(s, "enter", "connect"),
		keyHint(s, "j/k", "move"),
		keyHint(s, "/", "find"),
		keyHint(s, "a", "actions"),
	}, "  ")
	if m.width < 72 {
		hints = "enter · j/k · / find · a actions · h keys"
		if m.width < 48 {
			hints = "enter connect · a actions · h keys"
		}
	}
	right := ""
	if m.themePreview {
		right = "session theme: " + m.theme.Name
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
	if right == "" && m.probing && m.width >= 72 {
		right = fmt.Sprintf("◌ probing %d/%d", m.probeComplete, m.probeTotal)
	}
	if right == "" && m.width >= 72 {
		right = "[h] all keys"
	}
	if m.width < 72 {
		content := s.muted.Render(hints)
		if right != "" {
			content = ansi.Truncate(right, max(1, m.width-4), "…")
		}
		return s.panel.BorderBottom(false).BorderLeft(false).BorderRight(false).
			Width(max(1, m.width-2)).Padding(0, 1).Render(content)
	}
	right = ansi.Truncate(right, max(1, m.width-lipgloss.Width(hints)-5), "…")
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
	search := "[/] filter actions  ·  [j/k] move"
	if m.commandFiltering {
		search = "Filter: " + m.commandQuery + "█"
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
		rowWidth := max(12, width-8)
		labelWidth := min(20, max(8, rowWidth/3))
		descriptionWidth := max(1, rowWidth-labelWidth-1)
		line := fmt.Sprintf("%-*s %s",
			labelWidth,
			truncateText(command.Label, labelWidth),
			truncateText(command.Description, descriptionWidth),
		)
		if i == m.commandCursor {
			line = s.selected.Render("› " + padCell(line, rowWidth))
		} else {
			line = "  " + s.text.Render(line)
		}
		lines = append(lines, line)
	}
	footer := "[enter] choose   [esc] close   saved commands always confirm"
	if m.commandFiltering {
		footer = "[enter] choose first match   [backspace] edit   [esc] close"
	}
	lines = append(lines, "", s.muted.Render(footer))
	panel := s.panel.Width(width-4).Padding(1, 2).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

func (m dashboardModel) themePreviewView() string {
	s := m.styles()
	names := themeNames()
	width := min(max(46, m.width-16), 72)
	lines := []string{
		s.focus.Render("THEMES"),
		s.muted.Render("Preview live, then use once or save as your default"),
		"",
	}
	for index, name := range names {
		t := themes[name]
		swatch := themeSwatch(t, m.plain)
		status := ""
		if name == normalizeThemeName(loadedConfig.UI.Theme) {
			status = "  default"
		}
		line := fmt.Sprintf("%-12s %s%s", name, swatch, status)
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
		s.muted.Render("[j/k] preview   [enter] use once   [s] save default"),
		s.muted.Render("[esc] restore previous theme"),
	)
	if m.themeSaving {
		lines = append(lines, s.live.Render("◌ saving theme…"))
	} else if m.noticeError && strings.HasPrefix(m.notice, "Theme save failed:") {
		lines = append(lines, s.failure.Render(truncateText(m.notice, width-6)))
	}
	panel := s.panel.Width(width-4).Padding(1, 2).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, fitTerminalView(panel, m.width, m.height))
}

func (m dashboardModel) workspacePreviewView() string {
	s := m.styles()
	width := min(max(48, m.width-16), 76)
	lines := []string{
		s.focus.Render("WORKSPACE"),
		s.muted.Render("Choose how large terminals use their available space"),
		"",
	}
	descriptions := map[string]string{
		"workbench": "Host pulse, storage pressure, actions, and activity",
		"console":   "Give remote output and recent operations more room",
		"fleet":     "Compare cached facts and freshness across every host",
	}
	for index, name := range workspaceModes() {
		line := fmt.Sprintf("%-12s %s", name, descriptions[name])
		if index == m.workspaceCursor {
			line = s.selected.Render("› " + padCell(line, width-6))
		} else {
			line = "  " + s.text.Render(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines,
		"",
		s.muted.Render("Compact terminals keep the host-first layout in every mode."),
		"",
		s.muted.Render("[j/k] preview   [enter] use once   [s] save default"),
		s.muted.Render("[esc] restore previous workspace"),
	)
	if m.workspaceSaving {
		lines = append(lines, s.live.Render("◌ saving workspace…"))
	} else if m.noticeError && strings.HasPrefix(m.notice, "Workspace save failed:") {
		lines = append(lines, s.failure.Render(truncateText(m.notice, width-6)))
	}
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
	lines = append(lines, "", s.muted.Render("[esc] back   ·   refresh connections from Actions"))
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
		lines = append(lines, "", s.muted.Render("[j/k] move   [enter] choose   [esc] cancel"))
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

func (m dashboardModel) confirmCommandView() string {
	s := m.styles()
	selection := m.confirmAction
	width := min(max(48, m.width-16), 76)
	title := "CONFIRM SAVED COMMAND"
	explanation := "Review the exact target and command before Nexus runs it."
	warning := "This command runs on the remote host with your SSH access."
	runLabel := "[y] run"
	if selection.Action == actionCopyKey {
		title = "CONFIRM COPY SSH KEY"
		explanation = "Nexus will add your default public key to this host."
		warning = "This changes the remote authorized_keys file and may ask for your password."
		runLabel = "[y] continue"
	}
	lines := []string{
		s.warning.Render(title),
		s.muted.Render(explanation),
		"",
		s.text.Render("Name     ") + s.focus.Render(valueOr(selection.Command.Name, "unnamed")),
		s.text.Render("Target   ") + s.muted.Render(selection.Host),
		s.text.Render("Command  ") + s.text.Render(selection.Command.Command),
		"",
		s.warning.Render(warning),
		"",
		s.text.Render(runLabel) + s.muted.Render("   [esc] cancel"),
	}
	panel := s.panel.Width(width-4).Padding(1, 2).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, fitTerminalView(panel, m.width, m.height))
}

func (m dashboardModel) commandResultView() string {
	s := m.styles()
	result := m.commandResult
	if result == nil {
		return ""
	}
	width := min(max(48, m.width-12), 88)
	lines := []string{
		s.focus.Render("COMMAND OUTPUT"),
		s.text.Render(result.Command.Name) + s.muted.Render("  ·  "+result.Host),
		"",
	}
	if m.commandRunning {
		lines = append(lines,
			s.live.Render("◌ Running on the remote host…"),
			"",
			s.muted.Render("[esc] hide output"),
		)
	} else {
		status := s.success.Render("✓ Finished")
		if result.Err != "" {
			status = s.failure.Render("× " + result.Err)
		}
		lines = append(lines, status, "")
		output := result.Output
		if strings.TrimSpace(output) == "" {
			output = "(command completed without output)"
		}
		outputLines := strings.Split(output, "\n")
		maxRows := max(1, m.height-10)
		maxOffset := max(0, len(outputLines)-maxRows)
		offset := min(m.commandOffset, maxOffset)
		end := min(len(outputLines), offset+maxRows)
		for _, line := range outputLines[offset:end] {
			lines = append(lines, s.text.Render(truncateText(line, width-6)))
		}
		position := ""
		if len(outputLines) > maxRows {
			position = fmt.Sprintf("   %d–%d / %d", offset+1, end, len(outputLines))
		}
		lines = append(lines, "", s.muted.Render("[j/k] scroll   [r] run again   [esc] back"+position))
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
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		if t.Surface != "" {
			style = style.Background(lipgloss.Color(t.Surface))
		}
		blocks = append(blocks, style.Render("◆"))
	}
	return strings.Join(blocks, " ")
}

func (m dashboardModel) helpView() string {
	s := m.styles()
	if m.height < 22 || m.width < 60 {
		compact := []string{
			s.focus.Render("NEXUS KEYS"),
			"",
			"j/k move        enter connect",
			"/ find          a actions",
			"h keys          q quit",
			"",
			"lists: j/k move · enter choose",
			s.muted.Render("esc always goes back"),
		}
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			s.panel.Padding(1, 2).Render(strings.Join(compact, "\n")))
	}
	help := []string{
		s.focus.Render("NEXUS KEYS"),
		"",
		s.focus.Render("WORKSPACE"),
		"j / k        move between hosts",
		"enter        connect with SSH",
		"/            find hosts",
		"a            all actions and saved commands",
		"h            open this key reference",
		"q            quit Nexus",
		"",
		"LISTS         j/k move · enter choose · esc back",
		"ACTION FILTER / start · backspace edit · enter first match",
		"THEME         s save default",
		"FAILED SCAN   r retry",
		"OUTPUT        r run again",
		"CONFIRM       y run · esc cancel",
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
	return truncateText("NEXUS › "+m.selectedTarget()+" — enter connect · h keys", max(1, m.width)) + "\n"
}

func (m dashboardModel) shortView() string {
	s := m.styles()
	lines := []string{s.title.Render(truncateText("◆ NEXUS  j/k move · enter connect · a actions", m.width))}
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
	lines = append(lines, truncateText("a actions · h all keys · q quit", m.width))
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
	var resumeActivities []activityEvent
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
		model.configPath = a.configFile
		model.indexMode = normalizeRemoteIndexMode(a.remoteIndex)
		model.notice = notice
		model.noticeError = noticeError
		if len(resumeActivities) > 0 {
			model.activities = append([]activityEvent(nil), resumeActivities...)
		}
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
			model.sortHostsByAvailabilityKeeping(resumeTarget)
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
		resumeActivities = append([]activityEvent(nil), finalModel.activities...)
		startedAt := time.Now()
		err = a.executeDashboardSelection(finalModel.choice)
		finishedAt := time.Now()
		status := "success"
		summary := actionLabel(finalModel.choice.Action) + " finished"
		if err != nil {
			status = "error"
			summary = sanitizeTerminalText(err.Error())
		}
		if stateErr := recordLatestOperation(a.stateFile, operationSummary{
			Action:     string(finalModel.choice.Action),
			Label:      selectionLabel(finalModel.choice),
			Host:       finalModel.choice.Host,
			Status:     status,
			Summary:    summary,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
			Duration:   finishedAt.Sub(startedAt),
		}); stateErr != nil {
			logVerbose("failed to record latest operation: %v", stateErr)
		}
		resumeActivities = appendActivity(resumeActivities, activityEvent{
			Label: selectionLabel(finalModel.choice), Host: finalModel.choice.Host,
			Status: status, Summary: summary, FinishedAt: finishedAt,
			Duration: finishedAt.Sub(startedAt),
		})
		if err != nil {
			notice = actionLabel(finalModel.choice.Action) + " failed: " + sanitizeTerminalText(err.Error())
			noticeError = true
			continue
		}
		notice = actionLabel(finalModel.choice.Action) + " finished"
		noticeError = false
	}
}

func selectionLabel(selection dashboardSelection) string {
	if selection.Action == actionCustom && strings.TrimSpace(selection.Command.Name) != "" {
		return selection.Command.Name
	}
	return actionLabel(selection.Action)
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
		return "Storage"
	case actionCopyKey:
		return "SSH key setup"
	case actionCustom:
		return "Saved command"
	case actionConfig:
		return "Configuration"
	case actionWorkspace:
		return "Workspace"
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
	if selection.Action == actionCopyKey {
		err := runSSHCopyID(selection.Host)
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

type cappedCommandOutput struct {
	builder   strings.Builder
	limit     int
	truncated bool
}

func (output *cappedCommandOutput) Write(chunk []byte) (int, error) {
	size := len(chunk)
	remaining := max(0, output.limit-output.builder.Len())
	if remaining < size {
		output.truncated = true
	}
	if remaining > 0 {
		_, _ = output.builder.Write(chunk[:min(size, remaining)])
	}
	return size, nil
}

func (output *cappedCommandOutput) String() string {
	value := sanitizeCommandOutput(output.builder.String())
	if output.truncated {
		value = strings.TrimRight(value, "\n") + "\n… output truncated"
	}
	return value
}

func runConfiguredRemoteCommandCaptured(target, command string) (string, error) {
	if command = sanitizeCommandText(command); command == "" {
		return "", errors.New("configured command is empty or contains unsafe control characters")
	}
	remote := remoteShellCommand("sh", command)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd, err := buildSSHCommand(ctx, target, false, remote)
	if err != nil {
		return "", err
	}
	output := &cappedCommandOutput{limit: 64 * 1024}
	cmd.Stdout = output
	cmd.Stderr = output
	err = cmd.Run()
	if ctx.Err() != nil {
		return output.String(), fmt.Errorf("remote command timed out: %w", ctx.Err())
	}
	return output.String(), err
}

func sanitizeCommandOutput(value string) string {
	value = ansiCSI.ReplaceAllString(value, "")
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	for index := range lines {
		lines[index] = sanitizeTerminalText(lines[index])
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
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
