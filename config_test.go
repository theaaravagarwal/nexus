package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAppConfigPreservesLegacyAndAddsUIDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := `full_index_depth: 7
fzf:
  theme: cyberpunk
  layout: reverse
host_profiles:
  example.com:
    use_unix_discovery: true
    rsync_stability: true
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadAppConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Theme != "nexus" || cfg.FullIndexDepth != 7 {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
	if cfg.FZF.Theme != "cyberpunk" {
		t.Fatalf("legacy fzf theme=%q", cfg.FZF.Theme)
	}
	if !cfg.HostProfiles["example.com"].RsyncStability {
		t.Fatal("legacy host profile lost")
	}
}

func TestCommandsForTargetOverrideGlobalTagAndHost(t *testing.T) {
	previous := loadedConfig
	t.Cleanup(func() { loadedConfig = previous })
	loadedConfig = defaultAppConfig()
	loadedConfig.Commands = []commandConfig{{Name: "logs", Command: "global"}, {Name: "uptime", Command: "uptime"}}
	loadedConfig.TagCommands["prod"] = []commandConfig{{Name: "logs", Command: "tag"}}
	loadedConfig.HostProfiles["a@example.com:2222"] = discoveryProfile{
		Tags:     []string{"prod"},
		Commands: []commandConfig{{Name: "logs", Command: "host"}},
	}
	got := commandsForTarget("a@example.com:2222")
	if len(got) != 2 || got[0].Command != "host" || got[1].Name != "uptime" {
		t.Fatalf("commands=%#v", got)
	}
}

func TestCommandConfigSupportsInteractiveTerminalOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := `commands:
  - name: tmux attach
    description: Attach to tmux
    command: tmux attach
    interactive: true
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadAppConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Commands) != 1 || !cfg.Commands[0].Interactive {
		t.Fatalf("interactive command was not preserved: %#v", cfg.Commands)
	}
}

func TestEnsureConfigFileUsesPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nexus", "config.yaml")
	if err := ensureConfigFile(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode=%o", got)
	}
}

func TestEnsureConfigFileAddsExamplesToExistingConfigOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("full_index_depth: 7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureConfigFile(path); err != nil {
		t.Fatal(err)
	}
	if err := ensureConfigFile(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(raw), "# Saved command examples"); count != 1 {
		t.Fatalf("example marker count=%d:\n%s", count, raw)
	}
	if count := strings.Count(string(raw), "# Add interactive: true"); count != 1 {
		t.Fatalf("interactive note count=%d:\n%s", count, raw)
	}
	cfg, err := loadAppConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FullIndexDepth != 7 {
		t.Fatalf("existing setting changed: %#v", cfg)
	}
}

func TestSaveThemeToConfigPreservesSettingsAndComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := `# keep this note
full_index_depth: 7
ui:
  theme: nexus
  background: transparent
commands:
  - name: uptime
    description: Show uptime
    command: uptime
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveThemeToConfig(path, "nord"); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadAppConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Theme != "nord" || cfg.UI.Background != "transparent" || cfg.FullIndexDepth != 7 ||
		len(cfg.Commands) != 1 || cfg.Commands[0].Command != "uptime" {
		t.Fatalf("settings changed while saving theme: %#v", cfg)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "# keep this note") {
		t.Fatalf("comment was lost:\n%s", saved)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode=%o", got)
	}
}

func TestDefaultConfigIncludesSavedCommandExamples(t *testing.T) {
	config := defaultConfigYAML()
	for _, want := range []string{"choose Theme preview", "disk usage", "journalctl -u app", "exact user@host:port"} {
		if !strings.Contains(config, want) {
			t.Fatalf("default config missing %q", want)
		}
	}
}

func TestSanitizeTerminalTextRemovesControlSequences(t *testing.T) {
	got := sanitizeTerminalText("prod\x1b]52;c;secret\a\nweb")
	if got != "prod ]52;c;secret web" {
		t.Fatalf("got=%q", got)
	}
}

func TestLoadAppConfigRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("ui:\n  them: nord\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAppConfig(path); err == nil {
		t.Fatal("expected typo in config field to be rejected")
	}
}

func TestProfilesKeepPortSpecificOverrides(t *testing.T) {
	previous := loadedConfig
	t.Cleanup(func() { loadedConfig = previous })
	loadedConfig = defaultAppConfig()
	loadedConfig.HostProfiles["example.com"] = discoveryProfile{Alias: "default"}
	loadedConfig.HostProfiles["alice@example.com:2222"] = discoveryProfile{Alias: "alternate"}
	if got := profileForTarget("alice@example.com:2222").Alias; got != "alternate" {
		t.Fatalf("port-specific alias=%q", got)
	}
	if got := profileForTarget("alice@example.com").Alias; got != "default" {
		t.Fatalf("default alias=%q", got)
	}
	if got := profileForTarget("bob@example.com:2222").Alias; got != "default" {
		t.Fatalf("bare-host fallback alias=%q", got)
	}
}

func TestCommandsRejectTerminalControlCharacters(t *testing.T) {
	if got := sanitizeCommandText("uptime\x1b[2J"); got != "" {
		t.Fatalf("unsafe command was retained: %q", got)
	}
}
