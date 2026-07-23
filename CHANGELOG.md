# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog and this project follows Semantic Versioning.

## [Unreleased]

## [0.1.0] - 2026-07-23

### Added

- Persistent non-default SSH ports across sessions, discovery, diagnostics, and rsync:
  - `user@host:port` and bracketed IPv6 syntax
  - global `--port` override
  - backward-compatible host-history canonicalization
- Bubble Tea host/action dashboard, opened with `nexus`, `nexus tui`, or `nexus ui`.
- Adaptive colored help with `NO_COLOR` and non-TTY fallbacks.
- Configurable FZF themes, layout, prompt, pointer, and key bindings.
- Remote `top`, `net`, `info`, and `storage` diagnostics with command fallbacks.
- `doctor`, `version` / `--version`, shell `completion`, and transfer `--dry-run` QoL commands.
- SSH connection reuse and keepalives using a private Nexus cache directory.
- Compression-aware rsync arguments and stability retry error reporting.
- Active-entrypoint coverage for endpoints, SSH/rsync argv, diagnostics, help, FZF, dashboard state, and performance helpers.
- Production baseline repository standards:
  - CI + lint + vulnerability scanning workflows
  - Dependabot configuration
  - Contribution and security policy docs
  - Issue and PR templates
  - Initial unit tests for host store and path utilities
  - GoReleaser scaffolding for tagged releases
### Changed

- Updated Cobra and release/CI GitHub Actions to their current Dependabot-proposed major versions.
- Running `nexus` with no arguments opens the dashboard on a TTY and prints plain help when redirected.
