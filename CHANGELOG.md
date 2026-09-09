# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog and this project follows Semantic Versioning.

## [0.1.3] - 2026-09-09

### Added

- Expanded themes and polished UI surfaces across the dashboard, settings, help, and FZF.
- Optional workspace tabs and settings for Hosts, Monitor, and Fleet.
- Compact, adaptive Fleet telemetry for CPU, memory, load, network, and freshness.
- Linux configuration compatibility, including migration of the legacy Fleet/btop setting.

### Changed

- Monitor now provides a live btop view with exact terminal resizing and optimized streaming, with reconnect and full interactive monitor actions.
- SSH connection reuse, keepalives, and network diagnostics are optimized for more responsive remote work.
