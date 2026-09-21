package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// btopStderrCapture bounds and safely shares a subprocess's stderr between
// the goroutine draining its pipe and whichever goroutine later reads the
// captured text to build an error message.
type btopStderrCapture struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (c *btopStderrCapture) Write(chunk []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	remaining := max(0, maxMetadataBytes-c.buffer.Len())
	if remaining > 0 {
		c.buffer.Write(chunk[:min(len(chunk), remaining)])
	}
	return len(chunk), nil
}

func (c *btopStderrCapture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buffer.String()
}

const (
	// btopStreamFrameInterval rate-limits published frames regardless of how
	// often the remote screen repaints.
	btopStreamFrameInterval = 750 * time.Millisecond
	// btopStreamFallbackDelay is how long we wait after btop output begins
	// before considering the synchronized-frame fallback path.
	btopStreamFallbackDelay = 250 * time.Millisecond
	// btopStreamFallbackWindow is how long we can go without seeing a
	// synchronized-output end marker before assuming the remote btop never
	// wraps its repaints (or line-buffers them) and switching to a
	// time-based render instead of waiting for one forever.
	btopStreamFallbackWindow = 3 * time.Second
	// btopStreamTickInterval drives time-based render/watchdog checks even
	// when no new bytes have arrived from the remote process.
	btopStreamTickInterval = 50 * time.Millisecond
	btopStreamPreludeLimit = 64 * 1024

	btopTooSmallMarker = "Terminal size too small"
	btopSyncFrameEnd   = "\x1b[?2026l"
)

// btopStreamConnectingTimeout bounds how long a single ssh attempt may run
// without publishing a qualifying frame before it is treated as hung. It is
// a package-level var so hermetic tests can shorten it.
var btopStreamConnectingTimeout = 20 * time.Second

var btopNeededSizePattern = regexp.MustCompile(
	`Needed for current config:\s*Width\s*=\s*(\d+)\s*Height\s*=\s*(\d+)`,
)

type btopStreamEventMsg struct {
	Generation uint64
	Target     string
	Frame      string
	FrameCount uint64
	UpdatedAt  time.Time
	Installed  bool
	Done       bool
	Error      string
	// Stage and Status describe connection progress ("connecting",
	// "fitting", "live") for messages that carry no frame yet.
	Stage  string
	Status string
}

// btopBoxOption is one entry in the layout-fitting ladder: a shown_boxes
// value paired with the minimum viewport btop needs to render it, measured
// against a clean btop 1.3.0 config.
type btopBoxOption struct {
	boxes      string
	minColumns int
	minRows    int
}

// btopBoxLadder is ordered richest-first. streamRemoteBtopFrom walks it,
// from the entry after whatever failed, whenever the remote btop reports its
// own config needs more room than the Monitor viewport provides.
var btopBoxLadder = []btopBoxOption{
	{"cpu mem net proc", 80, 24},
	{"cpu mem proc", 80, 24},
	{"cpu net proc", 80, 24},
	{"cpu proc", 60, 24},
	{"cpu mem net", 60, 24},
	{"cpu mem", 60, 18},
	{"mem proc", 80, 16},
	{"proc", 44, 16},
	{"cpu net", 60, 14},
	{"cpu", 60, 8},
}

// btopFallbackLadder builds the ordered list of shown_boxes values to try
// for a given viewport, starting from startBoxes (which may be "" to mean
// "run btop with the user's own config unchanged"). Table entries whose
// measured minimum does not fit the viewport are skipped.
func btopFallbackLadder(startBoxes string, columns, rows int) []string {
	ladder := make([]string, 0, len(btopBoxLadder)+1)
	seen := make(map[string]bool, len(btopBoxLadder)+1)
	add := func(boxes string) {
		if seen[boxes] {
			return
		}
		seen[boxes] = true
		ladder = append(ladder, boxes)
	}
	add(startBoxes)
	for _, option := range btopBoxLadder {
		if columns < option.minColumns || rows < option.minRows {
			continue
		}
		add(option.boxes)
	}
	return ladder
}

func clampBtopViewport(columns, rows int) (int, int) {
	columns = min(220, max(1, columns))
	rows = min(70, max(1, rows))
	return columns, rows
}

// btopStreamCommand builds the remote script executed inside the PTY. With
// boxes == "" it runs btop with the user's own config exactly as before.
// With boxes set, it builds a temporary XDG config that preserves the
// user's other settings and theme but overrides shown_boxes (and forces a
// fast update_ms), so a too-small terminal can still show a useful subset
// of boxes. The caller is responsible for wrapping this with
// remoteShellCommand so it runs safely under the login shell.
func btopStreamCommand(columns, rows int, boxes string) string {
	columns, rows = clampBtopViewport(columns, rows)
	if boxes == "" {
		return fmt.Sprintf(`
if ! command -v btop >/dev/null 2>&1; then
  printf '%s\n'
  exit 0
fi
printf '%s\n'
export TERM=xterm-256color
stty cols %d rows %d 2>/dev/null || true
exec btop
`, btopUnavailableMarker, btopFrameMarker, columns, rows)
	}
	return fmt.Sprintf(`
if ! command -v btop >/dev/null 2>&1; then
  printf '%s\n'
  exit 0
fi
printf '%s\n'
export TERM=xterm-256color
stty cols %d rows %d 2>/dev/null || true
conf="${XDG_CONFIG_HOME:-$HOME/.config}/btop"
tmp=$(mktemp -d 2>/dev/null) || tmp="/tmp/nexus-btop-$$"
mkdir -p "$tmp/btop"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
if [ -f "$conf/btop.conf" ]; then
  grep -v -E '^[[:space:]]*(shown_boxes|update_ms)[[:space:]]*=' "$conf/btop.conf" > "$tmp/btop/btop.conf"
fi
if [ -d "$conf/themes" ]; then
  ln -s "$conf/themes" "$tmp/btop/themes" 2>/dev/null
fi
printf 'shown_boxes = "%s"\nupdate_ms = 1000\n' >> "$tmp/btop/btop.conf"
XDG_CONFIG_HOME="$tmp" btop
`, btopUnavailableMarker, btopFrameMarker, columns, rows, boxes)
}

func waitForBtopStream(events <-chan btopStreamEventMsg) tea.Cmd {
	return func() tea.Msg {
		message, ok := <-events
		if !ok {
			return btopStreamEventMsg{Done: true}
		}
		return message
	}
}

func publishBtopStreamEvent(
	ctx context.Context, events chan btopStreamEventMsg, message btopStreamEventMsg,
) bool {
	for {
		select {
		case events <- message:
			return true
		case <-ctx.Done():
			return false
		default:
		}
		// Keep only the freshest frame if rendering briefly falls behind.
		select {
		case <-events:
		default:
		}
	}
}

// streamRemoteBtop runs btop with the remote user's own config (no layout
// fitting hint). It is the entry point used directly by callers that have
// no memory of a previously-successful layout for this target.
func streamRemoteBtop(
	ctx context.Context,
	target string,
	generation uint64,
	columns, rows int,
	events chan btopStreamEventMsg,
) {
	streamRemoteBtopFrom(ctx, target, generation, columns, rows, "", events)
}

// streamRemoteBtopFrom drives the full session for one Monitor activation:
// it starts with startBoxes (which may be "" for the user's own config),
// and if the remote btop reports its terminal is too small for that config,
// it walks the fitting ladder until something renders or the ladder is
// exhausted. It returns the shown_boxes value that ultimately succeeded (or
// was last attempted), so a caller such as btopStreamPool can remember it.
func streamRemoteBtopFrom(
	ctx context.Context,
	target string,
	generation uint64,
	columns, rows int,
	startBoxes string,
	events chan btopStreamEventMsg,
) string {
	defer close(events)
	columns, rows = clampBtopViewport(columns, rows)
	ladder := btopFallbackLadder(startBoxes, columns, rows)
	logVerbose("btop stream %s: viewport=%dx%d start-boxes=%q ladder=%v", target, columns, rows, startBoxes, ladder)

	var frameCount uint64
	lastBoxes := startBoxes
	for index, boxes := range ladder {
		if ctx.Err() != nil {
			return lastBoxes
		}
		lastBoxes = boxes
		tooSmall, neededWidth, neededHeight := runBtopStreamAttempt(
			ctx, target, generation, columns, rows, boxes, events, &frameCount,
		)
		if !tooSmall {
			// Every other outcome (live stream ended, unavailable, auth
			// failure, watchdog) already published its own Done event.
			return boxes
		}
		if index == len(ladder)-1 {
			smallest := btopBoxLadder[len(btopBoxLadder)-1]
			logVerbose(
				"btop stream %s: fallback ladder exhausted, last attempt %q needed %dx%d",
				target, boxes, neededWidth, neededHeight,
			)
			publishBtopStreamEvent(ctx, events, btopStreamEventMsg{
				Generation: generation, Target: target, Done: true,
				Error: fmt.Sprintf(
					"Monitor pane %d×%d is too small for btop (needs at least %d×%d) · enlarge the terminal",
					columns, rows, smallest.minColumns, smallest.minRows,
				),
			})
			return boxes
		}
		next := ladder[index+1]
		logVerbose(
			"btop stream %s: %q reported too small (needs %dx%d), trying %q",
			target, boxes, neededWidth, neededHeight, next,
		)
		if !publishBtopStreamEvent(ctx, events, btopStreamEventMsg{
			Generation: generation, Target: target, Stage: "fitting",
			Status: fmt.Sprintf("Fitting btop layout to %d×%d (%s)…", columns, rows, next),
		}) {
			return boxes
		}
	}
	return lastBoxes
}

type btopChunkMsg struct {
	data []byte
	err  error
}

// readBtopStdout feeds stdout chunks over a channel instead of letting the
// main loop block on Read, so time-based render/watchdog checks still run
// even when no bytes are arriving. It stops as soon as stop is closed,
// including when it is blocked trying to send (the reader goroutine would
// otherwise leak past the point its attempt has already returned).
func readBtopStdout(stdout io.Reader, out chan<- btopChunkMsg, stop <-chan struct{}) {
	buffer := make([]byte, 32*1024)
	for {
		read, err := stdout.Read(buffer)
		if read > 0 {
			data := append([]byte(nil), buffer[:read]...)
			select {
			case out <- btopChunkMsg{data: data}:
			case <-stop:
				return
			}
		}
		if err != nil {
			select {
			case out <- btopChunkMsg{err: err}:
			case <-stop:
			}
			return
		}
	}
}

func parseBtopNeededSize(plain string) (width, height int) {
	match := btopNeededSizePattern.FindStringSubmatch(plain)
	if match == nil {
		return 0, 0
	}
	width, _ = strconv.Atoi(match[1])
	height, _ = strconv.Atoi(match[2])
	return width, height
}

func nextBtopSyncTail(combined string) string {
	if len(combined) > len(btopSyncFrameEnd)-1 {
		return combined[len(combined)-(len(btopSyncFrameEnd)-1):]
	}
	return combined
}

// runBtopStreamAttempt runs a single ssh session with a fixed shown_boxes
// value. It returns tooSmall=true (with the size btop reported it needs) if
// the remote screen is the "Terminal size too small" message, so the caller
// can retry with a smaller layout; for every other outcome it has already
// published a terminal (Done) event and the caller should stop.
func runBtopStreamAttempt(
	ctx context.Context,
	target string,
	generation uint64,
	columns, rows int,
	boxes string,
	events chan btopStreamEventMsg,
	frameCount *uint64,
) (tooSmall bool, neededWidth, neededHeight int) {
	logVerbose("btop stream %s: session start viewport=%dx%d boxes=%q", target, columns, rows, boxes)
	script := remoteShellCommand("sh", btopStreamCommand(columns, rows, boxes))
	args, err := buildMonitoringSSHArgs(target, true, script)
	if err != nil {
		publishBtopStreamEvent(ctx, events, btopStreamEventMsg{
			Generation: generation, Target: target, Done: true, Error: sanitizeTerminalText(err.Error()),
		})
		return false, 0, 0
	}
	command := exec.CommandContext(ctx, "ssh", args...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		publishBtopStreamEvent(ctx, events, btopStreamEventMsg{
			Generation: generation, Target: target, Done: true, Error: "unable to open btop stream",
		})
		return false, 0, 0
	}
	// Capture stderr through our own pipe/goroutine rather than handing
	// Cmd an io.Writer: Cmd would then create that pipe itself and make
	// Wait() block until its internal copy goroutine sees EOF. A killed
	// remote shell can leave a descendant (a forked `sleep`, in hermetic
	// tests; a shell builtin elsewhere) holding the write end open for a
	// while after the shell we asked to be killed is gone, which would
	// otherwise stall Wait() for as long as that descendant lives instead
	// of returning as soon as the process group is killed.
	stderrPipe, err := command.StderrPipe()
	if err != nil {
		publishBtopStreamEvent(ctx, events, btopStreamEventMsg{
			Generation: generation, Target: target, Done: true, Error: "unable to open btop stream",
		})
		return false, 0, 0
	}
	var stderr btopStderrCapture
	if err := command.Start(); err != nil {
		publishBtopStreamEvent(ctx, events, btopStreamEventMsg{
			Generation: generation, Target: target, Done: true,
			Error: friendlyBtopStreamError(stderr.String(), err),
		})
		return false, 0, 0
	}
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		buffer := make([]byte, 4096)
		for {
			read, readErr := stderrPipe.Read(buffer)
			if read > 0 {
				_, _ = stderr.Write(buffer[:read])
			}
			if readErr != nil {
				return
			}
		}
	}()

	processStartedAt := time.Now()
	deadline := processStartedAt.Add(btopStreamConnectingTimeout)
	publishBtopStreamEvent(ctx, events, btopStreamEventMsg{
		Generation: generation, Target: target, Stage: "connecting",
	})

	emulator := vt.NewEmulator(columns, rows)
	defer emulator.Close()
	emulator.SetScrollbackSize(0)

	stop := make(chan struct{})
	defer close(stop)
	chunks := make(chan btopChunkMsg, 8)
	go readBtopStdout(stdout, chunks, stop)
	ticker := time.NewTicker(btopStreamTickInterval)
	defer ticker.Stop()

	prelude := make([]byte, 0, 4096)
	started := false
	seenBytes := false
	syncTail := ""
	syncPending := false
	dirty := false
	var lastPublish time.Time
	var streamStartedAt time.Time
	var lastSyncAt time.Time

	feed := func(chunk []byte) {
		_, _ = emulator.Write(chunk)
		dirty = true
		combined := syncTail + string(chunk)
		if strings.Contains(combined, btopSyncFrameEnd) {
			syncPending = true
			lastSyncAt = time.Now()
		}
		syncTail = nextBtopSyncTail(combined)
	}

	tryRender := func() (isTooSmall, publishFailed bool) {
		frame := strings.TrimRight(emulator.Render(), "\r\n")
		plain := ansi.Strip(frame)
		if strings.Contains(plain, btopTooSmallMarker) {
			neededWidth, neededHeight = parseBtopNeededSize(plain)
			return true, false
		}
		if !dirty {
			return false, false
		}
		now := time.Now()
		frameDue := lastPublish.IsZero() || now.Sub(lastPublish) >= btopStreamFrameInterval
		fallbackReady := now.Sub(streamStartedAt) >= btopStreamFallbackDelay &&
			now.Sub(lastSyncAt) >= btopStreamFallbackWindow
		if !frameDue || (!syncPending && !fallbackReady) {
			return false, false
		}
		qualifies := false
		if *frameCount == 0 {
			qualifies = btopFrameScore(frame) >= 1000
		} else {
			qualifies = strings.TrimSpace(plain) != ""
		}
		dirty = false
		syncPending = false
		if !qualifies {
			return false, false
		}
		*frameCount++
		if *frameCount%10 == 0 {
			logVerbose("btop stream %s: published %d frames", target, *frameCount)
		}
		lastPublish = now
		ok := publishBtopStreamEvent(ctx, events, btopStreamEventMsg{
			Generation: generation, Target: target, Stage: "live", Frame: frame,
			FrameCount: *frameCount, UpdatedAt: now, Installed: true,
		})
		return false, !ok
	}

	killAndWait := func() {
		// Kill only the ssh client. Never kill its process group: the
		// ControlMaster mux master forked by ssh shares the group and
		// must outlive us so later connections reuse the session.
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
	}

	finishExited := func() {
		runErr := command.Wait()
		// The process has exited, so its own stderr fd is closed; give the
		// draining goroutine a brief, bounded window to see EOF and land
		// its last read before we snapshot the buffer.
		select {
		case <-stderrDone:
		case <-time.After(200 * time.Millisecond):
		}
		if ctx.Err() != nil {
			return
		}
		message := btopStreamEventMsg{
			Generation: generation, Target: target, Done: true, Installed: started,
		}
		if !started || runErr != nil {
			message.Error = friendlyBtopStreamError(stderr.String(), runErr)
		} else {
			message.Error = "btop stream ended · press r to retry"
		}
		logVerbose(
			"btop stream %s: exit status=%v stderr=%q",
			target, runErr, truncateText(sanitizeTerminalText(stderr.String()), 200),
		)
		publishBtopStreamEvent(ctx, events, message)
	}

	watchdogFire := func() {
		killAndWait()
		var reason string
		switch {
		case !seenBytes:
			reason = "no output from ssh"
		case !started:
			reason = "ssh connected but the remote shell never started btop"
		default:
			reason = "btop started but no complete frame arrived"
		}
		logVerbose("btop stream %s: connecting watchdog fired (%s)", target, reason)
		publishBtopStreamEvent(ctx, events, btopStreamEventMsg{
			Generation: generation, Target: target, Done: true, Installed: started, Error: reason,
		})
	}

	for {
		select {
		case <-ctx.Done():
			killAndWait()
			return false, 0, 0
		case msg := <-chunks:
			if len(msg.data) > 0 {
				seenBytes = true
				if !started {
					prelude = append(prelude, msg.data...)
					text := string(prelude)
					if strings.Contains(text, btopUnavailableMarker) {
						_ = command.Wait()
						if ctx.Err() == nil {
							publishBtopStreamEvent(ctx, events, btopStreamEventMsg{
								Generation: generation, Target: target, Done: true, Installed: false,
							})
						}
						return false, 0, 0
					}
					markerIndex := strings.Index(text, btopFrameMarker)
					if markerIndex < 0 {
						if len(prelude) > btopStreamPreludeLimit {
							killAndWait()
							publishBtopStreamEvent(ctx, events, btopStreamEventMsg{
								Generation: generation, Target: target, Done: true,
								Error: "remote shell did not start btop · press enter to connect",
							})
							return false, 0, 0
						}
					} else {
						logVerbose("btop stream %s: marker found", target)
						start := markerIndex + len(btopFrameMarker)
						for start < len(text) && (text[start] == '\r' || text[start] == '\n') {
							start++
						}
						remainder := []byte(text[start:])
						prelude = nil
						started = true
						streamStartedAt = time.Now()
						if len(remainder) > 0 {
							feed(remainder)
						}
					}
				} else {
					feed(msg.data)
				}
				if started {
					small, publishFailed := tryRender()
					if small {
						killAndWait()
						return true, neededWidth, neededHeight
					}
					if publishFailed {
						killAndWait()
						return false, 0, 0
					}
				}
			}
			if msg.err != nil {
				if !errors.Is(msg.err, io.EOF) && ctx.Err() == nil {
					_, _ = stderr.Write([]byte("; " + msg.err.Error()))
				}
				finishExited()
				return false, 0, 0
			}
		case <-ticker.C:
			if started {
				small, publishFailed := tryRender()
				if small {
					killAndWait()
					return true, neededWidth, neededHeight
				}
				if publishFailed {
					killAndWait()
					return false, 0, 0
				}
			}
			if *frameCount == 0 && time.Now().After(deadline) {
				watchdogFire()
				return false, 0, 0
			}
		}
	}
}

func friendlyBtopStreamError(stderr string, err error) string {
	detail := strings.TrimSpace(stderr)
	if err != nil {
		if detail != "" {
			detail += ": "
		}
		detail += err.Error()
	}
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "permission denied"), strings.Contains(lower, "authentication"):
		return "SSH authentication required · press enter to connect"
	case strings.Contains(lower, "host key verification"):
		return "Host key approval required · press enter to connect"
	case strings.Contains(lower, "timed out"):
		return "SSH connection timed out · press r to retry"
	case strings.Contains(lower, "connection refused"):
		return "SSH connection refused · press r to retry"
	case strings.Contains(lower, "no route to host"):
		return "Host is unreachable · press r to retry"
	case detail == "":
		return "btop stream ended · press r to retry"
	default:
		return truncateText(sanitizeTerminalText(detail), 120)
	}
}
