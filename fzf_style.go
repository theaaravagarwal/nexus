package main

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
)

type fzfConfig struct {
	Theme    string            `yaml:"theme"`
	Layout   string            `yaml:"layout"`
	Prompt   string            `yaml:"prompt"`
	Pointer  string            `yaml:"pointer"`
	Keybinds map[string]string `yaml:"keybinds"`
}

var fzfUIConfig = defaultFZFConfig()

func defaultFZFConfig() fzfConfig {
	return fzfConfig{
		Theme:   "dark",
		Layout:  "reverse",
		Prompt:  "Nexus ❯ ",
		Pointer: "→",
		Keybinds: map[string]string{
			"alt-h": "backward-kill-word",
			"alt-p": "toggle-preview",
		},
	}
}

func sanitizeFZFConfig(raw fzfConfig) fzfConfig {
	cfg := defaultFZFConfig()
	switch strings.ToLower(strings.TrimSpace(raw.Theme)) {
	case "light", "cyberpunk", "dark":
		cfg.Theme = strings.ToLower(strings.TrimSpace(raw.Theme))
	}
	switch strings.ToLower(strings.TrimSpace(raw.Layout)) {
	case "default", "reverse", "reverse-list":
		cfg.Layout = strings.ToLower(strings.TrimSpace(raw.Layout))
	}
	if raw.Prompt != "" {
		cfg.Prompt = raw.Prompt
	}
	if raw.Pointer != "" {
		cfg.Pointer = raw.Pointer
	}
	for key, action := range raw.Keybinds {
		key = strings.TrimSpace(strings.ToLower(key))
		action = strings.TrimSpace(action)
		if key != "" && action != "" {
			cfg.Keybinds[key] = action
		}
	}
	return cfg
}

func buildFZFArgs(prompt string) []string {
	cfg := fzfUIConfig
	if strings.TrimSpace(prompt) == "" {
		prompt = cfg.Prompt
	}
	args := []string{
		"--height", "45%",
		"--layout", cfg.Layout,
		"--border=rounded",
		"--info=inline",
		"--prompt", prompt,
		"--pointer", cfg.Pointer,
		"--color", fzfColorByTheme(cfg.Theme),
	}
	keys := make([]string, 0, len(cfg.Keybinds))
	for key := range cfg.Keybinds {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--bind", fmt.Sprintf("%s:%s", key, cfg.Keybinds[key]))
	}
	return args
}

func fzfColorByTheme(theme string) string {
	switch theme {
	case "light":
		return "light,fg+:25,hl:25,pointer:31,marker:31,header:24"
	case "cyberpunk":
		return "dark,fg+:51,hl:45,pointer:51,marker:45,header:207"
	default:
		return "dark,fg+:81,hl:141,pointer:81,marker:141,header:244"
	}
}

func fzfNotFoundError() error {
	hint := "install fzf with your package manager"
	switch runtime.GOOS {
	case "darwin":
		hint = "install it with `brew install fzf`"
	case "linux":
		hint = "install it with your distro package manager"
	case "windows":
		hint = "install it with Scoop or Chocolatey"
	}
	return fmt.Errorf("fzf not found in PATH; %s", hint)
}
