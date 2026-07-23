package main

import "testing"

func TestParseConnectionTarget(t *testing.T) {
	tests := []struct {
		input string
		want  connectionTarget
		text  string
	}{
		{"alice@example.com", connectionTarget{"alice", "example.com", 22}, "alice@example.com"},
		{"alice@example.com:2222", connectionTarget{"alice", "example.com", 2222}, "alice@example.com:2222"},
		{"root@192.0.2.5:2200", connectionTarget{"root", "192.0.2.5", 2200}, "root@192.0.2.5:2200"},
		{"alice@[2001:db8::1]", connectionTarget{"alice", "2001:db8::1", 22}, "alice@[2001:db8::1]"},
		{"alice@[2001:db8::1]:2200", connectionTarget{"alice", "2001:db8::1", 2200}, "alice@[2001:db8::1]:2200"},
		{"alice@2001:db8::1", connectionTarget{"alice", "2001:db8::1", 22}, "alice@[2001:db8::1]"},
		{"alice@example.com:22", connectionTarget{"alice", "example.com", 22}, "alice@example.com"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseConnectionTarget(tc.input)
			if err != nil {
				t.Fatalf("parseConnectionTarget() error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("parseConnectionTarget()=%#v want %#v", got, tc.want)
			}
			if got.String() != tc.text {
				t.Fatalf("String()=%q want %q", got.String(), tc.text)
			}
		})
	}
}

func TestParseConnectionTargetRejectsInvalidInputs(t *testing.T) {
	inputs := []string{
		"alice",
		"alice@@host",
		"@host",
		"alice@",
		"alice@host:0",
		"alice@host:65536",
		"alice@host:ssh",
		"alice@[2001:db8::1",
		"alice@[example.com]:22",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			if _, err := parseConnectionTarget(input); err == nil {
				t.Fatalf("parseConnectionTarget(%q) expected error", input)
			}
		})
	}
}

func TestCanonicalConnectionTargetPortOverride(t *testing.T) {
	got, err := canonicalConnectionTarget("alice@example.com:2200", 2022)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "alice@example.com:2022" {
		t.Fatalf("override target=%q", got.String())
	}
}
