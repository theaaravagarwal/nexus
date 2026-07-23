package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildRemoteProbeScriptPreservesFallbackCommands(t *testing.T) {
	script, commands := buildRemoteProbeScript([]string{"fastfetch", "neofetch --off", "", "fastfetch --logo"})
	if !strings.Contains(script, "command -v") {
		t.Fatalf("script=%q", script)
	}
	want := map[string]string{"fastfetch": "fastfetch", "neofetch": "neofetch --off"}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands=%v want %v", commands, want)
	}
}

func TestRequiresElevatedNetworkAccess(t *testing.T) {
	if !requiresElevatedNetworkAccess("iftop -n") {
		t.Fatal("iftop should require elevation")
	}
	if requiresElevatedNetworkAccess("speedtest-cli") {
		t.Fatal("speedtest-cli should not require elevation")
	}
}

func TestRemoteShellCommandDoesNotLoadLoginProfiles(t *testing.T) {
	got := remoteShellCommand("sh", "command -v fastfetch")
	if strings.Contains(got, " -l") {
		t.Fatalf("internal probe unexpectedly uses a login shell: %q", got)
	}
	if !strings.HasPrefix(got, "sh -c ") {
		t.Fatalf("command=%q", got)
	}
}

func TestRemoteProbeTreatsShellCommandMissAsFallback(t *testing.T) {
	err := exec.Command("sh", "-c", "exit 127").Run()
	if !isRemoteProbeMiss(err) {
		t.Fatalf("exit 127 should select the portable fallback: %v", err)
	}
}

func TestProbeSkipsLoginProfilesAndFallsBackWhenToolsAreMissing(t *testing.T) {
	binDir := t.TempDir()
	fakeSSH := filepath.Join(binDir, "ssh")
	script := `#!/bin/sh
case "$*" in
  *" -lc "*) echo "login shell requested" >&2; exit 2 ;;
esac
exit 127
`
	if err := os.WriteFile(fakeSSH, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	_, err := resolveRemoteProbeCommands("alice@example.com", []string{"fastfetch"})
	if !errors.Is(err, errNoRemoteProbeCandidates) {
		t.Fatalf("probe error=%v", err)
	}
}
