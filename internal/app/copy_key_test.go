package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBuildSSHCopyIDArgs(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   []string
	}{
		{
			name:   "default port",
			target: "alice@example.com",
			want:   []string{"alice@example.com"},
		},
		{
			name:   "nondefault port",
			target: "alice@example.com:6023",
			want:   []string{"-p", "6023", "alice@example.com"},
		},
		{
			name:   "IPv6",
			target: "alice@[2001:db8::1]:6023",
			want:   []string{"-p", "6023", "alice@2001:db8::1"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := buildSSHCopyIDArgs(test.target)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("buildSSHCopyIDArgs(%q)=%q want %q", test.target, got, test.want)
			}
		})
	}
}

func TestRunSSHCopyIDUsesDiscoveredBinary(t *testing.T) {
	installFakeSSHCopyID(t, "exit 0\n")
	if err := runSSHCopyID("alice@example.com:6023"); err != nil {
		t.Fatalf("runSSHCopyID() error=%v", err)
	}
}

func TestRunSSHCopyIDReportsCommandFailure(t *testing.T) {
	installFakeSSHCopyID(t, "exit 7\n")
	err := runSSHCopyID("alice@example.com")
	if err == nil || !strings.Contains(err.Error(), "ssh-copy-id failed") {
		t.Fatalf("runSSHCopyID() error=%v", err)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("runSSHCopyID() did not preserve exit status: %v", err)
	}
}

func TestRunSSHCopyIDReportsMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := runSSHCopyID("alice@example.com")
	if err == nil || !strings.Contains(err.Error(), "ssh-copy-id not found in PATH") {
		t.Fatalf("runSSHCopyID() error=%v", err)
	}
}

func installFakeSSHCopyID(t *testing.T, body string) {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "ssh-copy-id")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
}
