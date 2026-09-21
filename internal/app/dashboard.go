package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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
	actionSettings  dashboardAction = "settings"
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

	// DisplayName and MeaningfulDisks are derived from Alias/Target/Disks at
	// the two points those fields are ever set (construction and metadata
	// refresh) instead of being recomputed by every render. Target/Alias
	// never change after a host is created, and Disks is only replaced
	// wholesale (never mutated in place), so these caches cannot go stale.
	DisplayName     string
	MeaningfulDisks []diskUsage
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

type terminalActionFinishedMsg struct {
	OperationID uint64
	Selection   dashboardSelection
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

type settingsSaveMsg struct {
	Key     string
	Value   string
	Enabled bool
	Err     error
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
	hosts                  []dashboardHost
	filtered               []int
	cursor                 int
	query                  string
	filtering              bool
	width                  int
	height                 int
	choice                 dashboardSelection
	done                   bool
	commandOpen            bool
	commandFiltering       bool
	commandCursor          int
	commandQuery           string
	confirmOpen            bool
	confirmAction          dashboardSelection
	confirmOffset          int
	commandResult          *configuredCommandResult
	commandRunning         bool
	commandOffset          int
	actionUses             map[string]int
	transfer               *transferFlow
	helpOpen               bool
	themeOpen              bool
	themeCursor            int
	themeOriginal          theme
	themePreview           bool
	themeSaving            bool
	workspaceOpen          bool
	workspaceCursor        int
	workspaceOriginal      string
	workspaceSaving        bool
	workspace              string
	settingsOpen           bool
	settingsCursor         int
	settingsSaving         bool
	profile                string
	density                string
	experimentalTabs       bool
	experimentalFleetBtop  bool
	monitorActionFocus     bool
	monitorActionCursor    int
	activityOpen           bool
	activityCursor         int
	showTopology           bool
	probing                bool
	probeQueue             []string
	probeInitial           []string
	probeTargets           map[string]bool
	probeTotal             int
	probeComplete          int
	metadataBusy           map[string]bool
	statePath              string
	configPath             string
	indexMode              string
	notice                 string
	noticeError            bool
	plain                  bool
	theme                  theme
	now                    time.Time
	operation              *dashboardOperation
	operationPersisted     bool
	operationID            uint64
	operationProbeID       uint64
	terminalRunning        bool
	telemetry              map[string]hostTelemetry
	telemetryTarget        string
	telemetryGen           uint64
	telemetryFlight        uint64
	telemetryFocused       bool
	fleetTelemetryPool     *fleetTelemetryPool
	fleetTelemetryWaiting  bool
	fleetTelemetryRotation int
	btopStreamPool         *btopStreamPool
	btopStreamWaiting      bool
	btopStreamTarget       string
	btopStreamGeneration   uint64
	btopStreamColumns      int
	btopStreamRows         int
	btopStreamState        string
	btopStreamStatus       string
	btopStreamError        string
	btopStreamFrames       uint64
	btopStreamUpdatedAt    time.Time
	btopStreamRetryAt      time.Time
	btopStreamPausedAt     time.Time
	btopResizeGeneration   uint64
	btopResizePending      bool
	activities             []activityEvent

	// actionUsageGen counts every mutation of actionUses (see
	// recordActionUsage). It is an ordinary value field mutated through the
	// same pointer-receiver Update path as every other counter on this
	// model; it exists purely as a cheap cache-invalidation signal for
	// commandsCache.
	actionUsageGen uint64

	// renderCache and commandsCache are pointer-typed cache fields. View()
	// and its helpers have a value receiver (Bubble Tea semantics: a
	// dashboardModel is copied on every Update/View call), so a cache field
	// cannot be repopulated by simply assigning m.field = ...; that mutation
	// would be lost the moment the method returns. Instead these fields
	// hold a pointer allocated once, in the constructor, and every later
	// value-copy of dashboardModel (produced by Update, by value-receiver
	// helpers, by "m := dashboardModel{...}" in bench/tests, etc.) carries
	// the *same* pointer forward. Mutating *m.renderCache from inside a
	// value-receiver method is safe here specifically because Bubble Tea
	// drives Update and View sequentially on a single goroutine -- there is
	// never a concurrent reader or writer, so this is not a data race, only
	// state shared across otherwise-independent value copies. Code built
	// directly as a struct literal (bypassing the constructor, e.g. the
	// small probe models in minBtopTerminalWidth/Height) leaves these
	// pointers nil; every accessor below treats nil as "cache disabled" and
	// falls back to computing fresh, so that is only a missed optimization,
	// never a correctness issue.
	renderCache    *dashboardRenderCache
	commandsCache  *dashboardCommandsCache
	btopFrameCache *dashboardBtopFrameCache
}

// dashboardBtopFrameCache memoizes the fitTerminalView + strings.Split done
// on the live btop frame every time the console/monitor panel renders. A new
// remote frame only arrives every second or so (btop_stream.go), far less
// often than View() gets called (telemetry ticks, cursor moves, resizes),
// so this is keyed on btopStreamFrames -- bumped exactly when
// entry.Current.BtopFrame actually changes -- plus the viewport size the
// frame was fit to.
// traceMessages logs every tea.Msg type reaching Update (NEXUS_TRACE_MESSAGES=1
// with --verbose); used to find message storms that force needless renders.
var traceMessages = os.Getenv("NEXUS_TRACE_MESSAGES") != ""

type dashboardBtopFrameCache struct {
	valid  bool
	frames uint64
	width  int
	height int
	lines  []string
	// twins are ASCII stand-ins with the same cell width as lines, each
	// tagged with a unique zero-width marker (see btopTwinLine). Layout code
	// composes with the twins so lipgloss never measures the ANSI-dense
	// frame, then swaps the real lines back in (btopSwapTwins).
	twins []string
	// deferSwap is set by View() so consoleWorkspaceView leaves the twins in
	// place and records them here; View() swaps the real rows back in after
	// its own joins, right before finishView.
	deferSwap    bool
	pendingTwins []string
	pendingLines []string
}

// dashboardRenderCache memoizes the theme/plain-derived values that used to
// be recomputed from scratch on every single View() call: the dashboardStyles
// bundle (styles()) and the raw ANSI prefix strings paintTerminalSurface
// needs for the panel-surface and whole-screen backgrounds. All of it is
// invalidated together by comparing plain+theme, since every field here is a
// pure function of those two inputs.
type dashboardRenderCache struct {
	valid bool
	plain bool
	theme theme

	styles dashboardStyles

	// textForegroundPrefix/surfaceBackgroundPrefix/screenBackgroundPrefix
	// are the raw ANSI escape sequences terminalStylePrefix would otherwise
	// reconstruct (via a throwaway lipgloss.Style.Render call) on every
	// renderPanel/finishView call. Empty means "this color is unset for the
	// current theme", matching the old per-call guards.
	textForegroundPrefix    string
	surfaceBackgroundPrefix string
	screenBackgroundPrefix  string

	// footerHintsDefault/footerHintsTabs are the fully-rendered "[key]
	// label" fragments footerView joins together every frame. Their text
	// and count are fixed (they don't depend on host/telemetry state, only
	// on whether tabs are visible), so -- like styles() -- there is no
	// reason to pay for keyHint's two lipgloss.Style.Render calls per hint,
	// per frame, when the theme hasn't changed.
	footerHintsDefault []string
	footerHintsTabs    []string
}

// dashboardCommandsCache memoizes availableCommands(), which otherwise
// rebuilds the command list and re-derives commandsForTarget/profileForTarget
// (a map iteration + sort.Strings over every configured host profile) on
// every frame. It is keyed on the selected target and actionUsageGen, the
// only two things that can change the resulting (unsorted-then-sorted) list
// during a single dashboard session; PinnedActions only change via editing
// the YAML config on disk, never from inside a running session.
type dashboardCommandsCache struct {
	valid  bool
	target string
	gen    uint64

	commands []dashboardCommand
}

const (
	dashboardChromeRows  = 4
	btopResizeSettleTime = 120 * time.Millisecond
)

// btopStreamPauseGrace is how long a running btop stream is kept alive while
// a transient overlay (help, settings, command palette, ...) is open before
// it is torn down. It is a var so tests can shorten it.
var btopStreamPauseGrace = 30 * time.Second

type btopViewportSettledMsg struct {
	Generation uint64
}

func settleBtopViewport(generation uint64) tea.Cmd {
	return tea.Tick(btopResizeSettleTime, func(time.Time) tea.Msg {
		return btopViewportSettledMsg{Generation: generation}
	})
}

func newDashboardModel(hosts []string) dashboardModel {
	return newDashboardModelWithState(hosts, nexusState{Hosts: map[string]hostActivity{}}, time.Now())
}

func newDashboardModelWithState(hosts []string, state nexusState, now time.Time) dashboardModel {
	safe := dedupeKeepOrder(hosts)
	model := dashboardModel{
		width:                 100,
		height:                30,
		showTopology:          false,
		probeTargets:          make(map[string]bool),
		metadataBusy:          make(map[string]bool),
		actionUses:            make(map[string]int, len(state.Actions)),
		indexMode:             "lazy",
		plain:                 noColorRequested(),
		theme:                 activeTheme(),
		workspace:             normalizeWorkspaceMode(loadedConfig.UI.Workspace),
		profile:               normalizeVisualProfile(loadedConfig.UI.Profile),
		density:               normalizeUIDensity(loadedConfig.UI.Density),
		experimentalTabs:      loadedConfig.UI.ExperimentalTabs,
		experimentalFleetBtop: loadedConfig.UI.monitorBtopEnabled(),
		now:                   now,
		telemetry:             make(map[string]hostTelemetry),
		telemetryGen:          1,
		telemetryFocused:      true,
		renderCache:           &dashboardRenderCache{},
		commandsCache:         &dashboardCommandsCache{},
		btopFrameCache:        &dashboardBtopFrameCache{},
	}
	if model.experimentalTabs {
		model.workspace = "workbench"
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
		activity.Disks = meaningfulStorageDisks(activity.Disks)
		if len(activity.Disks) > 0 {
			activity.Disk = legacyDiskSummary(activity.Disks)
		}
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
			// activity.Disks is already meaningfulStorageDisks-filtered
			// above; MeaningfulDisks is still computed with its own call
			// (rather than aliasing Disks) so it stays correct even if a
			// future change stops pre-filtering Disks here.
			DisplayName:     resolveDisplayName(profile.Alias, target),
			MeaningfulDisks: meaningfulStorageDisks(activity.Disks),
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
	commands := []tea.Cmd{telemetryTick(time.Second, m.telemetryGen), fleetTelemetryRotateTick()}
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
	if traceMessages {
		logVerbose("update %T", message)
	}
	switch msg := message.(type) {
	case tea.FocusMsg:
		m.telemetryFocused = true
		return m, tea.Batch(m.ensureBtopStream(), m.ensureFleetTelemetry(), m.restartTelemetryTick(100*time.Millisecond))
	case tea.BlurMsg:
		m.telemetryFocused = false
		m.deactivateBtopStream()
		m.closeFleetTelemetry()
		return m, nil
	case tea.WindowSizeMsg:
		sizeChanged := m.width != max(1, msg.Width) || m.height != max(1, msg.Height)
		m.width = max(1, msg.Width)
		m.height = max(1, msg.Height)
		if m.height < 16 {
			m.activityOpen = false
		}
		if sizeChanged {
			settled := m.deferBtopViewportResize()
			return m, tea.Batch(
				m.ensureBtopStream(),
				settled,
			)
		}
		return m, tea.Batch(m.ensureBtopStream(), m.ensureFleetTelemetry())
	case btopViewportSettledMsg:
		if msg.Generation != m.btopResizeGeneration {
			return m, nil
		}
		m.btopResizePending = false
		return m, tea.Batch(m.ensureBtopStream(), m.ensureFleetTelemetry())
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
				m.restartTelemetryTick(100*time.Millisecond),
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
			m.hosts[i].MeaningfulDisks = meaningfulStorageDisks(msg.Activity.Disks)
			m.hosts[i].Tools = append([]string(nil), msg.Activity.Tools...)
			m.hosts[i].Updated = msg.Activity.Updated
			break
		}
		m.notice = "System snapshot refreshed for " + m.displayNameForTarget(msg.Target)
		m.noticeError = false
		usedAt := time.Now()
		hostCmd := m.recordHostSuccessCmd(msg.Target, usedAt)
		m.markHostUsed(msg.Target, usedAt)
		if msg.OperationID == m.operationID {
			return m, tea.Batch(hostCmd, m.finishOperation("success", "System details updated", ""))
		}
		return m, hostCmd
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
	case settingsSaveMsg:
		m.settingsSaving = false
		if msg.Err != nil {
			m.notice = "Setting save failed: " + sanitizeTerminalText(msg.Err.Error())
			m.noticeError = true
			return m, nil
		}
		switch msg.Key {
		case "profile":
			preset, ok := visualProfileByName(msg.Value)
			if !ok {
				m.notice = "Setting save failed: unknown visual profile"
				m.noticeError = true
				return m, nil
			}
			loadedConfig.UI.Profile = preset.Name
			loadedConfig.UI.Theme = preset.Theme
			loadedConfig.UI.Background = preset.Background
			loadedConfig.UI.Density = preset.Density
			m.profile = preset.Name
			m.density = preset.Density
			m.theme = themeWithBackground(preset.Theme, preset.Background)
			m.themeOriginal = m.theme
			m.themePreview = false
			m.notice = "Visual profile applied: " + preset.Name
		case "background":
			selectedTheme := m.theme.Name
			loadedConfig.UI.Background = msg.Value
			m.theme = resolvedTheme(selectedTheme)
			m.themeOriginal = m.theme
			m.themePreview = false
			m.notice = "Background saved: " + msg.Value
		case "density":
			loadedConfig.UI.Density = msg.Value
			m.density = msg.Value
			m.notice = "Interface density: " + msg.Value
		case "experimental_tabs":
			loadedConfig.UI.ExperimentalTabs = msg.Enabled
			m.experimentalTabs = msg.Enabled
			if msg.Enabled {
				m.workspace = "workbench"
				m.monitorActionFocus = false
			} else {
				m.workspace = normalizeWorkspaceMode(loadedConfig.UI.Workspace)
			}
			state := "off"
			if msg.Enabled {
				state = "on"
			}
			m.notice = "Experimental workspace tabs: " + state
		case "monitor_btop":
			loadedConfig.UI.MonitorBtop = &msg.Enabled
			loadedConfig.UI.ExperimentalFleetBtop = false
			m.experimentalFleetBtop = msg.Enabled
			state := "off"
			if msg.Enabled {
				state = "on"
			}
			m.notice = "Monitor btop: " + state
			m.noticeError = false
			if !msg.Enabled {
				m.closeBtopStreams()
				return m, nil
			}
			return m, tea.Batch(m.ensureBtopStream(), m.ensureFleetTelemetry())
		}
		m.noticeError = false
		return m, nil
	case terminalActionFinishedMsg:
		m.terminalRunning = false
		if msg.OperationID != m.operationID {
			return m, nil
		}
		actionErr := msg.Err
		if msg.Selection.Action == actionSSH && isRemoteShellExit(actionErr) {
			actionErr = nil
		}
		label := selectionLabel(msg.Selection)
		if actionErr != nil {
			m.notice = label + " failed: " + sanitizeTerminalText(actionErr.Error())
			if msg.Selection.Action == actionSSH {
				m.notice = "SSH connection failed · check the host, credentials, or network"
			}
			m.noticeError = true
			return m, tea.Batch(m.finishOperation("error", label+" failed", ""), m.ensureBtopStream())
		}
		usedAt := time.Now()
		hostCmd := m.recordHostSuccessCmd(msg.Selection.Host, usedAt)
		m.markHostUsed(msg.Selection.Host, usedAt)
		summary := label + " finished"
		if msg.Selection.Action == actionSSH {
			summary = "SSH session ended"
		}
		if msg.Selection.Action == actionCopyKey {
			summary = "SSH key installed"
		}
		m.notice = summary
		m.noticeError = false
		return m, tea.Batch(hostCmd, m.finishOperation("success", summary, ""), m.ensureBtopStream())
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
		}
		usedAt := time.Now()
		hostCmd := m.recordHostSuccessCmd(m.commandResult.Host, usedAt)
		m.markHostUsed(m.commandResult.Host, usedAt)
		if msg.OperationID == m.operationID {
			return m, tea.Batch(hostCmd, m.finishOperation("success", "Command finished", msg.Output))
		}
		return m, hostCmd
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
			m.deactivateBtopStream()
			return m, tea.Batch(m.ensureBtopStream(), m.ensureFleetTelemetry(), telemetryTick(100*time.Millisecond, m.telemetryGen))
		}
		btopCommand := m.ensureBtopStream()
		fleetCommand := m.ensureFleetTelemetry()
		if target == "" || !m.telemetryFocused || m.telemetryPaused() {
			return m, tea.Batch(btopCommand, fleetCommand, telemetryTick(telemetryInterval, m.telemetryGen))
		}
		workspace := normalizeWorkspaceMode(m.workspace)
		if workspace == "console" || workspace == "fleet" {
			// Live data (btop frames, fleet samples) re-renders on arrival;
			// this tick only refreshes relative ages, so keep it slow unless
			// a stream is still connecting and the pane shows progress.
			delay := telemetryIdleTick
			if m.btopStreamState == "connecting" {
				delay = time.Second
			}
			return m, tea.Batch(btopCommand, fleetCommand, telemetryTick(delay, m.telemetryGen))
		}
		if m.telemetryFlight != 0 {
			return m, tea.Batch(btopCommand, fleetCommand, telemetryTick(time.Second, m.telemetryGen))
		}
		host := m.selectedHost()
		if host.Reachability.Status != reachOnline {
			return m, tea.Batch(btopCommand, fleetCommand, telemetryTick(telemetryInterval, m.telemetryGen))
		}
		entry := m.telemetry[target]
		if wait := time.Until(entry.NextAttempt); wait > 0 {
			return m, tea.Batch(btopCommand, fleetCommand, telemetryTick(min(wait, telemetryMaxBackoff), m.telemetryGen))
		}
		m.telemetryFlight = m.telemetryGen
		return m, tea.Batch(btopCommand, fleetCommand, telemetryCommand(target, m.telemetryGen))
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
		m.telemetry[msg.Target] = mergeTelemetrySample(entry, msg.Sample)
		return m, telemetryTick(telemetryInterval, m.telemetryGen)
	case fleetTelemetryBatchMsg:
		m.fleetTelemetryWaiting = false
		for _, event := range msg.Events {
			if m.fleetTelemetryPool == nil || !m.fleetTelemetryPool.current(event.Target, event.Generation) {
				continue
			}
			entry := m.telemetry[event.Target]
			if event.Err != nil {
				entry.Failures++
				entry.LastErr = sanitizeTerminalText(event.Err.Error())
				m.telemetry[event.Target] = entry
				continue
			}
			m.telemetry[event.Target] = mergeTelemetrySample(entry, event.Sample)
		}
		return m, m.waitForFleetTelemetry()
	case fleetTelemetryPoolClosedMsg:
		m.fleetTelemetryWaiting = false
		return m, nil
	case fleetTelemetryRotateMsg:
		m.fleetTelemetryRotation++
		return m, tea.Batch(m.ensureFleetTelemetry(), fleetTelemetryRotateTick())
	case btopStreamEventMsg:
		m.btopStreamWaiting = false
		if msg.Generation == 0 || msg.Generation != m.btopStreamGeneration ||
			msg.Target != m.btopStreamTarget || msg.Target != m.selectedTarget() {
			return m, m.waitForBtopStream()
		}
		if msg.Status != "" {
			m.btopStreamStatus = msg.Status
		}
		if msg.Frame != "" {
			entry := m.telemetry[msg.Target]
			entry.Current.Target = msg.Target
			if entry.Current.CollectedAt.IsZero() {
				entry.Current.CollectedAt = msg.UpdatedAt
			}
			entry.Current.BtopInstalled = true
			entry.Current.BtopFrame = msg.Frame
			m.telemetry[msg.Target] = entry
			m.btopStreamState = "live"
			m.btopStreamError = ""
			m.btopStreamStatus = ""
			m.btopStreamFrames = msg.FrameCount
			m.btopStreamUpdatedAt = msg.UpdatedAt
		} else if msg.Stage == "fitting" {
			// A stage-only event (no frame yet) keeps the pane in the
			// connecting state instead of leaving it blank.
			m.btopStreamState = "connecting"
		}
		if msg.Done {
			m.btopStreamStatus = ""
			if !msg.Installed && msg.Error == "" {
				m.btopStreamState = "unavailable"
				m.btopStreamError = "btop is not installed on this host"
				m.btopStreamRetryAt = time.Now().Add(time.Minute)
			} else if msg.Error != "" {
				m.btopStreamState = "error"
				m.btopStreamError = msg.Error
				m.btopStreamRetryAt = time.Now().Add(15 * time.Second)
			} else {
				m.btopStreamState = "idle"
				m.btopStreamRetryAt = time.Now().Add(3 * time.Second)
			}
			return m, nil
		}
		return m, m.waitForBtopStream()
	case btopStreamPoolClosedMsg:
		m.btopStreamWaiting = false
		return m, nil
	case actionUsageMsg:
		if msg.Err != nil {
			logVerbose("failed to record action usage: %v", msg.Err)
		}
		return m, nil
	case hostActivityMsg:
		if msg.Err != nil {
			logVerbose("failed to record host activity: %v", msg.Err)
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
	key := dashboardNavigationKey(msg.String())
	if key == "ctrl+c" {
		m.closeBtopStreams()
		m.closeFleetTelemetry()
		m.done = true
		return m, tea.Quit
	}
	if m.transfer != nil {
		return m.updateTransfer(key)
	}
	// The confirm modal renders above the command result view (see View), so
	// it must also take the keys first: re-running a confirm-required command
	// from the result view opens it while the result is still on screen.
	if m.confirmOpen {
		switch key {
		case "y", "Y":
			if m.confirmAction.Action == actionCopyKey {
				usageCmd := m.recordActionUsage(actionCopyKey, commandConfig{})
				return m.startTerminalAction(m.confirmAction, usageCmd)
			}
			return m.startSavedCommand(m.confirmAction)
		case "esc", "n", "N":
			m.confirmOpen = false
			m.confirmAction = dashboardSelection{}
			m.confirmOffset = 0
		case "k":
			m.confirmOffset = max(0, m.confirmOffset-1)
		case "j":
			width := m.overlayWidth(76)
			bodyRows := len(m.confirmReviewLines(m.overlayContentWidth(width)))
			visibleRows := max(1, m.overlayContentHeight()-2)
			if bodyRows > visibleRows {
				visibleRows = max(1, visibleRows-1)
			}
			maxOffset := max(0, bodyRows-visibleRows)
			m.confirmOffset = min(maxOffset, m.confirmOffset+1)
		}
		return m, nil
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
				if m.commandResult.Action == actionCustom && m.commandResult.Command.Confirm {
					m.confirmOpen = true
					m.confirmOffset = 0
					m.confirmAction = dashboardSelection{
						Action: actionCustom, Host: m.commandResult.Host, Command: m.commandResult.Command,
					}
					return m, nil
				}
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
			return m, tea.Batch(m.ensureBtopStream(), m.ensureFleetTelemetry())
		}
		return m, nil
	}
	if m.activityOpen {
		events := m.activityDrawerEvents()
		switch key {
		case "esc", "o":
			m.activityOpen = false
			return m, m.deferBtopViewportResize()
		case "k":
			m.activityCursor = max(0, m.activityCursor-1)
		case "j":
			m.activityCursor = min(max(0, len(events)-1), m.activityCursor+1)
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
			m.theme = resolvedTheme(names[m.themeCursor])
		case "j":
			m.themeCursor = (m.themeCursor + 1) % len(names)
			m.theme = resolvedTheme(names[m.themeCursor])
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
	if m.settingsOpen {
		items := settingsItems()
		switch key {
		case "esc":
			m.settingsOpen = false
			return m, tea.Batch(m.ensureBtopStream(), m.ensureFleetTelemetry())
		case "k":
			m.settingsCursor = (m.settingsCursor + len(items) - 1) % len(items)
		case "j":
			m.settingsCursor = (m.settingsCursor + 1) % len(items)
		case "h", "left":
			return m.updateSetting(items[m.settingsCursor], -1)
		case "l", "right", "enter", " ":
			return m.updateSetting(items[m.settingsCursor], 1)
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
			if m.commandFiltering && msg.Type == tea.KeyRunes {
				m.commandQuery += "k"
				m.commandCursor = 0
			} else {
				m.commandCursor = max(0, m.commandCursor-1)
			}
		case "j":
			if m.commandFiltering && msg.Type == tea.KeyRunes {
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
			case actionSettings:
				m.commandOpen = false
				m.commandFiltering = false
				m.commandQuery = ""
				m.settingsOpen = true
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
						Name: "Storage", Description: "Mounted storage volumes",
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
				m.confirmOffset = 0
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
				selection := dashboardSelection{
					Action: actionCustom, Host: m.selectedTarget(), Command: command.Command,
				}
				if command.Command.Confirm {
					m.confirmOpen = true
					m.confirmOffset = 0
					m.confirmAction = selection
					return m, nil
				}
				return m.startSavedCommand(selection)
			}
			if command.Action == actionSSH {
				selection := dashboardSelection{Action: actionSSH, Host: m.selectedTarget()}
				return m.startTerminalAction(selection, usageCmd)
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
		case "k":
			m.moveCursor(-1)
		case "j":
			m.moveCursor(1)
		default:
			if msg.Type == tea.KeyRunes {
				m.query += sanitizeTerminalText(string(msg.Runes))
				m.applyFilter()
			}
		}
		return m, nil
	}
	if m.workspaceTabsVisible() && normalizeWorkspaceMode(m.workspace) == "console" {
		commands := m.monitorActions()
		if m.monitorActionFocus {
			switch key {
			case "h", "left", "esc":
				m.monitorActionFocus = false
				return m, nil
			case "k":
				m.monitorActionCursor = max(0, m.monitorActionCursor-1)
				return m, nil
			case "j":
				m.monitorActionCursor = min(max(0, len(commands)-1), m.monitorActionCursor+1)
				return m, nil
			case "enter":
				return m.activateMonitorAction()
			}
		} else if key == "l" || key == "right" {
			m.monitorActionFocus = true
			return m, nil
		}
	}

	switch key {
	case "q":
		m.closeBtopStreams()
		m.closeFleetTelemetry()
		m.done = true
		return m, tea.Quit
	case "tab":
		if m.workspaceTabsVisible() {
			m.cycleWorkspace(1)
			return m, tea.Batch(m.ensureBtopStream(), m.ensureFleetTelemetry(), m.restartTelemetryTick(100*time.Millisecond))
		}
	case "shift+tab":
		if m.workspaceTabsVisible() {
			m.cycleWorkspace(-1)
			return m, tea.Batch(m.ensureBtopStream(), m.ensureFleetTelemetry(), m.restartTelemetryTick(100*time.Millisecond))
		}
	case "left":
		if m.workspaceTabsVisible() {
			m.cycleWorkspace(-1)
			return m, tea.Batch(m.ensureBtopStream(), m.ensureFleetTelemetry(), m.restartTelemetryTick(100*time.Millisecond))
		}
	case "right":
		if m.workspaceTabsVisible() {
			m.cycleWorkspace(1)
			return m, tea.Batch(m.ensureBtopStream(), m.ensureFleetTelemetry(), m.restartTelemetryTick(100*time.Millisecond))
		}
	case "h":
		m.helpOpen = true
	case ",":
		m.settingsOpen = true
	case "o":
		if m.height >= 16 {
			m.activityOpen = true
			m.activityCursor = 0
			return m, m.deferBtopViewportResize()
		}
	case "r":
		refreshed, metadataCommand := m.startMetadataRefresh()
		m = refreshed.(dashboardModel)
		m.restartBtopStream()
		return m, tea.Batch(metadataCommand, m.ensureBtopStream())
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
			return m, m.restartTelemetryTick(100 * time.Millisecond)
		}
	case "j":
		before := m.selectedTarget()
		m.moveCursor(1)
		if before != m.selectedTarget() {
			m.resetTelemetryTarget()
			return m, m.restartTelemetryTick(100 * time.Millisecond)
		}
	case "enter":
		return m.choose(actionSSH)
	}
	return m, nil
}

func dashboardNavigationKey(key string) string {
	switch key {
	case "up":
		return "k"
	case "down":
		return "j"
	default:
		return key
	}
}

func (m dashboardModel) telemetryPaused() bool {
	return m.helpOpen || m.commandOpen || m.themeOpen || m.workspaceOpen || m.settingsOpen || m.confirmOpen ||
		m.transfer != nil || m.commandResult != nil || m.showTopology || m.terminalRunning
}

func (m *dashboardModel) ensureFleetTelemetry() tea.Cmd {
	workspace := normalizeWorkspaceMode(m.workspace)
	if !m.telemetryFocused || m.telemetryPaused() || (workspace != "console" && workspace != "fleet") {
		m.closeFleetTelemetry()
		return nil
	}
	specs := m.fleetTelemetrySpecs(workspace)
	if len(specs) == 0 {
		m.closeFleetTelemetry()
		return nil
	}
	if m.fleetTelemetryPool == nil {
		m.fleetTelemetryPool = newFleetTelemetryPool()
	}
	m.fleetTelemetryPool.sync(specs)
	return m.waitForFleetTelemetry()
}

func (m dashboardModel) fleetTelemetrySpecs(workspace string) []fleetTelemetrySpec {
	eligible := func(host dashboardHost) bool {
		return host.Reachability.Status == reachOnline || host.Reachability.Status == reachUnknown
	}
	selected := m.selectedTarget()
	if workspace == "console" {
		if selected == "" || !eligible(m.selectedHost()) {
			return nil
		}
		return []fleetTelemetrySpec{{Target: selected, Interval: 3 * time.Second}}
	}
	seen := make(map[string]bool, fleetTelemetryMaxStreams)
	specs := make([]fleetTelemetrySpec, 0, fleetTelemetryMaxStreams)
	appendSpec := func(target string, interval time.Duration) {
		if target == "" || seen[target] || len(specs) >= fleetTelemetryMaxStreams {
			return
		}
		for _, host := range m.hosts {
			if host.Target == target && eligible(host) {
				seen[target] = true
				specs = append(specs, fleetTelemetrySpec{Target: target, Interval: interval})
				return
			}
		}
	}
	appendSpec(selected, 3*time.Second)
	rowBudget := max(1, m.height-7)
	start, end := selectionWindow(len(m.filtered), m.cursor, rowBudget)
	for position := start; position < end; position++ {
		appendSpec(m.hosts[m.filtered[position]].Target, 5*time.Second)
	}
	if len(specs) >= fleetTelemetryMaxStreams || len(m.filtered) == 0 {
		return specs
	}
	offscreen := make([]string, 0, len(m.filtered))
	for position, index := range m.filtered {
		if position >= start && position < end {
			continue
		}
		target := m.hosts[index].Target
		if !seen[target] && eligible(m.hosts[index]) {
			offscreen = append(offscreen, target)
		}
	}
	if len(offscreen) > 0 {
		startAt := (m.fleetTelemetryRotation * max(1, fleetTelemetryMaxStreams-len(specs))) % len(offscreen)
		for offset := 0; offset < len(offscreen) && len(specs) < fleetTelemetryMaxStreams; offset++ {
			appendSpec(offscreen[(startAt+offset)%len(offscreen)], 15*time.Second)
		}
	}
	return specs
}

// restartTelemetryTick starts a fresh telemetry clock chain and retires every
// older one by bumping the generation, so event handlers that want an
// immediate tick (focus, selection change, refresh) never leave a second
// chain running alongside the first. Without this, chains accumulated over a
// session and each one forced a full View() on its own schedule.
func (m *dashboardModel) restartTelemetryTick(delay time.Duration) tea.Cmd {
	m.telemetryGen++
	return telemetryTick(delay, m.telemetryGen)
}

func (m *dashboardModel) waitForFleetTelemetry() tea.Cmd {
	if m.fleetTelemetryPool == nil || m.fleetTelemetryWaiting {
		return nil
	}
	m.fleetTelemetryWaiting = true
	return waitForFleetTelemetry(m.fleetTelemetryPool)
}

func (m *dashboardModel) closeFleetTelemetry() {
	pool := m.fleetTelemetryPool
	m.fleetTelemetryPool = nil
	m.fleetTelemetryWaiting = false
	if pool != nil {
		pool.close()
	}
}

func (m dashboardModel) monitorBtopViewport() (columns, rows int, ok bool) {
	if !m.experimentalFleetBtop || normalizeWorkspaceMode(m.workspace) != "console" ||
		!m.workspaceTabsVisible() {
		return 0, 0, false
	}
	_, workspaceHeight, _ := m.dashboardHeights()
	actionWidth := m.monitorActionWidth(m.width)
	monitorWidth := m.width - actionWidth
	_, btopHeight := monitorPaneHeights(workspaceHeight)
	columns, rows = btopFrameViewport(monitorWidth, btopHeight, false, true)
	if columns < btopMinColumns || rows < btopMinRows {
		return 0, 0, false
	}
	return columns, rows, true
}

// minBtopTerminalWidth returns the smallest terminal width, for the given
// density, that lets the Monitor action rail and the btop pane both fit at
// or above btopMinColumns. It solves monitorActionWidth/btopFrameViewport
// numerically instead of duplicating their clamped math.
func minBtopTerminalWidth(density string) int {
	probe := dashboardModel{density: density}
	for width := 1; width <= 600; width++ {
		actionWidth := probe.monitorActionWidth(width)
		columns, _ := btopFrameViewport(width-actionWidth, 1000, false, true)
		if columns >= btopMinColumns {
			return width
		}
	}
	return 600
}

// minBtopTerminalHeight returns the smallest terminal height that leaves the
// btop pane at or above btopMinRows, given whether the activity drawer is
// open. It solves dashboardHeights/monitorPaneHeights/btopFrameViewport
// numerically instead of duplicating their clamped math.
func minBtopTerminalHeight(activityOpen bool) int {
	probe := dashboardModel{activityOpen: activityOpen}
	for height := 1; height <= 600; height++ {
		probe.height = height
		_, workspaceHeight, _ := probe.dashboardHeights()
		_, btopHeight := monitorPaneHeights(workspaceHeight)
		_, rows := btopFrameViewport(1000, btopHeight, false, true)
		if rows >= btopMinRows {
			return height
		}
	}
	return 600
}

// btopGateReason explains why monitorBtopViewport currently refuses to run a
// live btop stream, mirroring its checks (plus the target/pause checks in
// ensureBtopStream) so the Monitor pane can tell the user why it is idle
// instead of showing a generic "connecting" message forever. It returns ""
// when a stream may run.
func (m dashboardModel) btopGateReason() string {
	if !m.experimentalFleetBtop {
		return "Monitor btop is off · press , to enable it in settings"
	}
	if !m.workspaceTabsVisible() {
		return "Live btop needs the Console workspace tabs (terminal ≥150×28)"
	}
	if _, _, ok := m.monitorBtopViewport(); !ok {
		// Tabs (checked above) already need 150 columns, so never quote a
		// smaller width than the user must actually reach.
		minWidth := max(150, minBtopTerminalWidth(m.density))
		minHeight := minBtopTerminalHeight(m.activityOpen)
		reason := fmt.Sprintf("Terminal too small for live btop: need ≥%d×%d, have %d×%d",
			minWidth, minHeight, m.width, m.height)
		if m.activityOpen {
			minHeightWithoutDrawer := minBtopTerminalHeight(false)
			if m.width >= minWidth && m.height >= minHeightWithoutDrawer {
				reason += " · press o to close the activity drawer"
			}
		}
		return reason
	}
	if m.selectedTarget() == "" {
		return "Select a host to start."
	}
	if m.telemetryPaused() {
		return "Paused while an overlay is open"
	}
	return ""
}

func (m *dashboardModel) ensureBtopStream() tea.Cmd {
	columns, rows, visible := m.monitorBtopViewport()
	target := m.selectedTarget()
	if !visible || target == "" || !m.telemetryFocused || m.terminalRunning {
		m.btopStreamPausedAt = time.Time{}
		m.deactivateBtopStream()
		return nil
	}
	if m.telemetryPaused() {
		// Keep a running session (and its last frame) alive across a
		// transient overlay such as help, settings, or the command palette,
		// instead of tearing it down and wasting the SSH session. Only give
		// up once the overlay has stayed open past the grace period.
		if m.btopStreamTarget == "" {
			return nil
		}
		if m.btopStreamPausedAt.IsZero() {
			m.btopStreamPausedAt = time.Now()
			return m.waitForBtopStream()
		}
		if time.Since(m.btopStreamPausedAt) >= btopStreamPauseGrace {
			m.btopStreamPausedAt = time.Time{}
			m.deactivateBtopStream()
			return nil
		}
		return m.waitForBtopStream()
	}
	m.btopStreamPausedAt = time.Time{}
	if m.btopResizePending {
		return m.waitForBtopStream()
	}
	if m.btopStreamTarget == target && m.btopStreamColumns == columns && m.btopStreamRows == rows {
		if m.btopStreamRetryAt.IsZero() || time.Now().Before(m.btopStreamRetryAt) {
			// Same target and viewport as the running (or recently
			// succeeded) session: just keep listening instead of re-entering
			// pool.activate every tick, which would otherwise re-derive state
			// from session.latest and could flip a live pane back to
			// "connecting" with an empty frame.
			return m.waitForBtopStream()
		}
		m.btopStreamRetryAt = time.Time{}
	}
	if m.btopStreamPool == nil {
		m.btopStreamPool = newBtopStreamPool()
	}
	previousTarget := m.btopStreamTarget
	latest, started := m.btopStreamPool.activate(target, columns, rows)
	m.btopStreamTarget = target
	m.btopStreamGeneration = latest.Generation
	m.btopStreamColumns = columns
	m.btopStreamRows = rows
	m.btopStreamRetryAt = time.Time{}
	if started || latest.Frame == "" {
		m.btopStreamState = "connecting"
		m.btopStreamError = ""
		m.btopStreamStatus = ""
		if previousTarget != target {
			// Only clear the last frame when the target actually changed.
			// A resize-triggered reconnect to the SAME target keeps showing
			// the stale frame (with a CONNECTING badge) instead of blanking
			// the pane while btop re-fits to the new terminal size.
			m.btopStreamFrames = 0
			m.btopStreamUpdatedAt = time.Time{}
			entry := m.telemetry[target]
			entry.Current.BtopFrame = ""
			entry.Current.BtopInstalled = false
			m.telemetry[target] = entry
		}
	} else {
		entry := m.telemetry[target]
		entry.Current.Target = target
		entry.Current.BtopInstalled = true
		entry.Current.BtopFrame = latest.Frame
		m.telemetry[target] = entry
		m.btopStreamState = "live"
		m.btopStreamError = ""
		m.btopStreamStatus = ""
		m.btopStreamFrames = latest.FrameCount
		m.btopStreamUpdatedAt = latest.UpdatedAt
	}
	return m.waitForBtopStream()
}

func (m *dashboardModel) deferBtopViewportResize() tea.Cmd {
	m.btopResizeGeneration++
	m.btopResizePending = true
	return settleBtopViewport(m.btopResizeGeneration)
}

func (m *dashboardModel) waitForBtopStream() tea.Cmd {
	if m.btopStreamPool == nil || m.btopStreamWaiting {
		return nil
	}
	m.btopStreamWaiting = true
	return waitForBtopStreamPool(m.btopStreamPool)
}

func (m *dashboardModel) deactivateBtopStream() {
	if m.btopStreamPool != nil {
		m.btopStreamPool.deactivate()
	}
	m.btopStreamTarget = ""
	m.btopStreamColumns = 0
	m.btopStreamRows = 0
	m.btopStreamState = ""
	m.btopStreamStatus = ""
	m.btopStreamError = ""
	m.btopStreamFrames = 0
	m.btopStreamUpdatedAt = time.Time{}
	m.btopStreamRetryAt = time.Time{}
	m.btopStreamPausedAt = time.Time{}
}

func (m *dashboardModel) restartBtopStream() {
	target := m.btopStreamTarget
	if target == "" {
		target = m.selectedTarget()
	}
	if m.btopStreamPool != nil {
		m.btopStreamPool.cancelTarget(target)
	}
	m.deactivateBtopStream()
}

func (m *dashboardModel) closeBtopStreams() {
	pool := m.btopStreamPool
	m.btopStreamPool = nil
	m.btopStreamWaiting = false
	if pool != nil {
		pool.close()
	}
	m.deactivateBtopStream()
}

func (m *dashboardModel) resetTelemetryTarget() {
	m.telemetryTarget = m.selectedTarget()
	m.telemetryGen++
	m.deactivateBtopStream()
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

func workspaceLabel(name string) string {
	switch normalizeWorkspaceMode(name) {
	case "workbench":
		return "Hosts"
	case "console":
		return "Monitor"
	case "fleet":
		return "Fleet"
	default:
		return "Hosts"
	}
}

type settingsItem string

const (
	settingProfile               settingsItem = "profile"
	settingTheme                 settingsItem = "theme"
	settingBackground            settingsItem = "background"
	settingDensity               settingsItem = "density"
	settingExperimentalTabs      settingsItem = "experimental-tabs"
	settingExperimentalFleetBtop settingsItem = "experimental-fleet-btop"
	settingWorkspace             settingsItem = "workspace"
	settingConfig                settingsItem = "config"
)

func settingsItems() []settingsItem {
	return []settingsItem{
		settingProfile,
		settingTheme,
		settingBackground,
		settingDensity,
		settingExperimentalTabs,
		settingExperimentalFleetBtop,
		settingWorkspace,
		settingConfig,
	}
}

func (m dashboardModel) updateSetting(item settingsItem, direction int) (tea.Model, tea.Cmd) {
	if m.settingsSaving {
		return m, nil
	}
	switch item {
	case settingProfile:
		names := visualProfileNames()
		name := cycleSettingValue(m.profile, names, direction)
		m.settingsSaving = true
		return m, m.saveSettingsCommand("profile", name, false)
	case settingTheme:
		if direction < 0 {
			return m, nil
		}
		m.openThemePreview()
	case settingBackground:
		background := "transparent"
		if loadedConfig.UI.Background == "transparent" {
			background = "opaque"
		}
		m.settingsSaving = true
		return m, m.saveSettingsCommand("background", background, false)
	case settingDensity:
		name := cycleSettingValue(m.density, []string{"adaptive", "compact", "comfortable"}, direction)
		m.settingsSaving = true
		return m, m.saveSettingsCommand("density", name, false)
	case settingExperimentalTabs:
		m.settingsSaving = true
		return m, m.saveSettingsCommand("experimental_tabs", "", !m.experimentalTabs)
	case settingExperimentalFleetBtop:
		m.settingsSaving = true
		return m, m.saveSettingsCommand("monitor_btop", "", !m.experimentalFleetBtop)
	case settingWorkspace:
		if direction < 0 {
			return m, nil
		}
		m.openWorkspacePreview()
	case settingConfig:
		if direction < 0 {
			return m, nil
		}
		m.settingsOpen = false
		return m.choose(actionConfig)
	}
	return m, nil
}

func cycleSettingValue(current string, values []string, direction int) string {
	index := 0
	for candidate, value := range values {
		if value == current {
			index = candidate
			break
		}
	}
	if direction < 0 {
		return values[(index+len(values)-1)%len(values)]
	}
	return values[(index+1)%len(values)]
}

func (m dashboardModel) workspaceTabsVisible() bool {
	return m.experimentalTabs && m.width >= 150 && m.height >= 28
}

func (m *dashboardModel) cycleWorkspace(delta int) {
	names := workspaceModes()
	current := 0
	for index, name := range names {
		if name == normalizeWorkspaceMode(m.workspace) {
			current = index
			break
		}
	}
	m.workspace = names[(current+delta+len(names))%len(names)]
	m.monitorActionFocus = false
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

func (m dashboardModel) saveSettingsCommand(key, value string, enabled bool) tea.Cmd {
	configPath := m.configPath
	return func() tea.Msg {
		var err error
		switch key {
		case "background":
			err = saveBackgroundToConfig(configPath, value)
		case "profile":
			err = saveVisualProfileToConfig(configPath, value)
		case "density":
			err = saveDensityToConfig(configPath, value)
		case "experimental_tabs":
			err = saveExperimentalTabsToConfig(configPath, enabled)
		case "monitor_btop":
			err = saveMonitorBtopToConfig(configPath, enabled)
		default:
			err = fmt.Errorf("unknown UI setting %q", key)
		}
		return settingsSaveMsg{Key: key, Value: value, Enabled: enabled, Err: err}
	}
}

func (m dashboardModel) configuredCommandCmd(host, command string, operationID uint64) tea.Cmd {
	return func() tea.Msg {
		output, err := runConfiguredRemoteCommandCaptured(host, command)
		return configuredCommandMsg{OperationID: operationID, Output: output, Err: err}
	}
}

var dashboardTerminalCommandBuilder = buildDashboardTerminalCommand

func buildDashboardTerminalCommand(selection dashboardSelection) (*exec.Cmd, error) {
	switch selection.Action {
	case actionSSH:
		if _, err := exec.LookPath("ssh"); err != nil {
			return nil, fmt.Errorf("ssh not found in PATH: %w", err)
		}
		return buildSSHCommand(context.Background(), selection.Host, true, "")
	case actionCustom:
		command := sanitizeCommandText(selection.Command.Command)
		if command == "" {
			return nil, errors.New("configured command is empty or contains unsafe control characters")
		}
		return buildSSHCommand(context.Background(), selection.Host, true, remoteShellCommand("sh", command))
	case actionCopyKey:
		binary, err := exec.LookPath("ssh-copy-id")
		if err != nil {
			return nil, fmt.Errorf("ssh-copy-id not found in PATH: %w", err)
		}
		args, err := buildSSHCopyIDArgs(selection.Host)
		if err != nil {
			return nil, err
		}
		return exec.Command(binary, args...), nil
	default:
		return nil, errors.New("action does not own the terminal")
	}
}

func (m dashboardModel) startTerminalAction(selection dashboardSelection, usageCmd tea.Cmd) (tea.Model, tea.Cmd) {
	command, err := dashboardTerminalCommandBuilder(selection)
	if err != nil {
		m.notice = selectionLabel(selection) + " unavailable: " + sanitizeTerminalText(err.Error())
		m.noticeError = true
		return m, nil
	}
	m.commandOpen = false
	m.commandFiltering = false
	m.commandQuery = ""
	m.confirmOpen = false
	m.confirmAction = dashboardSelection{}
	m.confirmOffset = 0
	m.deactivateBtopStream()
	m.terminalRunning = true
	m.notice = "OpenSSH session active"
	m.noticeError = false
	m.startOperation(selection.Action, selectionLabel(selection), selection.Host)
	operationID := m.operationID
	run := tea.ExecProcess(command, func(err error) tea.Msg {
		return terminalActionFinishedMsg{OperationID: operationID, Selection: selection, Err: err}
	})
	if usageCmd == nil {
		return m, run
	}
	return m, tea.Sequence(usageCmd, run)
}

func (m dashboardModel) startSavedCommand(selection dashboardSelection) (tea.Model, tea.Cmd) {
	usageCmd := m.recordActionUsage(actionCustom, selection.Command)
	if selection.Command.Interactive {
		return m.startTerminalAction(selection, usageCmd)
	}
	m.commandOpen = false
	m.commandFiltering = false
	m.commandQuery = ""
	m.confirmOpen = false
	m.confirmAction = dashboardSelection{}
	m.confirmOffset = 0
	m.commandRunning = true
	m.commandOffset = 0
	m.commandResult = &configuredCommandResult{
		Action: actionCustom, Host: selection.Host, Command: selection.Command,
	}
	m.startOperation(actionCustom, selection.Command.Name, selection.Host)
	return m, tea.Batch(usageCmd, m.configuredCommandCmd(selection.Host, selection.Command.Command, m.operationID))
}

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
		return "", errors.New("storage scan returned no storage volumes")
	}
	return renderStorageInventory(snapshot.Disks), nil
}

func renderStorageInventory(disks []diskUsage) string {
	disks = meaningfulStorageDisks(disks)
	lines := []string{"VOLUME @ MOUNT              USED / TOTAL       USAGE"}
	for _, disk := range disks {
		percent := diskPercent(disk)
		identity := storageVolumeLabel(disk) + " @ " + disk.Mountpoint
		capacity := valueOr(formatDecimalBytes(disk.UsedBytes), "0 GB") + " / " +
			valueOr(formatDecimalBytes(disk.TotalBytes), "0 GB")
		lines = append(lines, fmt.Sprintf(
			"%-27s  %-18s  %s %3.0f%%",
			truncateText(identity, 27),
			capacity,
			storageUsageBar(percent, 10),
			percent,
		))
	}
	return strings.Join(lines, "\n")
}

func storageVolumeLabel(disk diskUsage) string {
	source := strings.TrimSpace(disk.Filesystem)
	trimmed := strings.TrimRight(source, `\/`)
	if len(trimmed) >= 2 && trimmed[1] == ':' {
		return strings.ToUpper(trimmed[:2])
	}
	if strings.HasPrefix(trimmed, "/dev/") {
		return filepath.Base(trimmed)
	}
	return valueOr(trimmed, "volume")
}

func storageUsageBar(percent float64, cells int) string {
	if cells <= 0 {
		return ""
	}
	percent = min(100, max(0, percent))
	filled := int(percent*float64(cells)/100 + 0.5)
	filled = min(cells, max(0, filled))
	return strings.Repeat("█", filled) + strings.Repeat("░", cells-filled)
}

func styledStorageUsageBar(s dashboardStyles, percent float64, cells int) string {
	bar := storageUsageBar(percent, cells)
	filled := strings.Count(bar, "█")
	full, empty := bar[:filled*len("█")], bar[filled*len("█"):]
	style := s.success
	switch {
	case percent >= 90:
		style = s.failure
	case percent >= 75:
		style = s.warning
	}
	return style.Render(full) + s.muted.Render(empty)
}

func renderDashboardStorageRow(s dashboardStyles, disk diskUsage, width int) string {
	percent := diskPercent(disk)
	identityWidth := min(22, max(12, width/3))
	identity := padCell(truncateText(storageVolumeLabel(disk)+" · "+disk.Mountpoint, identityWidth), identityWidth)
	capacity := formatDecimalBytes(disk.UsedBytes) + " / " + formatDecimalBytes(disk.TotalBytes)
	capacityWidth := min(19, max(13, width/3))
	capacity = padCell(truncateText(capacity, capacityWidth), capacityWidth)
	barCells := 0
	switch {
	case width >= 60:
		barCells = 10
	case width >= 48:
		barCells = 6
	}
	line := s.text.Render(identity) + "  " + s.muted.Render(capacity)
	if barCells > 0 {
		line += "  " + styledStorageUsageBar(s, percent, barCells)
	}
	line += s.muted.Render(fmt.Sprintf(" %3.0f%%", percent))
	return ansi.Truncate(line, max(1, width), "")
}

func dashboardActionUsageKey(action dashboardAction, command commandConfig) string {
	if action == actionCustom {
		return "custom:" + strings.ToLower(strings.TrimSpace(command.Name))
	}
	return string(action)
}

// hostActivityMsg reports the outcome of the asynchronous frecency update.
type hostActivityMsg struct{ Err error }

// recordHostSuccessCmd persists a successful host interaction off the UI
// goroutine: updateState takes a file lock (up to 2 s) and fsyncs, which used
// to run inline in Update and stall rendering.
func (m dashboardModel) recordHostSuccessCmd(target string, usedAt time.Time) tea.Cmd {
	statePath := m.statePath
	if statePath == "" || target == "" {
		return nil
	}
	return func() tea.Msg {
		return hostActivityMsg{Err: recordHostSuccess(statePath, target, usedAt)}
	}
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
	m.actionUsageGen++
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
	if action == actionSSH {
		selection := dashboardSelection{Action: actionSSH, Host: m.selectedTarget()}
		return m.startTerminalAction(selection, m.recordActionUsage(actionSSH, commandConfig{}))
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

// availableCommands returns the sorted action/saved-command list for the
// currently selected host, served from m.commandsCache when possible.
// Recomputing it means calling commandsForTarget, which merges global, tag,
// and host-profile commands and calls profileForTarget -- a map iteration
// plus sort.Strings over every configured host profile -- so this used to
// run in full on every single frame from both actionRailView and (via
// filteredCommands) the command palette. The cache is keyed on the selected
// target and actionUsageGen, the only two things that change the result
// during a running session; PinnedActions is only ever edited in the YAML
// config on disk, never from inside the dashboard, so it needs no key entry.
func (m dashboardModel) availableCommands() []dashboardCommand {
	target := m.selectedTarget()
	if m.commandsCache != nil && m.commandsCache.valid &&
		m.commandsCache.target == target && m.commandsCache.gen == m.actionUsageGen {
		return m.commandsCache.commands
	}
	commands := m.computeAvailableCommands(target)
	if m.commandsCache != nil {
		*m.commandsCache = dashboardCommandsCache{valid: true, target: target, gen: m.actionUsageGen, commands: commands}
	}
	return commands
}

func (m dashboardModel) computeAvailableCommands(target string) []dashboardCommand {
	if target == "" {
		commands := []dashboardCommand{
			{Label: "Settings", Description: "Themes, workspace, and config", Action: actionSettings},
		}
		m.sortCommandsByUsage(commands)
		return commands
	}
	commands := []dashboardCommand{
		{"Connect", "Key, password, or MFA", actionSSH, commandConfig{}},
		{"Copy key", "Set up passwordless SSH", actionCopyKey, commandConfig{}},
		{"Pull", "Download from remote", actionPull, commandConfig{}},
		{"Push", "Upload to remote", actionPush, commandConfig{}},
		{"Refresh info", "Update system details", actionInfo, commandConfig{}},
		{"Check host", "Refresh this connection", actionProbe, commandConfig{}},
		{"Check all", "Refresh every connection", actionProbeAll, commandConfig{}},
		{"Monitor", "Open system monitor", actionTop, commandConfig{}},
		{"Network", "Open network diagnostics", actionNet, commandConfig{}},
		{"Storage", "Inspect storage volumes", actionStorage, commandConfig{}},
		{"Fleet", "Inspect saved hosts", actionFleet, commandConfig{}},
		{"Settings", "Themes, workspace, and config", actionSettings, commandConfig{}},
	}
	for _, command := range commandsForTarget(target) {
		commands = append(commands, dashboardCommand{
			Label: command.Name, Description: command.Description, Action: actionCustom, Command: command,
		})
	}
	m.sortCommandsByUsage(commands)
	return commands
}

func (m dashboardModel) sortCommandsByUsage(commands []dashboardCommand) {
	pinOrder := make(map[string]int, len(loadedConfig.UI.PinnedActions))
	for index, pin := range loadedConfig.UI.PinnedActions {
		pin = normalizedDashboardPin(pin)
		if _, exists := pinOrder[pin]; exists {
			continue
		}
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

func normalizedDashboardPin(pin string) string {
	switch pin {
	case "themes", "workspace", "config":
		return "settings"
	default:
		return pin
	}
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
	if m.helpOpen {
		return m.finishView(m.helpView())
	}
	if m.confirmOpen {
		return m.finishView(m.confirmCommandView())
	}
	if m.commandResult != nil {
		return m.finishView(m.commandResultView())
	}
	if m.transfer != nil {
		return m.finishView(m.transferView())
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
	if m.settingsOpen {
		return m.finishView(m.settingsView())
	}
	if m.height < 16 {
		return m.finishView(m.shortView())
	}
	s := m.styles()
	header := m.headerView(s)
	footer := m.footerView(s)
	bodyHeight, workspaceHeight, drawerHeight := m.dashboardHeights()
	if m.btopFrameCache != nil {
		m.btopFrameCache.deferSwap = true
		m.btopFrameCache.pendingTwins = nil
		m.btopFrameCache.pendingLines = nil
	}
	body := m.dashboardBodyView(s, m.width, workspaceHeight)
	if drawerHeight > 0 {
		body = lipgloss.JoinVertical(lipgloss.Left,
			fitTerminalView(body, m.width, workspaceHeight),
			fitTerminalView(m.activityDrawerView(s, m.width, drawerHeight), m.width, drawerHeight),
		)
	}
	body = fitTerminalView(body, m.width, bodyHeight)
	view := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	if m.btopFrameCache != nil {
		view = btopSwapTwins(view, m.btopFrameCache.pendingTwins, m.btopFrameCache.pendingLines)
		m.btopFrameCache.deferSwap = false
		m.btopFrameCache.pendingTwins = nil
		m.btopFrameCache.pendingLines = nil
	}
	return m.finishView(view)
}

func (m dashboardModel) dashboardHeights() (body, workspace, drawer int) {
	body = max(3, m.height-dashboardChromeRows)
	workspace = body
	if m.activityOpen && body >= 12 {
		drawer = min(12, max(7, body/3))
		workspace = max(3, body-drawer)
	}
	return body, workspace, drawer
}

func (m dashboardModel) dashboardBodyView(s dashboardStyles, width, bodyHeight int) string {
	var body string
	switch {
	case m.workspaceTabsVisible() && bodyHeight >= 12:
		body = m.ultraWideView(s, width, bodyHeight)
	case width >= 150 && bodyHeight >= 24:
		body = m.ultraWideView(s, width, bodyHeight)
	case width >= 96:
		hostWidth := min(48, max(38, width*38/100))
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			fitTerminalView(m.hostListView(s, hostWidth, bodyHeight), hostWidth, bodyHeight),
			fitTerminalView(m.detailView(s, width-hostWidth, bodyHeight), width-hostWidth, bodyHeight),
		)
	case width >= 72 && bodyHeight >= 18:
		hostHeight := max(10, bodyHeight*3/5)
		detailHeight := bodyHeight - hostHeight
		body = lipgloss.JoinVertical(lipgloss.Left,
			fitTerminalView(m.hostListView(s, width, hostHeight), width, hostHeight),
			fitTerminalView(m.compactDetailView(s, width, detailHeight), width, detailHeight),
		)
	default:
		body = m.compactView(s, width, bodyHeight)
	}
	return fitTerminalView(body, width, bodyHeight)
}

func (m dashboardModel) finishView(view string) string {
	rendered := fitTerminalView(view, m.width, m.height)
	if !m.plain && m.theme.Background != "" {
		lines := strings.Split(rendered, "\n")
		for len(lines) < m.height {
			lines = append(lines, "")
		}
		// lipgloss.PlaceHorizontal(width, Left, line, ...) is a no-op
		// whenever the line's cell width is already >= width (its gap<=0
		// early return), and it is a pure function of (width, line) --
		// every empty filler line above produces the exact same padded
		// blank, so we only need to compute that once. Panels in this UI
		// are built to fill their column, so most non-blank lines already
		// hit the no-op case too; skipping PlaceHorizontal's rune-by-rune
		// whitespace rendering for those is the actual win.
		var blankLine string
		haveBlankLine := false
		for index := range lines {
			if terminalWidth(lines[index]) >= m.width {
				continue
			}
			if lines[index] == "" {
				if !haveBlankLine {
					blankLine = lipgloss.PlaceHorizontal(
						m.width, lipgloss.Left, "",
						lipgloss.WithWhitespaceBackground(lipgloss.Color(m.theme.Background)),
					)
					haveBlankLine = true
				}
				lines[index] = blankLine
				continue
			}
			lines[index] = lipgloss.PlaceHorizontal(
				m.width,
				lipgloss.Left,
				lines[index],
				lipgloss.WithWhitespaceBackground(lipgloss.Color(m.theme.Background)),
			)
		}
		rendered = strings.Join(lines, "\n")
		cache := m.paintCache()
		rendered = paintTerminalSurface(rendered, cache.textForegroundPrefix, cache.screenBackgroundPrefix)
	}
	return rendered
}

func (m dashboardModel) renderPanel(style lipgloss.Style, content string) string {
	rendered := style.Render(content)
	if m.plain || m.theme.Surface == "" {
		return rendered
	}
	cache := m.paintCache()
	return paintTerminalSurface(rendered, cache.textForegroundPrefix, cache.surfaceBackgroundPrefix)
}

// paintTerminalSurface fills in the background (and, where unset, the
// foreground) of every printable cell in view that doesn't already carry its
// own SGR color, using precomputed ANSI prefixes (see dashboardRenderCache
// and terminalStylePrefix) rather than rebuilding a lipgloss.Style and
// rendering a throwaway marker on every call.
func paintTerminalSurface(view, foregroundPrefix, backgroundPrefix string) string {
	if view == "" || backgroundPrefix == "" {
		return view
	}

	var painted strings.Builder
	painted.Grow(len(view) + len(view)/4)
	foregroundSet, backgroundSet := false, false
	for index := 0; index < len(view); {
		if view[index] == '\x1b' && index+1 < len(view) && view[index+1] == '[' {
			end := index + 2
			for end < len(view) && (view[end] < 0x40 || view[end] > 0x7e) {
				end++
			}
			if end < len(view) {
				sequence := view[index : end+1]
				painted.WriteString(sequence)
				if view[end] == 'm' {
					updateTerminalColorState(sequence, &foregroundSet, &backgroundSet)
				}
				index = end + 1
				continue
			}
		}

		character := view[index]
		if character == '\n' {
			painted.WriteByte(character)
			foregroundSet, backgroundSet = false, false
			index++
			continue
		}
		if character != '\r' && character != '\t' && character >= 0x20 {
			if !backgroundSet {
				painted.WriteString(backgroundPrefix)
				backgroundSet = true
			}
			if !foregroundSet && foregroundPrefix != "" {
				painted.WriteString(foregroundPrefix)
				foregroundSet = true
			}
		}
		painted.WriteByte(character)
		index++
	}
	result := painted.String()
	if !strings.HasSuffix(result, "\x1b[0m") && !strings.HasSuffix(result, "\x1b[m") {
		painted.WriteString("\x1b[0m")
		result = painted.String()
	}
	return result
}

func terminalStylePrefix(style lipgloss.Style) string {
	const marker = "N"
	sample := style.Render(marker)
	markerIndex := strings.Index(sample, marker)
	if markerIndex <= 0 {
		return ""
	}
	return sample[:markerIndex]
}

func updateTerminalColorState(sequence string, foregroundSet, backgroundSet *bool) {
	parameters := strings.TrimSuffix(strings.TrimPrefix(sequence, "\x1b["), "m")
	if parameters == "" {
		*foregroundSet, *backgroundSet = false, false
		return
	}
	parts := strings.Split(parameters, ";")
	for index := 0; index < len(parts); index++ {
		code, err := strconv.Atoi(parts[index])
		if err != nil {
			continue
		}
		switch {
		case code == 0:
			*foregroundSet, *backgroundSet = false, false
		case code == 39:
			*foregroundSet = false
		case code == 49:
			*backgroundSet = false
		case code == 38:
			*foregroundSet = true
			index += extendedColorParameterCount(parts[index+1:])
		case code == 48:
			*backgroundSet = true
			index += extendedColorParameterCount(parts[index+1:])
		case code >= 30 && code <= 37 || code >= 90 && code <= 97:
			*foregroundSet = true
		case code >= 40 && code <= 47 || code >= 100 && code <= 107:
			*backgroundSet = true
		}
	}
}

func extendedColorParameterCount(parameters []string) int {
	if len(parameters) == 0 {
		return 0
	}
	switch parameters[0] {
	case "2":
		return min(4, len(parameters))
	case "5":
		return min(2, len(parameters))
	default:
		return 0
	}
}

func (m dashboardModel) ultraWideView(s dashboardStyles, width, height int) string {
	if !m.experimentalTabs {
		switch normalizeWorkspaceMode(m.workspace) {
		case "console":
			return m.classicConsoleWorkspaceView(s, width, height)
		case "fleet":
			return m.fleetWorkspaceView(s, width, height)
		default:
			return m.classicWorkbenchView(s, width, height)
		}
	}
	switch normalizeWorkspaceMode(m.workspace) {
	case "console":
		return m.consoleWorkspaceView(s, width, height)
	case "fleet":
		return m.fleetWorkspaceView(s, width, height)
	default:
		return m.workbenchWorkspaceView(s, width, height)
	}
}

func (m dashboardModel) classicWorkspaceColumns(s dashboardStyles, width, height int) string {
	hostWidth := min(42, max(38, width*24/100))
	actionWidth := min(38, max(32, width*20/100))
	detailWidth := max(56, width-hostWidth-actionWidth)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		fitTerminalView(m.hostListView(s, hostWidth, height), hostWidth, height),
		fitTerminalView(m.detailView(s, detailWidth, height), detailWidth, height),
		fitTerminalView(m.actionRailView(s, actionWidth, height), actionWidth, height),
	)
}

func (m dashboardModel) classicWorkbenchView(s dashboardStyles, width, height int) string {
	deckHeight := min(16, max(9, height/4))
	topHeight := max(1, height-deckHeight)
	pulseWidth := max(64, width*3/5)
	activityWidth := width - pulseWidth
	top := m.classicWorkspaceColumns(s, width, topHeight)
	deckBody := lipgloss.JoinHorizontal(lipgloss.Top,
		fitTerminalView(m.telemetryView(s, pulseWidth, deckHeight), pulseWidth, deckHeight),
		fitTerminalView(m.activityView(s, activityWidth, deckHeight), activityWidth, deckHeight),
	)
	deck := m.frameDeck(deckBody, width, deckHeight)
	return lipgloss.NewStyle().Width(width).Height(height).Render(
		lipgloss.JoinVertical(lipgloss.Left, top, deck),
	)
}

func (m dashboardModel) classicConsoleWorkspaceView(s dashboardStyles, width, height int) string {
	activityHeight := min(14, max(8, height/4))
	consoleHeight := max(1, height-activityHeight)
	hostWidth := min(42, max(36, width*24/100))
	consoleWidth := width - hostWidth
	console := lipgloss.JoinHorizontal(lipgloss.Top,
		fitTerminalView(m.hostListView(s, hostWidth, consoleHeight), hostWidth, consoleHeight),
		fitTerminalView(m.consoleOutputView(s, consoleWidth, consoleHeight), consoleWidth, consoleHeight),
	)
	activity := m.frameDeck(m.activityView(s, width, max(1, activityHeight-1)), width, activityHeight)
	return lipgloss.NewStyle().Width(width).Height(height).Render(
		lipgloss.JoinVertical(lipgloss.Left, console, activity),
	)
}

func (m dashboardModel) workspaceColumns(s dashboardStyles, width, height int) string {
	hostWidth := m.hostColumnWidth(width)
	detailWidth := max(56, width-hostWidth)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		fitTerminalView(m.hostListView(s, hostWidth, height), hostWidth, height),
		fitTerminalView(m.detailView(s, detailWidth, height), detailWidth, height),
	)
}

func (m dashboardModel) workbenchWorkspaceView(s dashboardStyles, width, height int) string {
	actionHeight := 2
	mainHeight := max(1, height-actionHeight)
	main := m.workspaceColumns(s, width, mainHeight)
	actions := fitTerminalView(m.hostActionBarView(s, width, actionHeight), width, actionHeight)
	content := lipgloss.JoinVertical(lipgloss.Left, main, actions)
	return lipgloss.NewStyle().Width(width).Height(height).Render(content)
}

func (m dashboardModel) consoleWorkspaceView(s dashboardStyles, width, height int) string {
	actionWidth := m.monitorActionWidth(width)
	monitorWidth := width - actionWidth
	summaryHeight, btopHeight := monitorPaneHeights(height)
	// Lay out with the pane's ASCII twin so the joins and fits below never
	// measure the ANSI-dense btop frame; swap the real rows in at the end.
	// The painted real pane rows only differ from the twin's by zero-width
	// escape sequences, so every width decision made here is identical.
	panel, twinPanel, _, _ := m.btopPanelViews(s, monitorWidth, btopHeight, false, true)
	panelRows := strings.Split(panel, "\n")
	twinRows := strings.Split(twinPanel, "\n")
	rowTwins := make([]string, 0, len(twinRows))
	rowLines := make([]string, 0, len(twinRows))
	for index := range twinRows {
		if index < len(panelRows) && twinRows[index] != panelRows[index] {
			rowTwins = append(rowTwins, twinRows[index])
			rowLines = append(rowLines, panelRows[index])
		}
	}
	monitor := lipgloss.JoinVertical(lipgloss.Left,
		fitTerminalView(m.monitorSummaryView(s, monitorWidth, summaryHeight), monitorWidth, summaryHeight),
		fitTerminalView(twinPanel, monitorWidth, btopHeight),
	)
	composed := lipgloss.NewStyle().Width(width).Height(height).Render(
		lipgloss.JoinHorizontal(lipgloss.Top,
			fitTerminalView(monitor, monitorWidth, height),
			fitTerminalView(m.monitorActionRailView(s, actionWidth, height), actionWidth, height),
		),
	)
	if m.btopFrameCache != nil && m.btopFrameCache.deferSwap {
		m.btopFrameCache.pendingTwins = rowTwins
		m.btopFrameCache.pendingLines = rowLines
		return composed
	}
	return btopSwapTwins(composed, rowTwins, rowLines)
}

func monitorPaneHeights(height int) (summary, btop int) {
	switch {
	case height >= 38:
		summary = max(10, height/3)
	case height >= 30:
		summary = 6
	default:
		summary = 4
	}
	// Keep the complete minimum btop terminal visible: 24 terminal rows plus
	// the pane's top border, title, and divider.
	if height >= btopMinRows+3 {
		summary = min(summary, max(1, height-(btopMinRows+3)))
	}
	return summary, max(1, height-summary)
}

func (m dashboardModel) monitorSummaryView(s dashboardStyles, width, height int) string {
	if height >= 9 {
		return m.telemetryView(s, width, height)
	}
	host := m.selectedHost()
	entry := m.telemetry[host.Target]
	lines := []string{s.focus.Render("LIVE MONITOR")}
	if host.Target == "" {
		lines = append(lines, s.muted.Render("Select a host to begin sampling."))
	} else if entry.Current.CollectedAt.IsZero() {
		lines = append(lines, s.text.Render(displayName(host))+"  "+m.statusText(s, host.Reachability),
			s.muted.Render("Waiting for a compact sample"))
	} else {
		sample := entry.Current
		stats := fmt.Sprintf("CPU %.0f%% · MEM %s · LOAD %.2f · ↓ %s ↑ %s",
			sample.CPUUtilization, capacityUsage(sample.MemoryUsed, sample.MemoryTotal), sample.LoadOne,
			formatByteRate(sample.NetworkRXRate), formatByteRate(sample.NetworkTXRate))
		lines = append(lines,
			s.text.Render(displayName(host))+"  "+m.statusText(s, host.Reachability)+
				s.muted.Render(" · "+relativeTime(sample.CollectedAt, time.Now())),
			s.muted.Render(truncateText(stats, max(1, width-4))),
		)
	}
	return m.renderPanel(
		s.panel.BorderLeft(false).BorderRight(false).BorderTop(false).BorderBottom(false).
			Width(max(1, width)).Height(max(1, height)).Padding(0, 1),
		strings.Join(lines, "\n"),
	)
}

func (m dashboardModel) hostColumnWidth(width int) int {
	percentage, minimum, maximum := 25, 36, 44
	switch m.density {
	case "compact":
		percentage, minimum, maximum = 22, 32, 40
	case "comfortable":
		percentage, minimum, maximum = 28, 40, 48
	}
	return min(maximum, max(minimum, width*percentage/100))
}

func (m dashboardModel) monitorActionWidth(width int) int {
	percentage, minimum, maximum := 23, 32, 40
	if m.density == "compact" {
		percentage, minimum, maximum = 20, 30, 36
	}
	if m.density == "comfortable" {
		percentage, minimum, maximum = 25, 36, 44
	}
	return min(maximum, max(minimum, width*percentage/100))
}

func (m dashboardModel) hostActionBarView(s dashboardStyles, width, height int) string {
	hints := strings.Join([]string{
		keyHint(s, "enter", "connect"),
		keyHint(s, "a", "actions"),
		keyHint(s, "r", "refresh"),
		keyHint(s, "o", "activity"),
	}, "  ")
	host := truncateText(valueOr(displayName(m.selectedHost()), "No host selected"), max(12, width-lipgloss.Width(hints)-8))
	gap := max(1, width-lipgloss.Width(hints)-lipgloss.Width(host)-4)
	return m.renderPanel(
		s.panel.BorderLeft(false).BorderRight(false).BorderBottom(false).
			Width(max(1, width-2)).Height(max(1, height-1)).Padding(0, 1),
		s.muted.Render(hints)+strings.Repeat(" ", gap)+s.focus.Render(host),
	)
}

func (m dashboardModel) fleetWorkspaceView(s dashboardStyles, width, height int) string {
	return fitTerminalView(m.fleetDeckView(s, width, height), width, height)
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
		pins[normalizedDashboardPin(pin)] = struct{}{}
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
	return m.renderPanel(
		s.panel.BorderTop(false).BorderRight(false).BorderBottom(false).
			Width(max(1, width-1)).Height(max(1, height)).Padding(1, 1),
		strings.Join(lines, "\n"),
	)
}

func (m dashboardModel) monitorActions() []dashboardCommand {
	return []dashboardCommand{
		{Label: "Connect", Description: "Open SSH session", Action: actionSSH},
		{Label: "Refresh info", Description: "Update system details", Action: actionInfo},
		{Label: "Storage", Description: "Inspect all volumes", Action: actionStorage},
		{Label: "Check host", Description: "Refresh connection", Action: actionProbe},
		{Label: "All actions", Description: "Commands and tools"},
	}
}

func (m dashboardModel) monitorActionRailView(s dashboardStyles, width, height int) string {
	lines := []string{
		s.focus.Render("ACTIONS"),
		s.muted.Render("Selected host"),
		"",
	}
	commands := m.monitorActions()
	cursor := min(max(0, m.monitorActionCursor), len(commands)-1)
	for index, command := range commands {
		label := truncateText(command.Label, max(1, width-6))
		description := truncateText(command.Description, max(1, width-8))
		if m.monitorActionFocus && index == cursor {
			lines = append(lines,
				s.selected.Render("› "+padCell(label, max(1, width-6))),
				s.selectedMuted.Render("  "+padCell(description, max(1, width-6))),
			)
			continue
		}
		marker := "  "
		if index == 0 {
			marker = "◆ "
		}
		lines = append(lines, s.text.Render(marker+label), s.muted.Render("  "+description))
	}
	footer := "[l/→] focus · [a] all actions"
	if m.monitorActionFocus {
		footer = "j/k move · enter run · h/← back"
	}
	lines = append(lines, "", s.muted.Render(truncateText(footer, max(1, width-5))))
	return m.renderPanel(
		s.panel.BorderTop(false).BorderRight(false).BorderBottom(false).
			Width(max(1, width-1)).Height(max(1, height)).Padding(1, 1),
		strings.Join(lines, "\n"),
	)
}

func (m dashboardModel) activateMonitorAction() (tea.Model, tea.Cmd) {
	commands := m.monitorActions()
	if len(commands) == 0 {
		return m, nil
	}
	command := commands[min(max(0, m.monitorActionCursor), len(commands)-1)]
	if command.Action == "" {
		m.monitorActionFocus = false
		m.commandOpen = true
		m.commandCursor = 0
		m.commandQuery = ""
		return m, nil
	}
	if m.selectedTarget() == "" {
		m.notice = "Select a host before running an action"
		m.noticeError = false
		return m, nil
	}
	usageCmd := m.recordActionUsage(command.Action, command.Command)
	switch command.Action {
	case actionSSH:
		m.monitorActionFocus = false
		return m.startTerminalAction(dashboardSelection{Action: actionSSH, Host: m.selectedTarget()}, usageCmd)
	case actionInfo:
		updated, commandCmd := m.startMetadataRefresh()
		return updated, tea.Batch(usageCmd, commandCmd)
	case actionStorage:
		m.monitorActionFocus = false
		m.commandRunning = true
		m.commandOffset = 0
		m.commandResult = &configuredCommandResult{
			Action: actionStorage,
			Host:   m.selectedTarget(),
			Command: commandConfig{
				Name: "Storage", Description: "Mounted storage volumes",
			},
		}
		m.startOperation(actionStorage, "Storage", m.selectedTarget())
		return m, tea.Batch(usageCmd, m.storageCommandCmd(m.selectedTarget(), m.operationID))
	case actionProbe:
		target := m.selectedTarget()
		if target != "" && !m.probing {
			m.startOperation(actionProbe, "Check host", target)
			m.operationProbeID = m.operationID
			m, commandCmd := m.beginProbe([]string{target})
			return m, tea.Batch(usageCmd, commandCmd)
		}
		return m, usageCmd
	default:
		return m, usageCmd
	}
}

func (m dashboardModel) telemetryView(s dashboardStyles, width, height int) string {
	host := m.selectedHost()
	entry := m.telemetry[host.Target]
	title := "HOST PULSE"
	if m.experimentalTabs {
		title = "LIVE MONITOR"
	}
	lines := []string{s.focus.Render(title)}
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
	if len(host.MeaningfulDisks) > 0 && len(lines) < height-5 {
		// Copy before sorting: host.MeaningfulDisks is a cached slice shared
		// across frames and other panels (detailView, compactDetailView),
		// which expect its original meaningfulStorageDisks order ("/" first,
		// then usage desc). Sorting it in place here would corrupt that
		// shared order for everyone else.
		disks := append([]diskUsage(nil), host.MeaningfulDisks...)
		sort.SliceStable(disks, func(left, right int) bool {
			return diskPercent(disks[left]) > diskPercent(disks[right])
		})
		lines = append(lines, "", s.focus.Render("STORAGE PRESSURE"))
		for _, disk := range disks[:min(4, len(disks))] {
			lines = append(lines, renderDashboardStorageRow(s, disk, max(1, width-6)))
		}
	}
	return m.renderPanel(
		s.panel.BorderLeft(false).BorderRight(false).BorderTop(false).BorderBottom(false).
			Width(max(1, width)).Height(max(1, height)).Padding(1, 2),
		strings.Join(lines, "\n"),
	)
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
	return m.renderPanel(
		s.panel.BorderRight(false).BorderTop(false).BorderBottom(false).
			Width(max(1, width-1)).Height(max(1, height)).Padding(1, 2),
		strings.Join(lines, "\n"),
	)
}

func (m dashboardModel) activityDrawerEvents() []activityEvent {
	events := make([]activityEvent, 0, len(m.activities)+1)
	if m.operation != nil && m.operation.Status == "running" {
		events = append(events, activityEvent{
			Label: m.operation.Label, Host: m.operation.Host, Status: "running",
			Summary: "Running " + compactDuration(time.Since(m.operation.StartedAt)),
			Output:  m.operation.Output,
		})
	}
	for index := len(m.activities) - 1; index >= 0; index-- {
		events = append(events, m.activities[index])
	}
	if len(events) == 0 && m.operation != nil && m.operationPersisted {
		events = append(events, activityEvent{
			Label: m.operation.Label, Host: m.operation.Host, Status: m.operation.Status,
			Summary: m.operation.Summary, Output: m.operation.Output, Duration: m.operation.Duration,
		})
	}
	return events
}

func (m dashboardModel) activityDrawerView(s dashboardStyles, width, height int) string {
	events := m.activityDrawerEvents()
	innerWidth := max(1, width-4)
	lines := []string{
		s.focus.Render("ACTIVITY") + s.muted.Render("  operations · newest first"),
	}
	if len(events) == 0 {
		lines = append(lines,
			"",
			s.text.Render("No operations yet."),
			s.muted.Render("Run an action and its result will appear here."),
		)
	} else {
		cursor := min(max(0, m.activityCursor), len(events)-1)
		rowBudget := max(1, min(len(events), height-5))
		start, end := selectionWindow(len(events), cursor, rowBudget)
		for index := start; index < end; index++ {
			event := events[index]
			icon, status := "✓", s.success
			switch event.Status {
			case "running":
				icon, status = "◌", s.warning
			case "error":
				icon, status = "×", s.failure
			}
			subject := event.Label
			if event.Host != "" {
				subject += " · " + event.Host
			}
			detail := valueOr(event.Summary, compactDuration(event.Duration))
			row := icon + " " + truncateText(subject, max(10, innerWidth-lipgloss.Width(detail)-4))
			gap := max(1, innerWidth-lipgloss.Width(row)-lipgloss.Width(detail))
			row += strings.Repeat(" ", gap) + detail
			if index == cursor {
				lines = append(lines, s.selected.Render(padCell(truncateText("› "+row, innerWidth), innerWidth)))
			} else {
				lines = append(lines, status.Render("  "+truncateText(row, max(1, innerWidth-2))))
			}
		}
		selected := events[cursor]
		preview := selected.Output
		if strings.TrimSpace(preview) == "" {
			preview = valueOr(selected.Summary, "No captured output")
		}
		if len(lines) < height-2 {
			label := "OUTPUT · " + selected.Label
			lines = append(lines, s.muted.Render(truncateText(label, innerWidth)))
			for _, line := range tailNonEmptyLines(preview, max(1, height-len(lines)-1)) {
				lines = append(lines, s.text.Render(truncateText(line, innerWidth)))
			}
		}
	}
	footer := "[j/k] move   [o/esc] close"
	if len(lines) < height {
		lines = append(lines, s.muted.Render(truncateText(footer, innerWidth)))
	}
	return m.renderPanel(
		s.panel.BorderLeft(false).BorderRight(false).BorderBottom(false).
			Width(max(1, width-2)).Height(max(1, height-1)).Padding(0, 1),
		strings.Join(lines, "\n"),
	)
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
	return m.renderPanel(
		s.panel.BorderLeft(false).BorderRight(false).BorderTop(false).BorderBottom(false).
			Width(max(1, width)).Height(max(1, height)).Padding(1, 2),
		strings.Join(lines, "\n"),
	)
}

func (m dashboardModel) fleetDeckView(s dashboardStyles, width, height int) string {
	return m.fleetInventoryView(s, width, height)
}

func (m dashboardModel) fleetInventoryView(s dashboardStyles, width, height int) string {
	lines := []string{
		s.focus.Render("FLEET WORKSPACE"),
		s.muted.Render("j/k select · cached inventory with explicit freshness"),
		"",
	}
	innerWidth := max(1, width-4)
	if len(m.filtered) == 0 {
		lines = append(lines, s.muted.Render("No hosts match the current filter."))
	} else {
		lines = append(lines, s.muted.Render(m.fleetInventoryHeader(innerWidth)))
		rowBudget := max(1, height-7)
		start, end := selectionWindow(len(m.filtered), m.cursor, rowBudget)
		for position := start; position < end; position++ {
			host := m.hosts[m.filtered[position]]
			row := m.fleetInventoryRow(host, innerWidth)
			if position == m.cursor {
				lines = append(lines, s.selected.Render("› "+padCell(truncateText(row, max(1, innerWidth-2)), max(1, innerWidth-2))))
			} else {
				lines = append(lines, s.text.Render("  "+truncateText(row, max(1, innerWidth-2))))
			}
		}
		position := fmt.Sprintf("%d–%d / %d hosts", start+1, end, len(m.filtered))
		lines = append(lines, s.muted.Render(truncateText(position, innerWidth)))
	}
	return m.renderPanel(
		s.panel.BorderLeft(false).BorderRight(false).BorderTop(false).BorderBottom(false).
			Width(max(1, width)).Height(max(1, height)).Padding(1, 2),
		strings.Join(lines, "\n"),
	)
}

func (m dashboardModel) fleetInventoryHeader(width int) string {
	switch {
	case width >= 100:
		return padCell("HOST", 22) + "  " + padCell("STATUS", 12) + "  " +
			padCell("CPU", 6) + "  " + padCell("MEM", 7) + "  " + padCell("LOAD", 7) + "  " +
			padCell("NETWORK", 22) + "  AGE"
	case width >= 72:
		return padCell("HOST", 18) + "  " + padCell("STATUS", 12) + "  " +
			padCell("CPU", 6) + "  " + padCell("MEM", 7) + "  " + padCell("LOAD", 7) + "  AGE"
	default:
		return padCell("HOST", max(12, width-28)) + "  CPU / MEM / AGE"
	}
}

func (m dashboardModel) fleetInventoryRow(host dashboardHost, width int) string {
	status := plainReachability(host.Reachability, m.probeTargets[host.Target])
	sample := m.telemetry[host.Target].Current
	freshness := "not scanned"
	if !sample.CollectedAt.IsZero() {
		freshness = relativeTime(sample.CollectedAt, time.Now())
	} else if !host.Updated.IsZero() {
		freshness = relativeTime(host.Updated, time.Now())
	}
	cpu, memory, load, network := "—", "—", "—", "—"
	if !sample.CollectedAt.IsZero() {
		cpu = fmt.Sprintf("%.0f%%", sample.CPUUtilization)
		if sample.MemoryTotal > 0 {
			memory = fmt.Sprintf("%.0f%%", 100*float64(sample.MemoryUsed)/float64(sample.MemoryTotal))
		}
		load = fmt.Sprintf("%.2f", sample.LoadOne)
		network = "↓ " + formatByteRate(sample.NetworkRXRate) + " ↑ " + formatByteRate(sample.NetworkTXRate)
	}
	switch {
	case width >= 100:
		return padCell(truncateText(displayName(host), 22), 22) + "  " +
			padCell(truncateText(status, 12), 12) + "  " +
			padCell(cpu, 6) + "  " + padCell(memory, 7) + "  " + padCell(load, 7) + "  " +
			padCell(truncateText(network, 22), 22) + "  " + freshness
	case width >= 72:
		return padCell(truncateText(displayName(host), 18), 18) + "  " +
			padCell(truncateText(status, 12), 12) + "  " +
			padCell(cpu, 6) + "  " + padCell(memory, 7) + "  " + padCell(load, 7) + "  " + freshness
	default:
		stats := cpu + " / " + memory + " / " + freshness
		nameWidth := max(12, width-lipgloss.Width(stats)-4)
		return padCell(truncateText(displayName(host), nameWidth), nameWidth) + "  " +
			truncateText(stats, max(8, width-nameWidth-2))
	}
}

func (m dashboardModel) btopPaneView(s dashboardStyles, width, height int) string {
	return m.btopPanelView(s, width, height, false, false)
}

func (m dashboardModel) btopPanelView(
	s dashboardStyles, width, height int, leftBorder, topBorder bool,
) string {
	panel, _, _, _ := m.btopPanelViews(s, width, height, leftBorder, topBorder)
	return panel
}

// btopPanelViews renders the BTOP pane and also returns an ASCII twin of it
// (same rows, same cell widths, frame rows tagged by marker) plus the swap
// lists, so a caller can lay out the twin cheaply and restore the real rows
// afterwards with btopSwapTwins. Rows other than the frame are identical in
// both, so swapping is a no-op for them.
func (m dashboardModel) btopPanelViews(
	s dashboardStyles, width, height int, leftBorder, topBorder bool,
) (panel, twinPanel string, twins, lines []string) {
	host := m.selectedHost()
	entry := m.telemetry[host.Target]
	innerWidth, frameHeight := btopFrameViewport(width, height, leftBorder, topBorder)
	innerWidth = max(1, innerWidth)
	frameHeight = max(1, frameHeight)
	streamState := s.muted.Render("○ IDLE")
	switch m.btopStreamState {
	case "connecting":
		streamState = s.warning.Render("◌ CONNECTING")
	case "live":
		age := relativeTime(m.btopStreamUpdatedAt, time.Now())
		streamState = s.success.Render("● LIVE") + s.muted.Render(" · "+age)
	case "unavailable":
		streamState = s.muted.Render("○ UNAVAILABLE")
	case "error":
		streamState = s.failure.Render("× DISCONNECTED")
	}
	header := s.focus.Render("BTOP") + "  " + streamState
	if host.Target != "" {
		context := truncateText(displayName(host), max(8, innerWidth-lipgloss.Width(header)-2))
		header += s.muted.Render("  ·  " + context)
	}
	rows := []string{header, s.muted.Render(strings.Repeat("─", innerWidth))}

	if host.Target == "" {
		rows = append(rows, "", s.muted.Render("Select a host to start."))
	} else {
		if m.btopStreamError != "" {
			rows = append(rows, s.failure.Render(truncateText(m.btopStreamError, innerWidth)))
		}
		if entry.Current.BtopFrame != "" {
			// A stale frame stays visible while the stream is gated (for
			// example after shrinking the terminal); say why above it so the
			// IDLE badge is explained instead of hidden behind the old frame.
			if m.btopStreamState == "" {
				if reason := m.btopGateReason(); reason != "" {
					rows = append(rows, s.warning.Render(truncateText(reason, innerWidth)))
					frameHeight = max(1, frameHeight-1)
				}
			}
			frameLines, frameTwins := m.btopFrameLines(entry.Current.BtopFrame, innerWidth, frameHeight)
			rows = append(rows, frameTwins...)
			twins, lines = frameTwins, frameLines
		} else {
			message := "Opening a live remote terminal…"
			switch m.btopStreamState {
			case "":
				// No session is running (or was ever started). Explain the
				// gate instead of implying one is silently connecting.
				if reason := m.btopGateReason(); reason != "" {
					message = reason
				} else {
					message = "Preparing a live remote terminal…"
				}
			case "connecting":
				if m.btopStreamStatus != "" {
					message = m.btopStreamStatus
				}
			case "unavailable":
				message = "btop is not installed on this host"
			case "error":
				message = m.btopStreamError
			case "live":
				message = "Waiting for the first complete btop frame…"
			}
			rows = append(rows, "", s.muted.Render(truncateText(message, innerWidth)))
		}
	}
	footer := "[j/k] host  ·  [r] reconnect  ·  [enter] SSH"
	if len(rows) < height-2 {
		rows = append(rows, "", s.muted.Render(truncateText(footer, innerWidth)))
	}
	panelStyle := s.panel.BorderRight(false).BorderBottom(false)
	panelWidth, panelHeight := width, height
	if !leftBorder {
		panelStyle = panelStyle.BorderLeft(false)
	} else {
		panelWidth--
	}
	if !topBorder {
		panelStyle = panelStyle.BorderTop(false)
	} else {
		panelHeight--
	}
	style := panelStyle.Width(max(1, panelWidth)).Height(max(1, panelHeight)).Padding(0, 1)
	// lipgloss lays out the twins (plain ASCII, cheap to measure); the real
	// frame rows are swapped in before the surface paint so the bytes match
	// rendering the real content directly.
	twinPanel = style.Render(strings.Join(rows, "\n"))
	panel = btopSwapTwins(twinPanel, twins, lines)
	if !m.plain && m.theme.Surface != "" {
		cache := m.paintCache()
		panel = paintTerminalSurface(panel, cache.textForegroundPrefix, cache.surfaceBackgroundPrefix)
	}
	return panel, twinPanel, twins, lines
}

// btopFrameLines returns fitTerminalView(frame, width, height) split into
// lines, served from m.btopFrameCache when the frame (identified by
// btopStreamFrames, see dashboardBtopFrameCache) and viewport size match the
// last computation.
func (m dashboardModel) btopFrameLines(frame string, width, height int) (lines, twins []string) {
	if m.btopFrameCache != nil && m.btopFrameCache.valid &&
		m.btopFrameCache.frames == m.btopStreamFrames &&
		m.btopFrameCache.width == width && m.btopFrameCache.height == height {
		return m.btopFrameCache.lines, m.btopFrameCache.twins
	}
	fitted := fitTerminalView(frame, width, height)
	lines = strings.Split(fitted, "\n")
	twins = make([]string, len(lines))
	for index, line := range lines {
		twins[index] = btopTwinLine(index, terminalWidth(line))
	}
	if m.btopFrameCache != nil {
		*m.btopFrameCache = dashboardBtopFrameCache{
			valid: true, frames: m.btopStreamFrames, width: width, height: height,
			lines: lines, twins: twins,
		}
	}
	return lines, twins
}

// btopTwinMarker is an unknown CSI sequence (zero width for ansi.StringWidth
// and copied through untouched by lipgloss) that tags a twin line so the real
// line can be swapped back in after layout.
const btopTwinMarker = "\x1b[9975;"

func btopTwinLine(index, width int) string {
	return btopTwinMarker + strconv.Itoa(index) + "~" + strings.Repeat("~", width)
}

// btopSwapTwins replaces every twin line in view with its real line.
func btopSwapTwins(view string, twins, lines []string) string {
	if len(twins) == 0 || !strings.Contains(view, btopTwinMarker) {
		return view
	}
	pairs := make([]string, 0, 2*len(twins))
	for index := range twins {
		if index < len(lines) && twins[index] != lines[index] {
			pairs = append(pairs, twins[index], lines[index])
		}
	}
	return strings.NewReplacer(pairs...).Replace(view)
}

func btopFrameViewport(width, height int, leftBorder, topBorder bool) (columns, rows int) {
	columns = width - 2 // one cell of content padding on each side
	if leftBorder {
		columns--
	}
	rows = height - 2 // title and divider
	if topBorder {
		rows--
	}
	return max(0, columns), max(0, rows)
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

// styles returns the dashboardStyles bundle for the current theme/plain
// setting, served from m.renderCache when possible. See dashboardRenderCache
// for why caching through a pointer field is safe despite the value
// receiver.
func (m dashboardModel) styles() dashboardStyles {
	cache := m.paintCache()
	return cache.styles
}

func (m dashboardModel) computeStyles() dashboardStyles {
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
	} else {
		selected = selected.Reverse(true)
		selectedMuted = selectedMuted.Reverse(true)
	}
	semantic := func(color string) lipgloss.Style {
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		if t.Surface != "" {
			style = style.Background(lipgloss.Color(t.Surface))
		}
		return style
	}
	key := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(t.Focus)).
		Padding(0, 1)
	if t.Elevated != "" {
		key = key.Background(lipgloss.Color(t.Elevated))
	} else {
		key = key.Underline(true)
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
		key:           key,
	}
}

// paintCache returns the memoized dashboardRenderCache for the current
// plain/theme pair, recomputing (and, when m.renderCache is non-nil,
// persisting) it on a cache miss. Every value that depends only on
// plain+theme -- the dashboardStyles bundle and the raw ANSI prefixes
// paintTerminalSurface needs -- lives here so it survives across the many
// value-receiver View() calls made while the theme stays the same.
func (m dashboardModel) paintCache() dashboardRenderCache {
	if m.renderCache != nil && m.renderCache.valid &&
		m.renderCache.plain == m.plain && m.renderCache.theme == m.theme {
		return *m.renderCache
	}
	entry := dashboardRenderCache{valid: true, plain: m.plain, theme: m.theme}
	entry.styles = m.computeStyles()
	entry.footerHintsDefault = []string{
		keyHint(entry.styles, "enter", "connect"), keyHint(entry.styles, "j/k", "move"),
		keyHint(entry.styles, "/", "find"), keyHint(entry.styles, "a", "actions"), keyHint(entry.styles, "o", "activity"),
	}
	entry.footerHintsTabs = []string{
		keyHint(entry.styles, "tab", "switch tab"), keyHint(entry.styles, "←/→", "navigate"),
		keyHint(entry.styles, ",", "settings"), keyHint(entry.styles, "h", "keys"),
	}
	if !m.plain {
		if m.theme.Text != "" {
			entry.textForegroundPrefix = terminalStylePrefix(lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Text)))
		}
		if m.theme.Surface != "" {
			entry.surfaceBackgroundPrefix = terminalStylePrefix(lipgloss.NewStyle().Background(lipgloss.Color(m.theme.Surface)))
		}
		if m.theme.Background != "" {
			entry.screenBackgroundPrefix = terminalStylePrefix(lipgloss.NewStyle().Background(lipgloss.Color(m.theme.Background)))
		}
	}
	if m.renderCache != nil {
		*m.renderCache = entry
	}
	return entry
}

func (m dashboardModel) headerView(s dashboardStyles) string {
	online := 0
	for _, host := range m.hosts {
		if host.Reachability.Status == reachOnline {
			online++
		}
	}
	status := s.muted.Render(fmt.Sprintf("%d hosts", len(m.hosts))) + "  " +
		s.live.Render(fmt.Sprintf("● %d online", online))
	if m.probing {
		status += "  " + s.warning.Render(fmt.Sprintf("◌ probing %d/%d", m.probeComplete, m.probeTotal))
	}
	left := s.title.Render("◆ NEXUS") + s.muted.Render("  choose a host")
	if m.workspaceTabsVisible() {
		left = s.title.Render("◆ NEXUS") + "  " + m.workspaceTabs(s)
	}
	if m.width < 60 {
		left = s.title.Render("◆ NEXUS")
		status = fmt.Sprintf("%d saved · %d up", len(m.hosts), online)
		if m.probing {
			status = fmt.Sprintf("probing %d/%d", m.probeComplete, m.probeTotal)
		}
	}
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(status)-4)
	return m.renderPanel(
		s.panel.BorderTop(false).BorderLeft(false).BorderRight(false).
			Width(max(1, m.width-2)).Padding(0, 1),
		left+strings.Repeat(" ", gap)+status,
	)
}

func (m dashboardModel) workspaceTabs(s dashboardStyles) string {
	active := normalizeWorkspaceMode(m.workspace)
	tabs := make([]string, 0, len(workspaceModes()))
	for _, name := range workspaceModes() {
		label := strings.ToUpper(workspaceLabel(name))
		if name == active {
			tabs = append(tabs, s.key.Render(label))
			continue
		}
		tabs = append(tabs, s.muted.Padding(0, 1).Render(label))
	}
	return strings.Join(tabs, " ")
}

func (m dashboardModel) hostListView(s dashboardStyles, width, height int) string {
	titleText := "HOSTS"
	if m.workspaceTabsVisible() {
		titleText += " · j/k move"
	} else if width == m.width && m.width < 72 {
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
		rowHeight := 2
		if m.density == "compact" {
			rowHeight = 1
		} else if m.density == "comfortable" {
			rowHeight = 3
		}
		rows := max(1, (height-6)/rowHeight)
		start := 0
		if m.cursor >= rows {
			start = m.cursor - rows + 1
		}
		end := min(len(m.filtered), start+rows)
		for position := start; position < end; position++ {
			host := m.hosts[m.filtered[position]]
			lines = append(lines, m.hostRow(s, host, width-4, position == m.cursor))
		}
		position := fmt.Sprintf("%d–%d / %d", start+1, end, len(m.filtered))
		if m.query != "" || len(m.filtered) != len(m.hosts) {
			position = fmt.Sprintf("%d–%d / %d matches · %d saved", start+1, end, len(m.filtered), len(m.hosts))
		}
		if start > 0 {
			position = "↑ " + position
		}
		if end < len(m.filtered) {
			position += " ↓"
		}
		lines = append(lines, s.muted.Render(truncateText(position, max(1, width-6))))
	}
	content := strings.Join(lines, "\n")
	panel := s.panel.BorderLeft(false).BorderTop(false).BorderBottom(false)
	panelWidth := max(1, width-1)
	if width == m.width {
		panel = panel.BorderRight(false)
		panelWidth = max(1, width)
	}
	return m.renderPanel(
		panel.Width(panelWidth).Height(max(1, height)).Padding(1, 1),
		content,
	)
}

func (m dashboardModel) hostRow(s dashboardStyles, host dashboardHost, width int, selected bool) string {
	name := host.Alias
	if name == "" {
		target, _ := parseConnectionTarget(host.Target)
		name = target.Host
	}
	status := m.statusText(s, host.Reachability)
	if m.density == "compact" {
		identity := name
		if host.Alias == "" {
			identity = host.Target
		}
		identity = truncateText(identity, max(8, width-lipgloss.Width(status)-3))
		gap := max(1, width-lipgloss.Width(identity)-lipgloss.Width(status)-2)
		row := identity + strings.Repeat(" ", gap) + status
		if selected {
			plainStatus := plainReachability(host.Reachability, m.probeTargets[host.Target])
			identity = truncateText(identity, max(8, width-lipgloss.Width(plainStatus)-3))
			gap = max(1, width-lipgloss.Width(identity)-lipgloss.Width(plainStatus)-2)
			return s.selected.Render("› " + padCell(identity+strings.Repeat(" ", gap)+plainStatus, width-2))
		}
		return s.text.Render("  " + row)
	}
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
	row := first + "\n" + second
	if m.density == "comfortable" {
		row += "\n"
	}
	return row
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
		return m.renderPanel(
			s.panel.BorderLeft(false).BorderTop(false).BorderRight(false).BorderBottom(false).
				Width(max(1, width)).Height(max(1, height)).Padding(1, 2),
			s.muted.Render("Select a host to inspect it."),
		)
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
	disks := host.MeaningfulDisks
	if len(disks) == 0 {
		lines = append(lines, s.muted.Render(valueOr(host.Disk, "Not scanned · choose Storage in Actions")))
	} else {
		diskLimit := min(len(disks), max(1, height-len(lines)-7))
		for _, disk := range disks[:diskLimit] {
			lines = append(lines, renderDashboardStorageRow(s, disk, max(1, width-6)))
		}
		if hidden := len(disks) - diskLimit; hidden > 0 {
			lines = append(lines, s.muted.Render(fmt.Sprintf("+%d more · Actions → Storage shows all volumes", hidden)))
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
	return m.renderPanel(
		s.panel.BorderLeft(false).BorderTop(false).BorderRight(false).BorderBottom(false).
			Width(max(1, width)).Height(max(1, height)).Padding(1, 2),
		content,
	)
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
	if disks := host.MeaningfulDisks; len(disks) > 0 {
		storage = fmt.Sprintf("%d storage volumes", len(disks))
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
	return m.renderPanel(
		s.panel.BorderLeft(false).BorderRight(false).BorderBottom(false).
			Width(max(1, width)).Height(max(1, height-1)).Padding(0, 1),
		content,
	)
}

func (m dashboardModel) compactView(s dashboardStyles, width, height int) string {
	return m.hostListView(s, width, height)
}

func (m dashboardModel) footerView(s dashboardStyles) string {
	cache := m.paintCache()
	hintItems := cache.footerHintsDefault
	if m.workspaceTabsVisible() {
		hintItems = cache.footerHintsTabs
	}
	hints := strings.Join(hintItems, "  ")
	if m.width < 72 {
		hints = "enter · j/k · / find · a actions · o activity"
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
	if right == "" && m.width >= 72 {
		right = "[h] all keys"
	}
	if m.width < 72 {
		content := s.muted.Render(hints)
		if right != "" {
			content = ansi.Truncate(right, max(1, m.width-4), "…")
		}
		return m.renderPanel(
			s.panel.BorderBottom(false).BorderLeft(false).BorderRight(false).
				Width(max(1, m.width-2)).Padding(0, 1),
			content,
		)
	}
	right = ansi.Truncate(right, max(1, m.width-lipgloss.Width(hints)-5), "…")
	gap := max(1, m.width-lipgloss.Width(hints)-lipgloss.Width(right)-4)
	return m.renderPanel(
		s.panel.BorderBottom(false).BorderLeft(false).BorderRight(false).
			Width(max(1, m.width-2)).Padding(0, 1),
		s.muted.Render(hints)+strings.Repeat(" ", gap)+s.focus.Render(right),
	)
}

func keyHint(s dashboardStyles, key, label string) string {
	return s.key.Render("["+key+"]") + s.muted.Render(" "+label)
}

func (m dashboardModel) overlayWidth(preferred int) int {
	return min(preferred, max(20, m.width-2))
}

func (m dashboardModel) overlayContentHeight() int {
	// A normal Lip Gloss border and one row of vertical padding consume four rows.
	return max(1, m.height-4)
}

func overlayHorizontalPadding(width int) int {
	if width < 44 {
		return 1
	}
	return 2
}

func (m dashboardModel) overlayContentWidth(width int) int {
	return max(1, width-2-(overlayHorizontalPadding(width)*2))
}

func (m dashboardModel) overlayPanel(s dashboardStyles, width int, lines []string) string {
	contentHeight := m.overlayContentHeight()
	contentWidth := m.overlayContentWidth(width)
	if len(lines) > contentHeight {
		// Individual overlays reserve their own action footer. Keep that recovery
		// path visible if an unexpected content state still exceeds the budget.
		lines = append(lines[:max(0, contentHeight-1)], lines[len(lines)-1])
	}
	for index := range lines {
		lines[index] = ansi.Truncate(lines[index], contentWidth, "…")
	}
	panel := m.renderPanel(s.panel.
		Width(max(1, width-2)).
		Padding(1, overlayHorizontalPadding(width)), strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		fitTerminalView(panel, m.width, m.height))
}

func selectionWindow(total, cursor, limit int) (int, int) {
	if total <= 0 || limit <= 0 {
		return 0, 0
	}
	limit = min(limit, total)
	start := max(0, cursor-limit+1)
	start = min(start, total-limit)
	return start, start + limit
}

func wrapTerminalText(value string, width int) []string {
	if width <= 0 || value == "" {
		return []string{value}
	}
	var lines []string
	var line strings.Builder
	for _, r := range value {
		if r == '\n' {
			lines = append(lines, line.String())
			line.Reset()
			continue
		}
		candidate := line.String() + string(r)
		if line.Len() > 0 && lipgloss.Width(candidate) > width {
			lines = append(lines, line.String())
			line.Reset()
		}
		line.WriteRune(r)
	}
	lines = append(lines, line.String())
	return lines
}

func labeledValueLines(label, value string, width int) []string {
	prefix := label + "  "
	continuation := strings.Repeat(" ", lipgloss.Width(prefix))
	valueWidth := max(1, width-lipgloss.Width(prefix))
	wrapped := wrapTerminalText(value, valueWidth)
	lines := make([]string, 0, len(wrapped))
	for index, line := range wrapped {
		if index == 0 {
			lines = append(lines, prefix+line)
		} else {
			lines = append(lines, continuation+line)
		}
	}
	return lines
}

func (m dashboardModel) commandPaletteView() string {
	s := m.styles()
	commands := m.filteredCommands()
	width := m.overlayWidth(74)
	innerWidth := m.overlayContentWidth(width)
	contentHeight := m.overlayContentHeight()
	search := "[/] filter actions  ·  [↑/↓ or j/k] move"
	if m.commandFiltering {
		search = "Filter: " + m.commandQuery + "█"
	}
	context := valueOr(displayName(m.selectedHost()), "Nexus")
	if target := m.selectedTarget(); target != "" {
		context += "  ·  " + target
	}
	lines := []string{
		s.focus.Render("ACTIONS"),
		s.muted.Render(truncateText(context, innerWidth)),
		s.text.Render(truncateText(search, innerWidth)),
	}
	details := []string{}
	if contentHeight >= 10 && len(commands) > 0 && m.commandCursor < len(commands) {
		selected := commands[m.commandCursor]
		switch selected.Action {
		case actionSSH:
			details = append(details, s.muted.Render("OpenSSH connection"))
		case actionCustom:
			policy := "runs immediately"
			if selected.Command.Confirm {
				policy = "asks for confirmation"
			}
			details = append(details,
				s.muted.Render(truncateText(policy+" · "+valueOr(selected.Command.ID, "saved command"), innerWidth)),
				s.text.Render(truncateText(selected.Command.Command, innerWidth)),
			)
		}
	}
	footer := "[enter] choose   [esc] close"
	if m.commandFiltering {
		footer = "[enter] choose   [backspace] edit   [esc] close"
	}
	if innerWidth < 40 {
		footer = "enter choose · esc close"
	}
	footer = truncateText(footer, innerWidth)
	rowBudget := max(1, contentHeight-len(lines)-len(details)-1)
	start, end := selectionWindow(len(commands), m.commandCursor, rowBudget)
	if len(commands) == 0 {
		lines = append(lines,
			s.text.Render(truncateText("No action matches “"+m.commandQuery+"”.", innerWidth)),
			s.muted.Render(truncateText("Backspace to broaden the search.", innerWidth)),
		)
	}
	for i := start; i < end; i++ {
		command := commands[i]
		rowWidth := max(8, innerWidth-2)
		labelWidth := min(20, max(8, rowWidth/3))
		descriptionWidth := max(1, rowWidth-labelWidth-1)
		description := command.Description
		if command.Action == actionCustom {
			var markers []string
			if command.Command.Interactive {
				markers = append(markers, "terminal")
			}
			if command.Command.Confirm {
				markers = append(markers, "confirm")
			}
			if len(markers) > 0 {
				description += " · " + strings.Join(markers, " · ")
			}
		}
		line := fmt.Sprintf("%-*s %s",
			labelWidth,
			truncateText(command.Label, labelWidth),
			truncateText(description, descriptionWidth),
		)
		if i == m.commandCursor {
			line = s.selected.Render("› " + padCell(line, rowWidth))
		} else {
			line = "  " + s.text.Render(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, details...)
	lines = append(lines, s.muted.Render(footer))
	return m.overlayPanel(s, width, lines)
}

func (m dashboardModel) themePreviewView() string {
	s := m.styles()
	names := themeNames()
	width := m.overlayWidth(84)
	innerWidth := m.overlayContentWidth(width)
	contentHeight := m.overlayContentHeight()
	lines := []string{
		s.focus.Render("THEMES"),
		s.muted.Render(truncateText(fmt.Sprintf("Preview live · %d palettes", len(names)), innerWidth)),
	}
	footer := "[j/k] preview   [enter] use once   [s] save default   [esc] back"
	if innerWidth < 44 {
		footer = "[enter] use  [esc] back"
	}
	var stateLine string
	if m.themeSaving {
		stateLine = s.live.Render("◌ saving theme…")
	} else if m.noticeError && strings.HasPrefix(m.notice, "Theme save failed:") {
		stateLine = s.failure.Render(truncateText(m.notice, innerWidth))
	}
	reserved := len(lines) + 1
	if stateLine != "" {
		reserved++
	}
	rowBudget := max(1, contentHeight-reserved)
	showPosition := len(names) > rowBudget
	if showPosition {
		rowBudget = max(1, rowBudget-1)
	}
	start, end := selectionWindow(len(names), m.themeCursor, rowBudget)
	for index := start; index < end; index++ {
		name := names[index]
		lines = append(lines, m.themePreviewRow(
			s,
			name,
			index == m.themeCursor,
			name == normalizeThemeName(loadedConfig.UI.Theme),
			max(1, innerWidth-2),
		))
	}
	if showPosition {
		position := fmt.Sprintf("%d–%d / %d", start+1, end, len(names))
		if start > 0 {
			position = "↑ more · " + position
		}
		if end < len(names) {
			position += " · more ↓"
		}
		lines = append(lines, s.muted.Render(truncateText(position, innerWidth)))
	}
	if stateLine != "" {
		lines = append(lines, stateLine)
	}
	lines = append(lines, s.muted.Render(truncateText(footer, innerWidth)))
	return m.overlayPanel(s, width, lines)
}

func (m dashboardModel) themePreviewRow(s dashboardStyles, name string, selected, defaultTheme bool, width int) string {
	if width < 18 {
		row := "  " + truncateText(name, max(1, width-2))
		if selected {
			row = "› " + truncateText(name, max(1, width-2))
			return s.selected.Render(padCell(row, width))
		}
		return s.text.Render(row)
	}

	t := resolvedTheme(name)
	nameWidth := min(16, max(4, width-12))
	displayName := truncateText(name, nameWidth)
	if m.plain {
		marker := "  "
		if selected {
			marker = "› "
		}
		row := marker + padCell(displayName, nameWidth) + " " + themeSwatch(t, true, false)
		remaining := max(0, width-lipgloss.Width(row))
		if remaining > 1 {
			detail := themeDescription(name)
			if defaultTheme {
				if remaining < 24 {
					detail = "default"
				} else {
					detail += " · default"
				}
			}
			row += "  " + truncateText(detail, remaining-2)
		}
		row = padCell(row, width)
		if selected {
			return s.selected.Render(row)
		}
		return s.text.Render(row)
	}

	marker := "  "
	labelStyle, detailStyle := s.text, s.muted
	if selected {
		marker = "› "
		labelStyle, detailStyle = s.selected, s.selectedMuted
	}
	left := labelStyle.Render(marker + padCell(displayName, nameWidth) + " ")
	swatch := themeSwatch(t, false, selected)
	remaining := max(0, width-lipgloss.Width(left)-lipgloss.Width(swatch))
	if remaining == 0 {
		return left + swatch
	}

	detail := themeDescription(name)
	if defaultTheme {
		if remaining < 24 {
			detail = "default"
		} else {
			detail += " · default"
		}
	}
	if remaining == 1 {
		return left + swatch + detailStyle.Render(" ")
	}
	return left + swatch + detailStyle.Render("  "+padCell(detail, remaining-2))
}

func (m dashboardModel) workspacePreviewView() string {
	s := m.styles()
	width := m.overlayWidth(76)
	innerWidth := m.overlayContentWidth(width)
	contentHeight := m.overlayContentHeight()
	lines := []string{
		s.focus.Render("CLASSIC WORKSPACE"),
		s.muted.Render(truncateText("Choose the large-terminal view used while tabs are off", innerWidth)),
	}
	descriptions := map[string]string{
		"workbench": "Hosts and focused system details",
		"console":   "Selected-host monitoring and contextual actions",
		"fleet":     "Compare cached facts and freshness across every host",
	}
	footer := "[j/k] preview   [enter] use once   [s] save   [esc] back"
	if innerWidth < 44 {
		footer = "[enter] use  [esc] back"
	}
	var stateLine string
	if m.workspaceSaving {
		stateLine = s.live.Render("◌ saving workspace…")
	} else if m.noticeError && strings.HasPrefix(m.notice, "Workspace save failed:") {
		stateLine = s.failure.Render(truncateText(m.notice, innerWidth))
	}
	reserved := len(lines) + 1
	if stateLine != "" {
		reserved++
	}
	names := workspaceModes()
	rowBudget := max(1, contentHeight-reserved)
	start, end := selectionWindow(len(names), m.workspaceCursor, rowBudget)
	for index := start; index < end; index++ {
		name := names[index]
		line := truncateText(fmt.Sprintf("%-12s %s", workspaceLabel(name), descriptions[name]), max(1, innerWidth-2))
		if index == m.workspaceCursor {
			line = s.selected.Render("› " + padCell(line, max(1, innerWidth-2)))
		} else {
			line = "  " + s.text.Render(line)
		}
		lines = append(lines, line)
	}
	if stateLine != "" {
		lines = append(lines, stateLine)
	}
	lines = append(lines, s.muted.Render(truncateText(footer, innerWidth)))
	return m.overlayPanel(s, width, lines)
}

func (m dashboardModel) settingsView() string {
	s := m.styles()
	width := m.overlayWidth(82)
	innerWidth := m.overlayContentWidth(width)
	contentHeight := m.overlayContentHeight()
	items := settingsItems()
	values := map[settingsItem]string{
		settingProfile:    strings.ToUpper(m.profile),
		settingTheme:      m.theme.Name,
		settingBackground: strings.ToUpper(loadedConfig.UI.Background),
		settingDensity:    strings.ToUpper(m.density),
		settingWorkspace:  strings.ToUpper(workspaceLabel(m.workspace)),
		settingConfig:     "OPEN IN EDITOR",
	}
	if m.experimentalTabs {
		values[settingExperimentalTabs] = "● ON · EXPERIMENTAL"
	} else {
		values[settingExperimentalTabs] = "○ OFF · EXPERIMENTAL"
	}
	if m.experimentalFleetBtop {
		values[settingExperimentalFleetBtop] = "● ON · EXPERIMENTAL"
	} else {
		values[settingExperimentalFleetBtop] = "○ OFF · EXPERIMENTAL"
	}
	details := map[settingsItem]string{
		settingProfile:               "Apply a coordinated theme, contrast, and density preset; customize anything afterward.",
		settingTheme:                 "Preview palettes live, then use once or save the default.",
		settingBackground:            "Opaque paints every cell; transparent preserves the terminal canvas.",
		settingDensity:               "Adaptive follows the terminal; Compact and Comfortable override its rhythm.",
		settingExperimentalTabs:      "Optional Hosts, Monitor, and Fleet navigation for wide terminals.",
		settingExperimentalFleetBtop: "Compressed live btop terminal for the selected Monitor host.",
		settingWorkspace:             "Choose the large-terminal view used when tabbed mode is off.",
		settingConfig:                "Exit Nexus and open the YAML configuration in your editor.",
	}

	lines := []string{
		s.focus.Render("SETTINGS"),
		s.muted.Render(truncateText("Calm defaults · optional power features", innerWidth)),
	}
	compact := contentHeight < 17
	start, end := 0, len(items)
	if compact {
		rowBudget := max(1, contentHeight-len(lines)-1)
		start, end = selectionWindow(len(items), m.settingsCursor, rowBudget)
	}
	for index := start; index < end; index++ {
		item := items[index]
		if !compact {
			switch item {
			case settingProfile:
				lines = append(lines, "", s.focus.Render("APPEARANCE"))
			case settingExperimentalTabs:
				lines = append(lines, "", s.focus.Render("NAVIGATION"))
			case settingExperimentalFleetBtop:
				lines = append(lines, "", s.focus.Render("EXPERIMENTAL"))
			case settingConfig:
				lines = append(lines, "", s.focus.Render("ADVANCED"))
			}
		}
		label := map[settingsItem]string{
			settingProfile:               "Visual profile",
			settingTheme:                 "Theme",
			settingBackground:            "Background",
			settingDensity:               "Density",
			settingExperimentalTabs:      "Workspace tabs",
			settingExperimentalFleetBtop: "Monitor btop",
			settingWorkspace:             "Classic workspace",
			settingConfig:                "Configuration file",
		}[item]
		lines = append(lines, settingsRow(s, label, values[item], index == m.settingsCursor, innerWidth))
	}
	if !compact {
		lines = append(lines, "", s.muted.Render(truncateText(details[items[m.settingsCursor]], innerWidth)))
	}
	if m.settingsSaving {
		lines = append(lines, s.live.Render("◌ saving setting…"))
	} else if m.noticeError && strings.HasPrefix(m.notice, "Setting save failed:") {
		lines = append(lines, s.failure.Render(truncateText(m.notice, innerWidth)))
	}
	footer := "[↑/↓ or j/k] move   [←/→ or h/l] change   [enter] open   [esc] back"
	if innerWidth < 32 {
		footer = "enter change · esc back"
	} else if innerWidth < 58 {
		footer = "j/k move · enter · esc back"
	}
	lines = append(lines, s.muted.Render(truncateText(footer, innerWidth)))
	return m.overlayPanel(s, width, lines)
}

func settingsRow(s dashboardStyles, label, value string, selected bool, width int) string {
	marker := "  "
	if selected {
		marker = "› "
	}
	label = truncateText(label, max(1, width-12))
	gap := max(1, width-lipgloss.Width(marker)-lipgloss.Width(label)-lipgloss.Width(value))
	row := marker + label + strings.Repeat(" ", gap) + value
	row = padCell(truncateText(row, width), width)
	if selected {
		return s.selected.Render(row)
	}
	return s.text.Render(row)
}

func (m dashboardModel) fleetView() string {
	s := m.styles()
	width := m.overlayWidth(78)
	innerWidth := m.overlayContentWidth(width)
	contentHeight := m.overlayContentHeight()
	lines := []string{
		s.focus.Render("FLEET"),
		s.muted.Render(truncateText("Saved endpoints · connection reachability", innerWidth)),
	}
	footer := "[esc] back · refresh connections from Actions"
	hostRows := max(1, contentHeight-len(lines)-1)
	limit := hostRows
	if len(m.hosts) > hostRows {
		limit = max(1, hostRows-1)
	}
	for index, host := range m.hosts {
		if index >= limit {
			break
		}
		status := m.statusText(s, host.Reachability)
		nameWidth := max(8, innerWidth-lipgloss.Width(status)-2)
		identity := host.Target
		if innerWidth >= 48 {
			identity = displayName(host) + " · " + host.Target
		}
		lines = append(lines, s.text.Render(truncateText(identity, nameWidth))+"  "+status)
	}
	if len(m.hosts) == 0 {
		lines = append(lines, s.muted.Render("No saved endpoints yet."))
	}
	if hidden := len(m.hosts) - min(len(m.hosts), limit); hidden > 0 {
		lines = append(lines, s.muted.Render(fmt.Sprintf("… and %d more", hidden)))
	}
	lines = append(lines, s.muted.Render(truncateText(footer, innerWidth)))
	return m.overlayPanel(s, width, lines)
}

func (m dashboardModel) transferView() string {
	s := m.styles()
	flow := m.transfer
	if flow == nil {
		return ""
	}
	width := m.overlayWidth(84)
	innerWidth := m.overlayContentWidth(width)
	contentHeight := m.overlayContentHeight()
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
		s.muted.Render(truncateText(flow.Host, innerWidth)),
		s.text.Render(truncateText(subtitle, innerWidth)),
	}
	if flow.Err != "" {
		lines = append(lines,
			s.failure.Render(truncateText("Scan failed: "+flow.Err, innerWidth)),
			s.muted.Render(truncateText("[r] retry   [esc] cancel", innerWidth)),
		)
	} else if strings.HasPrefix(string(flow.Stage), "scan-") {
		lines = append(lines,
			s.muted.Render("◌ Scanning paths…"),
			s.muted.Render("[esc] cancel"),
		)
	} else {
		footer := "[j/k] move   [enter] choose   [esc] cancel"
		if innerWidth < 40 {
			footer = "enter · esc cancel"
		}
		maxRows := max(1, contentHeight-len(lines)-1)
		start, end := selectionWindow(len(flow.Items), flow.Cursor, maxRows)
		if len(flow.Items) == 0 {
			lines = append(lines, s.muted.Render("No paths found."))
		}
		for index := start; index < end; index++ {
			label := transferPathLabel(flow.Items[index], flow.Stage)
			line := truncateText(label, max(1, innerWidth-2))
			if index == flow.Cursor {
				line = s.selected.Render("› " + padCell(line, max(1, innerWidth-2)))
			} else {
				line = "  " + s.text.Render(line)
			}
			lines = append(lines, line)
		}
		lines = append(lines, s.muted.Render(truncateText(footer, innerWidth)))
	}
	return m.overlayPanel(s, width, lines)
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

func (m dashboardModel) confirmReviewLines(width int) []string {
	selection := m.confirmAction
	lines := labeledValueLines("Name", valueOr(sanitizeTerminalText(selection.Command.Name), "unnamed"), width)
	lines = append(lines, labeledValueLines("Target", sanitizeTerminalText(selection.Host), width)...)
	lines = append(lines, labeledValueLines("Command", selection.Command.Command, width)...)
	warning := "Runs on the remote host with your SSH access."
	if selection.Action == actionCopyKey {
		warning = "Changes authorized_keys and may ask for your password."
	}
	lines = append(lines, wrapTerminalText(warning, width)...)
	return lines
}

func (m dashboardModel) confirmCommandView() string {
	s := m.styles()
	selection := m.confirmAction
	width := m.overlayWidth(76)
	innerWidth := m.overlayContentWidth(width)
	contentHeight := m.overlayContentHeight()
	title := "CONFIRM SAVED COMMAND"
	runLabel := "[y] run"
	if selection.Action == actionCopyKey {
		title = "CONFIRM COPY SSH KEY"
		runLabel = "[y] continue"
	}
	body := m.confirmReviewLines(innerWidth)
	visibleRows := max(1, contentHeight-2)
	scrolling := len(body) > visibleRows
	if scrolling {
		visibleRows = max(1, visibleRows-1)
	}
	maxOffset := max(0, len(body)-visibleRows)
	offset := min(m.confirmOffset, maxOffset)
	end := min(len(body), offset+visibleRows)
	lines := []string{s.warning.Render(truncateText(title, innerWidth))}
	for _, line := range body[offset:end] {
		lines = append(lines, s.text.Render(line))
	}
	if scrolling {
		lines = append(lines, s.muted.Render(fmt.Sprintf("j/k review · %d–%d / %d", offset+1, end, len(body))))
	}
	lines = append(lines, s.muted.Render(truncateText(runLabel+"   [esc] cancel", innerWidth)))
	return m.overlayPanel(s, width, lines)
}

func (m dashboardModel) commandResultView() string {
	s := m.styles()
	result := m.commandResult
	if result == nil {
		return ""
	}
	width := m.overlayWidth(88)
	innerWidth := m.overlayContentWidth(width)
	contentHeight := m.overlayContentHeight()
	lines := []string{
		s.focus.Render("COMMAND OUTPUT"),
		s.text.Render(truncateText(result.Command.Name+"  ·  "+result.Host, innerWidth)),
	}
	if m.commandRunning {
		lines = append(lines,
			s.live.Render(truncateText("◌ Running on the remote host…", innerWidth)),
			s.muted.Render("[esc] hide output"),
		)
	} else {
		status := s.success.Render("✓ Finished")
		if result.Err != "" {
			status = s.failure.Render("× " + result.Err)
		}
		lines = append(lines, status)
		output := result.Output
		if strings.TrimSpace(output) == "" {
			output = "(command completed without output)"
		}
		outputLines := strings.Split(output, "\n")
		maxRows := max(1, contentHeight-len(lines)-1)
		maxOffset := max(0, len(outputLines)-maxRows)
		offset := min(m.commandOffset, maxOffset)
		end := min(len(outputLines), offset+maxRows)
		for _, line := range outputLines[offset:end] {
			lines = append(lines, s.text.Render(truncateText(line, innerWidth)))
		}
		footer := "[j/k] scroll   [r] run again   [esc] back"
		if innerWidth < 40 {
			footer = "r again · esc back"
		}
		if len(outputLines) > maxRows {
			position := fmt.Sprintf(" · %d–%d/%d", offset+1, end, len(outputLines))
			footer = truncateText(footer, max(1, innerWidth-lipgloss.Width(position))) + position
		}
		lines = append(lines, s.muted.Render(truncateText(footer, innerWidth)))
	}
	return m.overlayPanel(s, width, lines)
}

func themeSwatch(t theme, plain, selected bool) string {
	roles := []string{t.Focus, t.Live, t.Success, t.Warning, t.Error}
	var blocks []string
	background := t.Surface
	if selected {
		background = t.Elevated
	}
	separator := lipgloss.NewStyle()
	if background != "" {
		separator = separator.Background(lipgloss.Color(background))
	} else if selected {
		separator = separator.Reverse(true)
	}
	for _, color := range roles {
		if plain {
			blocks = append(blocks, "◆")
			continue
		}
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		if background != "" {
			style = style.Background(lipgloss.Color(background))
		} else if selected {
			style = style.Reverse(true)
		}
		blocks = append(blocks, style.Render("◆"))
	}
	return strings.Join(blocks, separator.Render(" "))
}

func (m dashboardModel) helpView() string {
	s := m.styles()
	width := m.overlayWidth(76)
	innerWidth := m.overlayContentWidth(width)
	contentHeight := m.overlayContentHeight()
	if contentHeight < 19 || innerWidth < 52 {
		compact := []string{
			s.focus.Render("NEXUS KEYS"),
			truncateText("↑/↓ j/k · enter connect", innerWidth),
			truncateText("/ find · a actions · r refresh", innerWidth),
			truncateText("o activity · , settings", innerWidth),
			truncateText("h keys · q quit", innerWidth),
			truncateText("lists: arrows or j/k · enter choose", innerWidth),
			s.muted.Render(truncateText("esc closes this view", innerWidth)),
		}
		if m.experimentalTabs {
			compact = append(compact[:2], append([]string{truncateText("tab Hosts · Monitor · Fleet", innerWidth)}, compact[2:]...)...)
		}
		return m.overlayPanel(s, width, compact)
	}
	help := []string{
		s.focus.Render("NEXUS KEYS"),
		"",
		s.focus.Render("WORKSPACE"),
		"↑/↓ or j/k   move between hosts",
		"enter        connect with SSH",
		"/            find hosts",
		"a            all actions and saved commands",
		"r            refresh selected host details",
		"o            open or close Activity",
		",            open settings",
		"h            open this key reference",
		"q            quit Nexus",
		"",
		"LISTS         arrows or j/k move · enter choose · esc back",
		"MONITOR       l/right focus actions · h/left return",
		"ACTION FILTER / start · backspace edit · enter first match",
		"THEME         s save default",
		"FAILED SCAN   r retry",
		"OUTPUT        r run again",
		"CONFIRM       y run · esc cancel",
		"SSH AUTH      OpenSSH handles interactive prompts",
		"",
		"● online   ! refused   × unavailable   ◌ checking",
	}
	if m.experimentalTabs {
		help = append(help[:5], append([]string{"tab          cycle Hosts, Monitor, and Fleet"}, help[5:]...)...)
	}
	return m.overlayPanel(s, width, help)
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
	trimmed := strings.TrimSuffix(view, "\n")
	if terminalViewFits(trimmed, width, height) {
		return trimmed
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for index, line := range lines {
		lines[index] = ansi.Truncate(line, width, "")
	}
	return strings.Join(lines, "\n")
}

// terminalViewFits reports whether fitTerminalView(value, width, height)
// would return value unchanged: every line already at most width cells wide,
// and at most height lines. It walks value once with no allocation, instead
// of paying for the strings.Split slice, the per-line ansi.Truncate calls
// (each of which starts by checking the exact same thing), and the
// strings.Join the slow path needs -- all wasted work when the content
// already fits, which panels in this UI are built to do most of the time.
func terminalViewFits(value string, width, height int) bool {
	lineCount := 1
	start := 0
	for i := 0; i < len(value); i++ {
		if value[i] == '\n' {
			if terminalWidth(value[start:i]) > width {
				return false
			}
			lineCount++
			if lineCount > height {
				return false
			}
			start = i + 1
		}
	}
	return terminalWidth(value[start:]) <= width
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
	// host.Alias and host.Target never change after a dashboardHost is
	// constructed (see newDashboardModelWithState), so DisplayName -- set
	// once there -- is always correct when present. Hosts built without it
	// (only dashboardHost{} zero values today) fall back to computing it on
	// the spot, matching the previous unconditional behavior.
	if host.DisplayName != "" {
		return host.DisplayName
	}
	return resolveDisplayName(host.Alias, host.Target)
}

func resolveDisplayName(alias, target string) string {
	if alias != "" {
		return alias
	}
	spec, err := parseConnectionTarget(target)
	if err == nil {
		return spec.Host
	}
	return target
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
	return value + strings.Repeat(" ", max(0, width-terminalWidth(value)))
}

// truncateText sanitizes value (stripping control/escape bytes and
// collapsing whitespace, same as sanitizeTerminalText) and truncates it to
// at most width terminal cells, appending an ellipsis when it had to cut.
//
// The general (non-ASCII) branch below deliberately does not call
// ansi.Truncate: that function starts by computing ansi.StringWidth(s) to
// decide whether to truncate at all, which we have already just done one
// line above it (terminalGraphemeCut's total). Calling it anyway would
// re-walk every grapheme cluster in value a second time; instead
// terminalGraphemeCut finds the cut point in the same pass that computes
// the width, matching ansi.Truncate's output exactly (verified by
// TestTruncateTextMatchesReference) for one pass instead of two-plus.
func truncateText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if needsTerminalSanitize(value) {
		value = sanitizeTerminalText(value)
	}
	if w, ok := asciiPrintableWidth(value); ok {
		if w <= width {
			return value
		}
		if width == 1 {
			return "…"
		}
		return value[:width-1] + "…"
	}
	total, cutOffset, hasCut := terminalGraphemeCut(value, width-1)
	if total <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	if !hasCut {
		// Defensive only: total > width > width-1 always yields a cut in
		// terminalGraphemeCut, so this branch should be unreachable.
		return value + "…"
	}
	return value[:cutOffset] + "…"
}

// terminalGraphemeCut walks value's grapheme clusters -- value must already
// be free of ANSI escapes and control bytes, which is guaranteed by the
// needsTerminalSanitize/sanitizeTerminalText step above -- and returns its
// total cell width plus, if some prefix exceeds limit cells, the byte
// offset of the first cluster whose addition pushes the running width past
// limit (mirroring the cut point github.com/charmbracelet/x/ansi.Truncate
// computes internally).
func terminalGraphemeCut(value string, limit int) (total, cutOffset int, hasCut bool) {
	offset := 0
	for offset < len(value) {
		cluster, w := ansi.FirstGraphemeCluster(value[offset:], ansi.GraphemeWidth)
		if len(cluster) == 0 {
			break
		}
		if !hasCut && total+w > limit {
			cutOffset = offset
			hasCut = true
		}
		total += w
		offset += len(cluster)
	}
	return total, cutOffset, hasCut
}

// needsTerminalSanitize reports whether sanitizeTerminalText(value) would
// change value, so truncateText can skip it (and the strings.Builder +
// strings.Fields/Join allocations it does unconditionally) on the very
// common case of already-clean, single-spaced plain text. It must mirror
// sanitizeTerminalText exactly: any control/escape byte forces the slow
// path, and so does any leading/trailing whitespace or run of two or more
// whitespace runes (strings.Fields collapses those). A rune that is
// unicode-whitespace but not a plain ' ' is rare enough in practice (no
// hostnames, labels, or command names in this app use one) that we simply
// fall back to the real sanitizer for it rather than modeling it here.
func needsTerminalSanitize(value string) bool {
	if value == "" {
		return false
	}
	prevSpace := true // start-of-string counts as "just saw a space" to catch leading whitespace
	for _, r := range value {
		if r == '\n' || r == '\r' || r == '\t' || r == 0x1b || r < 0x20 || r == 0x7f {
			return true
		}
		if r == ' ' {
			if prevSpace {
				return true
			}
			prevSpace = true
			continue
		}
		if unicode.IsSpace(r) {
			return true
		}
		prevSpace = false
	}
	return prevSpace // trailing whitespace
}

// terminalWidth returns the terminal cell width of value, identical to
// ansi.StringWidth/lipgloss.Width. Pure printable ASCII -- the overwhelming
// majority of strings this package measures (hostnames, labels, numbers) --
// is fast-pathed to its byte length, skipping ansi.StringWidth's
// grapheme-cluster iteration (the single hottest call in this package's CPU
// profile) entirely.
func terminalWidth(value string) int {
	if width, ok := asciiPrintableWidth(value); ok {
		return width
	}
	if width, ok := simpleTerminalWidth(value); ok {
		return width
	}
	return ansi.StringWidth(value)
}

// simpleTerminalWidth measures strings made only of CSI/OSC escape sequences
// and runes that ansi.StringWidth is known to count as exactly one cell with
// no grapheme clustering (see simpleRuneWidthOne and
// TestSimpleTerminalWidthMatchesAnsi, which checks every rune in the ranges).
// That covers the live btop frame -- braille graphs, box drawing, blocks,
// arrows, ASCII -- whose grapheme-cluster measurement dominated the CPU
// profile. Anything else (CJK, emoji, combining marks, control characters)
// reports ok=false so the caller falls back to the full implementation.
func simpleTerminalWidth(value string) (int, bool) {
	width := 0
	for i := 0; i < len(value); {
		c := value[i]
		switch {
		case c == 0x1b:
			end, ok := simpleEscapeEnd(value, i)
			if !ok {
				return 0, false
			}
			i = end + 1
			continue
		case c < 0x20 || c == 0x7f:
			return 0, false
		case c < 0x80:
			width++
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(value[i:])
		if r == utf8.RuneError || !simpleRuneWidthOne(r) {
			return 0, false
		}
		width++
		i += size
	}
	return width, true
}

// simpleEscapeEnd returns the index of the final byte of a well-formed CSI
// (ESC [ params final) or OSC (ESC ] ... BEL | ESC \) sequence at start.
func simpleEscapeEnd(value string, start int) (int, bool) {
	if start+1 >= len(value) {
		return 0, false
	}
	switch value[start+1] {
	case '[':
		end := start + 2
		for end < len(value) && value[end] >= 0x20 && value[end] <= 0x3f {
			end++
		}
		if end < len(value) && value[end] >= 0x40 && value[end] <= 0x7e {
			return end, true
		}
		return 0, false
	case ']':
		for end := start + 2; end < len(value); end++ {
			if value[end] == 0x07 {
				return end, true
			}
			if value[end] == 0x1b && end+1 < len(value) && value[end+1] == '\\' {
				return end + 1, true
			}
		}
		return 0, false
	}
	return 0, false
}

// simpleRuneWidthOne reports runes that ansi.StringWidth measures as one
// cell and never merges into a wider grapheme cluster. Keep in sync with
// TestSimpleTerminalWidthMatchesAnsi, which verifies every listed rune.
func simpleRuneWidthOne(r rune) bool {
	switch {
	case r == 0x00ad: // soft hyphen is zero width
		return false
	case r >= 0x00a0 && r <= 0x017f: // Latin-1 supplement, Latin extended-A
		return true
	case r >= 0x2010 && r <= 0x2027: // dashes, quotes, bullets, ellipsis
		return true
	case r >= 0x2070 && r <= 0x209f: // superscripts and subscripts
		return true
	case r >= 0x2190 && r <= 0x22ff: // arrows, mathematical operators
		return true
	case r >= 0x2500 && r <= 0x259f: // box drawing, block elements
		return true
	case r >= 0x25a0 && r <= 0x25fc: // geometric shapes (25FD/25FE are wide)
		return true
	case r == 0x25ff:
		return true
	case r >= 0x2800 && r <= 0x28ff: // braille patterns
		return true
	}
	return false
}

func asciiPrintableWidth(value string) (int, bool) {
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return 0, false
		}
	}
	return len(value), true
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
			model.closeBtopStreams()
			model.closeFleetTelemetry()
			return fmt.Errorf("dashboard failed: %w", err)
		}
		finalModel, ok := result.(dashboardModel)
		if !ok || finalModel.choice.Action == "" {
			if ok {
				finalModel.closeBtopStreams()
				finalModel.closeFleetTelemetry()
			} else {
				model.closeBtopStreams()
				model.closeFleetTelemetry()
			}
			return nil
		}
		finalModel.closeBtopStreams()
		finalModel.closeFleetTelemetry()
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
	case actionSettings:
		return "Settings"
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
	value = stripTerminalSequences(value)
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	for index := range lines {
		var clean strings.Builder
		for _, r := range lines[index] {
			switch {
			case r == '\t':
				clean.WriteString("    ")
			case r < 0x20 || r == 0x7f:
				clean.WriteRune(' ')
			default:
				clean.WriteRune(r)
			}
		}
		lines[index] = strings.TrimRight(clean.String(), " ")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

func (a *app) newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run [command] [user@host[:port]]",
		Short: "Run a configured remote command",
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
			if selected.Confirm {
				if err := confirmRemoteCommand(target, selected); err != nil {
					if errors.Is(err, errCancelled) {
						return nil
					}
					return err
				}
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
