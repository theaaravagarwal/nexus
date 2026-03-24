package hosts

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	t.Parallel()

	valid := []string{
		"alice@127.0.0.1",
		"alice@example.com",
		"alice@[2001:db8::1]",
		"alice@2001:db8::1",
		"alice-name_1@sub.domain.local",
	}
	for _, tc := range valid {
		tc := tc
		t.Run(tc, func(t *testing.T) {
			t.Parallel()
			if err := Validate(tc); err != nil {
				t.Fatalf("Validate(%q) returned error: %v", tc, err)
			}
		})
	}

	invalid := []string{
		"alice",
		"alice@@host",
		"@host",
		"alice@",
		"ali ce@host",
		"alice@not_a_host",
	}
	for _, tc := range invalid {
		tc := tc
		t.Run(tc, func(t *testing.T) {
			t.Parallel()
			if err := Validate(tc); err == nil {
				t.Fatalf("Validate(%q) expected error, got nil", tc)
			}
		})
	}
}

func TestLoadSupportsLegacyAndPayloadFormats(t *testing.T) {
	t.Parallel()

	t.Run("legacy array", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "hosts.json")
		if err := os.WriteFile(path, []byte("[\"a@x\", \"a@x\", \"b@y\"]\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		s := &Store{path: path}
		got, err := s.Load()
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		want := []string{"a@x", "b@y"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Load()=%v want %v", got, want)
		}
	})

	t.Run("payload object", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "hosts.json")
		if err := os.WriteFile(path, []byte("{\"hosts\":[\"a@x\",\"\",\"a@x\",\"b@y\"]}"), 0o644); err != nil {
			t.Fatal(err)
		}

		s := &Store{path: path}
		got, err := s.Load()
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		want := []string{"a@x", "b@y"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Load()=%v want %v", got, want)
		}
	})
}

func TestSaveAddRemoveRoundTrip(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "nested", "hosts.json")
	s := &Store{path: path}

	if err := s.Save([]string{"a@x", "a@x", "", "b@y"}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if !strings.Contains(string(content), "\"hosts\"") {
		t.Fatalf("saved content does not look like payload: %s", string(content))
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if want := []string{"a@x", "b@y"}; !reflect.DeepEqual(loaded, want) {
		t.Fatalf("Load()=%v want %v", loaded, want)
	}

	added, err := s.Add("c@z")
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if !added {
		t.Fatal("Add() expected added=true")
	}

	addedAgain, err := s.Add("c@z")
	if err != nil {
		t.Fatalf("Add() duplicate error: %v", err)
	}
	if addedAgain {
		t.Fatal("Add() duplicate expected added=false")
	}

	removed, err := s.Remove("c@z")
	if err != nil {
		t.Fatalf("Remove() error: %v", err)
	}
	if !removed {
		t.Fatal("Remove() expected removed=true")
	}

	removedAgain, err := s.Remove("c@z")
	if err != nil {
		t.Fatalf("Remove() missing error: %v", err)
	}
	if removedAgain {
		t.Fatal("Remove() missing expected removed=false")
	}
}

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	t.Parallel()

	s := &Store{path: filepath.Join(t.TempDir(), "missing.json")}
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Load() expected empty list, got %v", got)
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "hosts.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Store{path: path}
	_, err := s.Load()
	if err == nil {
		t.Fatal("Load() expected error for invalid json")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("Load() error=%v, expected invalid parse context", err)
	}
}

func TestNewDefaultStorePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	s, err := NewDefaultStore()
	if err != nil {
		t.Fatalf("NewDefaultStore() error: %v", err)
	}
	want := filepath.Join(home, ".config", appConfigDir, fileName)
	if s.path != want {
		t.Fatalf("store path=%q want %q", s.path, want)
	}
}

func TestAddRejectsInvalidHost(t *testing.T) {
	t.Parallel()

	s := &Store{path: filepath.Join(t.TempDir(), "hosts.json")}
	added, err := s.Add("invalid")
	if err == nil {
		t.Fatal("Add() expected validation error")
	}
	if added {
		t.Fatal("Add() expected added=false on validation error")
	}
}
