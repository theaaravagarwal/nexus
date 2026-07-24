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
- Global, tag-inherited, and per-host custom remote commands with mandatory exact-command confirmation.
- Host aliases, tags, OS labels, and commands in YAML configuration.

### Changed

- UI themes now apply consistently to the dashboard, help output, and FZF.
- The dashboard now uses truthful workspace actions, freshness-aware system snapshots, distinct monochrome reachability glyphs, an honest saved-peer fleet overview, and live in-TUI theme previews.
- The dashboard now defaults to a calmer two-region daily-driver layout with compact host rows and on-demand fleet, themes, and saved commands.
- Reachability results stream per host, authenticated system snapshots refresh without leaving the TUI, and terminal-owning actions return to the same selected host with completion or recovery status.
- Pull and push path discovery now run inside a native TUI picker instead of dropping into an external FZF session.
- Themes can now be previewed, applied, and saved as the default directly inside the TUI with a discoverable shortcut.
- Saved-command setup now stays out of the primary TUI flow; concise global and tag-scoped examples live alongside the relevant YAML configuration, while exact-command confirmation remains in the TUI.
- The dashboard now uses a compact canonical key map (`j/k`, `Enter`, `/`, `a`, `h`, `q`); all operations live in the Actions list and every contextual key is shown where it applies.
- Saved commands now keep bounded, sanitized output inside a scrollable Nexus result view; commands marked `interactive: true` can still temporarily own the terminal.
- The Actions menu now uses concise names, accepts spaces in search, and keeps frequently used actions near the top.
- Selected host rows no longer expose terminal color-control fragments in narrow or highlighted layouts.
- Nexus configuration and state files use private permissions, atomic locked updates, strict YAML fields, and terminal-control sanitization.
- Remote diagnostics avoid login-shell startup files and use portable system and network summaries when optional probe utilities are unavailable.
- Interactive SSH sessions distinguish remote shell exit codes from connection failures.
- CLI validation now rejects unsupported indexing modes, explicit out-of-range ports, and stray command arguments instead of silently accepting them.

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
