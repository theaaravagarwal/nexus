package main

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

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

func TestTelemetryHistoryComputesRatesAndStaysBounded(t *testing.T) {
	start := time.Now()
	history := []telemetrySample{{
		CollectedAt: start, NetworkRX: 1_000, NetworkTX: 2_000,
	}}
	history = appendTelemetry(history, telemetrySample{
		CollectedAt: start.Add(10 * time.Second), NetworkRX: 11_000, NetworkTX: 22_000,
	})
	got := history[len(history)-1]
	if got.NetworkRXRate != 1_000 || got.NetworkTXRate != 2_000 {
		t.Fatalf("rates=(%v,%v)", got.NetworkRXRate, got.NetworkTXRate)
	}
	for index := 0; index < telemetryHistorySize+10; index++ {
		history = appendTelemetry(history, telemetrySample{CollectedAt: start.Add(time.Duration(index+20) * time.Second)})
	}
	if len(history) != telemetryHistorySize {
		t.Fatalf("history length=%d", len(history))
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
