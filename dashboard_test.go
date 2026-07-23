package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
	if cmd == nil || model.choice != (dashboardSelection{Action: actionSSH, Host: "bob@two:2222"}) {
		t.Fatalf("choice=%#v cmd=%v", model.choice, cmd)
	}
}

func TestDashboardDirectActionsDispatchExpectedCommands(t *testing.T) {
	tests := []struct {
		key    rune
		action dashboardAction
	}{
		{'p', actionPull},
		{'u', actionPush},
		{'i', actionInfo},
		{'n', actionNet},
		{'d', actionStorage},
	}
	for _, tc := range tests {
		model := newDashboardModel([]string{"alice@one:2222"})
		updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tc.key}})
		got := updated.(dashboardModel)
		if cmd == nil || got.choice.Action != tc.action || got.choice.Host != "alice@one:2222" {
			t.Fatalf("key=%q choice=%#v cmd=%v", tc.key, got.choice, cmd)
		}
	}
}

func TestDashboardNavigationAndCommandFilter(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	model.focus = focusNavigation
	model.navCursor = 2
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(dashboardModel)
	if !model.commandOpen {
		t.Fatal("Commands navigation item did not open palette")
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("storage")})
	model = updated.(dashboardModel)
	commands := model.filteredCommands()
	if len(commands) != 1 || commands[0].Action != actionStorage {
		t.Fatalf("filtered commands=%#v", commands)
	}
}

func TestDashboardThemePreviewStaysInsideTUI(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	model.focus = focusNavigation
	model.navCursor = 4
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(dashboardModel)
	if cmd != nil || !model.themeOpen {
		t.Fatalf("theme preview did not open in TUI: cmd=%v model=%#v", cmd, model)
	}
	before := model.theme.Name
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(dashboardModel)
	if model.theme.Name == before {
		t.Fatal("theme preview did not advance")
	}
	view := model.View()
	for _, want := range []string{"THEME PREVIEW", "use this session", "edit config to save"} {
		if !strings.Contains(view, want) {
			t.Fatalf("theme preview missing %q:\n%s", want, view)
		}
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
	if view := model.View(); !strings.Contains(view, "FLEET CONSTELLATION") {
		t.Fatalf("initial view missing topology:\n%s", view)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	model = updated.(dashboardModel)
	view := model.View()
	if strings.Contains(view, "FLEET CONSTELLATION") || !strings.Contains(view, "show map") {
		t.Fatalf("topology toggle had no visible effect:\n%s", view)
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
			for _, want := range []string{"FLEET CONSTELLATION", "QUICK ACTIONS", "SYSTEM SNAPSHOT", "TOOLS"} {
				if !strings.Contains(view, want) {
					t.Fatalf("wide layout missing %q:\n%s", want, view)
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
		for _, overlay := range []string{"help", "commands", "themes"} {
			model := newDashboardModel([]string{"alice@one"})
			model.width, model.height = size[0], size[1]
			switch overlay {
			case "help":
				model.helpOpen = true
			case "commands":
				model.commandOpen = true
			case "themes":
				model.openThemePreview()
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

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
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
		{Type: tea.KeyTab},
		{Type: tea.KeyShiftTab},
		{Type: tea.KeyRight},
		{Type: tea.KeyLeft},
		{Type: tea.KeyDown},
		{Type: tea.KeyPgDown},
		{Type: tea.KeyPgUp},
		{Type: tea.KeyEnd},
		{Type: tea.KeyHome},
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

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
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
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(dashboardModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(dashboardModel)
	if model.themeOpen || model.theme.Name != original {
		t.Fatalf("theme cancellation did not restore %q: %#v", original, model.theme)
	}

	batch := probeBatchMsg{
		{Target: "alice@one", Status: reachOnline, Latency: time.Millisecond},
		{Target: "bob@two", Status: reachRefused},
	}
	updated, cmd := model.Update(batch)
	model = updated.(dashboardModel)
	if model.probing || cmd == nil || model.hosts[0].Reachability.Status != reachOnline || model.hosts[1].Reachability.Status != reachRefused {
		t.Fatalf("probe batch was not applied: %#v cmd=%v", model.hosts, cmd)
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
