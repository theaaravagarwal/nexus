package nexus

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	telemetryInterval    = 10 * time.Second
	telemetryMaxBackoff  = 2 * time.Minute
	telemetryTimeout     = 4 * time.Second
	telemetryHistorySize = 30
)

type gpuTelemetry struct {
	Name        string
	Utilization int
	MemoryUsed  uint64
	MemoryTotal uint64
	Temperature int
}

type telemetrySample struct {
	Target        string
	CollectedAt   time.Time
	Uptime        time.Duration
	LoadOne       float64
	CPUCores      int
	MemoryUsed    uint64
	MemoryTotal   uint64
	NetworkRX     uint64
	NetworkTX     uint64
	NetworkRXRate float64
	NetworkTXRate float64
	GPUs          []gpuTelemetry
}

type hostTelemetry struct {
	Current     telemetrySample
	History     []telemetrySample
	Failures    int
	LastErr     string
	NextAttempt time.Time
}

type telemetryTickMsg struct {
	Generation uint64
}

type telemetryResultMsg struct {
	Generation uint64
	Target     string
	Sample     telemetrySample
	Err        error
}

const telemetryScript = `
uptime_seconds=""
load_one=""
cores=""
memory_total=""
memory_available=""
network_rx=0
network_tx=0

if [ -r /proc/uptime ]; then
  uptime_seconds=$(awk '{ printf "%.0f", $1 }' /proc/uptime)
  load_one=$(awk '{ print $1 }' /proc/loadavg 2>/dev/null)
  cores=$(getconf _NPROCESSORS_ONLN 2>/dev/null || true)
  memory_total=$(awk '$1 == "MemTotal:" { printf "%.0f", $2 * 1024 }' /proc/meminfo)
  memory_available=$(awk '$1 == "MemAvailable:" { printf "%.0f", $2 * 1024 }' /proc/meminfo)
  set -- $(awk '
    NR > 2 && $1 !~ /^lo:/ {
      gsub(/:/, "", $1)
      rx += $2
      tx += $10
    }
    END { printf "%.0f %.0f", rx, tx }
  ' /proc/net/dev)
  network_rx=${1:-0}
  network_tx=${2:-0}
else
  now=$(date +%s 2>/dev/null || echo 0)
  boot=$(sysctl -n kern.boottime 2>/dev/null | sed -n 's/.*sec = \([0-9]*\).*/\1/p')
  [ -n "$boot" ] && uptime_seconds=$((now - boot))
  load_one=$(sysctl -n vm.loadavg 2>/dev/null | tr -d '{},' | awk '{ print $1 }')
  cores=$(sysctl -n hw.ncpu 2>/dev/null || true)
  memory_total=$(sysctl -n hw.memsize 2>/dev/null || true)
  if command -v vm_stat >/dev/null 2>&1; then
    memory_available=$(vm_stat 2>/dev/null | awk '
      NR == 1 { gsub(/[^0-9]/, "", $8); page=$8+0 }
      /Pages free:|Pages inactive:|Pages speculative:/ {
        gsub(/[^0-9]/, "", $NF); pages += $NF
      }
      END { if (page > 0) printf "%.0f", pages * page }
    ')
  fi
fi
printf 'TELEMETRY=%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
  "$uptime_seconds" "$load_one" "$cores" "$memory_total" "$memory_available" "$network_rx" "$network_tx"

nvidia_smi=$(command -v nvidia-smi 2>/dev/null || true)
if [ -z "$nvidia_smi" ] && [ -x /usr/lib/wsl/lib/nvidia-smi ]; then
  nvidia_smi=/usr/lib/wsl/lib/nvidia-smi
fi
if [ -n "$nvidia_smi" ]; then
  "$nvidia_smi" --query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu \
    --format=csv,noheader,nounits 2>/dev/null |
    awk -F, '{
      for (i=1; i<=5; i++) gsub(/^[[:space:]]+|[[:space:]]+$/, "", $i)
      printf "GPU_TELEMETRY=%s\t%s\t%s\t%s\t%s\n", $1, $2, $3, $4, $5
    }'
fi
`

func telemetryTick(delay time.Duration, generation uint64) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return telemetryTickMsg{Generation: generation}
	})
}

func telemetryBackoff(failures int, generation uint64) time.Duration {
	failures = min(max(0, failures), 4)
	delay := telemetryInterval * time.Duration(1<<failures)
	delay = min(delay, telemetryMaxBackoff)
	jitter := time.Duration((generation*997)%2000) * time.Millisecond
	return min(telemetryMaxBackoff, delay+jitter)
}

func telemetryCommand(target string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), telemetryTimeout)
		defer cancel()
		args, err := buildSSHArgs(target, false, remoteShellCommand("sh", telemetryScript))
		if err != nil {
			return telemetryResultMsg{Generation: generation, Target: target, Err: err}
		}
		insertAt := len(args) - 2
		if insertAt < 0 {
			return telemetryResultMsg{
				Generation: generation, Target: target,
				Err: errors.New("invalid SSH telemetry arguments"),
			}
		}
		withBatch := make([]string, 0, len(args)+2)
		withBatch = append(withBatch, args[:insertAt]...)
		withBatch = append(withBatch, "-o", "BatchMode=yes")
		withBatch = append(withBatch, args[insertAt:]...)
		command := exec.CommandContext(ctx, "ssh", withBatch...)
		var output boundedMetadataOutput
		command.Stdout = &output
		if err := command.Run(); err != nil {
			if ctx.Err() != nil {
				err = fmt.Errorf("telemetry timed out: %w", ctx.Err())
			}
			return telemetryResultMsg{Generation: generation, Target: target, Err: err}
		}
		sample, err := parseTelemetry(output.String())
		sample.Target = target
		sample.CollectedAt = time.Now()
		return telemetryResultMsg{
			Generation: generation, Target: target, Sample: sample, Err: err,
		}
	}
}

func parseTelemetry(output string) (telemetrySample, error) {
	var sample telemetrySample
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "TELEMETRY":
			fields := strings.Split(value, "\t")
			if len(fields) != 7 {
				continue
			}
			uptime, _ := strconv.ParseInt(sanitizeMetadataValue(fields[0]), 10, 64)
			load, _ := strconv.ParseFloat(sanitizeMetadataValue(fields[1]), 64)
			cores, _ := strconv.Atoi(sanitizeMetadataValue(fields[2]))
			total, _ := strconv.ParseUint(sanitizeMetadataValue(fields[3]), 10, 64)
			available, _ := strconv.ParseUint(sanitizeMetadataValue(fields[4]), 10, 64)
			rx, _ := strconv.ParseUint(sanitizeMetadataValue(fields[5]), 10, 64)
			tx, _ := strconv.ParseUint(sanitizeMetadataValue(fields[6]), 10, 64)
			sample.Uptime = time.Duration(max(int64(0), uptime)) * time.Second
			sample.LoadOne = max(0, load)
			sample.CPUCores = max(0, cores)
			sample.MemoryTotal = total
			sample.MemoryUsed = total - min(total, available)
			sample.NetworkRX = rx
			sample.NetworkTX = tx
		case "GPU_TELEMETRY":
			if len(sample.GPUs) >= maxMetadataGPUs {
				continue
			}
			fields := strings.Split(value, "\t")
			if len(fields) != 5 {
				continue
			}
			utilization, _ := strconv.Atoi(sanitizeMetadataValue(fields[1]))
			usedMiB, _ := strconv.ParseFloat(sanitizeMetadataValue(fields[2]), 64)
			totalMiB, _ := strconv.ParseFloat(sanitizeMetadataValue(fields[3]), 64)
			temperature, _ := strconv.Atoi(sanitizeMetadataValue(fields[4]))
			sample.GPUs = append(sample.GPUs, gpuTelemetry{
				Name:        sanitizeMetadataValue(fields[0]),
				Utilization: min(100, max(0, utilization)),
				MemoryUsed:  uint64(max(0, usedMiB) * 1024 * 1024),
				MemoryTotal: uint64(max(0, totalMiB) * 1024 * 1024),
				Temperature: max(0, temperature),
			})
		}
	}
	if sample.Uptime == 0 && sample.LoadOne == 0 && sample.MemoryTotal == 0 && len(sample.GPUs) == 0 {
		return telemetrySample{}, errors.New("telemetry returned no usable data")
	}
	return sample, nil
}

func appendTelemetry(history []telemetrySample, sample telemetrySample) []telemetrySample {
	if len(history) > 0 {
		previous := history[len(history)-1]
		elapsed := sample.CollectedAt.Sub(previous.CollectedAt).Seconds()
		if elapsed > 0 {
			if sample.NetworkRX >= previous.NetworkRX {
				sample.NetworkRXRate = float64(sample.NetworkRX-previous.NetworkRX) / elapsed
			}
			if sample.NetworkTX >= previous.NetworkTX {
				sample.NetworkTXRate = float64(sample.NetworkTX-previous.NetworkTX) / elapsed
			}
		}
	}
	history = append(history, sample)
	if len(history) > telemetryHistorySize {
		history = append([]telemetrySample(nil), history[len(history)-telemetryHistorySize:]...)
	}
	return history
}
