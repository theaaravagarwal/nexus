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

Method 3: install via Homebrew (tap-based distribution):

```bash
brew tap <your-github-user>/nexus
brew install nexus
```

Homebrew maintainer notes:

1. Create a tap repository named `homebrew-nexus`.
2. On each release, publish tarballs for supported platforms.
3. Compute SHA256 checksums.
4. Add `Formula/nexus.rb` in the tap repo with release URLs and checksums.

Minimal formula template:

```ruby
class Nexus < Formula
  desc "SSH and remote file sync CLI"
  homepage "https://github.com/<your-github-user>/nexus"
  version "0.1.0"

  on_macos do
    on_arm do
      url "https://github.com/<your-github-user>/nexus/releases/download/v0.1.0/nexus_Darwin_arm64.tar.gz"
      sha256 "<sha256-darwin-arm64>"
    end
    on_intel do
      url "https://github.com/<your-github-user>/nexus/releases/download/v0.1.0/nexus_Darwin_x86_64.tar.gz"
      sha256 "<sha256-darwin-amd64>"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/<your-github-user>/nexus/releases/download/v0.1.0/nexus_Linux_arm64.tar.gz"
      sha256 "<sha256-linux-arm64>"
    end
    on_intel do
      url "https://github.com/<your-github-user>/nexus/releases/download/v0.1.0/nexus_Linux_x86_64.tar.gz"
      sha256 "<sha256-linux-amd64>"
    end
  end

  def install
    bin.install "nexus"
  end

  test do
    assert_match "Usage:", shell_output("#{bin}/nexus --help")
  end
end
```

## Quick Start

```bash
# 1) add a host once
nexus host add user@10.0.0.55

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

Default config:

```yaml
# NEXUS settings
full_index_depth: 5

# Host profiles
host_profiles:
  10.0.0.55:
    use_unix_discovery: true
    rsync_stability: true
```

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
```

Current status: packages compile; there are no test files yet.
