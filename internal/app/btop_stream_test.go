package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// setupBtopFakeSSH writes a fake ssh binary at <dir>/ssh and points PATH at
// dir only, so exec.Command("ssh", ...) inside streamRemoteBtop resolves to
// it hermetically (see docs/ARCHITECTURE.md's "hermetic tests" convention
// and the fake-binary pattern already used by e2e_test.go).
func setupBtopFakeSSH(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "ssh"), script)
	// Prepend (not replace): the fake ssh script itself still needs real
	// coreutils such as sleep on PATH.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func waitForBtopPIDGone(t *testing.T, pidPath string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var pid int
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(pidPath)
		if err == nil {
			if parsed, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil {
				pid = parsed
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatalf("fake ssh never wrote its PID to %s", pidPath)
	}
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("fake ssh pid %d is still alive after cancellation", pid)
}

// btopFakeFrameBody returns exactly `rows` non-blank lines (the first four
// carrying the cpu/mem/net/proc labels btopFrameScore looks for). The
// emulator trims only the trailing "\r\n" off a rendered frame, so a fake
// frame with real blank trailing rows would be trimmed down to whatever
// non-blank content came before it; filling every row keeps the fake frame's
// height equal to the requested viewport, like a real btop repaint (whose
// box borders reach every row) already does.
func btopFakeFrameBody(rows int) string {
	labels := []string{"cpu 12pct", "mem 34pct", "net up", "proc list"}
	lines := make([]string, rows)
	for i := range lines {
		if i < len(labels) {
			lines[i] = labels[i]
		} else {
			lines[i] = fmt.Sprintf("filler row %d", i)
		}
	}
	// No trailing "\r\n" after the last line: a newline while the cursor
	// sits on the final row would scroll the whole screen up by one,
	// leaving a genuinely blank last row (and shifting content off by one).
	return strings.Join(lines, `\r\n`)
}

// btopFakeLiveScript builds a shell snippet that repaints a full `rows`-line
// screen every 100ms, either wrapped in btop's synchronized-output markers
// (withSync) or as plain sequential writes (to exercise the fallback
// render path when no sync markers ever arrive).
func btopFakeLiveScript(rows int, withSync bool) string {
	body := btopFakeFrameBody(rows)
	paint := fmt.Sprintf(`printf '\033[2J\033[H%s'`, body)
	if withSync {
		paint = fmt.Sprintf(`printf '\033[?2026h\033[2J\033[H%s\033[?2026l'`, body)
	}
	return fmt.Sprintf(`
i=0
while [ $i -lt 200 ]; do
  %s
  i=$((i+1))
  sleep 0.1
done
`, paint)
}

func TestBtopStreamPublishesLiveFramesAndKillsProcessOnCancel(t *testing.T) {
	const columns, rows = 80, 24
	pidPath := filepath.Join(t.TempDir(), "ssh.pid")
	script := fmt.Sprintf(`#!/bin/sh
echo $$ > %q
printf 'NEXUS_BTOP_FRAME_BEGIN\r\n'
%s
`, pidPath, btopFakeLiveScript(rows, true))
	setupBtopFakeSSH(t, script)

	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan btopStreamEventMsg, 1)
	go streamRemoteBtop(ctx, "alice@example.com", 1, columns, rows, events)

	deadline := time.After(2 * time.Second)
	var installedFrame btopStreamEventMsg
	for installedFrame.Frame == "" {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("stream ended before publishing any frame")
			}
			if event.Installed && event.Frame != "" {
				installedFrame = event
			}
		case <-deadline:
			t.Fatal("timed out waiting for the first installed frame")
		}
	}
	if height := len(strings.Split(installedFrame.Frame, "\n")); height != rows {
		t.Fatalf("frame height=%d, want %d", height, rows)
	}

	countDeadline := time.After(3 * time.Second)
	for installedFrame.FrameCount < 2 {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("stream ended before a second frame")
			}
			if event.FrameCount > installedFrame.FrameCount {
				installedFrame = event
			}
		case <-countDeadline:
			t.Fatalf("frame count never reached 2, last=%d", installedFrame.FrameCount)
		}
	}

	cancel()
	waitForBtopPIDGone(t, pidPath, 3*time.Second)
}

func TestBtopStreamFallsBackToFittingLayoutWhenTerminalIsTooSmall(t *testing.T) {
	argsDir := t.TempDir()
	countPath := filepath.Join(argsDir, "count")
	script := fmt.Sprintf(`#!/bin/sh
count_file=%q
count=0
[ -f "$count_file" ] && count=$(cat "$count_file")
count=$((count+1))
printf '%%s' "$count" > "$count_file"
printf '%%s' "$*" > %q/"$count".args
if [ "$count" -eq 1 ]; then
  printf 'NEXUS_BTOP_FRAME_BEGIN\r\n'
  printf 'Terminal size too small: Width = 138 Height = 27\r\n'
  printf 'Needed for current config: Width = 80 Height = 35\r\n'
  sleep 5
  exit 0
fi
printf 'NEXUS_BTOP_FRAME_BEGIN\r\n'
%s
`, countPath, argsDir, btopFakeLiveScript(27, true))
	setupBtopFakeSSH(t, script)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	events := make(chan btopStreamEventMsg, 1)
	const columns, rows = 138, 27
	go streamRemoteBtop(ctx, "alice@example.com", 2, columns, rows, events)

	sawFitting := false
	var liveFrame btopStreamEventMsg
	deadline := time.After(5 * time.Second)
	for liveFrame.Frame == "" {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("stream ended before a live frame arrived")
			}
			if event.Stage == "fitting" && event.Status != "" {
				sawFitting = true
			}
			if event.Frame != "" {
				liveFrame = event
			}
		case <-deadline:
			t.Fatal("timed out waiting for a live frame after the fallback")
		}
	}
	if !sawFitting {
		t.Fatal("no Stage=\"fitting\" progress event was published before the fallback frame")
	}

	secondArgs, err := os.ReadFile(filepath.Join(argsDir, "2.args"))
	if err != nil {
		t.Fatalf("second ssh invocation never ran: %v", err)
	}
	if !strings.Contains(string(secondArgs), `shown_boxes = "cpu mem net proc"`) {
		t.Fatalf("second ssh invocation did not request the richest fitting box set:\n%s", secondArgs)
	}
}

func TestBtopStreamFallbackPicksSmallerLayoutForSmallerViewport(t *testing.T) {
	argsDir := t.TempDir()
	countPath := filepath.Join(argsDir, "count")
	script := fmt.Sprintf(`#!/bin/sh
count_file=%q
count=0
[ -f "$count_file" ] && count=$(cat "$count_file")
count=$((count+1))
printf '%%s' "$count" > "$count_file"
printf '%%s' "$*" > %q/"$count".args
if [ "$count" -eq 1 ]; then
  printf 'NEXUS_BTOP_FRAME_BEGIN\r\n'
  printf 'Terminal size too small: Width = 100 Height = 20\r\n'
  printf 'Needed for current config: Width = 80 Height = 35\r\n'
  sleep 5
  exit 0
fi
printf 'NEXUS_BTOP_FRAME_BEGIN\r\n'
%s
`, countPath, argsDir, btopFakeLiveScript(20, true))
	setupBtopFakeSSH(t, script)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	events := make(chan btopStreamEventMsg, 1)
	const columns, rows = 100, 20
	go streamRemoteBtop(ctx, "alice@example.com", 3, columns, rows, events)

	var liveFrame btopStreamEventMsg
	deadline := time.After(5 * time.Second)
	for liveFrame.Frame == "" {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("stream ended before a live frame arrived")
			}
			if event.Frame != "" {
				liveFrame = event
			}
		case <-deadline:
			t.Fatal("timed out waiting for a live frame after the fallback")
		}
	}

	secondArgs, err := os.ReadFile(filepath.Join(argsDir, "2.args"))
	if err != nil {
		t.Fatalf("second ssh invocation never ran: %v", err)
	}
	if !strings.Contains(string(secondArgs), `shown_boxes = "cpu mem"`) {
		t.Fatalf("100x20 viewport did not fall back to \"cpu mem\":\n%s", secondArgs)
	}
}

func TestBtopStreamAuthFailureIsFriendly(t *testing.T) {
	setupBtopFakeSSH(t, `#!/bin/sh
printf 'Permission denied (publickey).\n' >&2
exit 255
`)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	events := make(chan btopStreamEventMsg, 1)
	go streamRemoteBtop(ctx, "alice@example.com", 4, 80, 24, events)

	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("stream closed without a Done event")
			}
			if !event.Done {
				continue
			}
			if !strings.Contains(event.Error, "SSH authentication required") {
				t.Fatalf("error=%q, want an authentication message", event.Error)
			}
			return
		case <-ctx.Done():
			t.Fatal("timed out waiting for the auth-failure Done event")
		}
	}
}

func TestBtopStreamNotInstalledReportsNoError(t *testing.T) {
	setupBtopFakeSSH(t, `#!/bin/sh
printf 'NEXUS_BTOP_UNAVAILABLE\n'
exit 0
`)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	events := make(chan btopStreamEventMsg, 1)
	go streamRemoteBtop(ctx, "alice@example.com", 5, 80, 24, events)

	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("stream closed without a Done event")
			}
			if !event.Done {
				continue
			}
			if event.Installed || event.Error != "" {
				t.Fatalf("event=%#v, want Installed=false and no error", event)
			}
			return
		case <-ctx.Done():
			t.Fatal("timed out waiting for the not-installed Done event")
		}
	}
}

func TestBtopStreamFallbackPathRendersFramesWithoutSyncMarkers(t *testing.T) {
	const columns, rows = 80, 24
	setupBtopFakeSSH(t, fmt.Sprintf(`#!/bin/sh
printf 'NEXUS_BTOP_FRAME_BEGIN\r\n'
%s
`, btopFakeLiveScript(rows, false)))
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	events := make(chan btopStreamEventMsg, 1)
	go streamRemoteBtop(ctx, "alice@example.com", 6, columns, rows, events)

	var latest btopStreamEventMsg
	deadline := time.After(4 * time.Second)
	for latest.FrameCount < 2 {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("stream ended before two fallback-path frames arrived")
			}
			if event.FrameCount > latest.FrameCount {
				latest = event
			}
		case <-deadline:
			t.Fatalf("timed out waiting for two frames via the fallback path, last count=%d", latest.FrameCount)
		}
	}
	if plain := strings.ToLower(ansi.Strip(latest.Frame)); !strings.Contains(plain, "cpu") {
		t.Fatalf("fallback frame missing expected content: %q", plain)
	}
}

func TestBtopStreamWatchdogFiresWhenNoCompleteFrameArrives(t *testing.T) {
	previous := btopStreamConnectingTimeout
	btopStreamConnectingTimeout = 300 * time.Millisecond
	t.Cleanup(func() { btopStreamConnectingTimeout = previous })

	setupBtopFakeSSH(t, `#!/bin/sh
printf 'NEXUS_BTOP_FRAME_BEGIN\r\n'
while true; do
  printf '\r\n'
  sleep 0.05
done
`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events := make(chan btopStreamEventMsg, 1)
	go streamRemoteBtop(ctx, "alice@example.com", 7, 80, 24, events)

	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("stream closed without a Done event")
			}
			if !event.Done {
				continue
			}
			if !strings.Contains(event.Error, "no complete frame arrived") {
				t.Fatalf("error=%q, want the watchdog's no-complete-frame message", event.Error)
			}
			return
		case <-ctx.Done():
			t.Fatal("timed out waiting for the watchdog Done event")
		}
	}
}
