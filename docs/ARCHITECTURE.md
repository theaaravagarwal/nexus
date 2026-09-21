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

`ensureBootstrap()` runs **once per process** but reloads when `config.yaml` changes (via mtime/size comparison). It creates the config directory with mode 0700, seeds `hosts.json` if missing (via `probes.go`), calls `ensureConfigFile` to initialize YAML, then `loadConfigFromYAML` which populates package globals. Config file ownership is checked via `checkFileOwnership` (build-tagged in `proc_unix.go`/`proc_windows.go`):

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
- `sshTrafficMonitoring`: telemetry, fleet, and btop streams. Adds `BatchMode=yes`, `PreferredAuthentications=publickey`, `StrictHostKeyChecking=yes`, `ConnectTimeout=3`, one attempt. Never prompts; unknown hosts or password-only hosts fail fast with an explained error.

Both profiles get `sshMultiplexArgs()` appended (performance.go): ControlMaster=auto, ControlPersist=600, ControlPath under user cache dir. The `-q` flag (quiet mode) is **not** applied to PTY-backed monitoring streams, so ssh diagnostics (e.g., "Permission denied") reach stderr for friendly error handling. SSH Compression is enabled only for interactive PTY streams (btop monitor).

**Critical:** Remote scripts MUST be wrapped with `remoteShellCommand("sh", script)` which produces `sh -c '<shellQuote(script)>'`. The `shellQuote` function escapes for sh safety.

Rsync: `buildRsyncArgs` and `buildRsyncSSHCommand` construct `-e ssh -p <port>` options. The `--protect-args` flag is gated by `rsyncSupportsProtectArgs` (performance.go). Hosts.json mutations are guarded by `acquireFileLock` (state.go).

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

**Caching & pointer safety:** Render caches (`dashboardRenderCache`, `renderCache`, `commandsCache`, `btopFrameCache`) and host fields (`DisplayName`, `MeaningfulDisks`) are pointer-typed. Bubble Tea's single-goroutine event loop guarantees cache mutations are safe: the same pointer is forwarded through View/Update calls without external mutation. Render-parity test (`render_parity_test.go`, testdata in `testdata/render`) runs with `-update` flag to regenerate golden files. Differential test `truncate_text_test.go` validates text truncation.

**Workspaces & btop gate:** `workspaceModes()` returns [workbench, console, fleet]. Tabs are visible if `experimentalTabs && width>=150 && height>=28`. `monitorBtopViewport` requires console workspace, `width >= minBtopTerminalWidth(density)`, `height >= minBtopTerminalHeight(activityOpen)` (where btop needs ≥80×24 viewport). `btopGateReason` explains gate failures. Btop stream is kept alive for `btopStreamPauseGrace` (30s) after user focus leaves monitor pane; `btopStreamRetryAt` guards retry attempts.

Styles come from `styles()` (memoized in `renderCache`, recomputed only when the theme or plain mode changes) and `theme.go` (color scheme).

## Live Data Pipelines

Three independent subscription-driven data streams:

1. **Single-host telemetry** (telemetry.go): On `telemetryTickMsg`, runs `telemetryCommand` (fetch metrics from chosen host). Backoff via `telemetryBackoff(failures, generation)`. The Cmd must be re-returned after every message.

2. **Fleet telemetry** (fleet_telemetry.go): `fleetTelemetryPool` maintains up to `fleetTelemetryMaxStreams` (12) concurrent persistent SSH streams. Rotation every 30 seconds via `fleetTelemetryRotateInterval`. Idle sessions get `fleetTelemetryGracePeriod` (30s) before cleanup. Synced by `fleetTelemetryPool.sync(specs)` which manages desired active sessions.

3. **Btop stream** (btop_pool.go, btop_stream.go): Single PTY `ssh -tt` session running `btopStreamCommand(columns, rows, boxes)`. The **layout-fitting ladder** handles "Terminal size too small" errors from btop: `streamRemoteBtopFrom` walks `btopBoxLadder` (ordered richest-first box configurations) and `btopFallbackLadder` (fallback shown_boxes values), calling `runBtopStreamAttempt` for each attempt. The layout ladder is remembered per target in `lastBoxes` (btop_pool.go). Stream events carry `Stage` ("fitting"/"connecting"/"live") and `Status` (descriptive message). Time-based frame emission and watchdog checks run at `btopStreamTickInterval` (50ms). Output is replayed into `vt.NewEmulator`, frames scored by `btopFrameScore`, published as `btopStreamEventMsg`. UI gate checks `btopGateReason`, which validates workspace, dimensions, and viewport size (≥80×24 = `btopMinColumns`×`btopMinRows`). Btop stream is kept alive for `btopStreamPauseGrace` (30s) after leaving monitor pane. **Windows hosts:** `detectBtopPlatform` runs `echo %OS%` once per target (cached in `btopPlatformCache`); on `Windows_NT` the attempt uses no PTY and sends `windowsBtopStreamCommand`, an `-EncodedCommand` PowerShell supervisor that starts `waitfor | conhost.exe --headless --width W --height H -- btop` (btop4win needs a console; `waitfor` keeps the pseudoconsole's stdin open) and heartbeats a NUL byte through kernel32 `WriteFile` so it can kill the tree when the ssh channel closes (sshd on Windows kills nothing itself, and .NET's console stream hides the broken-pipe error). A PTY is never requested on Windows because a cmd AutoRun hook that starts WSL would wait for input forever; on non-PTY sessions it sees EOF and returns. No layout ladder on Windows (btop4win reads its own btop.conf). `telemetryCommand` and `streamRemoteFleetTelemetry` consult the same probe and send `windowsTelemetryCommand`/`windowsFleetTelemetryCommand` (telemetry_windows_remote.go): PowerShell emitting the same `TELEMETRY=`/`NX1` frames from `Win32_OperatingSystem`, `Win32_PerfRawData_PerfOS_Processor` (raw idle/total 100 ns counters feed the existing CPU delta math) and `Win32_PerfRawData_Tcpip_NetworkInterface`; the fleet loop writes through kernel32 `WriteFile` so it exits when the channel closes.

**Subscription pattern:** Each `Cmd` returned by a `waitFor*` function must be re-returned after every Update; the wait loop only fires once and must be subscribed again.

## Discovery & Diagnostics

- **probes.go**: TCP reachability checks (`Reachability.Concurrency` limit), host bootstrap discovery via `probes` command
- **metadata.go**: Remote script (`metadataScript` const) extracts CPU, memory, storage; results parsed into snapshots. `meaningfulStorageDisks` filters storage list by mount patterns.
- **diagnostics.go**: `runScript` with shell fallback (sh/bash/zsh), uses `remoteShellCommand("sh", …)` wrapper. `probeScript` constructs multi-command shell scripts.
- **Path discovery (main.go)**: `getGlobalIgnoreRegex` (single-escaped, anchored) plus `.gitignore` patterns filter remote `find` output in `buildUnixDiscoveryCommand`; transfers run through `buildRsyncArgs`, which adds `--protect-args` when `rsyncSupportsProtectArgs` says the binary is real rsync 3+. Hosts.json mutations are guarded by `acquireFileLock`.

## Testing

Tests are hermetic. `e2e_test.go` and `cli_matrix_test.go` create fake binaries (ssh, rsync, fzf, editor as `#!/bin/sh` scripts) in `t.TempDir()` and prepend to PATH. Network tests skip unless `NEXUS_TEST_SSH_HOST` is set. `dashboard_test.go` drives `Update`/`View` directly with fixed dimensions (no PTY). Render-parity test (`render_parity_test.go`) compares against golden files in `testdata/render/`; run with `-update` flag to regenerate. Differential test `truncate_text_test.go` validates text truncation behavior. CPU profiling via `NEXUS_CPUPROFILE=path` env var in `Execute` profiles TUI runtime.

## Build, CI, Release

- **Makefile**: `build`, `test`, `vet`, `lint`, `bench`, `check` targets (all in one command: `make check`)
- **ci.yml** (.github/workflows): Lint, vet, race-check, test per PR/main push
- **.golangci.yml**: Linter configuration (enabled checks and exclusions)
- **.goreleaser.yaml**: Binary naming, asset structure, release automation
- **install.sh**: Download and verify release binaries per OS/arch

Profiling: `NEXUS_CPUPROFILE=path` writes a CPU profile to `path` for analyzing TUI startup/rendering. Testing headlessly: `NEXUS_TEST_SSH_HOST=host` enables network tests. Driving TUI with tmux: `tmux new-session -x 180 -y 45 'nexus' ; tmux send-keys Tab ; tmux capture-pane -p` creates a headless session and captures rendered output.
