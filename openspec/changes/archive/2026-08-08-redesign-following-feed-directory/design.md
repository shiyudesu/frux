## Context

Frux currently renders `timeline`, `recommend`, `following`, and `hot` through the same `FeedPage` composition. The Following scene is data-correct but visually indistinguishable from other scenes and exposes no persistent view of the user's active relationships.

Authenticated Chrome DevTools research of Douyin's desktop Following page established the relevant layout facts without retaining account data:

| Viewport | Main navigation | People directory | Visible media width |
| --- | ---: | ---: | ---: |
| 1440px | 160px | 208px | about 1004px |
| 1200px | 72px | 208px | about 860px |
| 1024px | 72px | 208px | about 684px |
| 800px | 72px | 208px | about 460px |

The directory remains 208px wide at every measured desktop density. Its header and search occupy the top 124px below the 56px global header; the people list scrolls independently. Rows are approximately 48px high with 32px avatars. The video Feed continues as an independent vertical stage. User rows behave as profile navigation rather than an author filter for the adjacent Feed.

Frux already has an authenticated cursor-paginated following-list API, typed public-profile routes, relation cards, compact desktop density, and a complete Following Feed. It does not have live-stream state or durable per-author unread-work counts.

## Goals / Non-Goals

**Goals:**

- Let users browse followed people while continuing to consume the ordered Following Feed.
- Preserve the current Following Feed's source-of-truth filtering, cursor, preloading, interactions, and telemetry.
- Provide complete following-list search rather than a misleading current-page-only filter.
- Keep directory wheel, pointer, focus, and pagination behavior independent from Feed navigation.
- Preserve usable video width through collapse behavior and comment-panel coordination.
- Reuse existing profile navigation, identity fallbacks, API error handling, and desktop density tokens.

**Non-Goals:**

- Adding live streaming, live presence, unread-work counters, creator activity badges, or fake placeholder values.
- Filtering the right-hand Feed by the directory row a user selects.
- Replacing the existing profile relation modal.
- Changing recommendation policy, Following Feed ordering, fanout, media playback, or mobile navigation.
- Copying Douyin assets, class names, user data, or proprietary source code.

## Decisions

### 1. Add a Following-only third column

`FeedPage` will conditionally render a `FollowingFeedDirectory` before `feed-main` when `feedScene === "following"`. The Feed grid will support:

```text
normal scene:     [ stage ][ details ]
following scene:  [ 208px directory ][ stage ][ details ]
```

The directory width will be a shared token with a default value of 208px. It remains 208px under the existing 160px/72px main-navigation transition. The video stage keeps `minmax(0, 1fr)` so all remaining width belongs to media and interaction controls.

**Alternative considered:** Put a horizontal avatar strip above the video. Rejected because it consumes stage height, displays fewer relationships, and does not match the observed desktop interaction model.

### 2. Keep the directory collapsible but open by default

The Following scene opens with the directory visible so issue 38's core requirement is immediately satisfied. A labeled collapse control removes the 208px grid column; a compact reopen control remains attached to the stage edge.

Collapse state is local UI state and does not alter the Follow relationship or Feed request. It may persist while the mounted `FeedPage` changes index, but the first entry into a new application session uses the visible default.

### 3. Search the complete relationship set through the API

`GET /api/users/me/following` will accept an optional normalized `q` parameter:

- Empty `q` keeps the current recent-follow ordering.
- Nonempty `q` matches account or nickname case-insensitively.
- Query length and normalization follow existing search input limits.
- The cursor payload gains a version, list kind, and normalized query binding. Legacy unversioned cursors remain valid only for an empty query.
- Repository filtering remains parameterized and still requires `status=active` and a normal target account.

The relation response will add the account field required for truthful identity search and display. This is additive and does not remove existing fields.

**Alternative considered:** Load all relationship pages and filter in the browser. Rejected because it is incomplete beyond the existing bounded page loop, delays first render for large accounts, and makes cursor state difficult to reason about.

### 4. Use a dedicated frontend hook with request generations

A `useFollowingDirectory` hook will own:

- Controlled query and debounced normalized query.
- Items, cursor, `hasMore`, loading, loading-more, empty, and error states.
- Request generations so old query or pagination responses cannot append after a query change.
- ID-based deduplication.
- Removal or update of a row after a successful unfollow in the active Feed.

The first page loads only in the authenticated Following scene. Additional pages load near the directory scroll boundary, with a reachable explicit retry or load-more control as an accessibility fallback.

### 5. Directory rows navigate to profiles

Each row shows the real avatar fallback, nickname, optional account, and available bio. Activating a row calls the existing typed public-profile navigation. It does not change the current Feed index, issue a new author-specific Feed request, or imply unread state.

### 6. Isolate directory input from Feed navigation

The directory is a sibling of `feed-main`, not a descendant of the element carrying Feed pointer and wheel handlers. Its scroll container consumes wheel events locally. Search inputs and buttons use normal editable/control semantics, so Feed shortcuts remain suppressed by existing editable-target guards.

This avoids `stopPropagation` patches spread through list children and makes the interaction boundary visible in the DOM.

### 7. Coordinate comments with available stage width

At widths below 1280px, comments already use an overlay drawer and do not consume a grid column.

At 1280px through 1439px, opening push-style comments while the Following directory is visible would leave less stage width than the current desktop design expects. CSS will temporarily collapse the directory column while comments are open in this range, then restore it when comments close. At 1440px and above, directory, stage, and 346px comments panel may coexist.

The user's explicit collapsed state remains separate from this temporary layout rule.

### 8. Do not invent unsupported Douyin sections

The directory will include a title, collapse control, functional search, and "Following" list. It will not render "Live now", live rings, unread-work badges, or counts derived from guesses. Those require separate domain facts and specifications.

### 9. Verify geometry and behavior at the established matrix

Browser verification will cover 1440px, 1200px, 1024px, and 800px with:

- Directory open and collapsed.
- Search success, empty, error, stale-response rejection, and pagination.
- Directory wheel isolation while the Feed item remains unchanged.
- Profile navigation.
- Comments open and closed.
- No horizontal document overflow or clipped Feed controls.

## Risks / Trade-offs

- **[Risk] Two paginated resources on one screen can race** -> Keep Following Feed and directory cursors in separate hooks and bind every directory response to query generation.
- **[Risk] The 208px directory leaves a narrow stage at 800px** -> Keep collapse available, retain existing narrow Feed density, and validate controls at 800px.
- **[Risk] Comment opening can over-compress wide-but-not-large windows** -> Temporarily remove the directory grid column from 1280px through 1439px while push comments are open.
- **[Risk] Search cursor changes break existing callers** -> Make `q` optional and accept legacy cursors only for the unchanged empty-query path.
- **[Risk] Scroll events switch videos accidentally** -> Keep the directory outside `feed-main`, which owns wheel and pointer Feed navigation.
- **[Risk] Follow state changes leave stale directory rows** -> Apply successful relation mutations to both the follow map and directory state.
- **[Trade-off] Frux will not reproduce Douyin's live and unread sections** -> Truthful omission is preferred over cosmetic parity without product facts.

## Migration Plan

1. Extend relation domain/application/infrastructure/HTTP layers with query-bound following-list search and additive account output.
2. Update typed Web relation APIs and add the directory hook and component.
3. Integrate the conditional 208px grid column into `FeedPage`.
4. Add collapse, responsive comments coordination, independent scrolling, and accessibility styles.
5. Add backend API-flow, hook/component, Feed regression, and browser geometry tests.
6. Update relation, Feed, UI/UX, and issue documentation.

No database migration or rollout flag is required. Rollback consists of removing the optional query behavior and Following-only directory while preserving the existing Following Feed endpoint and scene.

## Open Questions

- The exact debounce interval and row secondary-text truncation can be tuned during browser verification without changing the requirements.
- Persisting the collapsed preference across browser sessions is deferred unless usability testing shows a strong need.
