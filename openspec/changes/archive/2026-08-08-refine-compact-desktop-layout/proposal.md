## Why

Frux currently collapses only the side navigation below 1280px while leaving the top bar, Feed overlays, action rail, and player controls at wide-desktop density. Narrow desktop windows therefore preserve the desktop shell but become crowded, clipped, and visually unbalanced instead of progressively compressing like mature desktop short-video products.

## What Changes

- Define wide, compact, and narrow desktop density tiers while continuing to reject a separate mobile navigation, 9:16 page layout, or bottom comment sheet.
- Progressively compact the shared top bar by clamping the search field, reducing the login presentation, and moving lower-priority actions into an accessible overflow control before horizontal space is exhausted.
- Refine the Feed composition so the media region, action rail, metadata, and player controls share space predictably at narrow widths.
- Introduce bounded Feed UI density variables for spacing, typography, and visible labels rather than scaling the entire application shell with `transform: scale()`.
- Preserve usable pointer and keyboard targets, fullscreen behavior, Feed gestures, comment drawer behavior, and existing typed navigation/API contracts.
- Extend browser geometry and screenshot coverage across representative wide, compact, and narrow desktop viewports.
- Synchronize the responsive behavior documented in `docs/uiux.md` and resolve the outstanding small-screen frontend issue.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `douyin-style-web-experience`: Refine the responsive desktop-shell and Feed requirements to define progressive density tiers, header action prioritization, bounded Feed UI scaling, and narrow-window usability.
- `web-browser-smoke-testing`: Expand browser verification to cover the new desktop viewport matrix, geometry, overflow, control reachability, and visual evidence.

## Impact

- Affected frontend areas include `TopNav`, the shared shell styles, Feed stage composition, metadata/action/player-control presentation, compact comment drawer integration, and responsive CSS tokens.
- Frontend tests and browser smoke checks will gain viewport-specific assertions and screenshots.
- `docs/uiux.md` and `docs/当前问题.md` will be updated to match the implemented behavior.
- No backend API, persistence model, route contract, or new runtime dependency is required.
