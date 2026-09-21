package app

import (
	"errors"
	"os/exec"
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

func TestBuildMonitoringSSHArgsUseCompressedFastFailProfile(t *testing.T) {
	args, err := buildMonitoringSSHArgs("alice@example.com", false, "uptime")
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{
		{"-o", "ConnectTimeout=3"},
		{"-o", "BatchMode=yes"},
		{"-o", "ConnectionAttempts=1"},
		{"-o", "PreferredAuthentications=publickey"},
		{"-o", "GSSAPIAuthentication=no"},
		{"-o", "Compression=yes"},
	} {
		if !containsPair(args, pair[0], pair[1]) {
			t.Fatalf("monitoring SSH args missing %q: %v", pair[1], args)
		}
	}
	if !containsPair(args, "-o", "StrictHostKeyChecking=yes") {
		t.Fatalf("monitoring SSH args missing strict host-key checking: %v", args)
	}
	if slices.Contains(args, "StrictHostKeyChecking=accept-new") {
		t.Fatalf("monitoring SSH args unexpectedly allow automatic host-key enrollment: %v", args)
	}
}

func TestBuildMonitoringSSHArgsUseStrictHostKeyCheckingForPTY(t *testing.T) {
	args, err := buildMonitoringSSHArgs("alice@example.com", true, "btop")
	if err != nil {
		t.Fatal(err)
	}
	if !containsPair(args, "-o", "StrictHostKeyChecking=yes") {
		t.Fatalf("PTY monitoring SSH args missing strict host-key checking: %v", args)
	}
	if slices.Contains(args, "StrictHostKeyChecking=accept-new") {
		t.Fatalf("PTY monitoring SSH args unexpectedly allow automatic host-key enrollment: %v", args)
	}
}

func TestBuildSSHArgsDropsQuietOnlyForInteractiveMonitoring(t *testing.T) {
	pty, err := buildMonitoringSSHArgs("alice@example.com", true, "btop")
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(pty, "-q") {
		t.Fatalf("PTY monitoring stream must keep ssh diagnostics on stderr (no -q): %v", pty)
	}

	batch, err := buildMonitoringSSHArgs("alice@example.com", false, "uptime")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(batch, "-q") {
		t.Fatalf("non-interactive monitoring SSH args should still be quiet: %v", batch)
	}

	interactiveDefault, err := buildSSHArgs("alice@example.com", true, "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(interactiveDefault, "-q") {
		t.Fatalf("default-profile interactive SSH args should still be quiet: %v", interactiveDefault)
	}

	nonInteractiveDefault, err := buildSSHArgs("alice@example.com", false, "uptime")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(nonInteractiveDefault, "-q") {
		t.Fatalf("default-profile non-interactive SSH args should still be quiet: %v", nonInteractiveDefault)
	}
}

func TestBuildSSHArgsKeepsDefaultNoninteractiveHostKeyBehavior(t *testing.T) {
	args, err := buildSSHArgs("alice@example.com", false, "uptime")
	if err != nil {
		t.Fatal(err)
	}
	if !containsPair(args, "-o", "StrictHostKeyChecking=accept-new") {
		t.Fatalf("default noninteractive SSH args changed host-key behavior: %v", args)
	}
	if slices.Contains(args, "StrictHostKeyChecking=yes") {
		t.Fatalf("default noninteractive SSH args unexpectedly use monitoring host-key policy: %v", args)
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
	previous := loadedConfig
	t.Cleanup(func() { loadedConfig = previous })
	loadedConfig = defaultAppConfig()
	loadedConfig.HostProfiles["example.com"] = discoveryProfile{RsyncStability: true}
	if !profileForHost("alice@example.com:2222").RsyncStability {
		t.Fatal("profile lookup did not ignore user and port")
	}
}

func TestRemoteShellExitIsNotAConnectionFailure(t *testing.T) {
	err := exec.Command("sh", "-c", "exit 1").Run()
	if !isRemoteShellExit(err) {
		t.Fatalf("remote shell status should be treated as a completed connection: %v", err)
	}
	transportErr := exec.Command("sh", "-c", "exit 255").Run()
	if isRemoteShellExit(transportErr) {
		t.Fatalf("transport status 255 was treated as a remote shell exit: %v", transportErr)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("test setup produced %v", err)
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
