package app

import (
	"bytes"
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestBuiltInThemeLibraryAndDescriptions(t *testing.T) {
	want := []string{
		"amber", "ayu-mirage", "catppuccin", "dracula", "everforest", "github", "gruvbox",
		"iceberg", "kanagawa", "mono", "nexus", "nord", "paper", "rose-pine",
		"solarized-dark", "solarized-light", "synthwave", "terminal", "tokyo-night", "vesper",
	}
	if got := themeNames(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("theme names=%v want=%v", got, want)
	}
	for _, name := range want {
		if themeDescription(name) == "" {
			t.Fatalf("theme %q has no picker description", name)
		}
	}
	if !themeUsesLightCanvas(themes["paper"]) || themeUsesLightCanvas(themes["nexus"]) {
		t.Fatal("light/dark canvas detection is incorrect")
	}
}

func TestThemeListIncludesPaletteDescriptions(t *testing.T) {
	previous := loadedConfig
	loadedConfig = defaultAppConfig()
	t.Cleanup(func() { loadedConfig = previous })

	var output bytes.Buffer
	cmd := newThemeCmd()
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"nexus            violet signal", "paper            bright violet", "solarized-light  balanced daylight"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("theme list missing %q:\n%s", want, output.String())
		}
	}
}

func TestOpaqueThemeTextRolesMeetContrastFloor(t *testing.T) {
	for name, palette := range themes {
		if palette.Surface == "" {
			continue
		}
		for role, color := range map[string]string{
			"text": palette.Text, "muted": palette.Muted, "focus": palette.Focus,
			"live": palette.Live, "success": palette.Success, "warning": palette.Warning,
			"error": palette.Error,
		} {
			if ratio := testContrastRatio(palette.Surface, color); ratio < 4.5 {
				t.Errorf("theme=%s role=%s surface contrast=%.2f", name, role, ratio)
			}
		}
		for role, color := range map[string]string{"text": palette.Text, "muted": palette.Muted} {
			if ratio := testContrastRatio(palette.Elevated, color); ratio < 4.5 {
				t.Errorf("theme=%s selected-%s contrast=%.2f", name, role, ratio)
			}
		}
	}
}

func testContrastRatio(left, right string) float64 {
	leftLuminance := testRelativeLuminance(left)
	rightLuminance := testRelativeLuminance(right)
	return (math.Max(leftLuminance, rightLuminance) + 0.05) /
		(math.Min(leftLuminance, rightLuminance) + 0.05)
}

func testRelativeLuminance(color string) float64 {
	channels := make([]float64, 3)
	for index := range channels {
		value, err := strconv.ParseUint(color[1+index*2:3+index*2], 16, 8)
		if err != nil {
			return 0
		}
		channel := float64(value) / 255
		if channel <= 0.04045 {
			channels[index] = channel / 12.92
		} else {
			channels[index] = math.Pow((channel+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*channels[0] + 0.7152*channels[1] + 0.0722*channels[2]
}

func TestResolvedThemeTransparentClearsEverySurface(t *testing.T) {
	previous := loadedConfig
	t.Cleanup(func() { loadedConfig = previous })
	loadedConfig = defaultAppConfig()
	loadedConfig.UI.Background = "transparent"

	resolved := resolvedTheme("dracula")
	if resolved.Background != "" || resolved.Surface != "" || resolved.Elevated != "" {
		t.Fatalf("transparent theme retained a painted surface: %#v", resolved)
	}

	model := newDashboardModel([]string{"alice@one"})
	model.plain = false
	model.theme = resolved
	styles := model.styles()
	if !styles.selected.GetReverse() || !styles.key.GetUnderline() {
		t.Fatalf("transparent selection lost its non-color fallback: selected=%#v key=%#v", styles.selected, styles.key)
	}
	if colors := fzfColorByTheme("dracula"); strings.Contains(colors, "bg+:") {
		t.Fatalf("transparent FZF theme emitted an empty highlight background: %q", colors)
	}
}

func TestResolvedThemeAppliesSemanticOverrides(t *testing.T) {
	previous := loadedConfig
	t.Cleanup(func() { loadedConfig = previous })
	loadedConfig = defaultAppConfig()
	loadedConfig.UI.Colors = map[string]string{
		"focus": "#112233",
		"live":  "45",
	}

	resolved := resolvedTheme("nord")
	if resolved.Focus != "#112233" || resolved.Live != "45" {
		t.Fatalf("semantic overrides were not applied: %#v", resolved)
	}
}

func TestValidateThemeOverridesRejectsUnknownOrInvalidValues(t *testing.T) {
	tests := []map[string]string{
		{"accent": "#112233"},
		{"focus": "#xyzxyz"},
		{"focus": "256"},
		{"focus": "-1"},
	}
	for _, colors := range tests {
		if err := validateThemeOverrides(colors); err == nil {
			t.Fatalf("accepted invalid theme overrides: %#v", colors)
		}
	}
	for _, colors := range []map[string]string{
		{"focus": "#A78BFA"},
		{"focus": "0"},
		{"focus": "255"},
		{"focus": ""},
	} {
		if err := validateThemeOverrides(colors); err != nil {
			t.Fatalf("rejected valid theme overrides %#v: %v", colors, err)
		}
	}
}
