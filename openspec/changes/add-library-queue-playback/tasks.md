## 1. Queue-Ready Library Data

- [x] 1.1 Extend library domain/application response models with additive author display and viewer like/favorite state needed by the shared playback surface.
- [x] 1.2 Add narrow batch profile/action readers and composition-root adapters so library pages hydrate queue cards without per-item requests.
- [x] 1.3 Update library HTTP DTOs and backend tests for additive fields, bounded reads, ordering, privacy, and compatibility.

## 2. Profile Library Scope

- [x] 2.1 Remove Recommend from `ProfilePrimaryTab`, profile tabs, `useProfileLibrary`, recommendation request state, and tab normalization.
- [x] 2.2 Preserve independent Likes, Favorites, Watch History, and Watch Later pagination and mutation state after the tab removal.

## 3. Full-Screen Collection Playback

- [x] 3.1 Introduce a typed collection queue item/source model that maps library responses into the existing `FeedVideo` playback contract and source scenes.
- [x] 3.2 Build the full-screen collection queue viewer by reusing `VideoStage`, swipe navigation, player preferences, comments, details, and bounded adjacent-resource lifecycle.
- [x] 3.3 Integrate profile grid selection by source tab and selected video ID, restore origin focus and grid scroll on close, and handle source-item removal safely.
- [x] 3.4 Trigger guarded source pagination near the loaded queue end and support continuous advance across appended pages and truthful collection-end states.
- [x] 3.5 Add responsive, focus-management, background-locking, and reduced-motion styles for the full-screen overlay.

## 4. Watch Later Playback Actions

- [x] 4.1 Add an authenticated “稍后再看” action to supported playback More menus using the existing idempotent PUT endpoint with busy, success, and error states.
- [x] 4.2 Add Watch Later queue removal behavior that updates only the removed item and selects the next, previous, or empty state correctly.

## 5. Tests, Documentation, and Validation

- [x] 5.1 Add frontend tests for selected-item startup, adjacent navigation, load-more thresholds, continuous play, close restoration, and stale source responses.
- [x] 5.2 Add tests for profile tab removal and Watch Later add/remove concurrency and failure behavior.
- [x] 5.3 Update profile, library, playback, UI/UX, and current-issue documentation to match the delivered behavior.
- [x] 5.4 Run targeted backend library tests, targeted frontend queue/profile tests, the Go build, and the frontend production/type-check build.
