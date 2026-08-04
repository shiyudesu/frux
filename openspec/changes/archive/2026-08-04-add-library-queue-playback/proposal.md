## Why

Profile library grids currently open a single-video modal, so liked, favorite, history, and Watch Later collections cannot be browsed as continuous short-video queues. The profile also exposes an unwanted recommendation tab, while Watch Later has durable APIs and a list but no practical add entry point.

## What Changes

- Remove the Recommend tab and its profile-specific recommendation request path from the authenticated profile.
- Replace single-video library viewing with a full-screen, dismissible queue player that starts at the selected item, supports adjacent navigation and continuous play, loads more from the active library tab, and returns to the original grid position when closed.
- Reuse the existing player, swipe, comments, playback preferences, and interaction patterns instead of building a second media stack.
- Add a truthful “稍后再看” action to video playback surfaces using the existing idempotent Watch Later API, with removal available from the Watch Later library.
- Preserve independent pagination and mutation state for Likes, Favorites, Watch History, and Watch Later.

## Capabilities

### New Capabilities

- `collection-queue-playback`: Full-screen ordered playback for videos selected from a profile library collection.

### Modified Capabilities

- `personal-video-library`: Add Web entry points and queue behavior for library videos and Watch Later.
- `profile-dashboard`: Remove Recommend and make remaining personal-library tabs open a continuous queue.
- `douyin-style-web-experience`: Replace the single-work profile viewer behavior with an immersive, dismissible collection player.

## Impact

- Affects profile types, `ProfilePage`, `useProfileLibrary`, `WorkViewer` or its replacement, shared player/swipe/comment components, action-rail controls, and profile/player styling.
- Uses existing library and Watch Later endpoints; additive response fields may be introduced only if required to render author or viewer state without per-item request growth.
- Requires focused frontend behavior tests and synchronized profile/library/playback documentation.
