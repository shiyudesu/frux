## 1. Scene Snapshot Model

- [x] 1.1 Add strict TypeScript scene-snapshot types and a fixed 120-card inactive retention limit covering ordered cards, active identity, viewer actions, pagination, request identity, authentication identity, and Recommendation-only state.
- [x] 1.2 Implement pure helpers to create, validate, activate, compact, and invalidate scene snapshots while preserving the active video and a valid contiguous pagination tail.
- [x] 1.3 Implement pure video-ID keyed helpers that patch counts/viewer actions across retained scenes and keep Recommendation feedback removal isolated to Recommendation.
- [x] 1.4 Add unit tests for snapshot restoration, index/video reconciliation, safe and unsafe compaction, per-scene refresh replacement, identity invalidation, and cross-scene mutation coherence.

## 2. Feed Hook Integration

- [x] 2.1 Refactor `useFeed` to own a typed per-scene snapshot map while preserving its existing public return shape and first-load behavior for scenes without snapshots.
- [x] 2.2 Add scene activation logic that restores a valid committed snapshot without a first-page request, resets swipe/comments, and keeps transient loading or error states out of resumable snapshots.
- [x] 2.3 Add an activation epoch to the existing generation, token, pagination epoch, serial, and cleanup guards so late first-page or load-more responses cannot commit after a scene switch.
- [x] 2.4 Preserve a committed snapshot when load-more is interrupted, permit safe retry from its retained cursor, and reload instead of restoring incomplete or unusable snapshots.
- [x] 2.5 Clear all snapshots when the authenticated identity changes and make explicit retry/refresh replace only the active scene from index zero.

## 3. Recommendation Scene Isolation

- [x] 3.1 Move Recommendation session ID, refresh index, request/context identity, signed cursor state, and suppressed video/author IDs into the Recommendation snapshot instead of clearing them on every scene change.
- [x] 3.2 Build Recommendation `recent_video_ids` and `current_video_id` only from retained Recommendation cards, including first load, explicit refresh, and return after another Feed scene.
- [x] 3.3 Restore valid Recommendation snapshots without a new query, preserve accepted-feedback suppression and pagination identity, and start a new logical session only after identity invalidation or an unusable snapshot.
- [x] 3.4 Add hook tests proving another Feed scene cannot contaminate Recommendation context and restored Recommendation pagination keeps the original request/session identity.

## 4. UI, Mutation, and Resource Coherence

- [x] 4.1 Route successful like, favorite, and comment-count card updates through the cross-scene patch helper so duplicate retained videos restore with current counts and viewer state.
- [x] 4.2 Keep the shared following map and Following directory behavior coherent while restoring Following cards, including collapse/search independence and no author-filter semantics.
- [x] 4.3 Confirm scene switches release inactive preload/player resources and restored cards create a fresh playback lifecycle without restoring playback time, comments, menus, fullscreen, focus, or swipe state.
- [x] 4.4 Add FeedPage integration tests that switch among Feed scenes and assert `data-active-video-id`, restored interaction state, closed transient UI, and unchanged first-page request counts.

## 5. Navigation and Regression Coverage

- [x] 5.1 Add router/component coverage for direct Feed navigation and browser Back restoration while keeping non-Feed navigation and full page reload outside the retained-state contract.
- [x] 5.2 Add deferred-request tests for late first-page responses, interrupted pagination, rapid multi-scene switching, explicit refresh, and logout/login identity changes.
- [x] 5.3 Run the affected `useFeed`, FeedPage, preloading, player-pool, router, and Following directory test suites and resolve regressions without weakening stale-response or strict typing guarantees.
- [x] 5.4 Run the existing frontend production/type-check build and verify no new dependency, explicit `any`, type suppression, routing library, or browser-persistence schema was introduced.

## 6. Browser Validation and Documentation

- [x] 6.1 Validate direct route switching and browser Back in Windows Chrome at wide, compact, and narrow desktop widths, recording active video IDs and network requests to prove restoration without duplicate first-page loads.
- [x] 6.2 Validate delayed-response isolation, Recommendation context payloads, authentication invalidation, comments-closed restoration, and absence of console errors or horizontal-layout regressions.
- [x] 6.3 Update `docs/modules/feed.md`, `docs/uiux.md`, and `docs/当前问题.md` with scene continuity, invalidation boundaries, Recommendation context isolation, bounded retention, and the resolved issue status.
- [x] 6.4 Run `openspec validate --all --strict` and confirm the change artifacts, affected main-spec expectations, implementation, tests, browser evidence, and documentation agree.

## 7. Per-Scene Refresh Controls

- [x] 7.1 Add a typed Feed refresh request provider that increments only the active scene's request counter and does not encode transient refresh intent in the URL.
- [x] 7.2 Add one separate accessible refresh button beside the active Feed destination without shrinking the primary navigation target in wide or compact desktop layouts; inactive Feed destinations show no refresh icon.
- [x] 7.3 Consume refresh requests inside `useFeed` scene activation so refreshing replaces only the active snapshot from page one while normal navigation still restores.
- [x] 7.4 Add component, hook, browser, documentation, build, and strict OpenSpec coverage for active-only refresh behavior and other-scene snapshot preservation.
