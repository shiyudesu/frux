## Context

The authenticated profile keeps separate paginated states for Likes, Favorites, Watch History, and Watch Later, but selecting any grid card stores one `Video` in `selectedWork` and opens `WorkViewer`, a single native `<video>` modal. That path does not use the Feed player, swipe navigation, comments, continuous-play preference, adjacent preloading, or source pagination.

The profile also maintains a Recommend tab backed by a separate Feed request even though the requested product behavior is for the personal page to contain personal content only. Watch Later is durable end to end and supports list/remove in the profile, but the Web does not expose an add action from playback surfaces.

## Goals / Non-Goals

**Goals:**

- Make profile library grids open an immersive full-screen queue at the selected item.
- Reuse the existing player, swipe, comments, playback preferences, and bounded adjacent-resource behavior.
- Preserve the profile route, tab state, pagination, grid scroll position, and keyboard focus when the overlay closes.
- Remove Recommend from the authenticated profile.
- Add a practical idempotent Watch Later entry point.

**Non-Goals:**

- Adding a new library playback URL or making the overlay directly shareable.
- Changing library ordering or privacy rules.
- Merging personal library sources with recommendation ranking.
- Replacing the main Feed page or its recommendation feedback behavior.

## Decisions

### Keep queue playback as a profile-owned full-screen overlay

`ProfilePage` will hold a queue selection containing the source tab and selected video ID instead of one `Video`. A new collection queue viewer will receive the tab's ordered items, cursor state, mutation callbacks, and selected index. It will lock background scrolling and use the existing dialog-focus behavior; closing restores focus to the originating card and leaves the profile DOM and scroll position intact.

A dedicated route was considered but rejected because the chosen behavior prioritizes returning to the exact profile list state rather than refreshable or shareable queue URLs.

### Reuse player primitives through a shared ordered-stage boundary

The queue viewer will use `VideoStage`, `useSwipe`, `useComments`, `FeedDetailsPanel`, `usePlayerPreferences`, and the existing preload/player-pool primitives. Shared stage composition may be extracted from `FeedPage`, but recommendation-specific fetching, feedback, and suppression remain in `FeedPage`.

Queue items will carry a source scene such as `library_likes`, `library_favorites`, `library_history`, or `library_watch_later` for view-event and playback diagnostics without pretending to be recommendation requests.

### Hydrate queue-ready library cards in bounded backend reads

Existing library responses contain media and counts but not author display or complete viewer like/favorite state. The library application boundary will gain narrow batch capabilities for author display and viewer action state, exposed as additive response fields. Infrastructure adapters will batch these reads; the Web will not issue one profile or action request per queue item.

Existing clients remain compatible because the current fields and endpoint paths are preserved.

### Load more before the queue reaches its current end

When the active index approaches the final loaded items and the source reports `has_more`, the viewer will invoke the tab's existing guarded pagination. Appended items retain server order and do not reset the active index. If continuous play ends while a next page is loading, the viewer waits for the next item or reports the true end of the collection.

### Make Watch Later an explicit idempotent playback action

The shared action rail's More menu will expose “稍后再看” when an authenticated viewer can add the current readable video. It will call the existing PUT endpoint, disable duplicate submission, and show success or failure truthfully. In a Watch Later queue, the action becomes removal and immediately updates the source through the existing optimistic removal logic.

The first version does not require every Feed card to include Watch Later state because PUT is idempotent and the primary entry action is add, not a misleading toggle.

### Remove Recommend from profile state and contracts

`ProfilePrimaryTab`, profile tabs, and `useProfileLibrary` will contain only Works, Likes, Favorites, History, and Watch Later. Recommendation remains available through the main recommendation Feed route.

## Risks / Trade-offs

- [Shared Feed composition is currently tightly coupled to recommendation behavior] → Extract only presentation and ordered navigation primitives; keep source-specific fetching and feedback in their existing owners.
- [Library response hydration adds cross-module reads] → Use narrow application interfaces and composition-root adapters with bounded batch queries.
- [Removing an active tab can leave stale local state during hot reload] → Default unknown/removed profile tab values to Works and cover tab normalization in tests.
- [Removing the active Watch Later item can shift the queue] → Select the next item when available, otherwise the previous item, and close with an empty-state message when the queue becomes empty.

## Migration Plan

1. Add additive queue-card fields and backend batch hydration.
2. Introduce the queue viewer and shared stage boundary behind the existing profile grid selection.
3. Add Watch Later playback actions.
4. Remove Recommend profile state and UI after the new queue paths are covered.

Rollback can restore `WorkViewer` and the Recommend tab while leaving additive backend fields harmless.

## Open Questions

None.
