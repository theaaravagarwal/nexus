package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestSimpleTerminalWidthMatchesAnsi proves the fast path exact: every rune
// simpleRuneWidthOne accepts must be one cell for ansi.StringWidth, alone and
// between two braille cells (no clustering), and mixed strings with escape
// sequences must agree with the reference implementation byte for byte.
func TestSimpleTerminalWidthMatchesAnsi(t *testing.T) {
	for r := rune(0x80); r <= 0x2bff; r++ {
		if !simpleRuneWidthOne(r) {
			continue
		}
		single := string(r)
		if got := ansi.StringWidth(single); got != 1 {
			t.Fatalf("U+%04X: ansi width %d, fast path assumes 1", r, got)
		}
		if got := ansi.StringWidth("\u2840" + single + "\u2840"); got != 3 {
			t.Fatalf("U+%04X clusters with neighbours: width %d", r, got)
		}
		if got, ok := simpleTerminalWidth(single); !ok || got != 1 {
			t.Fatalf("U+%04X: simpleTerminalWidth=%d ok=%v", r, got, ok)
		}
	}
	samples := []string{
		"",
		"plain ascii",
		"\x1b[38;2;10;20;30m\u2840\u2841\x1b[0m cpu \x1b[1;97m39\u00b0C\x1b[m",
		"\u256d\u2500\u2510\u00b9cpu\u250c\u2500\u2500\u2510menu\u250c\u2510 \u25a0\u25a0\u25a0 \u25bc \u2593\u2591 100%",
		"\x1b]8;;http://x\x07link\x1b]8;;\x1b\\ after",
		"\x1b[?25l\x1b[2J\x1b[H\u2588\u2588",
	}
	for _, sample := range samples {
		want := ansi.StringWidth(sample)
		if got := terminalWidth(sample); got != want {
			t.Fatalf("terminalWidth(%q)=%d want %d", sample, got, want)
		}
		if got, ok := simpleTerminalWidth(sample); !ok || got != want {
			t.Fatalf("simpleTerminalWidth(%q)=%d ok=%v want %d", sample, got, ok, want)
		}
	}
	fallbacks := []string{
		"\u4e2d\u6587",              // CJK, width 2 each
		"\U0001F600",                // emoji
		"e\u0301",                   // combining mark
		"\u2614",                    // wide misc symbol
		"tab\there",                 // control character
		"\x1b[31",                   // truncated CSI
		"\x1bMreverse index",        // non-CSI escape
		"a\u200db",                  // zero width joiner
		"\u25fd",                    // wide geometric shape
		strings.Repeat("\u00ad", 3), // soft hyphen
	}
	for _, sample := range fallbacks {
		if _, ok := simpleTerminalWidth(sample); ok {
			t.Fatalf("simpleTerminalWidth accepted %q, must fall back", sample)
		}
		if got, want := terminalWidth(sample), ansi.StringWidth(sample); got != want {
			t.Fatalf("terminalWidth(%q)=%d want %d", sample, got, want)
		}
	}
}
