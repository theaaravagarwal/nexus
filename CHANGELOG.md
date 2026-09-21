# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog and this project follows Semantic Versioning.

## [Unreleased]

### Fixed

- Monitor live btop pane no longer stays on "connecting" when the remote btop configuration needs more rows than the pane. The stream now reads btop's required size and retries with a smaller `shown_boxes` layout via a temporary `XDG_CONFIG_HOME` config that preserves the user's settings and theme.
- Remote `btop` processes no longer pile up on Linux hosts: every Monitor session (including the too-small attempt before a layout fallback, resizes, and reconnects) left its btop running because btop ignores the SIGHUP from the closed pty. The wrapper now backgrounds btop with the controlling tty, traps HUP/TERM, and watches the tty so btop is stopped within seconds of the session ending.
- Monitor pane now explains why live btop is not running (setting disabled, tabs hidden, terminal too small with exact size needed, or paused).
- File-extension ignore patterns for discovery now match correctly (regex double-escaping fixed); rsync passes `--protect-args` when supported so remote paths with spaces work; hosts.json writes are locked against concurrent mutations.
- `pull` opens media from the real destination directory rather than the current working directory.
- Per-host command overrides no longer drop a global `confirm: true` setting.
- Config directory permissions tightened to 0700 on startup.
- Stream failures (auth, host key, timeouts, watchdog) are now explained in the Monitor pane; harmless ssh warnings such as "ControlSocket … already exists, disabling multiplexing" are no longer shown as errors.

### Changed

- Opening help, palette, or settings no longer tears down the btop session (30 s grace period); resize operations reconnect while keeping the last frame.
- Dashboard idle CPU: the Console clock tick runs every 5 s instead of 1 s while nothing is connecting, telemetry tick chains no longer accumulate across focus/selection events, the btop stream renders its virtual terminal only when a frame is due, and the Console lays out the ANSI-dense btop pane through width-identical ASCII stand-ins so lipgloss never re-measures it. Console with a live btop dropped from about 8.7% to under 4% of a core; render output is byte-identical (golden and differential tests).
- The frecency update after a successful host action runs in a background command instead of blocking the UI on a file lock and fsync.
- Confirm modal accepts `Y`, `n`, and `N`, and takes keyboard focus when opened from the command result view (re-running a confirm-required command there was previously stuck).
- When the live btop pane is gated (for example after shrinking the terminal) the reason is shown above the stale frame.
- `NEXUS_CPUPROFILE=path` writes a CPU profile of a run; `NEXUS_TRACE_MESSAGES=1` with `--verbose` logs every message type reaching the dashboard.

### Added

- Windows hosts: when the remote shell is cmd.exe, the Monitor runs btop4win inside a headless console over a non-PTY session (no WSL involved), with a PowerShell supervisor that tears it down when the session ends; requires btop4win on the SSH user's PATH. Compact telemetry (uptime, cores, memory, CPU, network, NVIDIA GPUs) and Fleet samples come from PowerShell performance counters instead of showing zeros.
- `docs/ARCHITECTURE.md` with design and implementation notes.
- `Makefile` with `make check` and `make bench` targets.
- Benchmarks for dashboard rendering and text truncation.
- CI now runs `-race` detector, golangci-lint, and govulncheck.

## [0.1.3] - 2026-09-09

### Added

- Expanded themes and polished UI surfaces across the dashboard, settings, help, and FZF.
- Optional workspace tabs and settings for Hosts, Monitor, and Fleet.
- Compact, adaptive Fleet telemetry for CPU, memory, load, network, and freshness.
- Linux configuration compatibility, including migration of the legacy Fleet/btop setting.

### Changed

- Monitor now provides a live btop view with exact terminal resizing and optimized streaming, with reconnect and full interactive monitor actions.
- SSH connection reuse, keepalives, and network diagnostics are optimized for more responsive remote work.
