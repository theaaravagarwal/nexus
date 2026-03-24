# nexus

CLI for SSH sessions and remote file sync workflows.

`nexus` combines host history, interactive fuzzy selection, and `rsync`-based transfers so you can move between remote machines and file operations without retyping destinations.

## Highlights

- SSH into known hosts or enter a new `user@host` inline.
- Pull and push files/directories over `rsync` with interactive path selection.
- Host history persisted in `~/.config/nexus/hosts.json`.
- Remote indexing modes:
  - `lazy` (default): shallow listing for faster navigation.
  - `full`: deeper recursive listing (depth controlled via config).
- Cross-platform remote handling for Unix-like and Windows targets.
- Optional verbose logs for discovery and transfer diagnostics.

## Requirements

- Go `1.22+`
- `ssh` in `PATH`
- `rsync` in `PATH` (or set `NEXUS_RSYNC_PATH`)
- `fzf` in `PATH` (required for interactive selection)

## Install

Method 1: install from source with Go:

```bash
go install .
```

Method 2: build a local binary:

```bash
go build -o bin/nexus .
```

## Quick Start

```bash
# 1) add a host once
nexus host add user@example-host

# 2) open SSH session (interactive host picker if omitted)
nexus ssh

# 3) pull from remote (fully interactive if args omitted)
nexus pull

# 4) push local path to remote (fully interactive when only source is given)
nexus push ./build
```

## CLI

```bash
nexus [global flags] <command>
```

Commands:

- `ssh [user@ip]`
- `pull [user@ip] [remote-path] [local-dir]`
- `push [file] [user@ip] [remote-dir]`
- `host list`
- `host add [user@ip]`
- `host remove [user@ip]`
- `config` (opens `~/.config/nexus/config.yaml` in your editor)

Global flags:

- `-n, --dry-run`: print `rsync` command without transferring.
- `-v, --verbose`: enable debug logs.
- `--remote-index lazy|full`: remote discovery mode (default `lazy`).

## Configuration

On first run, nexus bootstraps:

- `~/.config/nexus/hosts.json`
- `~/.config/nexus/config.yaml`

Default config template:

```yaml
# NEXUS settings
# Maximum recursion depth when --remote-index full is used.
full_index_depth: 5

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

- `full_index_depth`: max depth used in `--remote-index full` mode.
- `host_profiles.<host>.use_unix_discovery`: force Unix-style discovery commands for that host.
- `host_profiles.<host>.rsync_stability`: enables conservative `rsync` profile for reliability on mixed environments.

## Environment Variables

- `NEXUS_RSYNC_PATH`: override `rsync` binary path.
- `VISUAL` / `EDITOR`: editor used by `nexus config`.

## Repository Layout

- `main.go`: active CLI entrypoint and command wiring.
- `cmd/`: alternate modular command package (currently not the active entrypoint).
- `internal/hosts`: host validation and persisted host-store helpers.
- `internal/remote`: SSH-based remote operations.
- `internal/transfer`: rsync transfer wrappers.
- `internal/ui`: fzf-backed interactive selection.
- `internal/pathutil`: local path expansion/normalization helpers.

## Operational Notes

- Host history accepts only `user@host` format (`host` can be IP or hostname).
- `pull` may auto-open media files (`.mp4`, `.mov`, `.png`, `.jpg`) on macOS after transfer.
- Remote discovery applies broad ignore filters and merges `.gitignore` patterns when available.

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
