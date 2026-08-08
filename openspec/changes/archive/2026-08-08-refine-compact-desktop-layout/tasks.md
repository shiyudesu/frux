## 1. Responsive Shell Foundation

- [x] 1.1 Add centralized wide, compact, and narrow desktop density variables and interaction insets to the existing web style tokens without introducing mobile-layout breakpoints.
- [x] 1.2 Refactor the shared shell responsive CSS so the 160px and 72px navigation states, 56px header, app-body offsets, padding, gaps, and clamped search width follow the three desktop tiers without horizontal document overflow.
- [x] 1.3 Update `TopNav` so upload and guest login can use compact icon presentations and lower-priority authenticated actions move into a nonempty accessible overflow menu when narrow density requires it.
- [x] 1.4 Add focused `TopNav` component tests for search preservation, compact action semantics, overflow keyboard operation, Escape or outside dismissal, and focus return.

## 2. Compact Feed Composition

- [x] 2.1 Apply shared Feed density and right-interaction inset variables to metadata, status messages, and stage copy so readable content never flows beneath the action rail or player controls.
- [x] 2.2 Compact action-rail visual sizes and spacing by desktop tier while retaining usable button hit boxes, accessible names, focus indicators, and existing follow or interaction behavior.
- [x] 2.3 Refine player-control layout and priority rules so progress, play or pause, mute, fullscreen, quality, playback rate, and continuous play remain operable at 1024px and 800px while optional text uses shorter or hidden presentations.
- [x] 2.4 Constrain quality, playback-rate, recommendation-feedback, and other Feed popovers to the available stage or viewport edge and preserve keyboard and Escape behavior.
- [x] 2.5 Clamp the compact details drawer to the available viewport while preserving scrim dismissal, dialog semantics, focus return, discussion state, and the active Feed item.
- [x] 2.6 Verify that `CollectionQueueViewer` and other surfaces sharing Feed stage classes inherit the compact density safely without changing their queue, swipe, comment, or close behavior.

## 3. Frontend Verification

- [x] 3.1 Add or update focused Feed component tests covering compact control semantics, menu reachability, action availability, and comment-drawer behavior without relying on CSS geometry in jsdom.
- [x] 3.2 Run the relevant `TopNav`, `VideoStage`, player-control, collection-queue, and threaded-comment Vitest suites and fix regressions introduced by the responsive changes.
- [x] 3.3 Run `pnpm -C apps/web run build` and resolve all strict TypeScript or production-build failures.
- [x] 3.4 Use Windows Chrome through Chrome DevTools to verify shell and Feed geometry at 1440px, 1200px, 1024px, and 800px, including horizontal overflow, search and primary-action reachability, metadata or action separation, and player menus.
- [x] 3.5 Verify comments closed and open at wide, compact, and narrow widths, then capture and review the required shell, Feed, authentication, profile, messages, and upload screenshots.

## 4. Documentation and Specification Validation

- [x] 4.1 Update `docs/uiux.md` with the three desktop density tiers, top-bar prioritization, bounded Feed UI density, native-coordinate shell rule, and viewport verification matrix.
- [x] 4.2 Mark the small-screen frontend entry in `docs/当前问题.md` resolved only after the 800px browser checks pass, including a concise description of the desktop-density solution.
- [x] 4.3 Run `openspec validate --all --strict` and resolve any proposal, design, delta-spec, or task-format errors.
