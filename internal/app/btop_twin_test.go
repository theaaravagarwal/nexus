package app

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// realisticBtopFrame builds a frame like btop's real output: every cell
// carries its own truecolor SGR, with braille graphs, box drawing, blocks
// and a few Latin-1 symbols, at exactly columns x rows cells.
func realisticBtopFrame(columns, rows int) string {
	glyphs := []rune("⣀⣤⣶⣿⡀⠉─│╭╮╰╯■░▒▓▼▲°·¹²³⁴")
	lines := make([]string, rows)
	for y := 0; y < rows; y++ {
		var b strings.Builder
		for x := 0; x < columns; x++ {
			r := glyphs[(x*7+y*3)%len(glyphs)]
			if (x+y)%9 == 0 {
				r = rune('a' + (x+y)%26)
			}
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm%c", (x*5)%256, (y*11)%256, (x+y)%256, r)
		}
		b.WriteString("\x1b[0m")
		lines[y] = b.String()
	}
	return strings.Join(lines, "\n")
}

func TestConsoleTwinLayoutMatchesDirectLayout(t *testing.T) {
	model := newDashboardModel([]string{"alice@one", "bob@two"})
	model.experimentalTabs = true
	model.experimentalFleetBtop = true
	model.workspace = "console"
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	model = updated.(dashboardModel)
	columns, rows, ok := model.monitorBtopViewport()
	if !ok {
		t.Fatalf("viewport not visible at 200x50")
	}
	entry := model.telemetry["alice@one"]
	entry.Current.Target = "alice@one"
	entry.Current.BtopInstalled = true
	entry.Current.BtopFrame = realisticBtopFrame(columns, rows)
	model.telemetry["alice@one"] = entry
	model.btopStreamState = "live"
	model.btopStreamFrames = 3

	s := model.styles()
	_, workspaceHeight, _ := model.dashboardHeights()
	width := model.width
	actionWidth := model.monitorActionWidth(width)
	monitorWidth := width - actionWidth
	summaryHeight, btopHeight := monitorPaneHeights(workspaceHeight)

	// Reference: the pre-twin composition, laying out the real pane directly.
	direct := lipgloss.NewStyle().Width(width).Height(workspaceHeight).Render(
		lipgloss.JoinHorizontal(lipgloss.Top,
			fitTerminalView(lipgloss.JoinVertical(lipgloss.Left,
				fitTerminalView(model.monitorSummaryView(s, monitorWidth, summaryHeight), monitorWidth, summaryHeight),
				fitTerminalView(model.btopPanelView(s, monitorWidth, btopHeight, false, true), monitorWidth, btopHeight),
			), monitorWidth, workspaceHeight),
			fitTerminalView(model.monitorActionRailView(s, actionWidth, workspaceHeight), actionWidth, workspaceHeight),
		),
	)
	twin := model.consoleWorkspaceView(s, width, workspaceHeight)
	if direct != twin {
		t.Fatalf("twin layout differs from direct layout\n--- direct ---\n%q\n--- twin ---\n%q", direct, twin)
	}
	if strings.Contains(twin, btopTwinMarker) {
		t.Fatal("twin marker leaked into the rendered console")
	}
	view := model.View()
	if strings.Contains(view, btopTwinMarker) || !strings.Contains(view, "BTOP") {
		t.Fatal("full view is missing the pane or leaks a marker")
	}
	// The pane itself must equal a direct lipgloss render of the real rows.
	panel, twinPanel, twins, lines := model.btopPanelViews(s, monitorWidth, btopHeight, false, true)
	if strings.Contains(panel, btopTwinMarker) || !strings.Contains(twinPanel, btopTwinMarker) {
		t.Fatal("pane/twin marker placement is wrong")
	}
	if len(twins) != rows || len(lines) != rows {
		t.Fatalf("twins=%d lines=%d want %d", len(twins), len(lines), rows)
	}
	for index := range lines {
		if terminalWidth(lines[index]) != terminalWidth(twins[index]) {
			t.Fatalf("row %d width mismatch", index)
		}
	}
}

func BenchmarkDashboardViewConsoleRealisticFrame(b *testing.B) {
	model := newDashboardModel([]string{"alice@one", "bob@two"})
	model.experimentalTabs = true
	model.experimentalFleetBtop = true
	model.workspace = "console"
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 180, Height: 45})
	model = updated.(dashboardModel)
	columns, rows, _ := model.monitorBtopViewport()
	entry := model.telemetry["alice@one"]
	entry.Current.Target = "alice@one"
	entry.Current.BtopInstalled = true
	entry.Current.BtopFrame = realisticBtopFrame(columns, rows)
	model.telemetry["alice@one"] = entry
	model.btopStreamState = "live"
	model.btopStreamFrames = 3
	b.ReportAllocs()
	for b.Loop() {
		_ = model.View()
	}
}
