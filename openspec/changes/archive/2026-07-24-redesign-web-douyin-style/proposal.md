## Why

GCFeed already supports the core short-video workflows, but its user frontend uses a generic glass-card visual system that does not consistently match the dense, immersive desktop experience users expect from Douyin-style products. The redesign should unify every user-facing page around a verified desktop short-video shell while preserving GCFeed's existing routes, API contracts, and business behavior.

## What Changes

- Replace the current 220px sidebar and 64px top bar with a compact dark desktop shell based on the observed Douyin public layout: a 160px navigation rail, 56px header, centered search, compact utility actions, and a GCFeed-specific cyan/rose wordmark.
- Redesign all four Feed scenes around a rounded immersive video stage, right-side action rail, bottom playback controls, author metadata overlay, and one-item vertical navigation.
- Change the desktop comment experience from an overlay that covers the video to a fixed-width side panel that reduces the video stage width; retain a bottom-sheet presentation on narrow screens.
- Restyle login/register, own profile, public profile, messages, upload, relation lists, work grids, and work viewer so they share the same typography, spacing, controls, iconography, and dark surfaces.
- Replace Material Symbols usage with a typed, locally owned icon system whose shapes and states fit the new visual language without copying Douyin trademarks or proprietary assets.
- Preserve the existing typed router, session contexts, APIs, Feed scenes, keyboard/swipe controls, loading/error/empty states, and authentication requirements.
- Keep the responsive experience product-safe: desktop follows the verified web layout, while tablet/mobile use a compact rail or bottom navigation and a 9:16 stage instead of reproducing the observed desktop site's horizontal overflow.
- Update the UI/UX documentation and browser smoke expectations for the redesigned shell and responsive states.

## Capabilities

### New Capabilities

- `douyin-style-web-experience`: Defines the unified GCFeed user shell, design tokens, Feed and comment layouts, page-specific presentations, responsive behavior, iconography, accessibility, and visual verification requirements.

### Modified Capabilities

- `web-browser-smoke-testing`: Extends browser verification to assert the redesigned shell, comment-panel layout change, responsive navigation, and critical visual states across user workflows.

## Impact

- Frontend code under `apps/web/src/components`, `apps/web/src/pages`, `apps/web/src/hooks`, and the global styling entry points.
- Frontend assets and potentially new typed presentation helpers; no routing library or new backend endpoint is required.
- Existing user routes and HTTP API contracts remain unchanged.
- `docs/uiux.md` must be synchronized with the new layout tokens, component recipes, responsive rules, and page specifications.
- Browser smoke coverage must continue to exercise authentication, all Feed scenes, comments, messages, profiles, relations, upload, and work viewing under the new shell.
