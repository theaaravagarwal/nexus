package app

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestBuildFZFArgsUsesSanitizedTheme(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatal(err)
	}
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

func TestFZFThemeUsesFullCanvasAndLightBase(t *testing.T) {
	previous := loadedConfig
	loadedConfig = defaultAppConfig()
	t.Cleanup(func() { loadedConfig = previous })

	light := fzfColorByTheme("paper")
	for _, want := range []string{
		"light,", "fg:#211A35", "bg:#F5F3FF", "bg+:#EDE9FE", "gutter:#F5F3FF",
		"border:#CBC3DC", "prompt:#6D28D9",
	} {
		if !strings.Contains(light, want) {
			t.Fatalf("paper FZF theme missing %q: %s", want, light)
		}
	}

	dark := fzfColorByTheme("nexus")
	for _, want := range []string{"dark,", "bg:#0B0A12", "bg+:#1B1728", "border:#423A56"} {
		if !strings.Contains(dark, want) {
			t.Fatalf("nexus FZF theme missing %q: %s", want, dark)
		}
	}

	loadedConfig.UI.Background = "transparent"
	transparent := fzfColorByTheme("paper")
	if !strings.HasPrefix(transparent, "light,") || strings.Contains(transparent, "bg:") || strings.Contains(transparent, "bg+:") {
		t.Fatalf("transparent paper FZF theme forced a canvas: %s", transparent)
	}
}
