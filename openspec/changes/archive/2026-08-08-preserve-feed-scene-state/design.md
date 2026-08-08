## Context

All four Feed routes render the same `FeedPage` component with a different `feedScene` prop. `useFeed` nevertheless owns only one set of cards, index, cursor, request identity, recommendation context, and suppression refs. Changing the prop rebuilds the scene session, reruns the load effect with an empty cursor, and replaces the list before setting the index to zero.

This behavior predates the current TypeScript component split. Contextual recommendation later added scene/session identifiers, recent-video context, stable request snapshots, and negative-feedback suppression, but kept the same single-scene client state model. As a result, leaving Recommendation discards the client identity needed to reuse its server snapshot, and returning can build recent-video context from the unrelated scene being left.

The Web router keeps one `FeedPage` mounted while users move among Feed routes, so the immediate defect can be fixed without changing URLs, adding browser storage, or introducing a global application store. The implementation must preserve stale-response protection, strict TypeScript, preload generation isolation, view-event attribution, and the current hand-written router.

## Goals / Non-Goals

**Goals:**

- Restore the active card and usable ordered Feed state when users move among the four Feed routes during one mounted Feed session.
- Keep cards, cursor, request identity, recommendation context, suppression state, and viewer actions isolated by scene.
- Prevent another Feed scene from contributing `recent_video_ids` or `current_video_id` to a Recommendation request.
- Retain current stale-response, authentication, pagination, interaction, view-event, player-pool, and preload safety.
- Bound inactive scene data and keep mutations coherent when the same video appears in more than one retained scene.
- Make restoration and intentional invalidation observable through focused unit/component tests and browser validation.

**Non-Goals:**

- Persisting Feed position across a full page reload, browser restart, logout/login boundary, or a new tab.
- Preserving media playback time, buffering state, open comments, open menus, an in-progress swipe, focus, or fullscreen state.
- Preserving Feed state after navigating to non-Feed pages such as Search, Profile, Messages, Upload, or Video Detail; that requires state ownership above the currently mounted `FeedPage` and can be proposed separately.
- Changing Feed APIs, cursor formats, recommendation ranking, Redis snapshot TTL, or backend persistence.
- Providing backward pagination for cards removed from a compacted inactive snapshot.

## Decisions

### 1. Keep a typed per-scene state map inside the Feed hook

Extract pure scene-state types and transitions into a focused Web helper and let `useFeed` own a `Record<FeedSceneKey, FeedSceneSnapshot | undefined>`. A committed snapshot contains:

- ordered mapped cards and active index/video identity;
- liked and favorited maps for retained cards;
- `nextCursor`, `hasMore`, request ID, and the last ready/error metadata needed to resume;
- Recommendation-only session ID, refresh index, typed context, and suppressed video/author IDs;
- the authentication identity key under which the snapshot was produced.

The active scene remains exposed through the existing `useFeed` return shape so `FeedPage`, `useSwipe`, comments, preloading, and `VideoStage` do not need parallel scene branches.

Alternative considered: mount four hidden `FeedPage` trees. Rejected because it would retain four player/preload/comment trees, continue background effects, duplicate listeners, and create media-resource pressure.

Alternative considered: store only `index` per route. Rejected because a numeric index is meaningless after a new first-page response and would restore a different video.

### 2. Restore committed data without starting a first-page request

On scene activation, the hook first invalidates the authority of requests from the scene being left. If the destination has a valid ready snapshot for the current authentication identity, it restores that snapshot synchronously and does not call `fetchFeedPage` with an empty cursor. The active video is resolved by retained video ID and clamped index so compaction or coherent mutations cannot select another item accidentally.

If no usable snapshot exists, the destination follows the current initial-load path. Explicit retry or refresh also follows the current load path and intentionally replaces that scene's list from index zero.

Alternative considered: always refetch and search the new response for the previous video ID. Rejected because Recommendation may return a different ordered snapshot, later pages may not contain the video, and the request/cursor identity would still be lost.

### 3. Separate committed snapshots from in-flight request authority

Loading and pagination requests are never retained as resumable state. A global activation epoch plus the existing request generation, token check, pagination epoch, serial, and effect cleanup determine whether a response may commit. Switching scenes:

1. closes transient Feed UI and cancels an in-progress swipe;
2. increments activation and pagination authority;
3. leaves the last committed destination-independent snapshot intact;
4. ignores every response whose scene, activation epoch, generation, request identity, or authentication identity is no longer current.

Returning to a scene that was left while its first page was still loading starts a new load because it has no committed ready snapshot. Returning to a scene with a committed page but an interrupted load-more restores the committed page and permits a later load-more retry from the retained cursor.

Alternative considered: let background scene requests complete into their snapshots. Rejected because hidden requests would continue consuming bandwidth and complicate authentication, refresh, feedback, and mutation ordering.

### 4. Preserve Recommendation identity only inside the Recommendation snapshot

Leaving Recommendation no longer creates a new recommendation session or clears accepted-feedback suppression. Returning restores the same client/server request identity, signed snapshot cursor, context, refresh index, and suppression sets without issuing a request.

When Recommendation is first loaded or intentionally refreshed, `recent_video_ids` and `current_video_id` are derived only from its own retained cards. Timeline, Following, and Hot cards are never read to build Recommendation context. Intentional refresh keeps the logical Recommendation session and increments its refresh index; authentication invalidation or an unusable snapshot starts a new session.

Alternative considered: share one session across all Feed scenes. Rejected because only Recommendation uses contextual ranking and signed recommendation snapshots, while the other scenes have different ordering and cursor contracts.

### 5. Bound inactive snapshots without retaining media resources

Only serializable card and request metadata are retained. Player adapters, media elements, buffered data, object URLs, preload handles, timers, observers, comments, swipe state, and open menus remain owned by the active render and are released on scene change.

Inactive snapshots use a fixed `MAX_RETAINED_FEED_ITEMS_PER_SCENE` of 120 cards. When a list exceeds the limit, the hook may retain a contiguous suffix only when that suffix contains the active video and the final loaded card, allowing the existing next cursor to remain valid. Viewer-action maps and suppressions are compacted to retained IDs where applicable. If the active video and pagination tail cannot both be retained safely, the snapshot is marked unusable and the next activation performs a clean load rather than inventing cursor continuity.

Alternative considered: retain every loaded card for all four scenes. Rejected because the current append-only pagination path is unbounded and multiplying it by four would create avoidable long-session memory growth.

### 6. Invalidate snapshots at explicit identity and freshness boundaries

All scene snapshots are cleared when the authenticated user identity changes, including logout and account replacement. This prevents private Following/Recommendation data and viewer actions from crossing users. An explicit scene refresh replaces only that scene. Invalid structure, a missing active video, an unusable compacted tail, or an incompatible request identity causes fail-safe reload.

Snapshots remain memory-only and disappear when `FeedPage` unmounts. Browser `history.state`, `localStorage`, and `sessionStorage` remain unchanged, so no new privacy or schema-migration surface is introduced.

### 7. Apply video mutations across retained copies

Card patches keyed by video ID, including like count, favorite count, comment count, and viewer action state, update every retained scene containing that video. This avoids restoring stale UI after an interaction succeeds in another scene. The page-level following map remains the shared source for author follow state.

Recommendation feedback removal and its suppression sets apply only to the Recommendation snapshot. Pagination and index adjustment reuse pure helpers so removing an active or preceding item keeps the intended successor active.

Alternative considered: invalidate every other scene after an interaction. Rejected because it would convert ordinary likes/comments into surprise first-page resets and defeat the continuity feature.

### 8. Verify state transitions below the browser layer and behavior in Chrome

Pure tests cover snapshot creation, active-video restoration, compaction, invalidation, cross-scene patching, and Recommendation-only context. Hook/component tests use deferred requests to prove:

- returning to a committed scene does not issue another first-page request;
- late first-page and pagination responses cannot overwrite the active scene;
- intentional refresh replaces only the current scene;
- identity changes clear authenticated snapshots;
- restored Recommendation pagination keeps its original request/context identity.

Browser validation uses Windows Chrome at the supported desktop widths. It records `data-active-video-id`, switches through another Feed route, returns through direct navigation and browser Back, and confirms the original active ID is restored without an extra first-page request or console/runtime failure.

### 9. Route explicit navigation refresh through a typed scene request context

Add a small Web-only `FeedRefreshProvider` inside the existing Router and Session providers. It owns one monotonically increasing request counter per `FeedSceneKey` and exposes a typed `requestRefresh(scene)` action.

`SideNav` renders the refresh control only inside the currently active Feed row. The row owns the shared highlighted background, while the main destination and refresh remain separate focusable controls. The refresh uses an original Frux single-direction circular-arrow glyph at 16px with low-emphasis idle color and no raised tile background, following the observed Douyin information hierarchy without copying its SVG path. `FeedPage` reads the counter for its current scene and passes it into `useFeed`. The hook consumes each new counter exactly once in its scene activation effect, treats the current valid snapshot as intentional refresh context, removes that scene snapshot, and starts a first-page load. Main navigation clicks continue to restore snapshots.

Alternative considered: dispatching a global DOM event. Rejected because React effect timing and event delivery can race under Strict Mode.

Alternative considered: encoding refresh in the URL query or `history.state`. Rejected because refresh is transient application intent, not a stable destination, and stale history entries must not replay refreshes.

## Risks / Trade-offs

- [Retained cards can become stale while another scene is active] -> Keep snapshots memory-only, patch successful local mutations across scenes, retain server visibility checks for future pages, and make explicit refresh available as the freshness boundary.
- [Four scene snapshots increase memory use] -> Retain data only, release all media resources, and compact inactive snapshots to a fixed 120-card bound.
- [Compaction can make an old active item incompatible with the retained pagination tail] -> Mark that snapshot unusable and reload rather than skip cards or fabricate a cursor.
- [Restoring request identity could accept a late obsolete response] -> Bind every commit to scene, activation epoch, generation, request identity, and authentication identity.
- [Scene restoration changes current analytics timing] -> Do not emit a new Feed request on restore; restored playback creates its normal new playback session while retaining the card's original Feed request ID for attribution.
- [Cross-scene mutation helpers increase state-update breadth] -> Use pure video-ID keyed transitions and focused tests instead of duplicating mutation logic in handlers.
- [Refresh controls can become cramped in the 72px rail] -> Keep the primary navigation hit area unchanged and position a compact, separately focusable refresh button at the row edge with an accessible label and visible focus.

## Migration Plan

1. Add pure scene-snapshot types, compaction, restoration, invalidation, and mutation helpers behind the existing `useFeed` interface.
2. Refactor `useFeed` state and refs to use the scene map while preserving current first-load behavior when no snapshot exists.
3. Add Recommendation context/session isolation and stale-response guards before enabling restoration.
4. Add component and browser coverage, then update Feed and UI/UX documentation.
5. Roll back by removing scene restoration and returning to unconditional `loadFeed`; no persisted data or backend migration requires cleanup.

## Open Questions

No blocking design questions remain. Continuity across non-Feed routes and full reloads is intentionally excluded from this change.
