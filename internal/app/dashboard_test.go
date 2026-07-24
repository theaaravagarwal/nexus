package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	if cmd == nil || !model.terminalRunning || model.done || model.operation == nil ||
		model.operation.Host != "bob@two:2222" || model.choice.Action != "" {
		t.Fatalf("SSH handoff=%#v cmd=%v", model, cmd)
	}
}

func TestDashboardActionListDispatchesExpectedCommands(t *testing.T) {
	tests := []struct {
		query  string
		action dashboardAction
	}{
		{"network", actionNet},
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
	pull, cmd := chooseDashboardAction(t, pull, "pull")
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
	push, cmd = chooseDashboardAction(t, push, "push")
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
	got, cmd := chooseDashboardAction(t, model, "refresh info")
	if cmd == nil || got.done || got.choice.Action != "" || !got.metadataBusy["alice@one:2222"] {
		t.Fatalf("refresh escaped TUI: choice=%#v done=%v busy=%v cmd=%v", got.choice, got.done, got.metadataBusy, cmd)
	}
}

func TestDashboardStorageOutputStaysInsideTUI(t *testing.T) {
	binDir := t.TempDir()
	sshPath := filepath.Join(binDir, "ssh")
	script := "#!/bin/sh\nprintf 'DISK=/dev/root\\t/\\t500000000000\\t500000000000\\t1000000000000\\nDISK=/dev/data\\t/data\\t1000000000000\\t1000000000000\\t2000000000000\\n'\n"
	if err := os.WriteFile(sshPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	model := newDashboardModel([]string{"alice@one:2222"})
	model, cmd := chooseDashboardAction(t, model, "storage")
	if cmd == nil || model.done || !model.commandRunning || model.commandResult == nil {
		t.Fatalf("storage escaped TUI: cmd=%v model=%#v", cmd, model)
	}
	updated, _ := model.Update(cmd())
	model = updated.(dashboardModel)
	view := ansiCSI.ReplaceAllString(model.View(), "")
	for _, want := range []string{
		"COMMAND OUTPUT", "Storage", "root @ /", "data @ /data", "500 GB / 1 TB", "1 TB / 2 TB", "█████░░░░░ 50%",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("storage result missing %q:\n%s", want, view)
		}
	}
}

func TestDashboardStorageFailureStaysRecoverableInsideTUI(t *testing.T) {
	binDir := t.TempDir()
	sshPath := filepath.Join(binDir, "ssh")
	if err := os.WriteFile(sshPath, []byte("#!/bin/sh\nprintf 'permission denied\\n' >&2\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	model := newDashboardModel([]string{"alice@one"})
	model, cmd := chooseDashboardAction(t, model, "storage")
	updated, _ := model.Update(cmd())
	model = updated.(dashboardModel)
	view := ansiCSI.ReplaceAllString(model.View(), "")
	if model.done || model.commandRunning || !strings.Contains(view, "storage scan failed") ||
		!strings.Contains(view, "permission denied") || !strings.Contains(view, "[r] run again") {
		t.Fatalf("storage failure was not recoverable in TUI:\n%s", view)
	}
}

func TestDashboardCopyKeyRequiresConfirmation(t *testing.T) {
	model := newDashboardModel([]string{"alice@example.com:6023"})
	model, cmd := chooseDashboardAction(t, model, "copy key")
	if cmd != nil || !model.confirmOpen || model.done {
		t.Fatalf("copy key did not stay in confirmation: cmd=%v model=%#v", cmd, model)
	}
	view := ansiCSI.ReplaceAllString(model.View(), "")
	for _, want := range []string{"CONFIRM COPY SSH KEY", "alice@example.com:6023", "ssh-copy-id -p 6023 alice@example.com"} {
		if !strings.Contains(view, want) {
			t.Fatalf("copy-key confirmation missing %q:\n%s", want, view)
		}
	}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = updated.(dashboardModel)
	if cmd == nil || model.done || !model.terminalRunning || model.operation == nil ||
		model.operation.Action != string(actionCopyKey) {
		t.Fatalf("confirmed copy key did not hand off terminal: cmd=%v model=%#v", cmd, model)
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

func TestDashboardOrdersOnlineHostsFirstAfterProbeBatch(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	state := nexusState{Hosts: map[string]hostActivity{
		"offline@one": {Score: 20, LastUsed: now},
		"fast@two":    {Score: 5, LastUsed: now},
		"slow@three":  {Score: 1, LastUsed: now},
	}}
	model := newDashboardModelWithState(
		[]string{"offline@one", "fast@two", "slow@three"}, state, now,
	)
	model.probeQueue = nil
	model.probeTargets = map[string]bool{
		"offline@one": true, "fast@two": true, "slow@three": true,
	}
	model.probeTotal = 3
	model.probeComplete = 0
	model.probing = true
	model.filtering = true
	model.query = "@"
	model.applyFilter()
	model.cursor = 2
	selected := model.selectedTarget()

	for _, result := range []reachabilityResult{
		{Target: "slow@three", Status: reachOnline, Latency: 20 * time.Millisecond},
		{Target: "offline@one", Status: reachTimeout},
		{Target: "fast@two", Status: reachOnline, Latency: 4 * time.Millisecond},
	} {
		updated, _ := model.Update(probeTargetMsg(result))
		model = updated.(dashboardModel)
	}
	got := []string{model.hosts[0].Target, model.hosts[1].Target, model.hosts[2].Target}
	want := []string{"fast@two", "slow@three", "offline@one"}
	if !slices.Equal(got, want) {
		t.Fatalf("host order=%v, want %v", got, want)
	}
	if model.selectedTarget() != selected || len(model.filtered) != 3 || model.query != "@" {
		t.Fatalf("selection/filter changed during reorder: selected=%q filtered=%v query=%q",
			model.selectedTarget(), model.filtered, model.query)
	}
}

func TestDashboardConfigIsDiscoverableFromActions(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	model, cmd := chooseDashboardAction(t, model, "config")
	if cmd == nil || !model.done || model.choice.Action != actionConfig {
		t.Fatalf("saved-command shortcut did not open config: cmd=%v model=%#v", cmd, model)
	}
}

func TestDashboardSavedCommandConfirmationStaysInContext(t *testing.T) {
	previous := loadedConfig
	loadedConfig = defaultAppConfig()
	loadedConfig.Commands = []commandConfig{{
		Name: "uptime", Description: "Show uptime", Command: "uptime", Confirm: true,
	}}
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

func TestDashboardSavedCommandRunsImmediatelyUnlessConfirmationEnabled(t *testing.T) {
	previous := loadedConfig
	loadedConfig = defaultAppConfig()
	loadedConfig.Commands = []commandConfig{{Name: "uptime", Description: "Show uptime", Command: "uptime"}}
	t.Cleanup(func() { loadedConfig = previous })

	model := newDashboardModel([]string{"alice@one"})
	model.commandOpen = true
	model.commandQuery = "uptime"
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(dashboardModel)
	if cmd == nil || model.confirmOpen || !model.commandRunning || model.commandResult == nil ||
		model.commandResult.Command.Name != "uptime" || model.done {
		t.Fatalf("default saved command did not run in place: cmd=%v model=%#v", cmd, model)
	}
}

func TestDashboardTerminalSSHSupportsPasswordPromptsAndReturnsInPlace(t *testing.T) {
	selection := dashboardSelection{Action: actionSSH, Host: "alice@example.com:6023"}
	command, err := buildDashboardTerminalCommand(selection)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command.Args, " ")
	for _, want := range []string{"-t -t", "-p 6023", "alice@example.com"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("interactive SSH args missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "BatchMode=yes") {
		t.Fatalf("interactive SSH disabled password authentication: %s", joined)
	}

	model := newDashboardModel([]string{selection.Host})
	updated, cmd := model.startTerminalAction(selection, nil)
	model = updated.(dashboardModel)
	if cmd == nil || !model.terminalRunning || model.done {
		t.Fatalf("SSH did not begin terminal handoff: cmd=%v model=%#v", cmd, model)
	}
	operationID := model.operationID
	updated, _ = model.Update(terminalActionFinishedMsg{OperationID: operationID, Selection: selection})
	model = updated.(dashboardModel)
	if model.terminalRunning || model.done || model.notice != "SSH session ended" ||
		len(model.activities) != 1 || model.activities[0].Summary != "SSH session ended" {
		t.Fatalf("SSH did not return to the same dashboard: %#v", model)
	}

	model.startOperation(actionSSH, "SSH session", selection.Host)
	updated, _ = model.Update(terminalActionFinishedMsg{
		OperationID: model.operationID,
		Selection:   selection,
		Err:         errors.New("Password: super-secret"),
	})
	model = updated.(dashboardModel)
	if strings.Contains(model.notice, "super-secret") || strings.Contains(model.operation.Summary, "super-secret") ||
		model.notice != "SSH connection failed · check the host, credentials, or network" {
		t.Fatalf("SSH auth failure leaked challenge text: %#v", model)
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

func TestDashboardActionFilterAcceptsSpaces(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'a'}},
		{Type: tea.KeyRunes, Runes: []rune{'/'}},
		{Type: tea.KeyRunes, Runes: []rune("check")},
		{Type: tea.KeySpace},
		{Type: tea.KeyRunes, Runes: []rune("host")},
	} {
		updated, _ := model.Update(key)
		model = updated.(dashboardModel)
	}
	commands := model.filteredCommands()
	if model.commandQuery != "check host" {
		t.Fatalf("query=%q, want %q", model.commandQuery, "check host")
	}
	if len(commands) != 1 || commands[0].Action != actionProbe {
		t.Fatalf("filtered commands=%#v", commands)
	}
}

func TestDashboardActionsAreRankedByRecordedUsage(t *testing.T) {
	previous := loadedConfig
	t.Cleanup(func() { loadedConfig = previous })
	loadedConfig = defaultAppConfig()
	loadedConfig.UI.PinnedActions = nil
	state := nexusState{
		Hosts: map[string]hostActivity{},
		Actions: map[string]int{
			string(actionStorage): 7,
			string(actionSSH):     3,
		},
	}
	model := newDashboardModelWithState([]string{"alice@one"}, state, time.Now())
	commands := model.availableCommands()
	if commands[0].Action != actionStorage || commands[1].Action != actionSSH {
		t.Fatalf("actions were not usage-ranked: %#v", commands[:2])
	}
	if commands[2].Action != actionCopyKey || commands[3].Action != actionPull {
		t.Fatalf("unused actions lost their stable default order: %#v", commands[2:4])
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
		actionTop, actionNet, actionStorage, actionCopyKey, actionFleet, actionThemes, actionConfig,
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
	model, cmd := chooseDashboardAction(t, model, "check host")
	if cmd == nil || !model.probing || model.probeTotal != 1 {
		t.Fatalf("selected connection check did not start: cmd=%v model=%#v", cmd, model)
	}

	model.probing = false
	model.probeInitial = nil
	model.probeTargets = map[string]bool{}
	model, cmd = chooseDashboardAction(t, model, "check all")
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

func TestSanitizeCommandOutputPreservesUsefulTableSpacing(t *testing.T) {
	input := "\x1b[31mDEVICE\tMOUNT    USE\x1b[0m\nnvme0n1  /        76%\n"
	got := sanitizeCommandOutput(input)
	if got != "DEVICE    MOUNT    USE\nnvme0n1  /        76%" {
		t.Fatalf("table spacing changed: %q", got)
	}
}

func TestDashboardSelectedHostRowDoesNotLeakANSIFragments(t *testing.T) {
	model := newDashboardModel([]string{"alice@192.168.0.66"})
	model.width = 80
	model.height = 24
	model.hosts[0].Reachability = reachabilityResult{
		Target:  "alice@192.168.0.66",
		Status:  reachOnline,
		Latency: 4 * time.Millisecond,
	}
	view := model.View()
	plain := ansiCSI.ReplaceAllString(view, "")
	for _, fragment := range []string{"[38;", "[0m"} {
		if strings.Contains(plain, fragment) {
			t.Fatalf("selected host row leaked ANSI fragment %q:\n%q", fragment, plain)
		}
	}
	if !strings.Contains(plain, "192.168.0.66") || !strings.Contains(plain, "● 4ms") {
		t.Fatalf("selected host row lost content:\n%q", plain)
	}
}

func TestDashboardInteractiveSavedCommandTemporarilyOwnsTerminal(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	model.confirmOpen = true
	model.confirmAction = dashboardSelection{
		Action: actionCustom,
		Host:   "alice@one",
		Command: commandConfig{
			Name: "tmux attach", Command: "tmux attach", Interactive: true, Confirm: true,
		},
	}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = updated.(dashboardModel)
	if cmd == nil || model.done || !model.terminalRunning || model.choice.Action != "" ||
		model.commandResult != nil || model.operation == nil {
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
		"alice@one:2222": {
			Score: 3, LastUsed: now.Add(-time.Hour), OS: "Ubuntu 24.04",
			GPUs: []string{"NVIDIA RTX 4090 · 25.8 GB VRAM"}, Memory: "34.4 GB",
			Disks: []diskUsage{
				{Filesystem: "/dev/root", Mountpoint: "/", UsedBytes: 500_000_000_000, TotalBytes: 1_000_000_000_000},
				{Filesystem: "/dev/data", Mountpoint: "/data", UsedBytes: 1_000_000_000_000, TotalBytes: 2_000_000_000_000},
			},
			Tools: []string{"btop", "duf"},
		},
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
			for _, want := range []string{"SYSTEM", "NVIDIA RTX 4090", "34.4 GB", "STORAGE", "/data", "TOOLS & COMMANDS"} {
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
		if width == 78 {
			for _, want := range []string{"System", "NVIDIA RTX 4090", "34.4 GB", "2 storage volumes"} {
				if !strings.Contains(view, want) {
					t.Fatalf("medium layout missing %q:\n%s", want, view)
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
		for _, size := range [][2]int{
			{20, 6}, {29, 10}, {30, 9}, {30, 10}, {40, 12}, {40, 15}, {40, 16},
			{71, 20}, {72, 20}, {95, 24},
			{96, 24}, {109, 24}, {110, 24}, {120, 25}, {120, 32},
			{149, 32}, {149, 80}, {150, 31}, {150, 32}, {240, 80},
		} {
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

func TestDashboardUltraWideWorkbenchUsesAvailableSpace(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	state := nexusState{
		Hosts: map[string]hostActivity{
			"alice@one:2222": {
				Score: 4, LastUsed: now.Add(-time.Minute), OS: "Ubuntu 26.04",
				CPU: "Example CPU", GPUs: []string{"Example GPU"}, Memory: "32 GB",
				Disks: []diskUsage{{
					Filesystem: "/dev/root", Mountpoint: "/",
					UsedBytes: 300_000_000_000, TotalBytes: 1_000_000_000_000,
				}},
			},
		},
		LatestOperation: &operationSummary{
			Action: string(actionStorage), Label: "Storage", Host: "alice@one:2222",
			Status: "success", Summary: "All filesystems inspected", Duration: 850 * time.Millisecond,
		},
	}
	model := newDashboardModelWithState([]string{"alice@one:2222", "bob@two"}, state, now)
	model.plain = true
	model.width, model.height = 240, 80
	model.operation.Output = "older output\n/dev/root  300 GB / 1.0 TB"
	view := model.View()
	for _, want := range []string{
		"HOSTS", "SYSTEM", "PINNED & FREQUENT", "HOST PULSE", "ACTIVITY",
		"All filesystems inspected", "/dev/root", "older output", "[a] open actions",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("ultra-wide layout missing %q:\n%s", want, view)
		}
	}
	assertTerminalBounds(t, view, 240, 80, "ultra-wide workspace")
}

func TestDashboardUltraWideBreakpointFallsBackCleanly(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		width, height int
		ultra         bool
	}{
		{149, 32, false},
		{150, 31, true},
		{150, 25, false},
		{150, 32, true},
	} {
		model := newDashboardModelWithState([]string{"alice@one"}, nexusState{
			Hosts: map[string]hostActivity{"alice@one": {OS: "Linux"}},
		}, now)
		model.plain = true
		model.width, model.height = tc.width, tc.height
		view := model.View()
		if got := strings.Contains(view, "HOST PULSE"); got != tc.ultra {
			t.Fatalf("size=%dx%d ultra=%v, want %v:\n%s", tc.width, tc.height, got, tc.ultra, view)
		}
		assertTerminalBounds(t, view, tc.width, tc.height, "responsive breakpoint")
	}
}

func TestDashboardStaleAsyncCompletionDoesNotReplaceLatestOperation(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	model.startOperation(actionInfo, "Refresh info", "alice@one")
	staleID := model.operationID
	model.startOperation(actionStorage, "Storage", "alice@one")
	latestID := model.operationID
	model.commandResult = &configuredCommandResult{
		Action: actionStorage, Host: "alice@one",
		Command: commandConfig{Name: "Storage"},
	}

	updated, _ := model.Update(metadataRefreshMsg{
		Target:      "alice@one",
		OperationID: staleID,
		Activity:    hostActivity{OS: "Linux", Updated: time.Now()},
	})
	model = updated.(dashboardModel)
	if model.operation.Label != "Storage" || model.operation.Status != "running" {
		t.Fatalf("stale metadata completion replaced latest operation: %#v", model.operation)
	}

	updated, _ = model.Update(configuredCommandMsg{
		OperationID: latestID,
		Output:      "/  300 GB / 1 TB",
	})
	model = updated.(dashboardModel)
	if model.operation.Label != "Storage" || model.operation.Status != "success" {
		t.Fatalf("latest completion was not applied: %#v", model.operation)
	}
}

func TestDashboardPinnedActionsLeadInConfiguredOrder(t *testing.T) {
	previous := loadedConfig
	t.Cleanup(func() { loadedConfig = previous })
	loadedConfig = defaultAppConfig()
	loadedConfig.Commands = sanitizeCommands([]commandConfig{
		{ID: "tmux", Name: "tmux list", Command: "tmux ls"},
	})
	loadedConfig.UI.PinnedActions = []string{"command:tmux", "storage"}
	model := newDashboardModel([]string{"alice@one"})
	model.actionUses[string(actionSSH)] = 100
	commands := model.availableCommands()
	if len(commands) < 3 || commands[0].Command.ID != "tmux" ||
		commands[1].Action != actionStorage || commands[2].Action != actionSSH {
		t.Fatalf("unexpected action order: %#v", commands[:min(4, len(commands))])
	}
}

func TestDashboardActivitiesCapAtFiveAndPreserveOutputLines(t *testing.T) {
	var events []activityEvent
	for index := 0; index < 7; index++ {
		events = appendActivity(events, activityEvent{
			Label:  fmt.Sprintf("task %d", index),
			Status: "success",
			Output: "first\nsecond",
		})
	}
	if len(events) != 5 || events[0].Label != "task 2" ||
		events[4].Output != "first\nsecond" {
		t.Fatalf("events=%#v", events)
	}
}

func TestDashboardAllWorkspaceModesStayResponsive(t *testing.T) {
	previous := loadedConfig
	t.Cleanup(func() { loadedConfig = previous })
	for _, mode := range workspaceModes() {
		loadedConfig = defaultAppConfig()
		loadedConfig.UI.Workspace = mode
		for _, size := range [][2]int{
			{20, 6}, {40, 12}, {71, 20}, {72, 20}, {95, 24},
			{96, 24}, {149, 32}, {150, 32}, {240, 80},
		} {
			model := newDashboardModel([]string{"alice@one", "bob@two"})
			model.plain = true
			model.width, model.height = size[0], size[1]
			view := model.View()
			assertTerminalBounds(t, view, size[0], size[1], mode)
			if size[0] >= 150 && size[1] >= 32 {
				marker := map[string]string{
					"workbench": "HOST PULSE",
					"console":   "OPERATIONS CONSOLE",
					"fleet":     "FLEET WORKSPACE",
				}[mode]
				if !strings.Contains(view, marker) {
					t.Fatalf("mode=%s size=%v missing %q:\n%s", mode, size, marker, view)
				}
			}
		}
	}
}

func TestDashboardThemeStylesPaintPanelTextContinuously(t *testing.T) {
	for name, theme := range themes {
		model := newDashboardModel([]string{"alice@one"})
		model.plain = false
		model.theme = theme
		styles := model.styles()
		if theme.Surface == "" {
			continue
		}
		wantSurface := lipgloss.Color(theme.Surface)
		for role, style := range map[string]lipgloss.Style{
			"text": styles.text, "muted": styles.muted, "focus": styles.focus,
			"live": styles.live, "success": styles.success, "warning": styles.warning,
			"failure": styles.failure, "panel": styles.panel,
		} {
			if got := style.GetBackground(); got != wantSurface {
				t.Fatalf("theme=%s role=%s background=%v want=%v", name, role, got, wantSurface)
			}
		}
		if got := styles.selected.GetBackground(); got != lipgloss.Color(theme.Elevated) {
			t.Fatalf("theme=%s selected background=%v", name, got)
		}
	}
}

func TestDashboardLargeDiskInventoryUsesBoundedSummary(t *testing.T) {
	disks := make([]diskUsage, 20)
	for index := range disks {
		disks[index] = diskUsage{
			Filesystem: fmt.Sprintf("/dev/disk%d", index),
			Mountpoint: fmt.Sprintf("/mnt/volume-%02d", index),
			UsedBytes:  500_000_000_000,
			TotalBytes: 1_000_000_000_000,
		}
	}
	now := time.Now()
	state := nexusState{Hosts: map[string]hostActivity{
		"alice@one": {Score: 1, LastUsed: now, Disks: disks},
	}}
	model := newDashboardModelWithState([]string{"alice@one"}, state, now)
	model.width, model.height = 100, 24
	view := ansiCSI.ReplaceAllString(model.View(), "")
	if !strings.Contains(view, "more · Actions → Storage shows all volumes") {
		t.Fatalf("large inventory did not explain progressive disclosure:\n%s", view)
	}
	assertTerminalBounds(t, model.View(), 100, 24, "large disk inventory")
	full := renderStorageInventory(disks)
	if !strings.Contains(full, "/mnt/volume-00") || !strings.Contains(full, "/mnt/volume-19") {
		t.Fatalf("full storage inventory omitted mounted filesystems:\n%s", full)
	}
}

func TestStorageUsageBarClampsAndRounds(t *testing.T) {
	tests := []struct {
		percent float64
		want    string
	}{
		{-10, "░░░░░░░░░░"},
		{0, "░░░░░░░░░░"},
		{74, "███████░░░"},
		{75, "████████░░"},
		{89, "█████████░"},
		{90, "█████████░"},
		{100, "██████████"},
		{120, "██████████"},
	}
	for _, test := range tests {
		if got := storageUsageBar(test.percent, 10); got != test.want {
			t.Fatalf("storageUsageBar(%v)=%q, want %q", test.percent, got, test.want)
		}
	}
}

func TestRenderStorageInventoryIsDeviceFirstFilteredAndANSIPlain(t *testing.T) {
	disks := []diskUsage{
		{Filesystem: "tmpfs", Mountpoint: "/run", FilesystemType: "tmpfs", TotalBytes: 10},
		{Filesystem: "/dev/nvme0n1p2", Mountpoint: "/", FilesystemType: "ext4", UsedBytes: 760, TotalBytes: 1000},
		{Filesystem: "/dev/loop0", Mountpoint: "/snap/core/1", FilesystemType: "squashfs", UsedBytes: 10, TotalBytes: 10},
		{Filesystem: "/dev/sda", Mountpoint: "/data1", FilesystemType: "ext4", UsedBytes: 340, TotalBytes: 1000},
	}
	got := renderStorageInventory(disks)
	for _, want := range []string{"VOLUME @ MOUNT", "nvme0n1p2 @ /", "sda @ /data1", "████████░░  76%"} {
		if !strings.Contains(got, want) {
			t.Fatalf("storage inventory missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"tmpfs", "loop0", "\x1b"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("storage inventory retained %q:\n%s", unwanted, got)
		}
	}
}

func TestDashboardOverlaysStayWithinTerminalBounds(t *testing.T) {
	for _, size := range [][2]int{{40, 16}, {72, 20}, {120, 32}} {
		for _, overlay := range []string{"help", "commands", "themes", "workspace", "confirm", "output", "fleet", "transfer"} {
			model := newDashboardModel([]string{"alice@one"})
			model.width, model.height = size[0], size[1]
			switch overlay {
			case "help":
				model.helpOpen = true
			case "commands":
				model.commandOpen = true
			case "themes":
				model.openThemePreview()
			case "workspace":
				model.openWorkspacePreview()
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
