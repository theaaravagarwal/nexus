package nexus

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRenderHelpPlainAndColor(t *testing.T) {
	app := &app{}
	root := app.newRootCmd()
	plain := renderHelp(root, false)
	for _, want := range []string{"NEXUS", "Commands", "Flags", "--port"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("plain help missing %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "\x1b[") {
		t.Fatal("plain help contains ANSI escapes")
	}
	colored := renderHelp(root, true)
	if !strings.Contains(colored, "\x1b[") {
		t.Fatal("colored help has no ANSI escapes")
	}
}

func TestShouldColorHelpHonorsEnvironment(t *testing.T) {
	previousNoColor, hadNoColor := os.LookupEnv("NO_COLOR")
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadNoColor {
			_ = os.Setenv("NO_COLOR", previousNoColor)
		} else {
			_ = os.Unsetenv("NO_COLOR")
		}
	})
	t.Setenv("CLICOLOR_FORCE", "1")
	if !shouldColorHelp(&bytes.Buffer{}) {
		t.Fatal("CLICOLOR_FORCE should enable color")
	}
	t.Setenv("NO_COLOR", "1")
	if shouldColorHelp(&bytes.Buffer{}) {
		t.Fatal("NO_COLOR should override forced color")
	}
}

func TestRootExposesVersionFlag(t *testing.T) {
	previous := version
	version = "0.1.0-test"
	t.Cleanup(func() { version = previous })

	root := (&app{}).newRootCmd()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetArgs([]string{"--version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(output.String()); got != "nexus 0.1.0-test" {
		t.Fatalf("--version output=%q", got)
	}
}
