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
- Medium: host list plus stacked details.
- Wide: two-region host list and selected-host summary. Fleet, themes, saved
  commands, and deeper actions appear only when requested.
- `Enter` always connects; `/` searches; `Ctrl+K` opens actions; `i` refreshes
  system details in the background; `a` explains saved commands; `?` explains
  the current surface.
- Selection and focus remain visible without relying on color.
- Reachability is visually encoded as `●` online, `!` refused, `×` unavailable,
  and `◌` checking, so state survives monochrome terminals.
- Fleet is an optional endpoint overview and explicitly describes TCP
  reachability without implying discovery, authentication, or dependency links.
- Finite scans update inside the TUI. Pull/push path discovery uses a native
  terminal picker; only the actual transfer temporarily owns the terminal.
  Terminal-owning actions restore the same selected host when they finish.
- The footer prioritizes active work, completion, and recovery over static
  theme or shortcut status.

## Component character

Use thin dividers, terminal-cell spacing, full-width selected rows, restrained
boxes, and one system monospace stack. Avoid ornamental glow, gradients,
glass, nested cards, giant radii, and decorative motion. Async changes update
state without moving the user's current selection.

## Themes and configuration

Built-in themes share identical semantic roles and component behavior. YAML
selects a named theme and opaque or transparent background; advanced users can
override semantic colors. Theme choice applies consistently to the TUI, help,
and FZF. The TUI can preview themes live without saving; detailed persistence
remains an explicit YAML edit.
