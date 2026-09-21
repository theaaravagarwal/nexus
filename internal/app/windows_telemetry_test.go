package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A Windows host (probe answers Windows_NT) must receive the encoded
// PowerShell telemetry scripts instead of the POSIX sh wrapper, and the
// frames they emit must parse like the POSIX ones.
func TestWindowsTelemetryUsesPowerShellScripts(t *testing.T) {
	argsPath := filepath.Join(t.TempDir(), "args.txt")
	script := fmt.Sprintf(`#!/bin/sh
%s
eval "last=\${$#}"
if [ "$last" = "echo %%OS%%" ]; then printf 'Windows_NT\r\n'; exit 0; fi
printf '%%s\n' "$@" >> %q
case "$last" in
  *NexusK32*|*Emit*) ;;
esac
printf 'NEXUS_FLEET_TELEMETRY_BEGIN\r\n'
printf 'NX1\t1\t1700000000\t3600\t1\t32\t34070192128\t15752916992\t20148737769\t1160417792\t100000000\t90000000\r\n'
printf 'TELEMETRY=3600\t1\t32\t34070192128\t15752916992\t20148737769\t1160417792\r\n'
printf 'GPU_TELEMETRY=RTX\t7\t512\t8192\t41\r\n'
sleep 1
`, btopFakeHandlesVer, argsPath)
	setupBtopFakeSSH(t, script)

	msg, ok := telemetryCommand("robot@windows.example", 4)().(telemetryResultMsg)
	if !ok || msg.Err != nil {
		t.Fatalf("one-shot telemetry failed: %#v", msg)
	}
	if msg.Sample.CPUCores != 32 || msg.Sample.MemoryTotal != 34070192128 || len(msg.Sample.GPUs) != 1 {
		t.Fatalf("one-shot sample not parsed: %#v", msg.Sample)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events := make(chan fleetTelemetryEvent, 4)
	go func() {
		_ = streamRemoteFleetTelemetry(ctx, "robot@windows.example", 2, time.Second, func(event fleetTelemetryEvent) {
			events <- event
		})
	}()
	select {
	case event := <-events:
		if event.Err != nil || event.Sample.CPUCores != 32 || event.Sample.CPUCounterTotal != 100000000 {
			t.Fatalf("fleet sample not parsed: %#v", event)
		}
	case <-ctx.Done():
		t.Fatal("no fleet sample arrived")
	}
	cancel()

	raw, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	commands := []string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "powershell -NoProfile -NonInteractive -EncodedCommand ") {
			commands = append(commands, strings.TrimPrefix(line, "powershell -NoProfile -NonInteractive -EncodedCommand "))
		}
	}
	if len(commands) != 2 {
		t.Fatalf("expected two encoded PowerShell commands, got %d in:\n%s", len(commands), raw)
	}
	if strings.Contains(string(raw), "sh -c") {
		t.Fatalf("POSIX wrapper sent to a Windows host:\n%s", raw)
	}
	oneShot, err := decodePowerShellCommand(commands[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Win32_OperatingSystem", "Win32_PerfRawData_PerfOS_Processor", "Win32_PerfRawData_Tcpip_NetworkInterface", "TELEMETRY=", "nvidia-smi"} {
		if !strings.Contains(oneShot, want) {
			t.Fatalf("one-shot script missing %q:\n%s", want, oneShot)
		}
	}
	fleet, err := decodePowerShellCommand(commands[1])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{fleetTelemetryMarker, fleetTelemetryProtocol, "WriteFile", "Start-Sleep -Seconds 1", "while ($true)"} {
		if !strings.Contains(fleet, want) {
			t.Fatalf("fleet script missing %q:\n%s", want, fleet)
		}
	}
	if strings.Contains(oneShot, "wsl") || strings.Contains(fleet, "wsl") {
		t.Fatal("telemetry scripts must not involve WSL")
	}
}
