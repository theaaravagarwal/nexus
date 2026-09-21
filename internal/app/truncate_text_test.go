package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// truncateTextReference is the pre-optimization implementation of
// truncateText, kept here only as a differential oracle: it iterates rune by
// rune, recomputing lipgloss.Width over the whole growing prefix each time,
// which is exactly what the rewritten truncateText (needsTerminalSanitize +
// terminalWidth fast paths, ansi.Truncate for the cut) is meant to match
// byte-for-byte while doing much less work.
func truncateTextReference(value string, width int) string {
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

// TestTruncateTextMatchesReference is the correctness backstop for the
// truncateText rewrite: the render-parity goldens exercise it only with the
// plain, mostly-ASCII strings this app actually produces (hostnames,
// labels, command names), so they would not catch a subtle mismatch in
// exotic cases like multi-rune grapheme clusters, doubled/leading/trailing
// whitespace, tabs, or boundary widths. This compares the optimized
// truncateText against the original algorithm across all of those.
func TestTruncateTextMatchesReference(t *testing.T) {
	inputs := []string{
		"",
		" ",
		"  ",
		" leading space",
		"trailing space ",
		"double  space  inside",
		"\ttab\tseparated\tvalues",
		"line\nbreak\nhere",
		"control\x00chars\x01here",
		"already ansi \x1b[31mred\x1b[0m text",
		"exact fit 12345",
		"a",
		"ab",
		"hostname-only",
		"user@host.example.com:2222",
		"中文日本語한글混合テキスト",
		"emoji rocket 🚀 target 🎯 check ✓ mix",
		"flag emoji 🏳️‍🌈 family 👨‍👩‍👧‍👦 sequence",
		"combining é́́ marks",
		strings.Repeat("wide中文", 20),
		strings.Repeat("x", 300),
		strings.Repeat("  ", 50) + "many double spaces",
	}
	widths := []int{-1, 0, 1, 2, 3, 4, 5, 8, 10, 12, 16, 20, 24, 32, 40, 64, 100, 300}

	for _, input := range inputs {
		for _, width := range widths {
			got := truncateText(input, width)
			want := truncateTextReference(input, width)
			if got != want {
				t.Errorf("truncateText(%q, %d) = %q, reference = %q", input, width, got, want)
			}
		}
	}
}

// TestNeedsTerminalSanitizeAgreesWithSanitizeTerminalText checks the
// truncateText fast-path predicate directly: whenever it reports "no
// sanitize needed", sanitizeTerminalText must actually be a no-op, across a
// range of clean and dirty inputs.
func TestNeedsTerminalSanitizeAgreesWithSanitizeTerminalText(t *testing.T) {
	inputs := []string{
		"",
		"clean text",
		" leading",
		"trailing ",
		"double  space",
		"a\tb",
		"a\nb",
		"a\x1b[31mb",
		"a b", // non-breaking space: rare, must fall back safely
		"single space ok",
		"multiple   internal   gaps",
		"unicode 中文 mixed with spaces",
	}
	for _, input := range inputs {
		needs := needsTerminalSanitize(input)
		changed := sanitizeTerminalText(input) != input
		if !needs && changed {
			t.Errorf("needsTerminalSanitize(%q) = false, but sanitizeTerminalText changes it to %q", input, sanitizeTerminalText(input))
		}
	}
}
