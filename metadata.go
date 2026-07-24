package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxMetadataBytes = 256 * 1024
	maxMetadataLine  = 4096
	maxMetadataValue = 512
	maxMetadataGPUs  = 16
	maxMetadataDisks = 128
	maxMetadataTools = 64
)

var (
	legacyCapacityPattern = regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)\s*(KIB|MIB|GIB|TIB|KI|MI|GI|TI|K|M|G|T)\b`)
	legacyCapacityPair    = regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)\s*/\s*([0-9]+(?:\.[0-9]+)?)\s*(KIB|MIB|GIB|TIB|KI|MI|GI|TI|K|M|G|T)\b`)
)

// diskUsage is kept byte-based so display units can be selected consistently
// instead of inheriting platform-specific output from df -h.
type diskUsage struct {
	Filesystem     string `json:"filesystem"`
	Mountpoint     string `json:"mountpoint"`
	UsedBytes      uint64 `json:"used_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
	TotalBytes     uint64 `json:"total_bytes"`
}

type hostMetadataSnapshot struct {
	OS          string
	CPU         string
	MemoryBytes uint64
	GPUs        []string
	Disks       []diskUsage
	Tools       []string
}

type boundedMetadataOutput struct {
	buffer bytes.Buffer
}

func (output *boundedMetadataOutput) Write(chunk []byte) (int, error) {
	size := len(chunk)
	remaining := max(0, maxMetadataBytes-output.buffer.Len())
	if remaining > 0 {
		_, _ = output.buffer.Write(chunk[:min(size, remaining)])
	}
	// Report the full chunk consumed so an overlong remote response is
	// discarded rather than causing ssh to fail with a short write.
	return size, nil
}

func (output *boundedMetadataOutput) String() string {
	return output.buffer.String()
}

// The wire format is deliberately line-oriented and requires only POSIX sh,
// awk, df and common platform utilities. Numeric values stay in bytes so Linux
// and macOS produce the same decimal units in the UI.
const metadataScript = `
os=""
if [ -r /etc/os-release ]; then
  os=$(sed -n 's/^PRETTY_NAME=//p' /etc/os-release | head -n1 | tr -d '"')
fi
[ -n "$os" ] || os=$(uname -srm 2>/dev/null || ver 2>/dev/null || true)
printf 'OS=%s\n' "$os"

cpu=$(command -v lscpu >/dev/null 2>&1 && lscpu | sed -n 's/^Model name:[[:space:]]*//p' | head -n1)
[ -n "$cpu" ] || cpu=$(sysctl -n machdep.cpu.brand_string 2>/dev/null || true)
printf 'CPU=%s\n' "$cpu"

memory_bytes=""
if [ -r /proc/meminfo ]; then
  memory_bytes=$(awk '$1 == "MemTotal:" { printf "%.0f", $2 * 1024; exit }' /proc/meminfo)
fi
[ -n "$memory_bytes" ] || memory_bytes=$(sysctl -n hw.memsize 2>/dev/null || true)
printf 'MEMORY_BYTES=%s\n' "$memory_bytes"

gpus=""
if command -v nvidia-smi >/dev/null 2>&1; then
  gpus=$(nvidia-smi --query-gpu=name,memory.total --format=csv,noheader,nounits 2>/dev/null |
    awk -F, '{
      name=$1
      memory=$2
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", name)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", memory)
      if (name != "" && memory ~ /^[0-9.]+$/) {
        printf "%s · %.1f GB VRAM\n", name, memory * 1048576 / 1000000000
      } else if (name != "") {
        print name
      }
    }')
fi
if [ -z "$gpus" ] && command -v system_profiler >/dev/null 2>&1; then
  gpus=$(system_profiler SPDisplaysDataType 2>/dev/null |
    awk -F: '/Chipset Model:/ {
      value=$2
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
      if (value != "") print value
    }')
fi
if [ -z "$gpus" ] && command -v lspci >/dev/null 2>&1; then
  gpus=$(lspci 2>/dev/null |
    awk '/VGA compatible controller:|3D controller:|Display controller:/ {
      sub(/^[^ ]+[[:space:]]+[^:]+:[[:space:]]*/, "")
      if ($0 != "") print
    }')
fi
if [ -n "$gpus" ]; then
  printf '%s\n' "$gpus" | while IFS= read -r gpu; do
    [ -n "$gpu" ] && printf 'GPU=%s\n' "$gpu"
  done
fi

LC_ALL=C df -Pk 2>/dev/null |
  awk 'NR > 1 && NF >= 6 && $2 ~ /^[0-9]+$/ && $2 > 0 {
    mount=$6
    for (field=7; field<=NF; field++) mount=mount " " $field
    printf "DISK=%s\t%s\t%.0f\t%.0f\t%.0f\n", $1, mount, $3 * 1024, $4 * 1024, $2 * 1024
  }'

tools=""
for tool in btop htop top duf ncdu df nload speedtest nvidia-smi rocm-smi; do
  command -v "$tool" >/dev/null 2>&1 && tools="${tools}${tools:+,}$tool"
done
printf 'TOOLS=%s\n' "$tools"
`

func refreshHostMetadata(statePath, target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
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
	var output boundedMetadataOutput
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("metadata refresh failed: %w", err)
	}
	snapshot := parseMetadata(output.String())
	if !snapshot.usable() {
		return fmt.Errorf("metadata refresh returned no usable data")
	}
	return updateState(statePath, func(state *nexusState) {
		entry := state.Hosts[target]
		if snapshot.OS != "" {
			entry.OS = snapshot.OS
		}
		if snapshot.CPU != "" {
			entry.CPU = snapshot.CPU
		}
		if snapshot.MemoryBytes > 0 {
			entry.Memory = formatDecimalBytes(snapshot.MemoryBytes)
		}
		if len(snapshot.GPUs) > 0 {
			entry.GPUs = append([]string(nil), snapshot.GPUs...)
		}
		if len(snapshot.Disks) > 0 {
			entry.Disks = append([]diskUsage(nil), snapshot.Disks...)
			entry.Disk = legacyDiskSummary(snapshot.Disks)
		}
		if len(snapshot.Tools) > 0 {
			entry.Tools = append([]string(nil), snapshot.Tools...)
		}
		entry.Updated = time.Now()
		state.Hosts[target] = entry
	})
}

func (snapshot hostMetadataSnapshot) usable() bool {
	return snapshot.OS != "" || snapshot.CPU != "" || snapshot.MemoryBytes > 0 ||
		len(snapshot.GPUs) > 0 || len(snapshot.Disks) > 0 || len(snapshot.Tools) > 0
}

func parseMetadata(output string) hostMetadataSnapshot {
	var snapshot hostMetadataSnapshot
	if len(output) > maxMetadataBytes {
		output = output[:maxMetadataBytes]
	}
	output = strings.ReplaceAll(output, "\r\n", "\n")
	gpus := make(map[string]struct{})
	disks := make(map[string]struct{})
	tools := make(map[string]struct{})
	for _, line := range strings.Split(output, "\n") {
		if len(line) > maxMetadataLine {
			line = line[:maxMetadataLine]
		}
		key, rawValue, ok := strings.Cut(strings.TrimSuffix(line, "\r"), "=")
		if !ok {
			continue
		}
		switch key {
		case "OS":
			snapshot.OS = sanitizeMetadataValue(rawValue)
		case "CPU":
			snapshot.CPU = sanitizeMetadataValue(rawValue)
		case "MEMORY_BYTES":
			value := sanitizeMetadataValue(rawValue)
			if parsed, err := strconv.ParseUint(value, 10, 64); err == nil {
				snapshot.MemoryBytes = parsed
			}
		case "GPU":
			if len(snapshot.GPUs) >= maxMetadataGPUs {
				continue
			}
			value := sanitizeMetadataValue(rawValue)
			dedupe := strings.ToLower(value)
			if value == "" {
				continue
			}
			if _, exists := gpus[dedupe]; exists {
				continue
			}
			gpus[dedupe] = struct{}{}
			snapshot.GPUs = append(snapshot.GPUs, value)
		case "DISK":
			if len(snapshot.Disks) >= maxMetadataDisks {
				continue
			}
			disk, ok := parseDiskUsage(rawValue)
			if !ok {
				continue
			}
			dedupe := disk.Filesystem + "\x00" + disk.Mountpoint
			if _, exists := disks[dedupe]; exists {
				continue
			}
			disks[dedupe] = struct{}{}
			snapshot.Disks = append(snapshot.Disks, disk)
		case "TOOLS":
			for _, rawTool := range strings.Split(rawValue, ",") {
				if len(snapshot.Tools) >= maxMetadataTools {
					break
				}
				tool := sanitizeMetadataValue(rawTool)
				dedupe := strings.ToLower(tool)
				if tool == "" {
					continue
				}
				if _, exists := tools[dedupe]; exists {
					continue
				}
				tools[dedupe] = struct{}{}
				snapshot.Tools = append(snapshot.Tools, tool)
			}
		}
	}
	return snapshot
}

func parseDiskUsage(value string) (diskUsage, bool) {
	fields := strings.Split(value, "\t")
	if len(fields) != 5 {
		return diskUsage{}, false
	}
	filesystem := sanitizeMetadataValue(fields[0])
	mountpoint := sanitizeMetadataValue(fields[1])
	used, usedErr := strconv.ParseUint(sanitizeMetadataValue(fields[2]), 10, 64)
	available, availableErr := strconv.ParseUint(sanitizeMetadataValue(fields[3]), 10, 64)
	total, totalErr := strconv.ParseUint(sanitizeMetadataValue(fields[4]), 10, 64)
	if filesystem == "" || mountpoint == "" || usedErr != nil || availableErr != nil || totalErr != nil || total == 0 {
		return diskUsage{}, false
	}
	return diskUsage{
		Filesystem: filesystem, Mountpoint: mountpoint,
		UsedBytes: used, AvailableBytes: available, TotalBytes: total,
	}, true
}

func sanitizeMetadataValue(value string) string {
	if len(value) > maxMetadataLine {
		value = value[:maxMetadataLine]
	}
	value = stripTerminalSequences(value)
	value = sanitizeTerminalText(value)
	return truncateRunes(value, maxMetadataValue)
}

func stripTerminalSequences(value string) string {
	var clean strings.Builder
	for index := 0; index < len(value); {
		if value[index] != 0x1b {
			r, size := utf8.DecodeRuneInString(value[index:])
			if r == 0x9b {
				index += size
				for index < len(value) {
					current := value[index]
					index++
					if current >= 0x40 && current <= 0x7e {
						break
					}
				}
				continue
			}
			if r >= 0x80 && r <= 0x9f {
				index += size
				continue
			}
			clean.WriteRune(r)
			index += size
			continue
		}
		index++
		if index >= len(value) {
			break
		}
		switch value[index] {
		case '[':
			index++
			for index < len(value) {
				current := value[index]
				index++
				if current >= 0x40 && current <= 0x7e {
					break
				}
			}
		case ']':
			index++
			for index < len(value) {
				if value[index] == 0x07 {
					index++
					break
				}
				if value[index] == 0x1b && index+1 < len(value) && value[index+1] == '\\' {
					index += 2
					break
				}
				index++
			}
		default:
			index++
		}
	}
	return clean.String()
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func formatDecimalBytes(bytes uint64) string {
	if bytes == 0 {
		return ""
	}
	const (
		gigabyte = uint64(1_000_000_000)
		terabyte = uint64(1_000_000_000_000)
	)
	if bytes >= terabyte {
		return formatDecimalValue(float64(bytes)/float64(terabyte)) + " TB"
	}
	return formatDecimalValue(float64(bytes)/float64(gigabyte)) + " GB"
}

func formatDecimalValue(value float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(value, 'f', 1, 64), ".0")
}

func normalizeLegacyCapacityText(value string) string {
	value = legacyCapacityPair.ReplaceAllString(value, "$1$3 / $2$3")
	return legacyCapacityPattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := legacyCapacityPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		amount, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return match
		}
		var multiplier float64
		switch strings.ToUpper(parts[2]) {
		case "K", "KI", "KIB":
			multiplier = 1024
		case "M", "MI", "MIB":
			multiplier = 1024 * 1024
		case "G", "GI", "GIB":
			multiplier = 1024 * 1024 * 1024
		case "T", "TI", "TIB":
			multiplier = 1024 * 1024 * 1024 * 1024
		default:
			return match
		}
		return formatDecimalBytes(uint64(amount * multiplier))
	})
}

func legacyDiskSummary(disks []diskUsage) string {
	if len(disks) == 0 {
		return ""
	}
	selected := disks[0]
	for _, disk := range disks {
		if disk.Mountpoint == "/" {
			selected = disk
			break
		}
	}
	percent := 0.0
	if selected.TotalBytes > 0 {
		percent = float64(selected.UsedBytes) * 100 / float64(selected.TotalBytes)
	}
	return fmt.Sprintf("%s / %s (%.0f%%)",
		formatDecimalBytes(selected.UsedBytes),
		formatDecimalBytes(selected.TotalBytes),
		percent,
	)
}
