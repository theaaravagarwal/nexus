# nexus

CLI for SSH sessions and remote file sync workflows.

`nexus` combines host history, a keyboard-driven dashboard, fuzzy path navigation, remote diagnostics, and `rsync` transfers so you can move between machines without retyping destinations.

## Highlights

- Open the `rip`-inspired host dashboard by running `nexus` with no command.
- SSH into `user@host`, `user@host:port`, IPv4, or bracketed IPv6 targets.
- Pull and push files/directories over `rsync` with interactive path selection.
- Inspect remote CPU, memory, network, and storage with portable command fallbacks.
- Host history persisted in `~/.config/nexus/hosts.json`.
- Remote indexing modes:
  - `lazy` (default): shallow listing for faster navigation.
  - `full`: deeper recursive listing (depth controlled via config).
- Cross-platform remote handling for Unix-like and Windows targets.
- Reused SSH connections, keepalives, and compression-aware rsync arguments.
- Adaptive colored help (`NO_COLOR=1` disables color).
- Optional verbose logs for discovery and transfer diagnostics.

## Requirements

- Go `1.26.1+`
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
curl -fsSL https://raw.githubusercontent.com/theaaravagarwal/nexus/main/install.sh | NEXUS_INSTALL_DIR=/usr/local/bin NEXUS_VERSION=v0.1.0 sh
```

The installer detects macOS/Linux and ARM64/x86-64, verifies the release checksum, and does not require `sudo` for the default location.

### Install from source

Clone and build:

```bash
git clone https://github.com/theaaravagarwal/nexus.git
cd nexus
go install .
```

Or build a local binary:

```bash
go build -o bin/nexus .
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
| `j` / `k` or arrows | Select a host |
| `/` or `f` | Filter hosts |
| `enter` / `s` | SSH |
| `p` / `u` | Pull / push |
| `t` / `i` | Top / system info |
| `n` / `d` | Network / storage |
| `q` / `esc` | Quit |

## Configuration

On first run, nexus bootstraps:

- `~/.config/nexus/hosts.json`
- `~/.config/nexus/config.yaml`

Default config template:

```yaml
# NEXUS settings
# Maximum recursion depth when --indexing full is used.
full_index_depth: 5

# Optional fuzzy-picker styling.
fzf:
  theme: dark
  layout: reverse
  prompt: "Nexus ❯ "
  pointer: "→"

# Optional per-host overrides.
# Keys must match the host part of your saved user@host entries.
# Example: if you add "alice@server.local", use "server.local" as the key.
host_profiles:
  <host-or-ip>:
    # Force Unix command style on remote discovery for this host.
    use_unix_discovery: true
    # Use conservative rsync args for flaky/mixed environments.
    rsync_stability: true
```

How to use the config:

1. Add hosts with `nexus host add user@host`.
2. Open the config with `nexus config`.
3. Under `host_profiles`, add one entry per remote using only the host/IP part (not `user@`).
4. Save and run `nexus pull`/`nexus push`; overrides are applied automatically for matching hosts.

Config keys:

- `full_index_depth`: max depth used in `--indexing full` mode.
- `fzf.theme`: `dark`, `light`, or `cyberpunk`.
- `fzf.layout`: `default`, `reverse`, or `reverse-list`.
- `fzf.prompt` / `fzf.pointer`: picker text and selection marker.
- `host_profiles.<host>.use_unix_discovery`: force Unix-style discovery commands for that host.
- `host_profiles.<host>.rsync_stability`: enables conservative `rsync` profile for reliability on mixed environments.

## Environment Variables

- `NEXUS_RSYNC_PATH`: override `rsync` binary path.
- `VISUAL` / `EDITOR`: editor used by `nexus config`.
- `NO_COLOR`: disable ANSI color in help output.
- `CLICOLOR_FORCE`: force colored help when output is not a TTY.

## Repository Layout

- `main.go`: active CLI entrypoint and command wiring.
- `connection.go`: validated target parsing and canonical history identities.
- `dashboard.go`: Bubble Tea host/action dashboard.
- `diagnostics.go`: portable remote inspection command selection.
- `help.go`: adaptive colored help renderer.
- `performance.go`: SSH multiplexing and rsync capability detection.
- `cmd/`: alternate modular command package (currently not the active entrypoint).
- `internal/hosts`: host validation and persisted host-store helpers.
- `internal/remote`: SSH-based remote operations.
- `internal/transfer`: rsync transfer wrappers.
- `internal/ui`: fzf-backed interactive selection.
- `internal/pathutil`: local path expansion/normalization helpers.

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
