# Nexus

## Product

Nexus is a history-first SSH and remote-transfer CLI for developers who move
between several computers. It keeps ordinary commands scriptable while making
the interactive terminal experience the fastest way to find, inspect, and
connect to a saved remote.

## Audience and scene

The primary user is a developer or operator working in a terminal, often in a
dim environment, who needs useful remote context without triggering surprise
authentication prompts. They value keyboard speed, exact targets, and visible
system state over decorative dashboard metrics.

## Core outcomes

- Connect to a saved `user@host[:port]` with one action.
- Surface the machines used most often without manual sorting.
- Distinguish reachability from authenticated SSH/system information.
- Run transfers, diagnostics, and configured remote commands without leaving
  the terminal.
- Keep the CLI, YAML configuration, and plain/no-color behavior dependable.

## Product truths

- A host's non-default SSH port is part of its saved connection identity.
- Background reachability checks are DNS/TCP checks only; they never authenticate.
- Custom commands are trusted executable configuration and always require an
  explicit confirmation before Nexus runs them.
- The TUI is the primary interactive experience, while detailed editing remains
  available through the CLI and YAML.

## Platform

Cross-platform Go CLI and terminal UI for macOS and Linux, with portable remote
handling for Unix-like and Windows targets.

