package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type commandConfig struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Command     string `yaml:"command"`
}

type uiConfig struct {
	Theme      string            `yaml:"theme"`
	Background string            `yaml:"background"`
	Colors     map[string]string `yaml:"colors,omitempty"`
}

type reachabilityConfig struct {
	Enabled      *bool `yaml:"enabled,omitempty"`
	TimeoutMS    int   `yaml:"timeout_ms"`
	Concurrency  int   `yaml:"concurrency"`
	CacheSeconds int   `yaml:"cache_seconds"`
}

type discoveryProfile struct {
	Alias            string          `yaml:"alias,omitempty"`
	Tags             []string        `yaml:"tags,omitempty"`
	OS               string          `yaml:"os,omitempty"`
	Commands         []commandConfig `yaml:"commands,omitempty"`
	UseUnixDiscovery bool            `yaml:"use_unix_discovery"`
	RsyncStability   bool            `yaml:"rsync_stability"`
}

type appConfig struct {
	FullIndexDepth int                         `yaml:"full_index_depth"`
	UI             uiConfig                    `yaml:"ui"`
	Reachability   reachabilityConfig          `yaml:"reachability"`
	FZF            fzfConfig                   `yaml:"fzf"`
	Commands       []commandConfig             `yaml:"commands,omitempty"`
	TagCommands    map[string][]commandConfig  `yaml:"tag_commands,omitempty"`
	HostProfiles   map[string]discoveryProfile `yaml:"host_profiles,omitempty"`
}

var loadedConfig = defaultAppConfig()

func defaultAppConfig() appConfig {
	enabled := true
	return appConfig{
		FullIndexDepth: defaultFullIndexDepth,
		UI: uiConfig{
			Theme:      "nexus",
			Background: "opaque",
		},
		Reachability: reachabilityConfig{
			Enabled:      &enabled,
			TimeoutMS:    1500,
			Concurrency:  8,
			CacheSeconds: 30,
		},
		FZF:          defaultFZFConfig(),
		TagCommands:  map[string][]commandConfig{},
		HostProfiles: map[string]discoveryProfile{},
	}
}

func ensureConfigFile(configPath string) error {
	if configPath == "" {
		return errors.New("config path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	info, err := os.Stat(configPath)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("config path is a directory: %s", configPath)
		}
		if info.Mode().Perm()&0o077 != 0 {
			if err := os.Chmod(configPath, 0o600); err != nil {
				return fmt.Errorf("failed to protect config file: %w", err)
			}
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to inspect config file: %w", err)
	}
	return os.WriteFile(configPath, []byte(defaultConfigYAML()), 0o600)
}

func defaultConfigYAML() string {
	return `# Nexus settings
# Open this file with: nexus config
# Preview themes with: nexus theme preview

full_index_depth: 5

ui:
  # nexus | nord | dracula | catppuccin | gruvbox | mono | terminal
  theme: nexus
  # opaque paints the theme background; transparent uses your terminal background.
  background: opaque
  # Optional semantic overrides accept hex or ANSI color values.
  # colors:
  #   focus: "#A78BFA"
  #   live: "#5EEAD4"

reachability:
  # DNS/TCP checks only. Nexus never authenticates in the background.
  enabled: true
  timeout_ms: 1500
  concurrency: 8
  cache_seconds: 30

fzf:
  layout: reverse
  prompt: "Nexus ❯ "
  pointer: "→"

# Commands here are available for every host. Nexus always asks before running.
commands: []
#  - name: uptime
#    description: Show system uptime
#    command: uptime

# Commands inherited by every host with a matching tag.
tag_commands: {}
#  prod:
#    - name: logs
#      description: Follow service logs
#      command: journalctl -u app -f

# Keys may be a full saved target (recommended) or host/IP for legacy configs.
host_profiles:
  <user@host[:port]>:
    alias: ""
    tags: []
    os: ""
    commands: []
    use_unix_discovery: true
    rsync_stability: true
`
}

func loadAppConfig(configPath string) (appConfig, error) {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return appConfig{}, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}
	cfg := defaultAppConfig()
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return appConfig{}, fmt.Errorf("invalid config YAML %s: %w", configPath, err)
	}
	cfg.FullIndexDepth = sanitizeFullIndexDepth(cfg.FullIndexDepth)
	cfg.FZF = sanitizeFZFConfig(cfg.FZF)
	cfg.UI.Theme = normalizeThemeName(cfg.UI.Theme)
	if _, ok := themes[cfg.UI.Theme]; !ok {
		return appConfig{}, fmt.Errorf("unknown UI theme %q (run `nexus theme list`)", cfg.UI.Theme)
	}
	if cfg.FZF.Theme == "nexus" {
		cfg.FZF.Theme = cfg.UI.Theme
	}
	switch strings.ToLower(strings.TrimSpace(cfg.UI.Background)) {
	case "", "opaque":
		cfg.UI.Background = "opaque"
	case "transparent":
		cfg.UI.Background = "transparent"
	default:
		return appConfig{}, fmt.Errorf("ui.background must be opaque or transparent")
	}
	if cfg.Reachability.Enabled == nil {
		enabled := true
		cfg.Reachability.Enabled = &enabled
	}
	if cfg.Reachability.TimeoutMS <= 0 || cfg.Reachability.TimeoutMS > 10000 {
		cfg.Reachability.TimeoutMS = 1500
	}
	if cfg.Reachability.Concurrency <= 0 || cfg.Reachability.Concurrency > 32 {
		cfg.Reachability.Concurrency = 8
	}
	if cfg.Reachability.CacheSeconds <= 0 {
		cfg.Reachability.CacheSeconds = 30
	}
	cfg.Commands = sanitizeCommands(cfg.Commands)
	for tag, commands := range cfg.TagCommands {
		cleanTag := sanitizeLabel(tag)
		if cleanTag == "" {
			delete(cfg.TagCommands, tag)
			continue
		}
		if cleanTag != tag {
			delete(cfg.TagCommands, tag)
		}
		cfg.TagCommands[cleanTag] = sanitizeCommands(commands)
	}
	cleanProfiles := make(map[string]discoveryProfile, len(cfg.HostProfiles))
	for key, profile := range cfg.HostProfiles {
		key = strings.TrimSpace(key)
		if key == "" || strings.HasPrefix(key, "<") {
			continue
		}
		profile.Alias = sanitizeLabel(profile.Alias)
		profile.OS = sanitizeLabel(profile.OS)
		profile.Tags = sanitizeLabels(profile.Tags)
		profile.Commands = sanitizeCommands(profile.Commands)
		cleanProfiles[key] = profile
	}
	cfg.HostProfiles = cleanProfiles
	return cfg, nil
}

// loadConfigFromYAML preserves the existing internal API while the richer
// configuration remains available through loadedConfig.
func loadConfigFromYAML(configPath string) (map[string]discoveryProfile, int, fzfConfig, error) {
	cfg, err := loadAppConfig(configPath)
	if err != nil {
		return nil, defaultFullIndexDepth, defaultFZFConfig(), err
	}
	loadedConfig = cfg
	profiles := make(map[string]discoveryProfile, len(cfg.HostProfiles))
	for host, profile := range cfg.HostProfiles {
		key := profileLookupKey(host)
		if key != "" {
			profiles[key] = profile
		}
	}
	return profiles, cfg.FullIndexDepth, cfg.FZF, nil
}

func sanitizeFullIndexDepth(raw int) int {
	if raw <= 0 {
		return defaultFullIndexDepth
	}
	return raw
}

func sanitizeCommands(commands []commandConfig) []commandConfig {
	byName := map[string]commandConfig{}
	order := make([]string, 0, len(commands))
	for _, command := range commands {
		command.Name = sanitizeLabel(command.Name)
		command.Description = sanitizeTerminalText(command.Description)
		command.Command = sanitizeCommandText(command.Command)
		if command.Name == "" || command.Command == "" {
			continue
		}
		if _, exists := byName[command.Name]; !exists {
			order = append(order, command.Name)
		}
		byName[command.Name] = command
	}
	out := make([]commandConfig, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out
}

func commandsForTarget(target string) []commandConfig {
	profile := profileForTarget(target)
	merged := make(map[string]commandConfig)
	order := []string{}
	add := func(commands []commandConfig) {
		for _, command := range commands {
			if _, exists := merged[command.Name]; !exists {
				order = append(order, command.Name)
			}
			merged[command.Name] = command
		}
	}
	add(loadedConfig.Commands)
	for _, tag := range profile.Tags {
		add(loadedConfig.TagCommands[tag])
	}
	add(profile.Commands)
	out := make([]commandConfig, 0, len(order))
	for _, name := range order {
		out = append(out, merged[name])
	}
	return out
}

func profileForTarget(target string) discoveryProfile {
	if profile, ok := loadedConfig.HostProfiles[target]; ok {
		return profile
	}
	spec, err := parseConnectionTarget(target)
	if err == nil {
		for key, profile := range loadedConfig.HostProfiles {
			if profileLookupKey(key) == strings.ToLower(spec.Host) {
				return profile
			}
		}
	}
	return discoveryProfile{}
}

func profileLookupKey(raw string) string {
	if spec, err := parseConnectionTarget(raw); err == nil {
		return strings.ToLower(spec.Host)
	}
	return strings.Trim(strings.ToLower(strings.TrimSpace(raw)), "[]")
}

func sanitizeLabel(value string) string {
	value = strings.TrimSpace(sanitizeTerminalText(value))
	if len([]rune(value)) > 48 {
		value = string([]rune(value)[:48])
	}
	return value
}

func sanitizeLabels(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = sanitizeLabel(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sanitizeTerminalText(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r == '\n' || r == '\r' || r == '\t' || r == 0x1b || r < 0x20 || r == 0x7f {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func sanitizeCommandText(value string) string {
	value = strings.TrimSpace(value)
	for _, r := range value {
		if r < 0x20 || r >= 0x7f && r <= 0x9f {
			return ""
		}
	}
	return value
}
