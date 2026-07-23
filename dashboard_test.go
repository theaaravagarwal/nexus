package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func chooseDashboardAction(t *testing.T, model dashboardModel, query string) (dashboardModel, tea.Cmd) {
	t.Helper()
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = updated.(dashboardModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = updated.(dashboardModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(query)})
	model = updated.(dashboardModel)
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return updated.(dashboardModel), cmd
}

func TestDashboardFiltersAndChoosesAction(t *testing.T) {
	model := newDashboardModel([]string{"alice@one", "bob@two:2222"})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = updated.(dashboardModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("two")})
	model = updated.(dashboardModel)
	if len(model.filtered) != 1 || model.selectedTarget() != "bob@two:2222" {
		t.Fatalf("filtered=%v", model.filtered)
	}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(dashboardModel)
	if cmd == nil || model.choice.Action != actionSSH || model.choice.Host != "bob@two:2222" {
		t.Fatalf("choice=%#v cmd=%v", model.choice, cmd)
	}
}

func TestDashboardActionListDispatchesExpectedCommands(t *testing.T) {
	tests := []struct {
		query  string
		action dashboardAction
	}{
		{"network", actionNet},
		{"storage", actionStorage},
	}
	for _, tc := range tests {
		model := newDashboardModel([]string{"alice@one:2222"})
		got, cmd := chooseDashboardAction(t, model, tc.query)
		if cmd == nil || got.choice.Action != tc.action || got.choice.Host != "alice@one:2222" {
			t.Fatalf("query=%q choice=%#v cmd=%v", tc.query, got.choice, cmd)
		}
	}
}

func TestDashboardTransferDiscoveryStaysInsideTUI(t *testing.T) {
	pull := newDashboardModel([]string{"alice@one:2222"})
	pull, cmd := chooseDashboardAction(t, pull, "pull files")
	if cmd == nil || pull.transfer == nil || pull.transfer.Stage != transferScanRemoteSource || pull.done {
		t.Fatalf("pull scan escaped TUI: cmd=%v model=%#v", cmd, pull)
	}
	updated, _ := pull.Update(transferScanMsg{Stage: transferScanRemoteSource, Items: []string{"projects/", "notes.txt"}})
	pull = updated.(dashboardModel)
	if pull.transfer.Stage != transferPickRemoteSource || !strings.Contains(pull.View(), "Choose what to download") {
		t.Fatalf("pull picker unavailable: %#v\n%s", pull.transfer, pull.View())
	}
	updated, cmd = pull.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pull = updated.(dashboardModel)
	if cmd == nil || pull.choice.Action != actionPull || len(pull.choice.Args) != 3 || pull.choice.Args[1] != "projects/" {
		t.Fatalf("pull selection=%#v cmd=%v", pull.choice, cmd)
	}

	push := newDashboardModel([]string{"alice@one:2222"})
	push, cmd = chooseDashboardAction(t, push, "push files")
	if cmd == nil || push.transfer == nil || push.transfer.Stage != transferScanLocalSource || push.done {
		t.Fatalf("push local scan escaped TUI: cmd=%v model=%#v", cmd, push)
	}
	updated, _ = push.Update(transferScanMsg{Stage: transferScanLocalSource, Items: []string{"/tmp/payload"}})
	push = updated.(dashboardModel)
	updated, cmd = push.Update(tea.KeyMsg{Type: tea.KeyEnter})
	push = updated.(dashboardModel)
	if cmd == nil || push.transfer.Stage != transferScanRemoteDest || push.transfer.LocalPath != "/tmp/payload" {
		t.Fatalf("push remote scan did not start: cmd=%v flow=%#v", cmd, push.transfer)
	}
	updated, _ = push.Update(transferScanMsg{Stage: transferScanRemoteDest, Items: []string{"uploads/"}})
	push = updated.(dashboardModel)
	updated, cmd = push.Update(tea.KeyMsg{Type: tea.KeyEnter})
	push = updated.(dashboardModel)
	if cmd == nil || push.choice.Action != actionPush || len(push.choice.Args) != 3 || push.choice.Args[2] != "uploads/" {
		t.Fatalf("push selection=%#v cmd=%v", push.choice, cmd)
	}
}

func TestDashboardSystemRefreshStaysInsideTUI(t *testing.T) {
	model := newDashboardModel([]string{"alice@one:2222"})
	got, cmd := chooseDashboardAction(t, model, "refresh snapshot")
	if cmd == nil || got.done || got.choice.Action != "" || !got.metadataBusy["alice@one:2222"] {
		t.Fatalf("refresh escaped TUI: choice=%#v done=%v busy=%v cmd=%v", got.choice, got.done, got.metadataBusy, cmd)
	}
}

func TestDashboardProbeResultsStreamPerHost(t *testing.T) {
	previous := loadedConfig.Reachability.Concurrency
	loadedConfig.Reachability.Concurrency = 1
	t.Cleanup(func() { loadedConfig.Reachability.Concurrency = previous })

	model := newDashboardModel([]string{"alice@one", "bob@two"})
	first := probeTargetMsg(reachabilityResult{Target: "alice@one", Status: reachOnline, Latency: 8 * time.Millisecond})
	updated, cmd := model.Update(first)
	model = updated.(dashboardModel)
	if !model.probing || model.probeComplete != 1 || model.hosts[0].Reachability.Status != reachOnline || cmd == nil {
		t.Fatalf("first streamed result=%#v cmd=%v", model, cmd)
	}
	second := probeTargetMsg(reachabilityResult{Target: "bob@two", Status: reachTimeout})
	updated, cmd = model.Update(second)
	model = updated.(dashboardModel)
	if model.probing || model.probeComplete != 2 || model.hosts[1].Reachability.Status != reachTimeout || cmd == nil {
		t.Fatalf("final streamed result=%#v cmd=%v", model, cmd)
	}
}

func TestDashboardConfigIsDiscoverableFromActions(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	model, cmd := chooseDashboardAction(t, model, "edit config")
	if cmd == nil || !model.done || model.choice.Action != actionConfig {
		t.Fatalf("saved-command shortcut did not open config: cmd=%v model=%#v", cmd, model)
	}
}

func TestDashboardSavedCommandConfirmationStaysInContext(t *testing.T) {
	previous := loadedConfig
	loadedConfig = defaultAppConfig()
	loadedConfig.Commands = []commandConfig{{Name: "uptime", Description: "Show uptime", Command: "uptime"}}
	t.Cleanup(func() { loadedConfig = previous })

	model := newDashboardModel([]string{"alice@one"})
	model.commandOpen = true
	model.commandQuery = "uptime"
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(dashboardModel)
	if cmd != nil || !model.confirmOpen || model.done {
		t.Fatalf("confirmation escaped TUI: cmd=%v model=%#v", cmd, model)
	}
	for _, want := range []string{"CONFIRM SAVED COMMAND", "alice@one", "uptime", "[y] run"} {
		if !strings.Contains(model.View(), want) {
			t.Fatalf("confirmation missing %q:\n%s", want, model.View())
		}
	}
	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(dashboardModel)
	if cmd != nil || model.confirmOpen || model.done {
		t.Fatalf("confirmation did not cancel in place: cmd=%v model=%#v", cmd, model)
	}
}

func TestDashboardNavigationAndCommandFilter(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = updated.(dashboardModel)
	if !model.commandOpen {
		t.Fatal("command key did not open palette")
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = updated.(dashboardModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("storage")})
	model = updated.(dashboardModel)
	commands := model.filteredCommands()
	if len(commands) != 1 || commands[0].Action != actionStorage {
		t.Fatalf("filtered commands=%#v", commands)
	}
}

func TestDashboardUsesOneCanonicalWorkspaceKeyMap(t *testing.T) {
	model := newDashboardModel([]string{"alice@one", "bob@two"})
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'?'}},
		{Type: tea.KeyRunes, Runes: []rune{'c'}},
		{Type: tea.KeyCtrlK},
		{Type: tea.KeyRunes, Runes: []rune{'p'}},
		{Type: tea.KeyRunes, Runes: []rune{'u'}},
		{Type: tea.KeyRunes, Runes: []rune{'t'}},
		{Type: tea.KeyRunes, Runes: []rune{'T'}},
		{Type: tea.KeyRunes, Runes: []rune{'i'}},
		{Type: tea.KeyRunes, Runes: []rune{'e'}},
		{Type: tea.KeyDown},
		{Type: tea.KeyUp},
	} {
		updated, cmd := model.Update(key)
		model = updated.(dashboardModel)
		if cmd != nil || model.done || model.commandOpen || model.helpOpen || model.transfer != nil ||
			model.themeOpen || model.showTopology || model.cursor != 0 {
			t.Fatalf("legacy key %q still changes the workspace: cmd=%v model=%#v", key.String(), cmd, model)
		}
	}

	for _, want := range []string{
		"j / k        move between hosts",
		"a            all actions and saved commands",
		"LISTS         j/k move · enter choose · esc back",
		"THEME         s save default",
		"OUTPUT        r run again",
		"CONFIRM       y run · esc cancel",
	} {
		model.helpOpen = true
		if !strings.Contains(model.View(), want) {
			t.Fatalf("key reference missing %q:\n%s", want, model.View())
		}
	}
}

func TestDashboardActionListExposesEveryOperation(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	actions := map[dashboardAction]bool{}
	for _, command := range model.availableCommands() {
		actions[command.Action] = true
	}
	for _, action := range []dashboardAction{
		actionSSH, actionPull, actionPush, actionInfo, actionProbe, actionProbeAll,
		actionTop, actionNet, actionStorage, actionFleet, actionThemes, actionConfig,
	} {
		if !actions[action] {
			t.Fatalf("action %q is not discoverable from Actions", action)
		}
	}
}

func TestDashboardConnectionChecksRunFromActions(t *testing.T) {
	model := newDashboardModel([]string{"alice@one", "bob@two"})
	model.probing = false
	model.probeInitial = nil
	model.probeTargets = map[string]bool{}
	model, cmd := chooseDashboardAction(t, model, "check connection")
	if cmd == nil || !model.probing || model.probeTotal != 1 {
		t.Fatalf("selected connection check did not start: cmd=%v model=%#v", cmd, model)
	}

	model.probing = false
	model.probeInitial = nil
	model.probeTargets = map[string]bool{}
	model, cmd = chooseDashboardAction(t, model, "check every")
	if cmd == nil || !model.probing || model.probeTotal != 2 {
		t.Fatalf("all connection check did not start: cmd=%v model=%#v", cmd, model)
	}
}

func TestDashboardSavedCommandOutputStaysInsideNexus(t *testing.T) {
	binDir := t.TempDir()
	sshPath := filepath.Join(binDir, "ssh")
	script := "#!/bin/sh\nprintf '\\033[31mdev: 1 windows\\033[0m\\nwork: 2 windows\\n'\n"
	if err := os.WriteFile(sshPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	model := newDashboardModel([]string{"alice@one"})
	model.confirmOpen = true
	model.confirmAction = dashboardSelection{
		Action: actionCustom,
		Host:   "alice@one",
		Command: commandConfig{
			Name: "tmux list", Command: "tmux ls",
		},
	}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = updated.(dashboardModel)
	if cmd == nil || model.done || !model.commandRunning || model.commandResult == nil {
		t.Fatalf("saved command left Nexus: cmd=%v model=%#v", cmd, model)
	}
	updated, _ = model.Update(cmd())
	model = updated.(dashboardModel)
	view := model.View()
	for _, want := range []string{"COMMAND OUTPUT", "tmux list", "dev: 1 windows", "work: 2 windows", "[r] run again"} {
		if !strings.Contains(view, want) {
			t.Fatalf("command result missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "\x1b[31m") {
		t.Fatalf("remote terminal control sequence reached the TUI:\n%q", view)
	}
	if strings.Contains(view, "[31m") || strings.Contains(view, "[0m") {
		t.Fatalf("remote ANSI fragments reached the TUI:\n%q", view)
	}
}

func TestDashboardInteractiveSavedCommandTemporarilyOwnsTerminal(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	model.confirmOpen = true
	model.confirmAction = dashboardSelection{
		Action: actionCustom,
		Host:   "alice@one",
		Command: commandConfig{
			Name: "tmux attach", Command: "tmux attach", Interactive: true,
		},
	}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = updated.(dashboardModel)
	if cmd == nil || !model.done || model.choice.Action != actionCustom || model.commandResult != nil {
		t.Fatalf("interactive saved command did not hand off the terminal: cmd=%v model=%#v", cmd, model)
	}
}

func TestDashboardThemePreviewStaysInsideTUI(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	model, cmd := chooseDashboardAction(t, model, "theme")
	if cmd != nil || !model.themeOpen {
		t.Fatalf("theme preview did not open in TUI: cmd=%v model=%#v", cmd, model)
	}
	before := model.theme.Name
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model = updated.(dashboardModel)
	if model.theme.Name == before {
		t.Fatal("theme preview did not advance")
	}
	view := model.View()
	for _, want := range []string{"THEMES", "use once", "save default"} {
		if !strings.Contains(view, want) {
			t.Fatalf("theme preview missing %q:\n%s", want, view)
		}
	}
}

func TestDashboardThemeShortcutSavesDefaultInsideTUI(t *testing.T) {
	previous := loadedConfig
	t.Cleanup(func() { loadedConfig = previous })
	loadedConfig = defaultAppConfig()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(defaultConfigYAML()), 0o600); err != nil {
		t.Fatal(err)
	}
	model := newDashboardModel([]string{"alice@one"})
	model.configPath = configPath
	model, cmd := chooseDashboardAction(t, model, "theme")
	if cmd != nil || !model.themeOpen {
		t.Fatalf("theme action did not open themes: cmd=%v model=%#v", cmd, model)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model = updated.(dashboardModel)
	selected := model.theme.Name
	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	model = updated.(dashboardModel)
	if cmd == nil || !model.themeSaving {
		t.Fatalf("theme save did not start: cmd=%v model=%#v", cmd, model)
	}
	message := cmd()
	updated, cmd = model.Update(message)
	model = updated.(dashboardModel)
	if cmd != nil || model.themeOpen || model.themeSaving || model.noticeError ||
		loadedConfig.UI.Theme != selected || !strings.Contains(model.notice, selected) {
		t.Fatalf("theme save did not finish inside TUI: cmd=%v model=%#v config=%#v", cmd, model, loadedConfig.UI)
	}
	cfg, err := loadAppConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Theme != selected {
		t.Fatalf("saved theme=%q want=%q", cfg.UI.Theme, selected)
	}
}

func TestDashboardThemeSaveFailureIsVisible(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	model.openThemePreview()
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	model = updated.(dashboardModel)
	if cmd == nil {
		t.Fatal("theme save did not start")
	}
	updated, _ = model.Update(cmd())
	model = updated.(dashboardModel)
	if !model.themeOpen || !model.noticeError || !strings.Contains(model.View(), "Theme save failed:") {
		t.Fatalf("theme save failure is not recoverable in context: %#v\n%s", model, model.View())
	}
}

func TestDashboardStartsInCheckingState(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	if !model.probing || !strings.Contains(model.View(), "checking") {
		t.Fatalf("initial probe state is not visible:\n%s", model.View())
	}
}

func TestDashboardTopologyToggleChangesWideWorkspace(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	model.width, model.height = 120, 30
	if view := model.View(); strings.Contains(view, "FLEET") {
		t.Fatalf("fleet should use progressive disclosure:\n%s", view)
	}
	model, _ = chooseDashboardAction(t, model, "fleet")
	view := model.View()
	if !strings.Contains(view, "FLEET") || !strings.Contains(view, "Saved endpoints") {
		t.Fatalf("fleet overlay did not open:\n%s", view)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(dashboardModel)
	if strings.Contains(model.View(), "Saved endpoints ·") {
		t.Fatal("fleet overlay did not close")
	}
}

func TestDashboardViewsRemainInformativeAcrossWidths(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	state := nexusState{Hosts: map[string]hostActivity{
		"alice@one:2222": {Score: 3, LastUsed: now.Add(-time.Hour), OS: "Ubuntu 24.04", Tools: []string{"btop", "duf"}},
	}}
	model := newDashboardModelWithState([]string{"alice@one:2222", "bob@two"}, state, now)
	model.hosts[0].Reachability = reachabilityResult{Target: "alice@one:2222", Status: reachOnline, Latency: 24 * time.Millisecond}
	for _, width := range []int{40, 78, 120} {
		model.width = width
		model.height = 30
		view := model.View()
		for _, want := range []string{"NEXUS", "alice@one:2222", "connect"} {
			if !strings.Contains(view, want) {
				t.Fatalf("width=%d missing %q:\n%s", width, want, view)
			}
		}
		if width == 120 {
			for _, want := range []string{"SYSTEM", "SAVED COMMANDS"} {
				if !strings.Contains(view, want) {
					t.Fatalf("wide layout missing %q:\n%s", want, view)
				}
			}
			for _, unwanted := range []string{"FLEET CONSTELLATION", "QUICK ACTIONS", "STATUS"} {
				if strings.Contains(view, unwanted) {
					t.Fatalf("wide layout retained clutter %q:\n%s", unwanted, view)
				}
			}
		}
	}
}

func TestDashboardRenderStaysWithinTerminalBounds(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	state := nexusState{Hosts: map[string]hostActivity{
		"alice@one:2222": {
			Score: 3, LastUsed: now.Add(-time.Hour), OS: "Ubuntu 24.04",
			CPU: "Example CPU", Memory: "32 GB", Disk: "20 / 100 GB", Tools: []string{"btop", "duf"},
		},
	}}
	for _, plain := range []bool{true, false} {
		for _, size := range [][2]int{{20, 6}, {40, 12}, {71, 20}, {72, 20}, {109, 24}, {110, 24}, {120, 25}, {120, 32}} {
			model := newDashboardModelWithState([]string{"alice@one:2222", "bob@two"}, state, now)
			model.plain = plain
			model.width, model.height = size[0], size[1]
			view := model.View()
			lines := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
			if len(lines) > size[1] {
				t.Fatalf("plain=%v size=%v height=%d\n%s", plain, size, len(lines), view)
			}
			for index, line := range lines {
				if width := lipgloss.Width(line); width > size[0] {
					t.Fatalf("plain=%v size=%v line=%d width=%d\n%s", plain, size, index, width, view)
				}
			}
		}
	}
}

func TestDashboardOverlaysStayWithinTerminalBounds(t *testing.T) {
	for _, size := range [][2]int{{40, 16}, {72, 20}, {120, 32}} {
		for _, overlay := range []string{"help", "commands", "themes", "confirm", "output", "fleet", "transfer"} {
			model := newDashboardModel([]string{"alice@one"})
			model.width, model.height = size[0], size[1]
			switch overlay {
			case "help":
				model.helpOpen = true
			case "commands":
				model.commandOpen = true
			case "themes":
				model.openThemePreview()
			case "confirm":
				model.confirmOpen = true
				model.confirmAction = dashboardSelection{
					Action: actionCustom, Host: "alice@one",
					Command: commandConfig{Name: "uptime", Command: "uptime"},
				}
			case "output":
				model.commandResult = &configuredCommandResult{
					Host: "alice@one",
					Command: commandConfig{
						Name: "tmux list", Command: "tmux ls",
					},
					Output: "dev: 1 windows\nwork: 2 windows",
				}
			case "fleet":
				model.showTopology = true
			case "transfer":
				model.transfer = &transferFlow{
					Action: actionPull, Stage: transferPickRemoteSource,
					Host: "alice@one", Items: []string{"projects/", "notes.txt"},
				}
			}
			assertTerminalBounds(t, model.View(), size[0], size[1], overlay)
		}
	}
}

func assertTerminalBounds(t *testing.T, view string, width, height int, label string) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
	if len(lines) > height {
		t.Fatalf("%s size=%dx%d height=%d\n%s", label, width, height, len(lines), view)
	}
	for index, line := range lines {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("%s size=%dx%d line=%d width=%d\n%s", label, width, height, index, got, view)
		}
	}
}

func TestDashboardEmptyDoesNotChoose(t *testing.T) {
	model := newDashboardModel(nil)
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(dashboardModel)
	if cmd != nil || got.choice.Action != "" {
		t.Fatalf("empty dashboard chose %#v", got.choice)
	}
}

func TestDashboardInteractionStateMatrix(t *testing.T) {
	model := newDashboardModel([]string{"alice@one", "bob@two", "carol@three"})

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = updated.(dashboardModel)
	if model.width != 100 || model.height != 30 {
		t.Fatalf("window size not applied: %dx%d", model.width, model.height)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	model = updated.(dashboardModel)
	if !model.helpOpen {
		t.Fatal("help did not open")
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(dashboardModel)
	if model.helpOpen {
		t.Fatal("help did not close")
	}

	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'j'}},
		{Type: tea.KeyRunes, Runes: []rune{'k'}},
	} {
		updated, _ = model.Update(key)
		model = updated.(dashboardModel)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = updated.(dashboardModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bob")})
	model = updated.(dashboardModel)
	if len(model.filtered) != 1 {
		t.Fatalf("search did not filter: %v", model.filtered)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	model = updated.(dashboardModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(dashboardModel)
	if model.filtering || model.query != "" || len(model.filtered) != 3 {
		t.Fatalf("search cancellation did not reset: %#v", model)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = updated.(dashboardModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = updated.(dashboardModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("net")})
	model = updated.(dashboardModel)
	if !model.commandOpen || len(model.filteredCommands()) == 0 {
		t.Fatal("command palette filtering failed")
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyDown}, {Type: tea.KeyUp}, {Type: tea.KeyBackspace}, {Type: tea.KeyEsc}} {
		updated, _ = model.Update(key)
		model = updated.(dashboardModel)
	}
	if model.commandOpen || model.commandQuery != "" {
		t.Fatal("command palette did not close cleanly")
	}

	model.openThemePreview()
	original := model.theme.Name
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model = updated.(dashboardModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(dashboardModel)
	if model.themeOpen || model.theme.Name != original {
		t.Fatalf("theme cancellation did not restore %q: %#v", original, model.theme)
	}

	var cmd tea.Cmd
	for _, result := range []reachabilityResult{
		{Target: "alice@one", Status: reachOnline, Latency: time.Millisecond},
		{Target: "bob@two", Status: reachRefused},
		{Target: "carol@three", Status: reachTimeout},
	} {
		updated, cmd = model.Update(probeTargetMsg(result))
		model = updated.(dashboardModel)
	}
	if model.probing || cmd == nil || model.hosts[0].Reachability.Status != reachOnline || model.hosts[1].Reachability.Status != reachRefused {
		t.Fatalf("streamed probes were not applied: %#v cmd=%v", model.hosts, cmd)
	}
	updated, cmd = model.Update(probeTickMsg{})
	model = updated.(dashboardModel)
	if !model.probing || cmd == nil {
		t.Fatalf("probe refresh did not start: probing=%v cmd=%v", model.probing, cmd)
	}

	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = updated.(dashboardModel)
	if !model.done || cmd == nil {
		t.Fatalf("ctrl+c did not quit: done=%v cmd=%v", model.done, cmd)
	}
}
