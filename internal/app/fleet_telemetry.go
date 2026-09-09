package app

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	fleetTelemetryProtocol       = "NX1"
	fleetTelemetryMarker         = "NEXUS_FLEET_TELEMETRY_BEGIN"
	fleetTelemetryMaxStreams     = 12
	fleetTelemetryBatchWindow    = 50 * time.Millisecond
	fleetTelemetryRetryMin       = 10 * time.Second
	fleetTelemetryRetryMax       = 2 * time.Minute
	fleetTelemetryScannerMaxSize = 16 * 1024
)

type fleetTelemetrySpec struct {
	Target   string
	Interval time.Duration
}

type fleetTelemetryEvent struct {
	Generation uint64
	Target     string
	Sample     telemetrySample
	Err        error
}

type fleetTelemetryBatchMsg struct {
	Events []fleetTelemetryEvent
}

type fleetTelemetryPoolClosedMsg struct{}

type fleetTelemetryRotateMsg struct{}

type fleetTelemetryPool struct {
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	sessions map[string]*fleetTelemetrySession
	updates  chan fleetTelemetryEvent
	nextGen  uint64
}

type fleetTelemetrySession struct {
	generation uint64
	interval   time.Duration
	cancel     context.CancelFunc
}

func newFleetTelemetryPool() *fleetTelemetryPool {
	ctx, cancel := context.WithCancel(context.Background())
	return &fleetTelemetryPool{
		ctx: ctx, cancel: cancel,
		sessions: make(map[string]*fleetTelemetrySession),
		updates:  make(chan fleetTelemetryEvent, 64),
	}
}

func (p *fleetTelemetryPool) sync(specs []fleetTelemetrySpec) {
	if p == nil {
		return
	}
	if len(specs) > fleetTelemetryMaxStreams {
		specs = specs[:fleetTelemetryMaxStreams]
	}
	desired := make(map[string]fleetTelemetrySpec, len(specs))
	for _, spec := range specs {
		if spec.Target != "" && spec.Interval > 0 {
			desired[spec.Target] = spec
		}
	}
	p.mu.Lock()
	for target, session := range p.sessions {
		spec, keep := desired[target]
		if keep && spec.Interval == session.interval {
			delete(desired, target)
			continue
		}
		session.cancel()
		delete(p.sessions, target)
	}
	for target, spec := range desired {
		if p.ctx.Err() != nil {
			break
		}
		p.nextGen++
		ctx, cancel := context.WithCancel(p.ctx)
		session := &fleetTelemetrySession{generation: p.nextGen, interval: spec.Interval, cancel: cancel}
		p.sessions[target] = session
		go p.run(ctx, target, session)
	}
	p.mu.Unlock()
}

func (p *fleetTelemetryPool) run(ctx context.Context, target string, session *fleetTelemetrySession) {
	failures := 0
	for ctx.Err() == nil {
		err := streamRemoteFleetTelemetry(ctx, target, session.generation, session.interval, p.publish)
		if ctx.Err() != nil {
			return
		}
		failures++
		p.publish(fleetTelemetryEvent{Generation: session.generation, Target: target, Err: err})
		delay := fleetTelemetryRetryMin * time.Duration(1<<min(3, failures-1))
		delay = min(delay, fleetTelemetryRetryMax)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (p *fleetTelemetryPool) publish(event fleetTelemetryEvent) {
	select {
	case p.updates <- event:
		return
	case <-p.ctx.Done():
		return
	default:
	}
	select {
	case <-p.updates:
	default:
	}
	select {
	case p.updates <- event:
	case <-p.ctx.Done():
	}
}

func (p *fleetTelemetryPool) close() {
	if p == nil {
		return
	}
	p.cancel()
	p.mu.Lock()
	for _, session := range p.sessions {
		session.cancel()
	}
	p.sessions = make(map[string]*fleetTelemetrySession)
	p.mu.Unlock()
}

func (p *fleetTelemetryPool) current(target string, generation uint64) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	session := p.sessions[target]
	return session != nil && session.generation == generation
}

func fleetTelemetryRotateTick() tea.Cmd {
	return tea.Tick(15*time.Second, func(time.Time) tea.Msg { return fleetTelemetryRotateMsg{} })
}

func waitForFleetTelemetry(pool *fleetTelemetryPool) tea.Cmd {
	return func() tea.Msg {
		select {
		case first := <-pool.updates:
			latest := map[string]fleetTelemetryEvent{first.Target: first}
			timer := time.NewTimer(fleetTelemetryBatchWindow)
			defer timer.Stop()
			for {
				select {
				case event := <-pool.updates:
					latest[event.Target] = event
				case <-timer.C:
					events := make([]fleetTelemetryEvent, 0, len(latest))
					for _, event := range latest {
						events = append(events, event)
					}
					return fleetTelemetryBatchMsg{Events: events}
				case <-pool.ctx.Done():
					return fleetTelemetryPoolClosedMsg{}
				}
			}
		case <-pool.ctx.Done():
			return fleetTelemetryPoolClosedMsg{}
		}
	}
}

func fleetTelemetryCommand(interval time.Duration) string {
	seconds := max(1, int(interval/time.Second))
	return strings.ReplaceAll(fleetTelemetryScript, "__INTERVAL__", strconv.Itoa(seconds))
}

const fleetTelemetryScript = `
printf 'NEXUS_FLEET_TELEMETRY_BEGIN\n'
seq=0
while :; do
  seq=$((seq + 1))
  now=$(date +%s 2>/dev/null || echo 0)
  uptime_seconds=0 load_one=0 cores=0 memory_total=0 memory_available=0
  network_rx=0 network_tx=0 cpu_total=0 cpu_idle=0
  if [ -r /proc/uptime ]; then
    uptime_seconds=$(awk '{ printf "%.0f", $1 }' /proc/uptime)
    load_one=$(awk '{ print $1 }' /proc/loadavg 2>/dev/null)
    cores=$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 0)
    memory_total=$(awk '$1 == "MemTotal:" { printf "%.0f", $2 * 1024 }' /proc/meminfo)
    memory_available=$(awk '$1 == "MemAvailable:" { printf "%.0f", $2 * 1024 }' /proc/meminfo)
    set -- $(awk 'NR == 1 { for (i=2; i<=NF; i++) total += $i; print total, $5 + $6 }' /proc/stat)
    cpu_total=${1:-0}; cpu_idle=${2:-0}
    set -- $(awk 'NR > 2 && $1 !~ /^lo:/ { gsub(/:/, "", $1); rx += $2; tx += $10 } END { print rx+0, tx+0 }' /proc/net/dev)
    network_rx=${1:-0}; network_tx=${2:-0}
  else
    boot=$(sysctl -n kern.boottime 2>/dev/null | sed -n 's/.*sec = \([0-9]*\).*/\1/p')
    [ -n "$boot" ] && uptime_seconds=$((now - boot))
    load_one=$(sysctl -n vm.loadavg 2>/dev/null | tr -d '{},' | awk '{ print $1 }')
    cores=$(sysctl -n hw.ncpu 2>/dev/null || echo 0)
    memory_total=$(sysctl -n hw.memsize 2>/dev/null || echo 0)
    if command -v vm_stat >/dev/null 2>&1; then
      memory_available=$(vm_stat 2>/dev/null | awk 'NR == 1 { gsub(/[^0-9]/, "", $8); page=$8+0 } /Pages free:|Pages inactive:|Pages speculative:/ { gsub(/[^0-9]/, "", $NF); pages += $NF } END { printf "%.0f", pages * page }')
    fi
    set -- $(sysctl -n kern.cp_time 2>/dev/null || echo 0 0 0 0 0)
    cpu_total=$((${1:-0} + ${2:-0} + ${3:-0} + ${4:-0} + ${5:-0})); cpu_idle=${4:-0}
  fi
  printf 'NX1\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$seq" "$now" "$uptime_seconds" "$load_one" "$cores" "$memory_total" \
    "$memory_available" "$network_rx" "$network_tx" "$cpu_total" "$cpu_idle"
  sleep __INTERVAL__
done
`

func streamRemoteFleetTelemetry(
	ctx context.Context, target string, generation uint64, interval time.Duration,
	publish func(fleetTelemetryEvent),
) error {
	args, err := buildMonitoringSSHArgs(target, false, remoteShellCommand("sh", fleetTelemetryCommand(interval)))
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "ssh", args...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr boundedMetadataOutput
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), fleetTelemetryScannerMaxSize)
	started := false
	for scanner.Scan() {
		line := scanner.Text()
		if !started {
			if strings.Contains(line, fleetTelemetryMarker) {
				started = true
			}
			continue
		}
		sample, parseErr := parseFleetTelemetryLine(line, target, time.Now())
		if parseErr != nil {
			continue
		}
		publish(fleetTelemetryEvent{Generation: generation, Target: target, Sample: sample})
	}
	runErr := command.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if scanErr := scanner.Err(); scanErr != nil && !errors.Is(scanErr, io.EOF) {
		return scanErr
	}
	if runErr != nil {
		return errors.New(friendlyBtopStreamError(stderr.String(), runErr))
	}
	return errors.New("Fleet telemetry stream ended")
}

func parseFleetTelemetryLine(line, target string, collectedAt time.Time) (telemetrySample, error) {
	fields := strings.Split(strings.TrimSpace(line), "\t")
	if len(fields) != 12 || fields[0] != fleetTelemetryProtocol {
		return telemetrySample{}, errors.New("invalid Fleet telemetry frame")
	}
	parseInt := func(index int) int64 {
		value, _ := strconv.ParseInt(sanitizeMetadataValue(fields[index]), 10, 64)
		return max(int64(0), value)
	}
	parseUint := func(index int) uint64 {
		value, _ := strconv.ParseUint(sanitizeMetadataValue(fields[index]), 10, 64)
		return value
	}
	load, _ := strconv.ParseFloat(sanitizeMetadataValue(fields[4]), 64)
	sample := telemetrySample{
		Target: target, CollectedAt: collectedAt,
		Uptime:          time.Duration(parseInt(3)) * time.Second,
		LoadOne:         max(0, load),
		CPUCores:        int(parseInt(5)),
		MemoryTotal:     parseUint(6),
		NetworkRX:       parseUint(8),
		NetworkTX:       parseUint(9),
		CPUCounterTotal: parseUint(10),
		CPUCounterIdle:  parseUint(11),
	}
	available := parseUint(7)
	sample.MemoryUsed = sample.MemoryTotal - min(sample.MemoryTotal, available)
	if sample.CPUCores == 0 && sample.MemoryTotal == 0 && sample.Uptime == 0 {
		return telemetrySample{}, errors.New("empty Fleet telemetry frame")
	}
	return sample, nil
}
