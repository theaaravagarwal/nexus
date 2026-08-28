package app

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

type theme struct {
	Name       string
	Background string
	Surface    string
	Elevated   string
	Text       string
	Muted      string
	Focus      string
	Live       string
	Success    string
	Warning    string
	Error      string
	Border     string
}

var themes = map[string]theme{
	"nexus":       {"nexus", "#0B0A12", "#13111C", "#1B1728", "#F4F1FF", "#A29AB8", "#A78BFA", "#5EEAD4", "#6EE7B7", "#FBBF24", "#FB7185", "#423A56"},
	"nord":        {"nord", "#2E3440", "#3B4252", "#434C5E", "#ECEFF4", "#BAC3D2", "#C3A6BE", "#88C0D0", "#A3BE8C", "#EBCB8B", "#F2A2AB", "#4C566A"},
	"dracula":     {"dracula", "#282A36", "#343746", "#44475A", "#F8F8F2", "#B8B8C7", "#BD93F9", "#8BE9FD", "#50FA7B", "#F1FA8C", "#FF7B7B", "#6272A4"},
	"catppuccin":  {"catppuccin", "#1E1E2E", "#242438", "#313244", "#CDD6F4", "#A6ADC8", "#CBA6F7", "#89DCEB", "#A6E3A1", "#F9E2AF", "#F38BA8", "#585B70"},
	"everforest":  {"everforest", "#2D353B", "#343F44", "#3D484D", "#D3C6AA", "#B3BCB4", "#D699B6", "#83C092", "#A7C080", "#DBBC7F", "#F09A9C", "#4F585E"},
	"github":      {"github", "#0D1117", "#161B22", "#21262D", "#F0F6FC", "#A8B3C2", "#D2A8FF", "#79C0FF", "#7EE787", "#E3B341", "#FF7B72", "#30363D"},
	"gruvbox":     {"gruvbox", "#282828", "#32302F", "#3C3836", "#EBDBB2", "#BDAE93", "#D3869B", "#83A598", "#B8BB26", "#FABD2F", "#FF7665", "#665C54"},
	"kanagawa":    {"kanagawa", "#1F1F28", "#2A2A37", "#363646", "#DCD7BA", "#9CABCA", "#D27E99", "#7FB4CA", "#98BB6C", "#E6C384", "#FF5D62", "#54546D"},
	"mono":        {"mono", "#101010", "#181818", "#242424", "#F2F2F2", "#B3B3B3", "#FFFFFF", "#D8D8D8", "#FFFFFF", "#C6C6C6", "#9C9C9C", "#555555"},
	"paper":       {"paper", "#F5F3FF", "#FFFFFF", "#EDE9FE", "#211A35", "#625B71", "#6D28D9", "#0F766E", "#15803D", "#8A5D00", "#BE123C", "#CBC3DC"},
	"rose-pine":   {"rose-pine", "#191724", "#1F1D2E", "#26233A", "#E0DEF4", "#908CAA", "#C4A7E7", "#9CCFD8", "#56949F", "#F6C177", "#EB6F92", "#403D52"},
	"terminal":    {"terminal", "", "", "", "252", "244", "141", "81", "42", "214", "203", "240"},
	"tokyo-night": {"tokyo-night", "#1A1B26", "#24283B", "#414868", "#C0CAF5", "#B4BDE3", "#BB9AF7", "#7DCFFF", "#9ECE6A", "#E0AF68", "#F7768E", "#3B4261"},
}

var themeDescriptions = map[string]string{
	"catppuccin":  "soft lavender",
	"dracula":     "electric violet",
	"everforest":  "forest dusk",
	"github":      "neutral contrast",
	"gruvbox":     "warm retro",
	"kanagawa":    "ink and sakura",
	"mono":        "high-contrast gray",
	"nexus":       "violet signal",
	"nord":        "polar calm",
	"paper":       "bright violet",
	"rose-pine":   "rose twilight",
	"terminal":    "terminal colors",
	"tokyo-night": "electric midnight",
}

func themeDescription(name string) string {
	return themeDescriptions[normalizeThemeName(name)]
}

func themeUsesLightCanvas(t theme) bool {
	if len(t.Background) != 7 || t.Background[0] != '#' {
		return false
	}
	red, redErr := strconv.ParseUint(t.Background[1:3], 16, 8)
	green, greenErr := strconv.ParseUint(t.Background[3:5], 16, 8)
	blue, blueErr := strconv.ParseUint(t.Background[5:7], 16, 8)
	if redErr != nil || greenErr != nil || blueErr != nil {
		return false
	}
	return (299*red + 587*green + 114*blue) >= 128_000
}

func normalizeThemeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || name == "dark" || name == "cyberpunk" {
		return "nexus"
	}
	return name
}

func themeNames() []string {
	names := make([]string, 0, len(themes))
	for name := range themes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func activeTheme() theme {
	return resolvedTheme(loadedConfig.UI.Theme)
}

func resolvedTheme(name string) theme {
	name = normalizeThemeName(name)
	t, ok := themes[name]
	if !ok {
		t = themes["nexus"]
	}
	for role, value := range loadedConfig.UI.Colors {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		switch strings.ToLower(role) {
		case "background":
			t.Background = value
		case "surface":
			t.Surface = value
		case "elevated":
			t.Elevated = value
		case "text":
			t.Text = value
		case "muted":
			t.Muted = value
		case "focus":
			t.Focus = value
		case "live":
			t.Live = value
		case "success":
			t.Success = value
		case "warning":
			t.Warning = value
		case "error":
			t.Error = value
		case "border":
			t.Border = value
		}
	}
	if loadedConfig.UI.Background == "transparent" {
		t.Background = ""
		t.Surface = ""
		t.Elevated = ""
	}
	return t
}

func validateThemeOverrides(colors map[string]string) error {
	roles := make([]string, 0, len(colors))
	for role := range colors {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	for _, role := range roles {
		value := strings.TrimSpace(colors[role])
		switch strings.ToLower(strings.TrimSpace(role)) {
		case "background", "surface", "elevated", "text", "muted", "focus", "live", "success", "warning", "error", "border":
		default:
			return fmt.Errorf("unknown ui.colors role %q", role)
		}
		if value == "" {
			continue
		}
		if len(value) == 7 && value[0] == '#' {
			if _, err := strconv.ParseUint(value[1:], 16, 24); err == nil {
				continue
			}
		}
		if index, err := strconv.Atoi(value); err == nil && index >= 0 && index <= 255 {
			continue
		}
		return fmt.Errorf("ui.colors.%s must be #RRGGBB or an ANSI color from 0 to 255", role)
	}
	return nil
}

func newThemeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "theme",
		Aliases: []string{"themes"},
		Short:   "List and preview Nexus color themes",
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			printThemeList(cmd, false)
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List built-in theme names",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			printThemeList(cmd, false)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "preview",
		Short: "Preview built-in theme colors",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			printThemeList(cmd, shouldColorHelp(cmd.OutOrStdout()))
		},
	})
	return cmd
}

func printThemeList(cmd *cobra.Command, color bool) {
	for _, name := range themeNames() {
		t := resolvedTheme(name)
		sample := themeDescription(name)
		if color {
			sample = lipgloss.NewStyle().Foreground(lipgloss.Color(t.Focus)).Render("focus") + "  " +
				lipgloss.NewStyle().Foreground(lipgloss.Color(t.Live)).Render("live") + "  " +
				lipgloss.NewStyle().Foreground(lipgloss.Color(t.Success)).Render("success") + "  " +
				lipgloss.NewStyle().Foreground(lipgloss.Color(t.Warning)).Render("warning") + "  " +
				lipgloss.NewStyle().Foreground(lipgloss.Color(t.Error)).Render("error") + "  " +
				themeDescription(name)
		}
		marker := " "
		if name == normalizeThemeName(loadedConfig.UI.Theme) {
			marker = "›"
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %-13s %s\n", marker, name, sample)
	}
}
