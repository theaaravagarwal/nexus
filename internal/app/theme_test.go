package app

import (
	"strings"
	"testing"
)

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
