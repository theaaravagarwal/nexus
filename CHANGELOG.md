# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog and this project follows Semantic Versioning.

## [0.1.1] - 2026-07-23

### Added

- Host-first TUI with navigation, selected-host details, command palette, responsive layouts, and contextual help.
- Zoxide-style host frecency ordering based on successful use and recency.
- Saved-port TCP reachability and latency checks that never authenticate in the background.
- Cached remote OS, CPU, memory, disk, and supported-tool summaries refreshed through explicit system-info actions.
- Built-in Nexus, Nord, Dracula, Catppuccin, Gruvbox, monochrome, and terminal themes.
- Theme previews and configuration inspection commands.
- Global, tag-inherited, and per-host trusted remote commands with opt-in exact-command confirmation.
- Host aliases, tags, OS labels, and commands in YAML configuration.
- Confirmed `ssh-copy-id` setup for selected hosts, including saved non-default ports.
- Workbench, Console, and Fleet dashboard workspaces with configurable pinned actions.
- Lightweight selected-host telemetry for uptime, load, memory, network throughput, and GPU health.
- Secure in-place OpenSSH terminal handoff for key, password, keyboard-interactive, host-key, and MFA authentication.

### Changed

- UI themes now apply consistently to the dashboard, help output, and FZF.
- The dashboard now uses truthful workspace actions, freshness-aware system snapshots, distinct monochrome reachability glyphs, an honest saved-peer fleet overview, and live in-TUI theme previews.
- The dashboard now defaults to a calmer two-region daily-driver layout with compact host rows and on-demand fleet, themes, and saved commands.
- Reachability results stream per host, authenticated system snapshots refresh without leaving the TUI, and terminal-owning actions return to the same selected host with completion or recovery status.
- Pull and push path discovery now run inside a native TUI picker instead of dropping into an external FZF session.
- Themes can now be previewed, applied, and saved as the default directly inside the TUI with a discoverable shortcut.
- Saved-command setup stays out of the primary TUI flow; trusted commands run immediately by default, while `confirm: true` enables exact-command review.
- The dashboard now uses a compact canonical key map (`j/k`, `Enter`, `/`, `a`, `h`, `q`); all operations live in the Actions list and every contextual key is shown where it applies.
- Saved commands now keep bounded, sanitized output inside a scrollable Nexus result view; commands marked `interactive: true` can still temporarily own the terminal.
- The Actions menu now uses concise names, accepts spaces in search, and keeps frequently used actions near the top.
- Selected host rows no longer expose terminal color-control fragments in narrow or highlighted layouts.
- Completed reachability probes now group online hosts first, with frequency and recency as the stable suborder.
- System snapshots now include bounded multi-GPU and mounted-filesystem inventories, with RAM and storage rendered in decimal GB/TB.
- Storage output now stays inside the scrollable TUI result view, while medium and wide layouts use spare space for selected-host resources.
- Captured commands have a timeout, metadata strips terminal-control sequences completely, and diagnostic shell fallbacks no longer rerun ordinary exit-code failures.
- Nexus configuration and state files use private permissions, atomic locked updates, strict YAML fields, and terminal-control sanitization.
- Remote diagnostics avoid login-shell startup files and use portable system and network summaries when optional probe utilities are unavailable.
- Interactive SSH sessions distinguish remote shell exit codes from connection failures.
- CLI validation now rejects unsupported indexing modes, explicit out-of-range ports, and stray command arguments instead of silently accepting them.
- GPU discovery combines native, WSL, PCI, system-profiler, and host-graphics sources so hybrid and multi-GPU systems are represented together.
- Ultra-wide dashboards use adaptive host pulse, activity, console, or fleet decks while smaller terminals retain compact layouts.
- Theme surfaces now paint consistently behind semantic text, selections, overlays, and dashboard panels.
- Background telemetry uses a single bounded request with quiet exponential backoff and never changes host usage ordering.
- Activity output and the latest operation remain inside Nexus, with only a concise previous-session summary persisted.
- Storage views filter pseudo-filesystems, Snap/loop mounts, runtime mounts, and platform plumbing while retaining root, physical, external, WSL drive-letter, dataset, and network volumes.
- Device-first storage rows now include responsive usage bars and pressure-aware theme colors.
- The repository now uses a conventional `cmd/nexus` executable and
  `internal/nexus` application layout, replacing the duplicate prototype
  command tree and root-level Go source sprawl.
- Opaque and transparent themes now resolve through one semantic path across
  dashboard panels, overlays, previews, help, and FZF; transparent mode no
  longer leaks elevated backgrounds and retains a visible non-color selection.
- Invalid custom theme roles and color values now fail with actionable
  configuration errors instead of rendering inconsistently.

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
