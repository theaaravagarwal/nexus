# nexus

CLI for SSH sessions and remote file sync workflows.

`nexus` combines host history, a keyboard-driven dashboard, fuzzy path navigation, remote diagnostics, and `rsync` transfers so you can move between machines without retyping destinations.

## Highlights

- Open the `rip`-inspired host dashboard by running `nexus` with no command.
- SSH into `user@host`, `user@host:port`, IPv4, or bracketed IPv6 targets.
- Pull and push files/directories over `rsync` with interactive path selection.
- Inspect remote CPU, GPU, decimal RAM, network, and meaningful storage volumes with portable fallbacks.
- Host history persisted in `~/.config/nexus/hosts.json`.
- Reachable hosts sort first, with zoxide-style frequency and recency ordering inside each status group.
- Saved-port reachability and TCP latency without background SSH authentication.
- Rich responsive workspace with host dossiers, topology, command palette, and contextual help.
- Seven built-in themes plus semantic color overrides in YAML.
- Trusted custom commands inherited globally, by tag, or per host, with opt-in confirmation.
- Install your local SSH key through a confirmed action that respects the saved port.
- Remote indexing modes:
  - `lazy` (default): shallow listing for faster navigation.
  - `full`: deeper recursive listing (depth controlled via config).
- Cross-platform remote handling for Unix-like and Windows targets.
- Reused SSH connections, keepalives, and compression-aware rsync arguments.
- Adaptive colored help (`NO_COLOR=1` disables color).
- Optional verbose logs for discovery and transfer diagnostics.

## Requirements

- Go `1.26.3+`
- `ssh` in `PATH`
- `rsync` in `PATH` for transfers (or set `NEXUS_RSYNC_PATH`)
- `fzf` in `PATH` for fuzzy host/path selection

## Install

### Fresh install from GitHub

Install the latest release to `~/.local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/theaaravagarwal/nexus/main/install.sh | sh
```

If `~/.local/bin` is not already on your path, add this to `~/.zshrc` or `~/.bashrc`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

Choose a different install directory or version:

```bash
curl -fsSL https://raw.githubusercontent.com/theaaravagarwal/nexus/main/install.sh | NEXUS_INSTALL_DIR=/usr/local/bin NEXUS_VERSION=v0.1.1 sh
```

The installer detects macOS/Linux and ARM64/x86-64, verifies the release checksum, and does not require `sudo` for the default location.

### Install from source

Clone and build:

```bash
git clone https://github.com/theaaravagarwal/nexus.git
cd nexus
go install ./cmd/nexus
```

Or build a local binary:

```bash
go build -o bin/nexus ./cmd/nexus
```

## Quick Start

```bash
# Add default and non-default-port hosts
nexus host add user@example-host
nexus host add admin@example-host:2222

# Open the dashboard
nexus

# Open SSH directly or choose from history
nexus ssh
nexus ssh admin@example-host:2222

# Pull and push (fully interactive when paths are omitted)
nexus pull
nexus push ./build

# Inspect a remote
nexus top admin@example-host:2222
nexus info admin@example-host:2222
```

## CLI

```bash
nexus [global flags] <command>
```

Commands:

- `tui` / `ui` / `dashboard`
- `ssh [user@host[:port]]`
- `pull [user@host[:port]] [remote-path] [local-dir]`
- `push [file] [user@host[:port]] [remote-dir]`
- `top [user@host[:port]]`
- `net [user@host[:port]]`
- `info [user@host[:port]]`
- `storage [user@host[:port]]`
- `run [command] [user@host[:port]]`
- `host list`
- `host add [user@host[:port]]`
- `host remove [user@host[:port]]`
- `config` (opens `~/.config/nexus/config.yaml` in your editor)
- `doctor`
- `completion bash|zsh|fish|powershell`
- `version`

Global flags:

- `-n, --dry-run`: run rsync in dry-run mode and print the command.
- `-i, --indexing lazy|full`: indexing mode used by interactive path discovery (default `lazy`).
- `-p, --port 1..65535`: override the target's SSH port for this invocation.
- `-v, --verbose`: enable debug logs.
- `--version`: print the release version.

Indexing mode quick guide:

- `lazy`: fast, shallow discovery for large filesystems.
- `full`: deeper recursive discovery (depth controlled by `full_index_depth`).

Example:

```bash
nexus -i full pull
nexus --port 2200 ssh user@example-host
```

### Dashboard keys

| Key | Action |
|---|---|
| `j` / `k` | Select a host or move within a list |
| `enter` | Connect, or choose the selected list item |
| `/` | Find hosts; inside Actions, filter the action list |
| `a` | Open every operation and saved command |
| `h` | Open the complete contextual key reference |
| `esc` | Go back or cancel the current context |
| `q` | Quit from the workspace |

The Actions list contains pull, push, SSH key installation, selected/all
connection checks, background system refresh, system/network/storage tools,
fleet, themes, configuration, and saved commands. Storage inventories and
ordinary saved-command output stay in a scrollable Nexus result view.
Interactive SSH temporarily hands the terminal to OpenSSH, so native key,
password, keyboard-interactive, host-key, and MFA prompts work securely before
Nexus restores the same workspace.

## Configuration

On first run, nexus bootstraps:

- `~/.config/nexus/hosts.json`
- `~/.config/nexus/config.yaml`

Default config template:

```yaml
full_index_depth: 5

ui:
  # nexus | nord | dracula | catppuccin | gruvbox | mono | terminal
  theme: nexus
  # opaque paints the theme background; transparent preserves your terminal.
  background: opaque
  # workbench | console | fleet
  workspace: workbench
  pinned_actions: [ssh, info, storage]

reachability:
  # DNS/TCP checks only; no background authentication.
  enabled: true
  timeout_ms: 1500
  concurrency: 8
  cache_seconds: 30

fzf:
  layout: reverse
  prompt: "Nexus ❯ "
  pointer: "→"

# Available for every host. Trusted commands run immediately by default.
# Add confirm: true when Nexus should review the exact command and target.
commands: []

# Inherited by hosts with matching tags.
tag_commands: {}

# Full saved targets are recommended; legacy host/IP keys still work.
host_profiles:
  alice@server.local:2222:
    alias: production
    tags: [prod, web]
    os: Ubuntu
    commands:
      - id: logs
        name: logs
        description: Follow application logs
        command: journalctl -u app -f
        interactive: true
        confirm: true
    use_unix_discovery: true
    rsync_stability: true
```

How to use the config:

1. Add hosts with `nexus host add user@host`.
2. Open the config with `nexus config`.
3. Under `host_profiles`, use the full saved target when profiles differ by user or port.
4. Save and return to Nexus; aliases, tags, and commands reload automatically.

For themes, press `a`, choose **Themes**, move with `j/k`, then press
`enter` to use one for the current session or `s` to save it as the default.
Choose **Workspace** the same way to preview or save Workbench, Console, or
Fleet. `ui.pinned_actions` accepts stable built-in IDs and `command:<id>`;
configured pins lead in exact order, followed by frequency-ranked actions.
Hosts are grouped with reachable machines first, then ordered by frequency and
recency within each group. Probe completion preserves the selected target and
active host filter.

Useful configuration commands:

```bash
nexus config          # edit YAML
nexus config show     # print source YAML
nexus config path     # print its path
nexus theme preview   # preview every built-in theme
```

Config keys:

- `full_index_depth`: max depth used in `--indexing full` mode.
- `ui.theme`: `nexus`, `nord`, `dracula`, `catppuccin`, `gruvbox`, `mono`, or `terminal`.
- `ui.background`: `opaque` or `transparent`.
- `ui.workspace`: `workbench`, `console`, or `fleet`.
- `ui.pinned_actions`: ordered built-in IDs or `command:<id>` references.
- `ui.colors`: optional semantic overrides such as `focus` and `live`.
- `reachability.*`: bounds saved-target DNS/TCP checks; these are not SSH/login latency checks.
- `fzf.layout`: `default`, `reverse`, or `reverse-list`.
- `fzf.prompt` / `fzf.pointer`: picker text and selection marker.
- `commands`: global trusted remote commands.
- `tag_commands.<tag>`: commands inherited by matching host tags.
- `host_profiles.<target>.alias` / `tags` / `os` / `commands`: TUI metadata and per-host commands.
- `commands[].interactive`: set to `true` only when a command needs to own the
  terminal, such as `tmux attach`; ordinary command output stays inside Nexus.
- `commands[].confirm`: set to `true` to require an exact command/target review;
  omitted or `false` runs the trusted configured command immediately.
- `host_profiles.<host>.use_unix_discovery`: force Unix-style discovery commands for that host.
- `host_profiles.<host>.rsync_stability`: enables conservative `rsync` profile for reliability on mixed environments.

## Environment Variables

- `NEXUS_RSYNC_PATH`: override `rsync` binary path.
- `VISUAL` / `EDITOR`: editor used by `nexus config`.
- `NO_COLOR`: disable ANSI color in help output.
- `CLICOLOR_FORCE`: force colored help when output is not a TTY.

## Repository Layout

- `cmd/nexus`: the thin executable entrypoint.
- `internal/nexus`: the application, grouped by responsibility into CLI,
  connection, configuration, diagnostics, dashboard, theme, state, and transfer
  source files with colocated tests.
- `.github`: CI, security, dependency, issue, and release automation.
- Root documentation and release configuration stay visible to contributors.

Go projects conventionally use `cmd` and `internal` directly rather than a
generic `src` directory. This keeps the public executable obvious and prevents
application internals from becoming importable API by accident.

## Operational Notes

- Host history accepts `user@host`, `user@host:port`, `user@[IPv6]`, and `user@[IPv6]:port`.
- `user@host:22` is canonicalized to `user@host`, so default-port entries deduplicate.
- A global `--port` overrides an embedded or saved port for that invocation.
- SSH control sockets live in a private Nexus cache directory; Nexus never changes permissions on `~/.ssh`.
- `pull` may auto-open media files (`.mp4`, `.mov`, `.png`, `.jpg`) on macOS after transfer.
- Remote discovery applies broad ignore filters and merges `.gitignore` patterns when available.

## Research-informed design

The release prioritizes remote workflows, multiplexing, color consistency, and graphical integration based on [Terminal Lucidity](https://alphaxiv.org/abs/2504.13994), an empirical study of terminal pain points. Connection reuse and latency-sensitive defaults are also consistent with the transport findings in [Towards SSH3](https://alphaxiv.org/abs/2312.08396). Nexus keeps ordinary commands scriptable and adds the dashboard as an evolutionary layer rather than replacing the CLI.

## Development

```bash
go test ./...
go vet ./...
```

Optional local lint:

```bash
golangci-lint run ./...
```

## Release

- Releases are automated from git tags (`v*`) via GitHub Actions + GoReleaser.
- Artifacts are published for macOS and Linux (`amd64`, `arm64`) with checksums.

## Community

- Contributions: `CONTRIBUTING.md`
- Security reporting: `SECURITY.md`
- Code of conduct: `CODE_OF_CONDUCT.md`
- Changelog: `CHANGELOG.md`
