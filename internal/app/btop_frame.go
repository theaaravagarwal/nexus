package app

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// renderBtopFrame replays btop's real terminal output into a bounded virtual
// terminal. The resulting ANSI view can be composed inside the Monitor pane
// without allowing btop's cursor movement or alternate-screen controls to
// escape into Nexus itself.
func renderBtopFrame(raw string, columns, rows int) string {
	if raw == "" {
		return ""
	}
	columns = min(180, max(btopMinColumns, columns))
	rows = min(60, max(btopMinRows, rows))
	emulator := vt.NewEmulator(columns, rows)
	defer emulator.Close()
	emulator.SetScrollbackSize(0)

	// btop wraps each repaint in synchronized-output markers. Preserve the
	// richest completed repaint instead of blindly using the last terminal
	// state: timeout/SSH teardown can switch out of the alternate screen and
	// leave only a final "Killed" message behind.
	const synchronizedFrameEnd = "\x1b[?2026l"
	bestFrame := ""
	bestScore := 0
	for len(raw) > 0 {
		chunkEnd := len(raw)
		if index := strings.Index(raw, synchronizedFrameEnd); index >= 0 {
			chunkEnd = index + len(synchronizedFrameEnd)
		}
		_, _ = emulator.WriteString(raw[:chunkEnd])
		raw = raw[chunkEnd:]
		frame := strings.TrimRight(emulator.Render(), "\r\n")
		if score := btopFrameScore(frame); score > bestScore {
			bestFrame, bestScore = frame, score
		}
	}
	if bestScore == 0 {
		return ""
	}
	return bestFrame
}

func btopFrameScore(frame string) int {
	plain := strings.ToLower(ansi.Strip(frame))
	score := 0
	for _, character := range plain {
		if !unicode.IsSpace(character) {
			score++
		}
	}
	for _, label := range []string{"cpu", "mem", "net", "proc"} {
		if strings.Contains(plain, label) {
			score += 1000
		}
	}
	return score
}
