package main

import (
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
