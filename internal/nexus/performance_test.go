package nexus

import (
	"os"
	"path/filepath"
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

func TestEnsurePrivateDirectoryRejectsPermissiveExistingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mux")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateDirectory(path); err == nil {
		t.Fatal("expected permissive existing directory to be rejected")
	}
}
