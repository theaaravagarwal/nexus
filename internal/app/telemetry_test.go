package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestBtopStreamCapturesMultipleRemoteFramesWhenHostIsProvided(t *testing.T) {
	target := os.Getenv("NEXUS_TEST_SSH_HOST")
	if target == "" {
		t.Skip("set NEXUS_TEST_SSH_HOST to run the remote btop integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	events := make(chan btopStreamEventMsg, 1)
	startedAt := time.Now()
	firstFrameAt := time.Time{}
	const columns, rows = 138, 27
	go streamRemoteBtop(ctx, target, 7, columns, rows, events)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("remote btop stream ended before two frames")
			}
			if event.Error != "" {
				t.Fatal(event.Error)
			}
			if event.Frame != "" && firstFrameAt.IsZero() {
				firstFrameAt = time.Now()
			}
			if event.FrameCount < 2 {
				continue
			}
			plainFrame := strings.ToLower(ansi.Strip(event.Frame))
			if !strings.Contains(plainFrame, "cpu") || !strings.Contains(plainFrame, "proc") {
				t.Fatalf("remote stream is not a recognizable btop frame:\n%s", plainFrame)
			}
			if height := len(strings.Split(event.Frame, "\n")); height != rows {
				t.Fatalf("remote frame height=%d, want exact Monitor viewport height %d", height, rows)
			}
			t.Logf("first frame=%s, second frame=%s", firstFrameAt.Sub(startedAt), time.Since(startedAt))
			return
		case <-ctx.Done():
			t.Fatal("timed out waiting for two remote btop frames")
		}
	}
}

func TestFleetTelemetryStreamsMultipleSamplesWhenHostIsProvided(t *testing.T) {
	target := os.Getenv("NEXUS_TEST_SSH_HOST")
	if target == "" {
		t.Skip("set NEXUS_TEST_SSH_HOST to run the remote Fleet telemetry integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	events := make(chan fleetTelemetryEvent, 4)
	done := make(chan error, 1)
	go func() {
		done <- streamRemoteFleetTelemetry(ctx, target, 11, time.Second, func(event fleetTelemetryEvent) {
			events <- event
		})
	}()
	for count := 0; count < 2; {
		select {
		case event := <-events:
			if event.Err != nil {
				t.Fatal(event.Err)
			}
			if event.Generation != 11 || event.Target != target || event.Sample.MemoryTotal == 0 {
				t.Fatalf("invalid remote Fleet sample: %#v", event)
			}
			count++
		case err := <-done:
			t.Fatalf("Fleet telemetry ended early: %v", err)
		case <-ctx.Done():
			t.Fatal("timed out waiting for two Fleet telemetry samples")
		}
	}
}

func TestBtopStreamPoolReusesSelectedRemoteHostWhenHostIsProvided(t *testing.T) {
	target := os.Getenv("NEXUS_TEST_SSH_HOST")
	if target == "" {
		t.Skip("set NEXUS_TEST_SSH_HOST to run the remote btop integration test")
	}
	pool := newBtopStreamPool()
	defer pool.close()
	_, started := pool.activate(target, 90, 28)
	if !started {
		t.Fatal("first visit did not start a btop stream")
	}
	var first btopStreamEventMsg
	deadline := time.After(8 * time.Second)
	for first.Frame == "" {
		select {
		case event := <-pool.updates:
			// Progress-only events (e.g. Stage "connecting"/"fitting" while
			// the layout-fitting ladder finds a shown_boxes set that fits
			// this viewport) carry no frame yet; keep waiting for the
			// first real one.
			if event.Error != "" {
				t.Fatalf("first Monitor frame failed: %#v", event)
			}
			if event.Frame != "" {
				first = event
			}
		case <-deadline:
			t.Fatal("timed out waiting for first Monitor frame")
		}
	}

	cacheDeadline := time.Now().Add(3 * time.Second)
	var cached btopStreamEventMsg
	for time.Now().Before(cacheDeadline) {
		pool.mu.Lock()
		session := pool.sessions[target]
		if session != nil {
			cached = session.latest
		}
		pool.mu.Unlock()
		if cached.FrameCount > first.FrameCount {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if cached.FrameCount <= first.FrameCount {
		t.Fatal("selected btop session stopped updating")
	}
	warm, restarted := pool.activate(target, 90, 28)
	if restarted || warm.Frame == "" || warm.FrameCount < cached.FrameCount {
		t.Fatalf("selected host did not reuse its live session: restarted=%t event=%#v", restarted, warm)
	}
}

func TestParseFleetTelemetryLine(t *testing.T) {
	now := time.Now()
	sample, err := parseFleetTelemetryLine(
		"NX1\t8\t1770000000\t86461\t2.50\t16\t32000000000\t8000000000\t1000\t2000\t9000\t3000",
		"alice@one", now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if sample.Target != "alice@one" || sample.CollectedAt != now ||
		sample.Uptime != 86461*time.Second || sample.LoadOne != 2.5 ||
		sample.CPUCores != 16 || sample.MemoryUsed != 24_000_000_000 ||
		sample.NetworkRX != 1000 || sample.NetworkTX != 2000 ||
		sample.CPUCounterTotal != 9000 || sample.CPUCounterIdle != 3000 {
		t.Fatalf("sample=%#v", sample)
	}
}

func TestFleetTelemetryProtocolRejectsMalformedFrames(t *testing.T) {
	for _, line := range []string{
		"", "NX0\t1\t2\t3\t4\t5\t6\t7\t8\t9\t10\t11",
		"NX1\t1\t2\t3", "NX1\t1\t2\t0\t0\t0\t0\t0\t0\t0\t0\t0",
	} {
		if _, err := parseFleetTelemetryLine(line, "alice@one", time.Now()); err == nil {
			t.Fatalf("malformed frame was accepted: %q", line)
		}
	}
}

func TestFleetTelemetrySpecsAreAdaptiveAndBounded(t *testing.T) {
	hosts := make([]string, 20)
	for index := range hosts {
		hosts[index] = "alice@host" + string(rune('a'+index))
	}
	model := newDashboardModel(hosts)
	model.workspace = "fleet"
	model.height = 15
	for index := range model.hosts {
		model.hosts[index].Reachability.Status = reachOnline
	}

	specs := model.fleetTelemetrySpecs("fleet")
	if len(specs) != fleetTelemetryMaxStreams {
		t.Fatalf("streams=%d, want %d", len(specs), fleetTelemetryMaxStreams)
	}
	intervals := make(map[string]time.Duration, len(specs))
	for _, spec := range specs {
		if _, duplicate := intervals[spec.Target]; duplicate {
			t.Fatalf("duplicate stream for %q", spec.Target)
		}
		intervals[spec.Target] = spec.Interval
	}
	if intervals[model.selectedTarget()] != 3*time.Second {
		t.Fatalf("selected host interval=%s", intervals[model.selectedTarget()])
	}
	seenVisible, seenOffscreen := false, false
	for _, interval := range intervals {
		seenVisible = seenVisible || interval == 5*time.Second
		seenOffscreen = seenOffscreen || interval == 15*time.Second
	}
	if !seenVisible || !seenOffscreen {
		t.Fatalf("adaptive intervals missing: %#v", intervals)
	}
}

func TestParseTelemetryCollectsSystemAndMultipleGPUs(t *testing.T) {
	sample, err := parseTelemetry(strings.Join([]string{
		"TELEMETRY=86461\t2.50\t16\t32000000000\t8000000000\t1000\t2000",
		"GPU_TELEMETRY=NVIDIA RTX 4090\t75\t1024\t24576\t68",
		"GPU_TELEMETRY=Intel UHD 770\t12\t0\t0\t0",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if sample.Uptime != 86461*time.Second || sample.LoadOne != 2.5 ||
		sample.CPUCores != 16 || sample.MemoryUsed != 24_000_000_000 ||
		len(sample.GPUs) != 2 || sample.GPUs[0].Utilization != 75 {
		t.Fatalf("sample=%#v", sample)
	}
}

func TestBtopStreamCommandUsesRequestedViewportWithoutForcingAFloor(t *testing.T) {
	if strings.Contains(telemetryScript, btopFrameMarker) {
		t.Fatal("ordinary telemetry unexpectedly includes btop streaming")
	}
	// The remote PTY size must track the real Monitor viewport (however
	// small), not a hardcoded 80x24 floor: the layout-fitting ladder is
	// what decides whether btop can actually render at that size.
	script := btopStreamCommand(72, 12, "")
	for _, want := range []string{
		btopUnavailableMarker, btopFrameMarker, "command -v btop", "stty cols 72 rows 12",
		"exec btop",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("btop capture script missing %q:\n%s", want, script)
		}
	}
}

func TestBtopStreamCommandWithBoxesBuildsTemporaryFittedConfig(t *testing.T) {
	script := btopStreamCommand(138, 27, "cpu mem net proc")
	for _, want := range []string{
		btopUnavailableMarker, btopFrameMarker, "stty cols 138 rows 27",
		`shown_boxes = "cpu mem net proc"`, "update_ms = 1000",
		"XDG_CONFIG_HOME=", "mktemp -d", "rm -rf \"$tmp\"",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("fitted btop script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "exec btop") {
		t.Fatal("fitted btop script must not exec so the temp config gets cleaned up")
	}
}

func TestBtopStreamBackpressureKeepsNewestFrame(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan btopStreamEventMsg, 1)
	if !publishBtopStreamEvent(ctx, events, btopStreamEventMsg{Frame: "old", FrameCount: 1}) ||
		!publishBtopStreamEvent(ctx, events, btopStreamEventMsg{Frame: "new", FrameCount: 2}) {
		t.Fatal("stream publisher stopped unexpectedly")
	}
	message := <-events
	if message.Frame != "new" || message.FrameCount != 2 {
		t.Fatalf("backpressure retained stale frame: %#v", message)
	}
}

func TestBtopStreamErrorsExplainRecovery(t *testing.T) {
	tests := map[string]string{
		"Permission denied (publickey,password)": "authentication required",
		"Host key verification failed":           "Host key approval required",
		"connect to host: Operation timed out":   "connection timed out",
	}
	for detail, want := range tests {
		if got := friendlyBtopStreamError(detail, nil); !strings.Contains(got, want) {
			t.Fatalf("friendlyBtopStreamError(%q)=%q, want %q", detail, got, want)
		}
	}
}

func TestSplitAndRenderBtopOutputReplaysRealTerminalFrame(t *testing.T) {
	output := "TELEMETRY=60\t0.2\t2\t1000\t500\t10\t20\r\nBTOP_STATUS=available\r\n" +
		btopFrameMarker + "\r\n\x1b[2J\x1b[H\x1b[32m┌─ cpu ─┐\x1b[2;1H│ 42%    │\x1b[3;1H└─────────┘\x1b[0m"
	metadata, raw, ok := splitBtopOutput(output)
	if !ok || !strings.Contains(metadata, "BTOP_STATUS=available") {
		t.Fatalf("split failed: ok=%t metadata=%q raw=%q", ok, metadata, raw)
	}
	frame := renderBtopFrame(raw, 40, 12)
	for _, want := range []string{"cpu", "42%", "└─────────┘"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("rendered btop frame missing %q:\n%q", want, frame)
		}
	}
}

func TestRenderBtopFrameKeepsSnapshotBeforeAlternateScreenTeardown(t *testing.T) {
	raw := "\x1b[?1049h\x1b[?2026h\x1b[2J\x1b[H┌ cpu ┐\x1b[2;1H│ 42% │" +
		"\x1b[3;1H├ proc ┤\x1b[4;1H│ worker │\x1b[?2026l" +
		"\x1b[?1049lKilled\r\n"
	frame := ansi.Strip(renderBtopFrame(raw, 80, 24))
	if !strings.Contains(frame, "cpu") || !strings.Contains(frame, "proc") ||
		!strings.Contains(frame, "worker") {
		t.Fatalf("completed btop snapshot was lost during teardown:\n%s", frame)
	}
}

func TestTelemetryHistoryComputesRatesAndStaysBounded(t *testing.T) {
	start := time.Now()
	history := []telemetrySample{{
		CollectedAt: start, NetworkRX: 1_000, NetworkTX: 2_000,
		CPUCounterTotal: 1_000, CPUCounterIdle: 600,
	}}
	history = appendTelemetry(history, telemetrySample{
		CollectedAt: start.Add(10 * time.Second), NetworkRX: 11_000, NetworkTX: 22_000,
		CPUCounterTotal: 2_000, CPUCounterIdle: 800,
	})
	got := history[len(history)-1]
	if got.NetworkRXRate != 1_000 || got.NetworkTXRate != 2_000 || got.CPUUtilization != 80 {
		t.Fatalf("rates=(%v,%v) cpu=%v", got.NetworkRXRate, got.NetworkTXRate, got.CPUUtilization)
	}
	for index := 0; index < telemetryHistorySize+10; index++ {
		history = appendTelemetry(history, telemetrySample{CollectedAt: start.Add(time.Duration(index+20) * time.Second)})
	}
	if len(history) != telemetryHistorySize {
		t.Fatalf("history length=%d", len(history))
	}
}

func TestTelemetryKeepsBtopFrameOnlyInCurrentSample(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	model.hosts[0].Reachability.Status = reachOnline
	model.telemetryFlight = model.telemetryGen
	model.telemetry["alice@one"] = hostTelemetry{Current: telemetrySample{
		BtopInstalled: true, BtopFrame: "┌─ cpu ─┐",
	}}
	sample := telemetrySample{Target: "alice@one", CollectedAt: time.Now(), Uptime: time.Hour}
	updated, _ := model.Update(telemetryResultMsg{
		Generation: model.telemetryGen, Target: "alice@one", Sample: sample,
	})
	entry := updated.(dashboardModel).telemetry["alice@one"]
	if entry.Current.BtopFrame == "" || !entry.Current.BtopInstalled {
		t.Fatalf("current btop frame=%#v", entry.Current)
	}
	if len(entry.History) != 1 || entry.History[0].BtopFrame != "" || entry.History[0].BtopInstalled {
		t.Fatalf("btop frame retained in history: %#v", entry.History)
	}
}

func TestTelemetryBackoffIsCappedAndResettable(t *testing.T) {
	previous := time.Duration(0)
	for failures := 0; failures < 10; failures++ {
		delay := telemetryBackoff(failures, 3)
		if delay < previous || delay > telemetryMaxBackoff {
			t.Fatalf("failures=%d delay=%s previous=%s", failures, delay, previous)
		}
		previous = delay
	}
	if got := telemetryBackoff(0, 0); got != telemetryInterval {
		t.Fatalf("reset delay=%s", got)
	}
}

func TestTelemetryTickIsOfflineSafeAndOneFlight(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	model.hosts[0].Reachability = reachabilityResult{Target: "alice@one", Status: reachTimeout}
	updated, _ := model.Update(telemetryTickMsg{Generation: model.telemetryGen})
	model = updated.(dashboardModel)
	if model.telemetryFlight != 0 {
		t.Fatal("offline host started telemetry")
	}

	model.hosts[0].Reachability.Status = reachOnline
	updated, _ = model.Update(telemetryTickMsg{Generation: model.telemetryGen})
	model = updated.(dashboardModel)
	if model.telemetryFlight != model.telemetryGen {
		t.Fatal("online host did not start telemetry")
	}
	flight := model.telemetryFlight
	updated, _ = model.Update(telemetryTickMsg{Generation: model.telemetryGen})
	model = updated.(dashboardModel)
	if model.telemetryFlight != flight {
		t.Fatal("second tick replaced in-flight telemetry")
	}
}

func TestMonitorBtopVisibilityDoesNotDependOnReachabilityProbe(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	model.experimentalTabs = true
	model.experimentalFleetBtop = true
	model.workspace = "console"
	model.width, model.height = 160, 36
	model.hosts[0].Reachability = reachabilityResult{Target: "alice@one", Status: reachTimeout}

	if _, _, visible := model.monitorBtopViewport(); !visible {
		t.Fatal("Monitor btop was hidden by a reachability probe false negative")
	}
}

func TestDashboardConsumesContinuousBtopFrames(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	model.btopStreamTarget = "alice@one"
	model.btopStreamGeneration = 9
	model.btopStreamPool = newBtopStreamPool()
	t.Cleanup(model.closeBtopStreams)
	now := time.Now()

	updated, command := model.Update(btopStreamEventMsg{
		Generation: 9, Target: "alice@one", Frame: "┌ cpu 10% ┐\n┌ proc one ┐",
		FrameCount: 1, UpdatedAt: now, Installed: true,
	})
	model = updated.(dashboardModel)
	if command == nil || model.btopStreamFrames != 1 ||
		!strings.Contains(model.telemetry["alice@one"].Current.BtopFrame, "proc one") {
		t.Fatal("first live btop frame was not retained")
	}

	updated, _ = model.Update(btopStreamEventMsg{
		Generation: 9, Target: "alice@one", Frame: "┌ cpu 20% ┐\n┌ proc two ┐",
		FrameCount: 2, UpdatedAt: now.Add(time.Second), Installed: true,
	})
	model = updated.(dashboardModel)
	frame := model.telemetry["alice@one"].Current.BtopFrame
	if model.btopStreamFrames != 2 || !strings.Contains(frame, "proc two") || strings.Contains(frame, "proc one") {
		t.Fatalf("second live btop frame did not replace the first: %q", frame)
	}
}

func TestBtopStreamPoolReusesWarmSessionAndCachedFrame(t *testing.T) {
	pool := newBtopStreamPool()
	defer pool.close()
	want := btopStreamEventMsg{
		Generation: 4, Target: "alice@one", Frame: "cached frame", FrameCount: 8,
	}
	pool.sessions[want.Target] = &pooledBtopStream{
		target: want.Target, generation: want.Generation, columns: 90, rows: 28,
		running: true, latest: want,
	}

	got, started := pool.activate(want.Target, 90, 28)
	if started || got.Frame != want.Frame || got.FrameCount != want.FrameCount {
		t.Fatalf("warm stream was not reused: started=%t event=%#v", started, got)
	}
}

func TestBtopStreamPoolDeactivateStopsAndRemovesActiveSession(t *testing.T) {
	pool := newBtopStreamPool()
	defer pool.close()
	cancelled := false
	session := &pooledBtopStream{
		target: "alice@one", generation: 2, running: true,
		cancel: func() { cancelled = true },
	}
	pool.sessions[session.target] = session
	pool.active = session.target

	pool.deactivate()
	if !cancelled || pool.active != "" || len(pool.sessions) != 0 {
		t.Fatalf("active session was not fully stopped: cancelled=%t active=%q sessions=%d", cancelled, pool.active, len(pool.sessions))
	}
}

func TestMonitorAndFleetUsePersistentTelemetryInsteadOfOneShotPolling(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	model.experimentalTabs = true
	model.experimentalFleetBtop = true
	model.width, model.height = 160, 36
	model.hosts[0].Reachability.Status = reachOnline
	model.telemetryFocused = true
	defer model.closeBtopStreams()
	defer model.closeFleetTelemetry()

	for _, workspace := range []string{"console", "fleet"} {
		model.workspace = workspace
		updated, _ := model.Update(telemetryTickMsg{Generation: model.telemetryGen})
		model = updated.(dashboardModel)
		if model.telemetryFlight != 0 {
			t.Fatalf("%s started one-shot telemetry alongside persistent telemetry", workspace)
		}
	}
}

func TestTelemetryStaleResultCannotPaintNewSelectionOrCreateActivity(t *testing.T) {
	model := newDashboardModel([]string{"alice@one", "bob@two"})
	oldGeneration := model.telemetryGen
	model.moveCursor(1)
	model.resetTelemetryTarget()
	updated, _ := model.Update(telemetryResultMsg{
		Generation: oldGeneration,
		Target:     "alice@one",
		Sample: telemetrySample{
			Target: "alice@one", CollectedAt: time.Now(), Uptime: time.Hour,
		},
	})
	model = updated.(dashboardModel)
	if _, exists := model.telemetry["alice@one"]; exists {
		t.Fatal("stale result updated telemetry")
	}
	if model.operation != nil || len(model.activities) != 0 {
		t.Fatal("automatic telemetry created operation history")
	}
}

func TestTelemetryFailureBacksOffWithoutChangingUsageOrOperations(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	model.hosts[0].Reachability.Status = reachOnline
	model.telemetryFlight = model.telemetryGen
	updated, _ := model.Update(telemetryResultMsg{
		Generation: model.telemetryGen,
		Target:     "alice@one",
		Err:        context.DeadlineExceeded,
	})
	model = updated.(dashboardModel)
	entry := model.telemetry["alice@one"]
	if entry.Failures != 1 || !entry.NextAttempt.After(time.Now()) {
		t.Fatalf("failure did not establish backoff: %#v", entry)
	}
	if len(model.activities) != 0 || model.operation != nil ||
		model.hosts[0].Score != 0 || len(model.actionUses) != 0 {
		t.Fatal("telemetry failure changed user activity state")
	}
}

func TestTelemetryPausesOnBlur(t *testing.T) {
	model := newDashboardModel([]string{"alice@one"})
	updated, _ := model.Update(tea.BlurMsg{})
	model = updated.(dashboardModel)
	if model.telemetryFocused {
		t.Fatal("blur did not pause telemetry")
	}
	updated, _ = model.Update(tea.FocusMsg{})
	if !updated.(dashboardModel).telemetryFocused {
		t.Fatal("focus did not resume telemetry")
	}
}
