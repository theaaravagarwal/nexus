package main

import (
	"slices"
	"testing"
)

func TestBuildFZFArgsUsesSanitizedTheme(t *testing.T) {
	previous := fzfUIConfig
	t.Cleanup(func() { fzfUIConfig = previous })
	fzfUIConfig = sanitizeFZFConfig(fzfConfig{Theme: "cyberpunk", Layout: "reverse", Pointer: ">"})
	args := buildFZFArgs("host> ")
	if !slices.Contains(args, "dark,fg+:51,hl:45,pointer:51,marker:45,header:207") {
		t.Fatalf("args=%v", args)
	}
	if !slices.Contains(args, ">") {
		t.Fatalf("args=%v", args)
	}
}
