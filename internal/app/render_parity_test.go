package app

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// updateRenderGoldens regenerates the render-parity golden files instead of
// comparing against them. Run: go test ./internal/app -run TestDashboardRenderParity -update
var updateRenderGoldens = flag.Bool("update", false, "update render parity golden files")

// renderParityNow is the fixed instant used as m.now for every render-parity
// scenario. Anything the view code measures against m.now (relativeTime for
// host.LastUsed / host.Updated) is therefore fully deterministic.
var renderParityNow = time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)

// renderParityEpoch is a fixed instant far enough in the past (more than a
// week before renderParityNow, and before any conceivable test run) that
// call sites which measure age against the real time.Now() instead of
// m.now (telemetry sample freshness, fleet inventory freshness, btop stream
// age) always land in relativeTime's ">7 days" bucket, which renders as
// value.Format("Jan 2") -- a value that depends only on the fixed timestamp,
// never on wall-clock time. That keeps those cells deterministic without
// touching the production code paths that legitimately use time.Now().
var renderParityEpoch = time.Date(2023, 1, 10, 8, 0, 0, 0, time.UTC)

// renderParityHosts returns a small, varied fleet: online/refused/timeout/
// unknown reachability, an alias + tags on one host, GPUs, multiple disks,
// and saved tools, so the golden renders exercise most host-row and detail
// formatting branches.
func renderParityHosts() []string {
	return []string{
		"alice@edge-1.example.com",
		"bob@build.example.com:2222",
		"carol@db.example.internal",
		"dave@10.0.0.5",
		"erin@archive.example.com",
		"frank@gpu-node.example.com",
	}
}

func renderParityConfigureLoadedConfig(t *testing.T) {
	t.Helper()
	previous := loadedConfig
	t.Cleanup(func() { loadedConfig = previous })
	loadedConfig = defaultAppConfig()
	loadedConfig.Commands = sanitizeCommands([]commandConfig{
		{ID: "uptime", Name: "Uptime", Description: "Show load and uptime", Command: "uptime"},
		{ID: "disk-report", Name: "Disk report", Description: "Full df -h listing", Command: "df -h"},
	})
	loadedConfig.HostProfiles = map[string]discoveryProfile{
		"alice@edge-1.example.com": {
			Alias: "edge-1",
			Tags:  []string{"gpu", "prod"},
		},
	}
}

func renderParityBaseModel(t *testing.T) dashboardModel {
	t.Helper()
	renderParityConfigureLoadedConfig(t)
	hosts := renderParityHosts()
	state := nexusState{Hosts: map[string]hostActivity{}}
	for i, target := range hosts {
		state.Hosts[target] = hostActivity{
			Score:    float64(10 - i),
			LastUsed: renderParityNow.Add(-time.Duration(i+1) * time.Hour),
			OS:       "Linux 6.8",
			CPU:      "8x Cortex-A76",
			GPUs:     []string{"NVIDIA RTX 4080 · 16GB VRAM"},
			Memory:   "12GB / 32GB",
			Disks: []diskUsage{
				{Filesystem: "/dev/sda1", Mountpoint: "/", FilesystemType: "ext4", UsedBytes: 300 << 30, AvailableBytes: 700 << 30, TotalBytes: 1000 << 30},
				{Filesystem: "/dev/sdb1", Mountpoint: "/data", FilesystemType: "ext4", UsedBytes: 900 << 30, AvailableBytes: 100 << 30, TotalBytes: 1000 << 30},
			},
			Tools:   []string{"tmux", "docker", "git"},
			Updated: renderParityEpoch,
		}
	}
	model := newDashboardModelWithState(hosts, state, renderParityNow)
	model.plain = false
	model.theme = themes["nexus"]
	model.probing = false
	model.probeTargets = map[string]bool{}
	model.probeQueue = nil
	model.probeInitial = nil
	statuses := []reachabilityStatus{reachOnline, reachOnline, reachRefused, reachTimeout, reachUnknown, reachOnline}
	for i := range model.hosts {
		status := statuses[i%len(statuses)]
		model.hosts[i].Reachability = reachabilityResult{
			Target:  model.hosts[i].Target,
			Status:  status,
			Latency: time.Duration(8+i*7) * time.Millisecond,
		}
	}
	model.applyFilter()
	model.telemetryTarget = model.selectedTarget()
	model.telemetry[hosts[0]] = hostTelemetry{
		Current: telemetrySample{
			Target:         hosts[0],
			CollectedAt:    renderParityEpoch,
			Uptime:         52 * time.Hour,
			LoadOne:        1.24,
			CPUCores:       8,
			CPUUtilization: 37.5,
			MemoryUsed:     6 * 1024 * 1024 * 1024,
			MemoryTotal:    32 * 1024 * 1024 * 1024,
			NetworkRX:      1024 * 1024,
			NetworkTX:      512 * 1024,
			NetworkRXRate:  2.5 * 1024 * 1024,
			NetworkTXRate:  512 * 1024,
			GPUs: []gpuTelemetry{
				{Name: "NVIDIA RTX 4080", Utilization: 42, MemoryUsed: 4 << 30, MemoryTotal: 16 << 30, Temperature: 61},
			},
		},
	}
	model.telemetry[hosts[len(hosts)-1]] = hostTelemetry{
		Current: telemetrySample{
			Target:         hosts[len(hosts)-1],
			CollectedAt:    renderParityEpoch,
			Uptime:         10 * time.Hour,
			LoadOne:        0.4,
			CPUCores:       16,
			CPUUtilization: 12,
			MemoryUsed:     8 * 1024 * 1024 * 1024,
			MemoryTotal:    64 * 1024 * 1024 * 1024,
			NetworkRXRate:  1024,
			NetworkTXRate:  1024,
		},
	}
	return model
}

func renderParityResize(model dashboardModel, width, height int) dashboardModel {
	updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return updated.(dashboardModel)
}

func renderParityCanonicalBtopFrame() string {
	lines := []string{
		"\x1b[31mcpu: 38% idle\x1b[0m",
		"\x1b[32mmem: 6.0GB / 32GB\x1b[0m",
		"\x1b[33mnet: rx:2.5MB/s tx:512KB/s\x1b[0m",
	}
	for i := 0; i < 18; i++ {
		lines = append(lines, "\x1b[36mproc: worker-"+string(rune('a'+i%26))+" ................\x1b[0m")
	}
	lines = append(lines,
		"\x1b[35mload avg: 1.24 0.98 0.80\x1b[0m",
		"\x1b[34muptime: 2 days, 4 hours\x1b[0m",
		"\x1b[0mend of frame\x1b[0m",
	)
	out := ""
	for i, line := range lines {
		if i > 0 {
			out += "\n"
		}
		out += line
	}
	return out
}

// TestDashboardRenderParity renders a fixed set of dashboardModel scenarios
// and compares View() byte-for-byte against golden fixtures. It exists to
// give the View()/finishView() performance work in this package a hard
// "zero visual change" contract: every optimization must keep this test
// green. Regenerate the goldens (only after confirming a change is an
// intentional visual change) with:
//
//	go test ./internal/app -run TestDashboardRenderParity -update
func TestDashboardRenderParity(t *testing.T) {
	scenarios := []struct {
		name  string
		build func(t *testing.T) dashboardModel
	}{
		{
			name: "workbench_100x30",
			build: func(t *testing.T) dashboardModel {
				m := renderParityBaseModel(t)
				m.experimentalTabs = false
				m.workspace = "workbench"
				return renderParityResize(m, 100, 30)
			},
		},
		{
			name: "workbench_200x50_tabs",
			build: func(t *testing.T) dashboardModel {
				m := renderParityBaseModel(t)
				m.experimentalTabs = true
				m.workspace = "workbench"
				return renderParityResize(m, 200, 50)
			},
		},
		{
			name: "console_200x50_btop_activity",
			build: func(t *testing.T) dashboardModel {
				m := renderParityBaseModel(t)
				m.experimentalTabs = true
				m.experimentalFleetBtop = true
				m.workspace = "console"
				m = renderParityResize(m, 200, 50)
				m.btopStreamState = "live"
				m.btopStreamUpdatedAt = renderParityEpoch
				m.btopStreamColumns = 160
				m.btopStreamRows = 40
				hosts := renderParityHosts()
				sample := m.telemetry[hosts[0]]
				sample.Current.BtopInstalled = true
				sample.Current.BtopFrame = renderParityCanonicalBtopFrame()
				m.telemetry[hosts[0]] = sample
				m.telemetryTarget = hosts[0]
				m.activityOpen = true
				m.activities = []activityEvent{
					{Label: "Refresh info", Host: hosts[0], Status: "success", Summary: "Updated snapshot", Duration: 820 * time.Millisecond},
					{Label: "Storage", Host: hosts[1], Status: "error", Summary: "SSH timed out", Duration: 3 * time.Second},
					{Label: "Copy key", Host: hosts[2], Status: "success", Summary: "Key installed", Duration: 410 * time.Millisecond},
				}
				return m
			},
		},
		{
			name: "fleet_200x50",
			build: func(t *testing.T) dashboardModel {
				m := renderParityBaseModel(t)
				m.experimentalTabs = true
				m.workspace = "fleet"
				return renderParityResize(m, 200, 50)
			},
		},
		{
			name: "help_overlay",
			build: func(t *testing.T) dashboardModel {
				m := renderParityBaseModel(t)
				m = renderParityResize(m, 120, 40)
				m.helpOpen = true
				return m
			},
		},
		{
			name: "command_palette",
			build: func(t *testing.T) dashboardModel {
				m := renderParityBaseModel(t)
				m = renderParityResize(m, 120, 40)
				m.commandOpen = true
				return m
			},
		},
		{
			name: "settings",
			build: func(t *testing.T) dashboardModel {
				m := renderParityBaseModel(t)
				m = renderParityResize(m, 120, 40)
				m.settingsOpen = true
				return m
			},
		},
		{
			name: "theme_picker",
			build: func(t *testing.T) dashboardModel {
				m := renderParityBaseModel(t)
				m = renderParityResize(m, 120, 40)
				m.openThemePreview()
				return m
			},
		},
		{
			name: "narrow_59x20",
			build: func(t *testing.T) dashboardModel {
				m := renderParityBaseModel(t)
				m.experimentalTabs = false
				return renderParityResize(m, 59, 20)
			},
		},
	}

	goldenDir := filepath.Join("testdata", "render")
	if *updateRenderGoldens {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			model := scenario.build(t)
			got := model.View()
			goldenPath := filepath.Join(goldenDir, scenario.name+".golden")
			if *updateRenderGoldens {
				if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden %s: %v (run with -update to generate it)", goldenPath, err)
			}
			if got != string(want) {
				t.Fatalf("render mismatch for %s (run with -update to inspect/regenerate)\n--- got ---\n%s\n--- want ---\n%s", scenario.name, got, string(want))
			}
		})
	}
}
