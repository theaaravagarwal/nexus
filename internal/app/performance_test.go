package app

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestRsyncVersionSupportsSkipCompress(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"rsync  version 3.2.7  protocol version 31", true},
		{"rsync version 3.1.0 protocol version 31", true},
		{"rsync version 3.0.9 protocol version 30", false},
		{"rsync version 2.6.9 protocol version 29", false},
		{"unknown", false},
	}
	for _, tc := range tests {
		if got := rsyncVersionSupportsSkipCompress(tc.text); got != tc.want {
			t.Fatalf("rsyncVersionSupportsSkipCompress(%q)=%v want %v", tc.text, got, tc.want)
		}
	}
}

func TestRsyncVersionSupportsProtectArgs(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"rsync  version 3.2.7  protocol version 31", true},
		{"rsync version 3.1.0 protocol version 31", true},
		{"rsync version 3.0.0 protocol version 30", true},
		{"rsync version 2.6.9 protocol version 29", false},
		{"openrsync version 1.2.3", false},
		{"openrsync (OpenBSD version 1.2.3)", false},
		{"unknown", false},
	}
	for _, tc := range tests {
		if got := rsyncVersionSupportsProtectArgs(tc.text); got != tc.want {
			t.Fatalf("rsyncVersionSupportsProtectArgs(%q)=%v want %v", tc.text, got, tc.want)
		}
	}
}

func TestRsyncCapabilityCaching(t *testing.T) {
	// Seed the cache with a known value for a nonexistent binary
	nonexistentBinary := "/nonexistent/rsync-binary-" + strconv.Itoa(os.Getpid())

	// Pre-populate cache
	rsyncSkipCache.Store(nonexistentBinary, true)
	rsyncProtectCache.Store(nonexistentBinary, false)

	// These should return cached values without attempting to execute
	if !rsyncSupportsSkipCompress(nonexistentBinary) {
		t.Fatal("rsyncSupportsSkipCompress should use cached value")
	}
	if rsyncSupportsProtectArgs(nonexistentBinary) {
		t.Fatal("rsyncSupportsProtectArgs should use cached value")
	}

	// Verify that if we cleared the cache, it would fail
	rsyncSkipCache.Delete(nonexistentBinary)
	rsyncProtectCache.Delete(nonexistentBinary)
}

func TestEnsurePrivateDirectoryRejectsPermissiveExistingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mux")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateDirectory(path); err == nil {
		t.Fatal("expected permissive existing directory to be rejected")
	}
}
