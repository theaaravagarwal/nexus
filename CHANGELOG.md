# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog and this project follows Semantic Versioning.

## [Unreleased]

### Fixed

- Monitor live btop pane no longer stays on "connecting" when the remote btop configuration needs more rows than the pane. The stream now reads btop's required size and retries with a smaller `shown_boxes` layout via a temporary `XDG_CONFIG_HOME` config that preserves the user's settings and theme.
- Monitor pane now explains why live btop is not running (setting disabled, tabs hidden, terminal too small with exact size needed, or paused).
- File-extension ignore patterns for discovery now match correctly (regex double-escaping fixed); rsync passes `--protect-args` when supported so remote paths with spaces work; hosts.json writes are locked against concurrent mutations.
- `pull` opens media from the real destination directory rather than the current working directory.
- Per-host command overrides no longer drop a global `confirm: true` setting.
- Config directory permissions tightened to 0700 on startup.
- Stream failures (auth, host key, timeouts, watchdog) are now explained in the Monitor pane.

### Changed

- Opening help, palette, or settings no longer tears down the btop session (30 s grace period); resize operations reconnect while keeping the last frame.

### Added

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
