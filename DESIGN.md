# Nexus Design System

## Direction

Nexus is a layered remote-computer workspace, not a flat status table. Its
visual vocabulary combines a compact navigation rail, state-rich host rows,
focused information panels, a connected-node view, and a command palette.
Information appears progressively so the interface remains usable in narrow
terminals.

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
- Wide: navigation rail, host workspace, detail/tool panels, and connected-node view.
- `Enter` connects; `/` searches; `Tab` and `h/l` move focus; `Ctrl+K` opens
  commands; `e` performs lightweight edits; `?` explains the current surface.
- Selection and focus remain visible without relying on color.

## Component character

Use thin dividers, terminal-cell spacing, full-width selected rows, restrained
boxes, and one system monospace stack. Avoid ornamental glow, gradients,
glass, nested cards, giant radii, and decorative motion. Async changes update
state without moving the user's current selection.

## Themes and configuration

Built-in themes share identical semantic roles and component behavior. YAML
selects a named theme and opaque or transparent background; advanced users can
override semantic colors. Theme choice applies consistently to the TUI, help,
and FZF.

