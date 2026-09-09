package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/vt"
)

const (
	btopStreamFrameInterval          = 750 * time.Millisecond
	btopStreamFallbackDelay          = 250 * time.Millisecond
	btopStreamFallbackRenderInterval = 100 * time.Millisecond
	btopStreamPreludeLimit           = 64 * 1024
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
}

func btopStreamCommand(columns, rows int) string {
	columns = min(180, max(btopMinColumns, columns))
	rows = min(60, max(btopMinRows, rows))
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

func streamRemoteBtop(
	ctx context.Context,
	target string,
	generation uint64,
	columns, rows int,
	events chan btopStreamEventMsg,
) {
	defer close(events)
	args, err := buildMonitoringSSHArgs(target, true, btopStreamCommand(columns, rows))
	if err != nil {
		publishBtopStreamEvent(ctx, events, btopStreamEventMsg{
			Generation: generation, Target: target, Done: true, Error: sanitizeTerminalText(err.Error()),
		})
		return
	}
	command := exec.CommandContext(ctx, "ssh", args...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		publishBtopStreamEvent(ctx, events, btopStreamEventMsg{
			Generation: generation, Target: target, Done: true, Error: "unable to open btop stream",
		})
		return
	}
	var stderr boundedMetadataOutput
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		publishBtopStreamEvent(ctx, events, btopStreamEventMsg{
			Generation: generation, Target: target, Done: true,
			Error: friendlyBtopStreamError(stderr.String(), err),
		})
		return
	}

	columns = min(180, max(btopMinColumns, columns))
	rows = min(60, max(btopMinRows, rows))
	emulator := vt.NewEmulator(columns, rows)
	defer emulator.Close()
	emulator.SetScrollbackSize(0)

	buffer := make([]byte, 32*1024)
	prelude := make([]byte, 0, 4096)
	started := false
	syncTail := ""
	lastFrame := time.Time{}
	lastRenderAttempt := time.Time{}
	streamStartedAt := time.Time{}
	seenSynchronizedFrame := false
	var frameCount uint64
	for {
		read, readErr := stdout.Read(buffer)
		if read > 0 {
			chunk := buffer[:read]
			if !started {
				prelude = append(prelude, chunk...)
				text := string(prelude)
				if strings.Contains(text, btopUnavailableMarker) {
					_ = command.Wait()
					publishBtopStreamEvent(ctx, events, btopStreamEventMsg{
						Generation: generation, Target: target, Done: true, Installed: false,
					})
					return
				}
				marker := strings.Index(text, btopFrameMarker)
				if marker < 0 {
					if len(prelude) > btopStreamPreludeLimit {
						_ = command.Process.Kill()
						_ = command.Wait()
						publishBtopStreamEvent(ctx, events, btopStreamEventMsg{
							Generation: generation, Target: target, Done: true,
							Error: "remote shell did not start btop · press enter to connect",
						})
						return
					}
					if readErr != nil {
						break
					}
					continue
				}
				start := marker + len(btopFrameMarker)
				for start < len(text) && (text[start] == '\r' || text[start] == '\n') {
					start++
				}
				chunk = []byte(text[start:])
				prelude = nil
				started = true
				streamStartedAt = time.Now()
			}

			_, _ = emulator.Write(chunk)
			combinedTail := syncTail + string(chunk)
			completedFrame := strings.Contains(combinedTail, "\x1b[?2026l")
			if completedFrame {
				seenSynchronizedFrame = true
			}
			if len(combinedTail) > len("\x1b[?2026l")-1 {
				syncTail = combinedTail[len(combinedTail)-(len("\x1b[?2026l")-1):]
			} else {
				syncTail = combinedTail
			}
			now := time.Now()
			frameDue := lastFrame.IsZero() || now.Sub(lastFrame) >= btopStreamFrameInterval
			fallbackReady := !seenSynchronizedFrame &&
				now.Sub(streamStartedAt) >= btopStreamFallbackDelay &&
				(lastRenderAttempt.IsZero() || now.Sub(lastRenderAttempt) >= btopStreamFallbackRenderInterval)
			if frameDue && (completedFrame || fallbackReady) {
				lastRenderAttempt = now
				frame := strings.TrimRight(emulator.Render(), "\r\n")
				if btopFrameScore(frame) >= 1000 {
					frameCount++
					if !publishBtopStreamEvent(ctx, events, btopStreamEventMsg{
						Generation: generation, Target: target, Frame: frame, FrameCount: frameCount,
						UpdatedAt: now, Installed: true,
					}) {
						return
					}
					lastFrame = now
				}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) && ctx.Err() == nil {
				stderr.buffer.WriteString("; " + readErr.Error())
			}
			break
		}
	}

	runErr := command.Wait()
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
	publishBtopStreamEvent(ctx, events, message)
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
