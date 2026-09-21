package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// Re-running a confirm-required command from the result view must open the
// confirm modal and let y/Y run it and n/N/esc cancel it, even though the
// result view is still underneath.
func TestDashboardRerunFromResultViewHonoursConfirm(t *testing.T) {
	previous := loadedConfig
	loadedConfig = defaultAppConfig()
	loadedConfig.Commands = []commandConfig{{Name: "uptime", Description: "Show uptime", Command: "uptime", Confirm: true}}
	t.Cleanup(func() { loadedConfig = previous })

	model := newDashboardModel([]string{"alice@one"})
	model.commandResult = &configuredCommandResult{
		Action: actionCustom, Host: "alice@one",
		Command: commandConfig{Name: "uptime", Command: "uptime", Confirm: true},
		Output:  "up 1 day",
	}
	updated, cmd := model.Update(runeKey('r'))
	model = updated.(dashboardModel)
	if cmd != nil || !model.confirmOpen || model.commandResult == nil {
		t.Fatalf("r did not open the confirm modal over the result: cmd=%v confirm=%v", cmd, model.confirmOpen)
	}
	if !strings.Contains(model.View(), "CONFIRM SAVED COMMAND") {
		t.Fatalf("confirm modal not rendered:\n%s", model.View())
	}
	// n cancels in place and clears the confirm state.
	updated, cmd = model.Update(runeKey('n'))
	model = updated.(dashboardModel)
	if cmd != nil || model.confirmOpen || model.confirmAction.Host != "" {
		t.Fatalf("n did not cancel the confirm modal: %#v", model.confirmAction)
	}
	// Reopen and run with Y.
	updated, _ = model.Update(runeKey('r'))
	model = updated.(dashboardModel)
	updated, cmd = model.Update(runeKey('Y'))
	model = updated.(dashboardModel)
	if cmd == nil || model.confirmOpen || !model.commandRunning {
		t.Fatalf("Y did not start the command: cmd=%v confirm=%v running=%v", cmd, model.confirmOpen, model.commandRunning)
	}
}

// Event handlers that ask for an immediate telemetry tick must retire the
// previous tick chain, otherwise chains accumulate and each forces a render.
func TestDashboardRestartTelemetryTickRetiresOldChain(t *testing.T) {
	model := newDashboardModel([]string{"alice@one", "bob@two"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = updated.(dashboardModel)
	oldGen := model.telemetryGen
	updated, cmd := model.Update(runeKey('j'))
	model = updated.(dashboardModel)
	if cmd == nil || model.telemetryGen == oldGen {
		t.Fatalf("selection change did not restart the tick chain: gen %d -> %d", oldGen, model.telemetryGen)
	}
	updated, cmd = model.Update(telemetryTickMsg{Generation: oldGen})
	if cmd != nil {
		t.Fatal("a tick from the retired chain was not ignored")
	}
	model = updated.(dashboardModel)
	if _, cmd = model.Update(telemetryTickMsg{Generation: model.telemetryGen}); cmd == nil {
		t.Fatal("the current chain stopped ticking")
	}
}

// The frecency write after a successful refresh must happen in a Cmd, not
// inline in Update.
func TestDashboardHostSuccessIsRecordedAsynchronously(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	model.statePath = filepath.Join(t.TempDir(), "state.json")
	updated, cmd := model.Update(metadataRefreshMsg{Target: "alice@one", Activity: hostActivity{OS: "Linux"}})
	model = updated.(dashboardModel)
	if cmd == nil {
		t.Fatal("no command returned for the host activity write")
	}
	if _, err := os.Stat(model.statePath); err == nil {
		t.Fatal("state was written synchronously inside Update")
	}
	deadline := time.Now().Add(5 * time.Second)
	msg := cmd()
	for time.Now().Before(deadline) {
		if _, ok := msg.(hostActivityMsg); ok {
			break
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			found := false
			for _, item := range batch {
				if item == nil {
					continue
				}
				if _, ok := item().(hostActivityMsg); ok {
					found = true
				}
			}
			if found {
				break
			}
		}
		t.Fatalf("unexpected message %T", msg)
	}
	state, err := loadState(model.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.Hosts["alice@one"].Score < 1 {
		t.Fatalf("host score not recorded: %#v", state.Hosts["alice@one"])
	}
}

// A stale frame left on screen after the stream is gated must show the gate
// reason instead of hiding it.
func TestDashboardGateReasonShownAboveStaleFrame(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	model.experimentalTabs = true
	model.experimentalFleetBtop = true
	model.workspace = "console"
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	model = updated.(dashboardModel)
	entry := model.telemetry["alice@one"]
	entry.Current.Target = "alice@one"
	entry.Current.BtopInstalled = true
	entry.Current.BtopFrame = strings.Repeat("cpu mem net proc\n", 24)
	model.telemetry["alice@one"] = entry
	model.btopStreamState = ""
	updated, _ = model.Update(tea.WindowSizeMsg{Width: 150, Height: 30})
	model = updated.(dashboardModel)
	view := model.View()
	if !strings.Contains(view, "IDLE") || !strings.Contains(view, "Terminal too small") {
		t.Fatalf("gate reason missing above the stale frame:\n%s", view)
	}
}
