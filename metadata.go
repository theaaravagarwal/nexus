package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const metadataScript = `os=""; if [ -r /etc/os-release ]; then os=$(sed -n 's/^PRETTY_NAME=//p' /etc/os-release | head -n1 | tr -d '"'); fi; [ -n "$os" ] || os=$(uname -srm 2>/dev/null || ver 2>/dev/null || true); printf 'OS=%s\n' "$os"; cpu=$(command -v lscpu >/dev/null 2>&1 && lscpu | sed -n 's/^Model name:[[:space:]]*//p' | head -n1); [ -n "$cpu" ] || cpu=$(sysctl -n machdep.cpu.brand_string 2>/dev/null || true); printf 'CPU=%s\n' "$cpu"; mem=$(free -h 2>/dev/null | awk '/^Mem:/ {print $2}'); [ -n "$mem" ] || mem=$(sysctl -n hw.memsize 2>/dev/null || true); printf 'MEMORY=%s\n' "$mem"; disk=$(df -h . 2>/dev/null | awk 'NR==2 {print $3 " / " $2 " (" $5 ")"}'); printf 'DISK=%s\n' "$disk"; tools=""; for cmd in btop htop top duf ncdu df nload speedtest; do command -v "$cmd" >/dev/null 2>&1 && tools="${tools}${tools:+,}$cmd"; done; printf 'TOOLS=%s\n' "$tools"`

func refreshHostMetadata(statePath, target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	args, err := buildSSHArgs(target, false, remoteShellCommand("sh", metadataScript))
	if err != nil {
		return err
	}
	// Options must precede the destination and remote command.
	insertAt := len(args) - 2
	if insertAt < 0 {
		return fmt.Errorf("invalid SSH metadata arguments")
	}
	withBatch := make([]string, 0, len(args)+2)
	withBatch = append(withBatch, args[:insertAt]...)
	withBatch = append(withBatch, "-o", "BatchMode=yes")
	withBatch = append(withBatch, args[insertAt:]...)
	cmd := exec.CommandContext(ctx, "ssh", withBatch...)
	var output bytes.Buffer
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("metadata refresh failed: %w", err)
	}
	values := parseMetadata(output.String())
	if values["OS"] == "" && values["TOOLS"] == "" {
		return fmt.Errorf("metadata refresh returned no usable data")
	}
	return updateState(statePath, func(state *nexusState) {
		entry := state.Hosts[target]
		entry.OS = values["OS"]
		entry.CPU = values["CPU"]
		entry.Memory = values["MEMORY"]
		entry.Disk = values["DISK"]
		if values["TOOLS"] != "" {
			entry.Tools = sanitizeLabels(strings.Split(values["TOOLS"], ","))
		}
		entry.Updated = time.Now()
		state.Hosts[target] = entry
	})
}

func parseMetadata(output string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "OS", "CPU", "MEMORY", "DISK", "TOOLS":
			values[key] = sanitizeTerminalText(value)
		}
	}
	return values
}
