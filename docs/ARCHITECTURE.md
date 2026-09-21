# Architecture

Nexus is a CLI + TUI for SSH host management. The entry point (`cmd/nexus/main.go`, 15 lines) calls `app.Execute()`. All implementation lives in `internal/app`, a single package with no sub-packages.

## Layout

`internal/app` source files by purpose:

- **main.go**: CLI entry point, `app` struct (config/state paths + flags), `newRootCmd`, `PersistentPreRunE` hooks, global flag validation, bootstrap sequence
- **btop_frame.go**: VT-100 frame evaluation and scoring for btop session output
- **btop_pool.go**: Singleton PTY-backed `btopStreamPool` for live remote monitor, event routing
- **btop_stream.go**: Remote btop session execution via SSH, stream replay into VT emulator, event publishing
- **config.go**: YAML parsing (strict `KnownFields(true)`), defaults, surgical edits via `yaml.Node`, atomic writes with fsync
- **connection.go**: `connectionTarget{User,Host,Port}` parsing, validation regexes, canonical string form, SSH/rsync destination builders
- **copy_key.go**: SSH public-key enrollment from local or remote source
- **dashboard.go**: Bubble Tea `dashboardModel` value receiver, `runDashboard` loop, UI event dispatch, workspace/overlay logic, all view renderers
- **diagnostics.go**: Remote script execution with shell fallbacks (sh/bash/zsh), probe runner, `remoteShellCommand` wrapper
- **fleet_telemetry.go**: Persistent SSH stream pool (up to 12 concurrent) for batch metrics collection across hosts
- **fzf_style.go**: FZF UI config from YAML (colors, layout, previews)
- **help.go**: Help text snippets
- **metadata.go**: Cached host metadata (CPU, memory, disks, OS); remote script results parsed into struct
- **performance.go**: SSH multiplexing args (ControlMaster/ControlPersist/ControlPath cache)
- **probes.go**: TCP reachability tests with concurrency limits, host discovery bootstrap
- **qol.go**: Quality-of-life helpers (path expansion, key copying, clipboard interaction)
- **state.go**: Frecency-ranked host state, zoxide-style decay, file-locked mutations, `updateState` guarded by `acquireStateLock`
- **telemetry.go**: Single-host metrics fetch on `telemetryTickMsg`, exponential backoff on failures, btop minimum dimensions
- **theme.go**: Color scheme (light/dark/auto), lipgloss styles applied to terminal output

## CLI Wiring

The `app` struct holds four file paths:
- `~/.config/nexus/config.yaml` (parsed into `appConfig`)
- `~/.config/nexus/hosts.json` (discovered hosts, persisted to state)
- `~/.config/nexus/state.json` (frecency, metadata cache, persisted state)
- Plus flags: `--config-dir`, `--dry-run`, `--verbose`, `--ssh-port`, `--remote-index`

`Execute()` creates an `app`, calls `newRootCmd(a)` to build the cobra CLI tree, and runs it. The root command's `PersistentPreRunE`:

1. Calls `a.validateGlobalOptions(cmd)` to check flag constraints
2. Calls `a.ensureBootstrap()` **unless** `cmd.Annotations["skip-bootstrap"] == "true"`

`ensureBootstrap()` creates the config directory, seeds `hosts.json` if missing (via `probes.go`), calls `ensureConfigFile` to initialize YAML, then `loadConfigFromYAML` which populates package globals:

- `loadedConfig` (type `appConfig`; config.go)
- `fzfUIConfig` (type `fzfConfig`; fzf_style.go)
- `fullIndexDepth` (int; main.go)
- `verboseLogging` (bool; main.go)

Downstream code reads these globals directly; tests mutate and restore them.

## Target Model

Every remote reference is a `connectionTarget{User,Host,Port}` (connection.go). `parseConnectionTarget(raw string)` parses `user@host[:port]` format with validation:

- User matches `userPattern` (`^[A-Za-z0-9._-]+$`)
- Host matches `hostPattern` (valid hostname) or is a valid IP
- Port is 1–65535, defaults to 22

The `.String()` method returns canonical form: `:22` is dropped, IPv6 addresses are bracketed. This string is the key for `hosts.json` and `state.json`.

SSH destination: `sshDestination()` formats as `user@host`. Rsync destination: `rsyncDestination()` formats as `user@host:`. Port is always passed separately via SSH `-p` flag.

## Config

Config is YAML (edited manually or via `nexus config`). Parsing is strict: `decoder.KnownFields(true)` rejects unknown keys. `loadAppConfig(path)` validates and normalizes the loaded structure.

UI value edits (monitorBtopEnabled, workspace, etc.) are **surgical**: `saveUIValuesRemoving` loads the YAML into a `yaml.Node` tree, mutates the tree in place (preserving comments), then `atomicWritePrivate` writes it: temp file → chmod 0600 → fsync → rename.

`defaultAppConfig()` provides zero-values. Global `loadedConfig` is set after parsing. Profiles are merged: `commandsForTarget` and `profileForTarget` query tag defaults, then host-specific overrides (main.go).

## State

`state.go` persists zoxide-style frecency (access count, last-access time) and cached metadata per host. All mutations happen via `updateState(path, mutate)`:

- Acquires lock via `acquireStateLock` (mkdir lock dir, 2s timeout, 30s stale reaper)
- Calls user's mutation function
- Calls `atomicWritePrivate` (temp + chmod + fsync + rename)

`sortHostsByFrecency` ranks hosts by frecency decay; state is recomputed on each dashboard load.

## Shelling Out

SSH argv is built by `buildSSHArgsForTraffic(host, interactive, remoteCmd, profile)` where profile is:

- `sshTrafficDefault`: NonInteractive BatchMode, publickey-only, StrictHostKeyChecking=yes, short timeouts
- `sshTrafficMonitoring`: Like Default but with ssh-agent fallback, for telemetry streams

Both profiles get `sshMultiplexArgs()` appended (performance.go): ControlMaster=auto, ControlPersist=600, ControlPath under user cache dir.

**Critical:** Remote scripts MUST be wrapped with `remoteShellCommand("sh", script)` (diagnostics.go) which produces `sh -c '<shellQuote(script)>'`. The `shellQuote` function (main.go) escapes for sh safety.

Rsync: `buildRsyncArgs` and `buildRsyncSSHCommand` construct `-e ssh -p <port>` options. SSH args are never built as a shell string; always as argv.

## TUI

`dashboardModel` is a **value receiver** (not a pointer). The `runDashboard` loop:

1. Read hosts → load state → sort by frecency
2. Build model → `tea.NewProgram(model, tea.WithAltScreen()).Run()`
3. On action selection, run outside Bubble Tea, then loop

**Update dispatch:** `Update` method handles Focus/Blur/WindowSize/tick/telemetry/btop/key messages. `updateKey` is a waterfall of overlay-flag checks (e.g., command running, detail pane active, help shown); each branch returns the updated model and a Cmd.

**View hierarchy:**

1. `View()` → `finishView()` (top-level formatting)
2. `dashboardBodyView()` → `ultraWideView()` (tab-based workspaces) or classic variants
3. Pane renderers: `hostListView`, `detailView`, `telemetryView`, `activityView`, `actionRailView`, `btopPanelView`
4. `renderPanel` → `paintTerminalSurface` (lipgloss + ANSI recolor) → `finishView`

**Workspaces:** `workspaceModes()` returns [workbench, console, fleet]. Tabs are visible if `experimentalTabs && width>=150 && height>=28` (per `workspaceTabsVisible()`). `ultraWideView` dispatches to `workbenchWorkspaceView`, `consoleWorkspaceView`, or `fleetWorkspaceView`.

Styles come from `styles()` (computed per frame) and `theme.go` (color scheme). Overlays and pause state: `telemetryPaused()` checks telemetry focus + operation state.

## Live Data Pipelines

Three independent subscription-driven data streams:

1. **Single-host telemetry** (telemetry.go): On `telemetryTickMsg`, runs `telemetryCommand` (fetch metrics from chosen host). Backoff via `telemetryBackoff(failures, generation)`. The Cmd must be re-returned after every message.

2. **Fleet telemetry** (fleet_telemetry.go): `fleetTelemetryPool` maintains up to `fleetTelemetryMaxStreams` (12) persistent SSH streams, each running a remote metrics loop. Batched via `waitForFleetTelemetry`. Rotation via `fleetTelemetryRotateTick` (cycles active streams).

3. **Btop stream** (btop_pool.go, btop_stream.go): Single PTY `ssh -tt` session running `btopStreamCommand`. Output replayed into `vt.NewEmulator`, frames scored by `btopFrameScore`, published as `btopStreamEventMsg`. UI gate: `ensureBtopStream`, `monitorBtopViewport` (requires console workspace, width>=120, height>=20, viewport >= 80x24 = `btopMinColumns`x`btopMinRows`). Published via `waitForBtopStreamPool`.

**Subscription pattern:** Each `Cmd` returned by a `waitFor*` function must be re-returned after every Update; the wait loop only fires once and must be subscribed again.

## Probes, Metadata, Diagnostics

- **probes.go**: TCP reachability checks (`Reachability.Concurrency` limit), host bootstrap discovery
- **metadata.go**: Remote script (`metadataScript` const) extracts CPU, memory, storage; results parsed into snapshots. `meaningfulStorageDisks` filters storage list.
- **diagnostics.go**: `runScript` with shell fallback (sh/bash/zsh), uses `remoteShellCommand` wrapper. `probeScript` (buildRemoteProbeScript) constructs a multi-command shell script.

## Testing

Tests are hermetic. `e2e_test.go` and `cli_matrix_test.go` create fake binaries (ssh, rsync, fzf, editor as `#!/bin/sh` scripts) in `t.TempDir()` and prepend to PATH. Network tests in `telemetry_test.go` skip unless `NEXUS_TEST_SSH_HOST` is set. `dashboard_test.go` drives `Update`/`View` directly with fixed dimensions (no PTY needed).

## Build, CI, Release

- **ci.yml** (.github/workflows): Run linters, tests, coverage
- **.golangci.yml**: Linter configuration
- **.goreleaser.yaml**: Binary naming, asset structure
- **install.sh**: Download and verify release binaries per OS/arch

Run `go build ./... && go vet ./... && go test -race ./...` before finishing. A `make check` target is being added in parallel; prefer it once available.
