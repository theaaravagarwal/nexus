package app

import (
	"fmt"
	"time"
)

// windowsTelemetryProbe is the PowerShell body shared by the one-shot and
// streaming Windows telemetry scripts. It fills the same variables the POSIX
// scripts derive from /proc, using raw performance counters so the existing
// delta arithmetic (CPUCounterTotal/CPUCounterIdle, NetworkRX/TX) applies:
// Win32_PerfRawData_PerfOS_Processor(_Total).PercentProcessorTime is the
// cumulative idle time in 100 ns units and Timestamp_Sys100NS the matching
// total. ProcessorQueueLength stands in for the load average, which Windows
// does not have.
const windowsTelemetryProbe = `
$os = Get-CimInstance Win32_OperatingSystem
$now = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
$uptime = [int64](New-TimeSpan -Start $os.LastBootUpTime -End (Get-Date)).TotalSeconds
$cores = [int]$env:NUMBER_OF_PROCESSORS
$memoryTotal = [uint64]$os.TotalVisibleMemorySize * 1024
$memoryAvailable = [uint64]$os.FreePhysicalMemory * 1024
$cpu = Get-CimInstance Win32_PerfRawData_PerfOS_Processor -Filter "Name='_Total'"
$cpuTotal = [uint64]$cpu.Timestamp_Sys100NS
$cpuIdle = [uint64]$cpu.PercentProcessorTime
$queue = Get-CimInstance Win32_PerfRawData_PerfOS_System
$load = [double]$queue.ProcessorQueueLength
$rx = [uint64]0; $tx = [uint64]0
foreach ($nic in (Get-CimInstance Win32_PerfRawData_Tcpip_NetworkInterface)) {
  $rx += [uint64]$nic.BytesReceivedPersec
  $tx += [uint64]$nic.BytesSentPersec
}
`

// windowsGPUTelemetryProbe appends GPU_TELEMETRY lines when nvidia-smi is
// available, in the same layout the POSIX script produces.
const windowsGPUTelemetryProbe = `
$smi = Get-Command nvidia-smi -ErrorAction SilentlyContinue
if ($smi) {
  foreach ($row in (& $smi.Source --query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu --format=csv,noheader,nounits 2>$null)) {
    $parts = $row -split ',' | ForEach-Object { $_.Trim() }
    if ($parts.Count -eq 5) { Write-Output ("GPU_TELEMETRY=" + ($parts -join [char]9)) }
  }
}
`

// windowsTelemetryScript is the one-shot sample used by telemetryCommand.
func windowsTelemetryScript() string {
	return "$ErrorActionPreference = 'SilentlyContinue'\n" + windowsTelemetryProbe +
		"Write-Output ('TELEMETRY=' + (@($uptime, $load, $cores, $memoryTotal, $memoryAvailable, $rx, $tx) -join [char]9))\n" +
		windowsGPUTelemetryProbe
}

func windowsTelemetryCommand() string {
	return "powershell -NoProfile -NonInteractive -EncodedCommand " +
		encodePowerShellCommand(windowsTelemetryScript())
}

// windowsFleetTelemetryScript streams NX1 frames every interval. Output goes
// through kernel32 WriteFile so a closed ssh channel ends the loop: sshd on
// Windows never kills the command and .NET's console stream swallows the
// broken-pipe error (see windowsBtopStreamScript).
func windowsFleetTelemetryScript(interval time.Duration) string {
	seconds := max(1, int(interval/time.Second))
	return fmt.Sprintf(`$ErrorActionPreference = 'SilentlyContinue'
$sig = @'
[DllImport("kernel32.dll", SetLastError=true)] public static extern IntPtr GetStdHandle(int nStdHandle);
[DllImport("kernel32.dll", SetLastError=true)] public static extern bool WriteFile(IntPtr hFile, byte[] lpBuffer, uint nNumberOfBytesToWrite, out uint lpNumberOfBytesWritten, IntPtr lpOverlapped);
'@
$k = Add-Type -MemberDefinition $sig -Name NexusK32 -Namespace Nexus -PassThru
$h = $k::GetStdHandle(-11)
function Emit([string]$text) {
  $bytes = [Text.Encoding]::UTF8.GetBytes($text + [char]10)
  [uint32]$written = 0
  return $k::WriteFile($h, $bytes, $bytes.Length, [ref]$written, [IntPtr]::Zero)
}
if (-not (Emit '%s')) { exit 0 }
$seq = 0
while ($true) {
  $seq++
%s
  $line = @('%s', $seq, $now, $uptime, $load, $cores, $memoryTotal, $memoryAvailable, $rx, $tx, $cpuTotal, $cpuIdle) -join [char]9
  if (-not (Emit $line)) { exit 0 }
  Start-Sleep -Seconds %d
}
`, fleetTelemetryMarker, windowsTelemetryProbe, fleetTelemetryProtocol, seconds)
}

func windowsFleetTelemetryCommand(interval time.Duration) string {
	return "powershell -NoProfile -NonInteractive -EncodedCommand " +
		encodePowerShellCommand(windowsFleetTelemetryScript(interval))
}
