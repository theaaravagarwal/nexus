# Nexus Design System

## Direction

Nexus is a calm, persistent remote-computer workspace, not a flat status table
or a feature showcase. Its visual vocabulary combines compact host rows, a
focused selected-host summary, a command palette, and on-demand operational
overlays. Information appears progressively so the primary connect workflow
remains obvious at every terminal size.

## Visual scene

A developer operates several remote machines from a dim terminal and needs to
scan identity, health, and recency without losing keyboard flow. The default
theme therefore uses a near-black violet ground, violet only for focus, and
cyan only for live/reachable state.

## Semantic color

Every theme defines background, surface, elevated surface, primary text, muted
text, focus, live, success, warning, error, and border roles. Components use
roles rather than literal colors. `NO_COLOR`, `TERM=dumb`, and the `terminal`
theme preserve a complete non-color hierarchy.

The default `nexus` theme is anchored by:

- Background `#0B0A12`
- Surface `#13111C`
- Elevated/selection `#1B1728`
- Text `#F4F1FF`
- Muted `#A29AB8`
- Focus `#A78BFA`
- Live `#5EEAD4`
- Success `#6EE7B7`
- Warning `#FBBF24`
- Error `#FB7185`

## Layout and interaction

- Narrow: host picker with compact selected-host context and a one-line action hint.
- Medium: host list plus a compact stacked selected-host summary.
- Wide: two-region host list and selected-host summary organized as identity,
  system, storage, and tools/commands. Fleet, themes, saved
  commands, and deeper actions appear only when requested.
- Ultra-wide: an adaptive upper workspace reveals more real inventory when it
  exists, while a full-width lower deck uses the remaining room for selected-host
  pulse, bounded output, and five session events. Workbench is the default;
  Console and Fleet reuse the same hierarchy for output-heavy and cross-host work.
- Short and tiny terminals progressively collapse to a host-first list and then
  a single selected-target line. Rich panes require both width and usable body
  height, so resizing never clips the connect path or its essential keys.
- The workspace uses six canonical keys: `j/k` move, `Enter` connects, `/`
  finds, `a` opens every action, `h` explains every key, and `q` quits.
  Lists reuse `j/k`, `Enter`, and `Esc`; specialized keys appear only in the
  context that uses them.
- Selection and focus remain visible without relying on color.
- Reachability is visually encoded as `●` online, `!` refused, `×` unavailable,
  and `◌` checking, so state survives monochrome terminals.
- Completed probe batches group online hosts first and retain frecency as the
  suborder without moving selection away from the active target.
- Fleet is an optional endpoint overview and explicitly describes TCP
  reachability without implying discovery, authentication, or dependency links.
- Finite scans update inside the TUI. Pull/push path discovery uses a native
  terminal picker; only the actual transfer temporarily owns the terminal.
  Terminal-owning actions restore the same selected host when they finish.
- Saved commands display bounded, sanitized stdout/stderr in a scrollable TUI
  result view. Only commands explicitly marked `interactive` temporarily own
  the terminal.
- Activity holds five bounded events in memory and previews current output.
  State persists one task summary—never remote output or an accumulating
  history—and ignores stale async completions after a newer action begins.
- Selected-host telemetry is lightweight and session-only. It permits one
  noninteractive SSH request at a time, pauses when inappropriate, and backs off
  quietly on failure without changing frecency, action use, or persisted state.
- Static storage inventories stay in the same result view. Cached detail uses
  decimal GB/TB and a bounded mounted-filesystem summary; the Storage action
  exposes the complete inventory.
- The footer shows the four primary workspace actions and points to `h` for the
  complete key reference. Active work, completion, and recovery replace that
  secondary hint when relevant.

## Component character

Use thin dividers, terminal-cell spacing, full-width selected rows, restrained
boxes, and one system monospace stack. Avoid ornamental glow, gradients,
glass, nested cards, giant radii, and decorative motion. Async changes update
state without moving the user's current selection.

## Themes and configuration

Built-in themes share identical semantic roles and component behavior. Open
Actions with `a`, choose Themes, then use `Enter` for the session or `s`
to save the YAML default. Advanced users can still override semantic colors in
YAML. Theme choice applies consistently to the TUI, help, and FZF. Every opaque
semantic span paints its surface; transparent mode emits no forced background.
Workspace mode and ordered built-in or `command:<id>` pins use the same
session/default model without introducing another global shortcut.
