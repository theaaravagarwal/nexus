package app

import (
	"regexp"
	"testing"
)

func TestGetGlobalIgnoreRegex(t *testing.T) {
	regex := getGlobalIgnoreRegex()
	if regex == "" {
		t.Fatal("regex should not be empty")
	}

	// Compile to ensure valid regex
	re, err := regexp.Compile(regex)
	if err != nil {
		t.Fatalf("failed to compile regex: %v", err)
	}

	tests := []struct {
		name        string
		path        string
		shouldMatch bool
	}{
		// File extensions - should match
		{"object file", "foo.o", true},
		{"executable", "prog.exe", true},
		{"python bytecode", "module.pyc", true},
		{"cmake cache", "CMakeCache.txt", true},

		// Directory names - should match
		{"node_modules", "node_modules", true},
		{"node_modules subdir", "node_modules/x", true},
		{"bin directory", "bin", true},
		{"bin subdir", "bin/", true},
		{"build directory", "build", true},

		// False positives - should NOT match
		{"myenv not matching env", "myenv", false},
		{"notes-bin not matching bin", "notes-bin", false},
		{"src/bin should match bin", "src/bin", true}, // has bin as full component
		{"golang source", "main.go", false},
		{"python source", "script.py", false},
	}

	for _, test := range tests {
		if re.MatchString(test.path) != test.shouldMatch {
			t.Errorf("regex match for %q: got %v, want %v", test.path, !test.shouldMatch, test.shouldMatch)
		}
	}
}

func TestNormalizeDiscoveryEntry(t *testing.T) {
	tests := []struct {
		entry  string
		base   string
		want   string
		wantOK bool
	}{
		// Basic cases
		{"file.txt", ".", "file.txt", true},
		{"dir/", ".", "dir/", true},

		// Base path handling
		{"./file.txt", ".", "file.txt", true},
		{"/file.txt", ".", "/file.txt", true},

		// Relative to base
		{"subdir/file.txt", "/home/user", "subdir/file.txt", true},
		{"/home/user/file.txt", "/home/user", "file.txt", true},

		// Invalid cases
		{"", ".", "", false},
		{".", ".", "", false},
		{"/home/user", "/home/user", "", false},

		// Windows paths converted to forward slash
		{"dir\\file.txt", ".", "dir/file.txt", true},
	}

	for _, test := range tests {
		got, ok := normalizeDiscoveryEntry(test.entry, test.base)
		if ok != test.wantOK {
			t.Errorf("normalizeDiscoveryEntry(%q, %q): got ok=%v, want %v", test.entry, test.base, ok, test.wantOK)
		}
		if got != test.want {
			t.Errorf("normalizeDiscoveryEntry(%q, %q): got %q, want %q", test.entry, test.base, got, test.want)
		}
	}
}

func TestRemoteJoinPath(t *testing.T) {
	tests := []struct {
		current string
		child   string
		want    string
	}{
		{"/home/user", "file.txt", "/home/user/file.txt"},
		{"/home/user/", "file.txt", "/home/user/file.txt"},
		{"/home/user", "subdir/file.txt", "/home/user/subdir/file.txt"},
		{"/home/user", "", "/home/user"},
		{"/home/user", ".", "/home/user"},
		{".", "file.txt", "file.txt"},
		{"/", "file.txt", "file.txt"},
		{"", "file.txt", "file.txt"},
		{"/home", "/absolute/path", "/absolute/path"},
	}

	for _, test := range tests {
		got := remoteJoinPath(test.current, test.child)
		if got != test.want {
			t.Errorf("remoteJoinPath(%q, %q): got %q, want %q", test.current, test.child, got, test.want)
		}
	}
}

func TestEnsureWindowsRemotePath(t *testing.T) {
	tests := []struct {
		host       string
		remotePath string
		check      func(string) bool // Check the result
	}{
		{"user@windows-host", "file.txt", func(p string) bool { return p != "" }},
		{"user@windows-host", "/home/user/file.txt", func(p string) bool { return p == "/home/user/file.txt" }},
		{"user@windows-host", "", func(p string) bool { return p != "" }},  // Should return default base
		{"user@windows-host", ".", func(p string) bool { return p != "" }}, // Should return default base
	}

	for _, test := range tests {
		got := ensureWindowsRemotePath(test.host, test.remotePath)
		if !test.check(got) {
			t.Errorf("ensureWindowsRemotePath(%q, %q): got %q", test.host, test.remotePath, got)
		}
	}
}

func TestBuildRsyncArgsIncludesProtectArgs(t *testing.T) {
	opts := rsyncOptions{sshPort: 22, dryRun: false, forceRemoteRsyncPath: false, stabilityProfile: false}
	args := buildRsyncArgs("rsync", "/local/src", "user@host:/remote/dst", opts)

	hasProtectArgs := false
	for _, arg := range args {
		if arg == "--protect-args" {
			hasProtectArgs = true
			break
		}
	}

	if !hasProtectArgs {
		t.Errorf("buildRsyncArgs: --protect-args flag not found in args: %v", args)
	}
}

func TestMergeIgnorePatternsWithEmptyGlobal(t *testing.T) {
	// Test the guard for empty globalRegex
	// Use escaped patterns (as they would be from parseGitignorePatterns)
	escapedPatterns := []string{regexp.QuoteMeta("*.log"), regexp.QuoteMeta("tmp")}
	result := mergeIgnorePatterns("", escapedPatterns)
	if result == "" {
		t.Errorf("mergeIgnorePatterns with empty global should not return empty string")
	}

	// Verify it's a valid regex
	if _, err := regexp.Compile(result); err != nil {
		t.Errorf("mergeIgnorePatterns result is not valid regex: %v", err)
	}
}
