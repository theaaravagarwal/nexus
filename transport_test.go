package main

import (
	"slices"
	"strings"
	"testing"
)

func TestBuildSSHArgsSeparatesPortFromDestination(t *testing.T) {
	args, err := buildSSHArgs("alice@example.com:2222", false, "uname -a")
	if err != nil {
		t.Fatal(err)
	}
	if !containsPair(args, "-p", "2222") {
		t.Fatalf("SSH args do not contain validated port pair: %v", args)
	}
	if slices.Contains(args, "alice@example.com:2222") {
		t.Fatalf("port leaked into SSH destination: %v", args)
	}
	if !slices.Contains(args, "alice@example.com") || !slices.Contains(args, "uname -a") {
		t.Fatalf("SSH destination/command missing: %v", args)
	}
	if !slices.Contains(args, "-T") {
		t.Fatalf("batch SSH missing -T: %v", args)
	}
}

func TestBuildSSHArgsInteractiveTTY(t *testing.T) {
	args, err := buildSSHArgs("alice@example.com", true, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(args) < 2 || args[0] != "-t" || args[1] != "-t" {
		t.Fatalf("interactive SSH does not force a TTY: %v", args)
	}
}

func TestFormatRemoteEndpointIPv6AndPort(t *testing.T) {
	endpoint, err := formatRemoteEndpoint("alice@[2001:db8::1]:2222", "/tmp/a b", false)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "alice@[2001:db8::1]:/tmp/a b" {
		t.Fatalf("endpoint=%q", endpoint)
	}
}

func TestBuildRsyncArgsPropagatesPortAndDryRun(t *testing.T) {
	args := buildRsyncArgs("/not/a/real/rsync", "source", "destination", rsyncOptions{
		sshPort: 2222,
		dryRun:  true,
	})
	index := slices.Index(args, "-e")
	if index < 0 || index+1 >= len(args) {
		t.Fatalf("rsync args missing remote shell: %v", args)
	}
	if !strings.Contains(args[index+1], "-p 2222") {
		t.Fatalf("rsync remote shell missing port: %v", args)
	}
	if !slices.Contains(args, "--dry-run") {
		t.Fatalf("rsync args missing dry-run: %v", args)
	}
}

func TestNormalizeHostHistoryDedupesDefaultPort(t *testing.T) {
	got := normalizeHostHistory([]string{"alice@example.com", "alice@example.com:22", "alice@example.com:2222"})
	want := []string{"alice@example.com", "alice@example.com:2222"}
	if !slices.Equal(got, want) {
		t.Fatalf("normalizeHostHistory()=%v want %v", got, want)
	}
}

func TestProfileForHostIgnoresUserAndPort(t *testing.T) {
	previous := hostDiscoveryProfiles
	t.Cleanup(func() { hostDiscoveryProfiles = previous })
	hostDiscoveryProfiles = map[string]discoveryProfile{
		"example.com": {RsyncStability: true},
	}
	if !profileForHost("alice@example.com:2222").RsyncStability {
		t.Fatal("profile lookup did not ignore user and port")
	}
}

func containsPair(items []string, first, second string) bool {
	for i := 0; i+1 < len(items); i++ {
		if items[i] == first && items[i+1] == second {
			return true
		}
	}
	return false
}
