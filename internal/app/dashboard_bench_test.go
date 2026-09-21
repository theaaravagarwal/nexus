package app

import (
	"strings"
	"testing"
	"time"
)

func BenchmarkDashboardViewWorkbench(b *testing.B) {
	// Create 50 synthetic hosts with targets like user0@host0.example.com
	hosts := make([]string, 0, 50)
	for i := 0; i < 50; i++ {
		hosts = append(hosts, "user"+string(rune(48+i%10))+"@host"+string(rune(48+i%10))+".example.com")
	}

	model := newDashboardModel(hosts)
	model.width = 200
	model.height = 50
	model.experimentalTabs = true
	model.workspace = "workbench"

	// Populate some hosts with metadata and telemetry
	for i := 0; i < len(model.hosts) && i < 10; i++ {
		model.hosts[i].Reachability = reachabilityResult{
			Target: model.hosts[i].Target,
			Status: reachOnline,
			Latency: time.Duration((i + 1) * 5) * time.Millisecond,
		}
		model.telemetry[model.hosts[i].Target] = hostTelemetry{
			Current: telemetrySample{
				Target:         model.hosts[i].Target,
				CollectedAt:    time.Now(),
				Uptime:         time.Duration(i+1) * time.Hour,
				LoadOne:        0.5 + float64(i)*0.1,
				CPUCores:       4 + i,
				CPUUtilization: 25.0 + float64(i)*5.0,
				MemoryUsed:     uint64(i+1) * 1024 * 1024 * 100,
				MemoryTotal:    8 * 1024 * 1024 * 1024,
				NetworkRX:      uint64(i+1) * 1024 * 1024,
				NetworkTX:      uint64(i+1) * 512 * 1024,
			},
		}
	}

	b.ReportAllocs()
	for b.Loop() {
		_ = model.View()
	}
}

func BenchmarkDashboardViewConsole(b *testing.B) {
	// Create 50 synthetic hosts
	hosts := make([]string, 0, 50)
	for i := 0; i < 50; i++ {
		hosts = append(hosts, "user"+string(rune(48+i%10))+"@host"+string(rune(48+i%10))+".example.com")
	}

	model := newDashboardModel(hosts)
	model.width = 200
	model.height = 50
	model.experimentalTabs = true
	model.experimentalFleetBtop = true
	model.workspace = "console"
	model.btopStreamState = "live"
	model.btopStreamUpdatedAt = time.Now()

	// Create a canned 24-line ANSI frame with ANSI codes and keywords
	frameLines := make([]string, 24)
	frameLines[0] = "\x1b[31mcpu: 45% idle\x1b[0m"
	frameLines[1] = "\x1b[32mmem: 2.5GB / 8GB\x1b[0m"
	frameLines[2] = "\x1b[33mnet: rx:100Mb/s tx:50Mb/s\x1b[0m"
	for i := 3; i < 20; i++ {
		frameLines[i] = "\x1b[36mproc: " + strings.Repeat(".", 30) + "\x1b[0m"
	}
	frameLines[20] = "\x1b[35mload avg: 1.2 0.8 0.5\x1b[0m"
	frameLines[21] = "\x1b[34muptime: 45 days\x1b[0m"
	frameLines[22] = "\x1b[31mwarning: high cpu\x1b[0m"
	frameLines[23] = "\x1b[0mend of frame\x1b[0m"

	btopFrame := strings.Join(frameLines, "\n")

	// Populate telemetry with btop frame for first host
	if len(model.hosts) > 0 {
		model.hosts[0].Reachability = reachabilityResult{
			Target: model.hosts[0].Target,
			Status: reachOnline,
			Latency: 5 * time.Millisecond,
		}
		model.telemetry[model.hosts[0].Target] = hostTelemetry{
			Current: telemetrySample{
				Target:         model.hosts[0].Target,
				CollectedAt:    time.Now(),
				BtopInstalled:  true,
				BtopFrame:      btopFrame,
			},
		}
		model.telemetryTarget = model.hosts[0].Target
	}

	b.ReportAllocs()
	for b.Loop() {
		_ = model.View()
	}
}

func BenchmarkTruncateText(b *testing.B) {
	// Create a 200-rune string with ANSI codes and CJK/emoji runes
	var sb strings.Builder
	ansiCodes := []string{
		"\x1b[31m", "\x1b[32m", "\x1b[33m", "\x1b[34m", "\x1b[35m", "\x1b[36m", "\x1b[0m",
	}
	cjkRunes := []rune{'中', '文', '日', '本', '語', '한', '글', '🚀', '🎯', '✓'}

	runes := 0
	for runes < 200 {
		if runes%20 == 0 {
			sb.WriteString(ansiCodes[runes/20%len(ansiCodes)])
		}
		sb.WriteRune(cjkRunes[runes%len(cjkRunes)])
		runes++
	}

	input := sb.String()

	b.ReportAllocs()
	for b.Loop() {
		_ = truncateText(input, 40)
	}
}

func BenchmarkFinishView(b *testing.B) {
	model := newDashboardModel([]string{"alice@one"})
	model.width = 200
	model.height = 50

	// Create a 200x50 block of text
	lines := make([]string, 50)
	for i := 0; i < 50; i++ {
		lines[i] = strings.Repeat("x", 200)
	}
	view := strings.Join(lines, "\n")

	b.ReportAllocs()
	for b.Loop() {
		_ = model.finishView(view)
	}
}
