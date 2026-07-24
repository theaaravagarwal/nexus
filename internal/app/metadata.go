package app

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
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
	FilesystemType string `json:"filesystem_type,omitempty"`
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

type gpuProbeRecord struct {
	Source string
	ID     string
	Label  string
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

// GNU df exposes filesystem types; macOS, BSD, and smaller BusyBox builds may
// not. The fallback keeps the legacy five-field record so older targets remain
// supported and filtering can make a conservative decision.
const storageInventoryScript = `if LC_ALL=C df -PkT / >/dev/null 2>&1; then
  LC_ALL=C df -PkT 2>/dev/null |
    awk 'NR > 1 && NF >= 7 && $3 ~ /^[0-9]+$/ && $3 > 0 {
      mount=$7
      for (field=8; field<=NF; field++) mount=mount " " $field
      printf "DISK=%s\t%s\t%.0f\t%.0f\t%.0f\t%s\n", $1, mount, $4 * 1024, $5 * 1024, $3 * 1024, $2
    }'
else
  LC_ALL=C df -Pk 2>/dev/null |
    awk 'NR > 1 && NF >= 6 && $2 ~ /^[0-9]+$/ && $2 > 0 {
      mount=$6
      for (field=7; field<=NF; field++) mount=mount " " $field
      printf "DISK=%s\t%s\t%.0f\t%.0f\t%.0f\n", $1, mount, $3 * 1024, $4 * 1024, $2 * 1024
    }'
fi`

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

gpu_records=""
nvidia_smi=$(command -v nvidia-smi 2>/dev/null || true)
if [ -z "$nvidia_smi" ] && [ -x /usr/lib/wsl/lib/nvidia-smi ]; then
  nvidia_smi=/usr/lib/wsl/lib/nvidia-smi
fi
if [ -n "$nvidia_smi" ]; then
  records=$("$nvidia_smi" --query-gpu=pci.bus_id,name,memory.total --format=csv,noheader,nounits 2>/dev/null |
    awk -F, '{
      bus=$1
      name=$2
      memory=$3
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", bus)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", name)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", memory)
      if (name != "" && memory ~ /^[0-9.]+$/) {
        printf "GPU_RECORD=nvidia\t%s\t%s · %.1f GB VRAM\n", bus, name, memory * 1048576 / 1000000000
      } else if (name != "") {
        printf "GPU_RECORD=nvidia\t%s\t%s\n", bus, name
      }
    }')
  [ -n "$records" ] && gpu_records="${gpu_records}${gpu_records:+
}${records}"
fi
if command -v system_profiler >/dev/null 2>&1; then
  records=$(system_profiler SPDisplaysDataType 2>/dev/null |
    awk -F: '/Chipset Model:/ {
      value=$2
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
      if (value != "") {
        count++
        printf "GPU_RECORD=system_profiler\t%d\t%s\n", count, value
      }
    }')
  [ -n "$records" ] && gpu_records="${gpu_records}${gpu_records:+
}${records}"
fi
if command -v lspci >/dev/null 2>&1; then
  records=$(lspci -D 2>/dev/null |
    awk '/VGA compatible controller:|3D controller:|Display controller:/ {
      bus=$1
      sub(/^[^ ]+[[:space:]]+[^:]+:[[:space:]]*/, "")
      if ($0 != "") printf "GPU_RECORD=lspci\t%s\t%s\n", bus, $0
    }')
  [ -n "$records" ] && gpu_records="${gpu_records}${gpu_records:+
}${records}"
fi
case "$(uname -r 2>/dev/null)" in
  *[Mm]icrosoft*)
    powershell=""
    [ -x /mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe ] &&
      powershell=/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe
    if [ -n "$powershell" ]; then
      records=$("$powershell" -NoProfile -NonInteractive -Command \
        'Get-CimInstance Win32_VideoController | ForEach-Object { "{0}|{1}" -f $_.PNPDeviceID, $_.Name }' \
        2>/dev/null | tr -d '\r' |
        awk -F '|' 'NF >= 2 && $2 != "" {
          printf "GPU_RECORD=wsl-host\t%s\t%s\n", $1, $2
        }')
      [ -n "$records" ] && gpu_records="${gpu_records}${gpu_records:+
}${records}"
    fi
    ;;
esac
if [ -z "$gpu_records" ]; then
  for card in /sys/class/drm/card[0-9]*; do
    [ -r "$card/device/vendor" ] || continue
    vendor=$(cat "$card/device/vendor" 2>/dev/null)
    device=$(cat "$card/device/device" 2>/dev/null)
    case "$vendor" in
      0x10de) vendor_name=NVIDIA ;;
      0x1002) vendor_name=AMD ;;
      0x8086) vendor_name=Intel ;;
      *) vendor_name=GPU ;;
    esac
    printf 'GPU_RECORD=sysfs\t%s\t%s GPU (%s:%s)\n' "${card##*/}" "$vendor_name" "$vendor" "$device"
  done
else
  printf '%s\n' "$gpu_records"
fi

` + storageInventoryScript + `

tools=""
for tool in btop htop top duf ncdu df nload speedtest nvidia-smi rocm-smi; do
  command -v "$tool" >/dev/null 2>&1 && tools="${tools}${tools:+,}$tool"
done
[ -n "$nvidia_smi" ] && case ",$tools," in *,nvidia-smi,*) ;; *) tools="${tools}${tools:+,}nvidia-smi" ;; esac
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
	gpuRecords := make([]gpuProbeRecord, 0)
	gpuRecordIDs := make(map[string]struct{})
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
		case "GPU_RECORD":
			fields := strings.SplitN(rawValue, "\t", 3)
			if len(fields) != 3 || len(gpuRecords) >= maxMetadataGPUs*8 {
				continue
			}
			record := gpuProbeRecord{
				Source: sanitizeMetadataValue(fields[0]),
				ID:     sanitizeMetadataValue(fields[1]),
				Label:  sanitizeMetadataValue(fields[2]),
			}
			if record.Source == "" || record.Label == "" {
				continue
			}
			recordKey := strings.ToLower(record.Source + "\x00" + record.ID + "\x00" + record.Label)
			if _, exists := gpuRecordIDs[recordKey]; exists {
				continue
			}
			gpuRecordIDs[recordKey] = struct{}{}
			gpuRecords = append(gpuRecords, record)
		case "DISK":
			if len(snapshot.Disks) >= maxMetadataDisks {
				continue
			}
			disk, ok := parseDiskUsage(rawValue)
			if !ok || !isMeaningfulStorageDisk(disk) {
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
	legacyGPUs := make(map[string]struct{}, len(gpus))
	for label := range gpus {
		legacyGPUs[label] = struct{}{}
	}
	for _, label := range aggregateGPURecords(gpuRecords, max(0, maxMetadataGPUs-len(snapshot.GPUs))) {
		dedupe := strings.ToLower(label)
		if _, exists := legacyGPUs[dedupe]; exists {
			continue
		}
		snapshot.GPUs = append(snapshot.GPUs, label)
	}
	snapshot.Disks = meaningfulStorageDisks(snapshot.Disks)
	return snapshot
}

func aggregateGPURecords(records []gpuProbeRecord, limit int) []string {
	if limit <= 0 || len(records) == 0 {
		return nil
	}
	type aggregate struct {
		label        string
		sourceCounts map[string]int
		order        int
	}
	groups := make(map[string]*aggregate)
	order := make([]string, 0)
	for _, record := range records {
		identity := normalizeGPUIdentity(record.Label)
		if identity == "" {
			continue
		}
		group, ok := groups[identity]
		if !ok {
			group = &aggregate{label: record.Label, sourceCounts: map[string]int{}, order: len(order)}
			groups[identity] = group
			order = append(order, identity)
		}
		group.sourceCounts[strings.ToLower(record.Source)]++
		if strings.Contains(strings.ToLower(record.Label), "vram") &&
			!strings.Contains(strings.ToLower(group.label), "vram") {
			group.label = record.Label
		}
	}
	result := make([]string, 0, min(limit, len(records)))
	for _, identity := range order {
		group := groups[identity]
		count := 0
		for _, sourceCount := range group.sourceCounts {
			count = max(count, sourceCount)
		}
		for range count {
			if len(result) >= limit {
				return result
			}
			result = append(result, group.label)
		}
	}
	return result
}

func normalizeGPUIdentity(label string) string {
	label = strings.ToLower(strings.TrimSpace(label))
	if before, _, found := strings.Cut(label, " · "); found {
		label = before
	}
	label = strings.Join(strings.Fields(label), " ")
	return label
}

func parseDiskUsage(value string) (diskUsage, bool) {
	fields := strings.Split(value, "\t")
	if len(fields) != 5 && len(fields) != 6 {
		return diskUsage{}, false
	}
	filesystem := sanitizeMetadataValue(fields[0])
	mountpoint := sanitizeMetadataValue(fields[1])
	used, usedErr := strconv.ParseUint(sanitizeMetadataValue(fields[2]), 10, 64)
	available, availableErr := strconv.ParseUint(sanitizeMetadataValue(fields[3]), 10, 64)
	total, totalErr := strconv.ParseUint(sanitizeMetadataValue(fields[4]), 10, 64)
	filesystemType := ""
	if len(fields) == 6 {
		filesystemType = strings.ToLower(sanitizeMetadataValue(fields[5]))
	}
	if filesystem == "" || mountpoint == "" || usedErr != nil || availableErr != nil || totalErr != nil || total == 0 {
		return diskUsage{}, false
	}
	return diskUsage{
		Filesystem: filesystem, Mountpoint: mountpoint, FilesystemType: filesystemType,
		UsedBytes: used, AvailableBytes: available, TotalBytes: total,
	}, true
}

func isMeaningfulStorageDisk(disk diskUsage) bool {
	source := strings.ToLower(strings.TrimSpace(disk.Filesystem))
	mountpoint := strings.TrimSpace(disk.Mountpoint)
	filesystemType := strings.ToLower(strings.TrimSpace(disk.FilesystemType))

	// Root is the primary capacity signal even when a container or WSL reports
	// it through an overlay rather than a conventional block-device source.
	if mountpoint == "/" {
		return true
	}
	if source == "" || mountpoint == "" {
		return false
	}

	switch filesystemType {
	case "proc", "procfs", "sysfs", "devfs", "devtmpfs", "devpts",
		"tmpfs", "ramfs", "cgroup", "cgroup2", "pstore", "securityfs",
		"debugfs", "tracefs", "configfs", "fusectl", "mqueue", "hugetlbfs",
		"rpc_pipefs", "binfmt_misc", "nsfs", "autofs", "squashfs",
		"fuse.snapfuse", "overlay":
		return false
	}
	for _, prefix := range []string{"/dev/loop", "/dev/ram", "/dev/zram"} {
		if strings.HasPrefix(source, prefix) {
			return false
		}
	}
	switch source {
	case "none", "rootfs", "tmpfs", "devtmpfs", "udev", "drivers", "snapfuse":
		return false
	}
	for _, prefix := range []string{
		"/proc", "/sys", "/dev", "/snap", "/init", "/tmp",
		"/usr/lib/wsl", "/mnt/wsl", "/system/volumes",
	} {
		lowerMount := strings.ToLower(mountpoint)
		if lowerMount == prefix || strings.HasPrefix(lowerMount, prefix+"/") {
			return false
		}
	}
	lowerMount := strings.ToLower(mountpoint)
	if lowerMount == "/boot" || strings.HasPrefix(lowerMount, "/boot/") {
		return false
	}
	if (lowerMount == "/run" || strings.HasPrefix(lowerMount, "/run/")) &&
		!strings.HasPrefix(lowerMount, "/run/media/") {
		return false
	}

	// WSL exposes Windows volumes as drive-letter sources mounted directly
	// beneath /mnt. Other 9p entries are WSL plumbing and were rejected above.
	if len(source) >= 2 && source[1] == ':' &&
		len(lowerMount) == len("/mnt/x") && strings.HasPrefix(lowerMount, "/mnt/") {
		return true
	}
	if strings.HasPrefix(source, "/dev/") || strings.HasPrefix(source, "uuid=") ||
		strings.HasPrefix(source, "label=") {
		return true
	}
	switch filesystemType {
	case "apfs", "hfs", "hfsplus", "zfs", "btrfs", "nfs", "nfs4",
		"cifs", "smbfs", "fuse.sshfs", "sshfs", "9p", "drvfs":
		return true
	}

	// Legacy five-field records do not contain a filesystem type. Keep unknown
	// non-system mounts rather than silently deleting a legitimate custom,
	// network, or external volume from cached state.
	return true
}

func meaningfulStorageDisks(disks []diskUsage) []diskUsage {
	result := make([]diskUsage, 0, len(disks))
	seen := make(map[string]struct{}, len(disks))
	for _, disk := range disks {
		if !isMeaningfulStorageDisk(disk) {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(disk.Filesystem)) + "\x00" +
			strings.TrimSpace(disk.Mountpoint) + "\x00" + strconv.FormatUint(disk.TotalBytes, 10)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, disk)
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].Mountpoint == "/" {
			return result[right].Mountpoint != "/"
		}
		if result[right].Mountpoint == "/" {
			return false
		}
		leftPercent := float64(result[left].UsedBytes) / float64(result[left].TotalBytes)
		rightPercent := float64(result[right].UsedBytes) / float64(result[right].TotalBytes)
		if leftPercent != rightPercent {
			return leftPercent > rightPercent
		}
		return result[left].Mountpoint < result[right].Mountpoint
	})
	return result
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
