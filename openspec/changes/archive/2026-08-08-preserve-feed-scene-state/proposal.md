## Why

The four Web Feed routes currently share one transient `useFeed` state, so changing scenes reloads the first page, resets the active index to zero, and discards the previous scene's request, cursor, and recommendation-session identity. Returning to Recommendation can also derive context from the scene being left, which breaks scene continuity and can mix unrelated recent-video context.

## What Changes

- Retain a bounded in-memory snapshot for each of `timeline`, `recommend`, `following`, and `hot`, including the ordered cards, active item, pagination state, request identity, and viewer-action state needed to resume the scene.
- Restore a valid scene snapshot when users switch among Feed routes instead of issuing a new first-page request and resetting to the first card.
- Add an independently labeled refresh control beside the currently active Feed destination in the left navigation; activating it intentionally replaces only that scene from its first page.
- Keep recommendation session, request, recent-video context, negative-feedback suppression, and pagination state isolated to Recommendation so other Feed scenes cannot contaminate it.
- Define explicit invalidation for authentication identity changes, intentional refreshes, unusable snapshots, and bounded retention; transient gestures, open overlays, and media playback position are not persisted.
- Keep mutations coherent when the same video is present in multiple retained scene snapshots, while continuing to release inactive player and preload resources.
- Add focused frontend state tests and real-browser route-switch coverage for restoration, invalidation, request isolation, and stale-response rejection.

## Capabilities

### New Capabilities

- `feed-scene-continuity`: Defines scene-scoped Web Feed snapshots, restoration semantics, bounded retention, invalidation, and cross-scene mutation coherence.

### Modified Capabilities

- `contextual-recommendation`: Requires Web recommendation context, request identity, suppression state, and session continuity to remain scoped to the Recommendation scene.
- `douyin-style-web-experience`: Changes Feed-route switching from unconditional first-page reload to restoration of the retained active scene and item.
- `web-browser-smoke-testing`: Adds browser verification that Feed route switching restores the active video and does not leak request context or accept stale responses.

## Impact

- Primarily affects `apps/web/src/hooks/useFeed.ts`, `apps/web/src/pages/FeedPage.tsx`, related typed state helpers, and frontend tests.
- May add a small Web-only scene-state store or reducer, but does not add a routing library, browser persistence dependency, public API, or backend schema change.
- Preserves the existing Feed response, cursor, recommendation context, view-event, interaction, preload, and player-adapter contracts.
- Requires synchronized updates to Feed/UI documentation and the affected OpenSpec main specifications after implementation.
