package main

import "testing"

func TestParseMetadataSanitizesAndSelectsKnownFields(t *testing.T) {
	got := parseMetadata("OS=Ubuntu 24.04\nCPU=Example\x1b[31m\nTOOLS=btop,duf\nSECRET=nope\n")
	if got["OS"] != "Ubuntu 24.04" || got["CPU"] != "Example [31m" || got["TOOLS"] != "btop,duf" {
		t.Fatalf("got=%#v", got)
	}
	if _, exists := got["SECRET"]; exists {
		t.Fatal("unknown field accepted")
	}
}
