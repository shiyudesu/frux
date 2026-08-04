## ADDED Requirements

### Requirement: Full-screen collection queue
The Web SHALL open a full-screen, dismissible playback overlay when a user selects a readable video from Likes, Favorites, Watch History, or Watch Later. The overlay SHALL start on the selected video and SHALL preserve the underlying profile tab, loaded pages, grid scroll position, and origin focus for restoration on close.

#### Scenario: User opens a library video
- **WHEN** the user selects a video card in a supported personal-library tab
- **THEN** the full-screen player opens on that exact video with the surrounding library items retained in server order

#### Scenario: User closes collection playback
- **WHEN** the user dismisses the full-screen player
- **THEN** the profile returns to the same tab and scroll position and focus returns to the originating video card when it still exists

### Requirement: Ordered queue navigation and pagination
Collection playback SHALL support adjacent swipe, wheel, keyboard, and continuous-play navigation using the loaded library order. It SHALL request the next library page before the active item exhausts the loaded queue and SHALL append results without changing the current item.

#### Scenario: User advances through loaded items
- **WHEN** the user navigates to the next or previous queue item
- **THEN** the adjacent item becomes active without reopening the overlay or resetting playback preferences

#### Scenario: User approaches the loaded end
- **WHEN** the active index enters the configured end threshold and the library source has another cursor
- **THEN** the next page is requested once and its unique items are appended in server order

#### Scenario: Continuous play reaches the next item
- **WHEN** continuous play is enabled and the active video ends while a next queue item is available
- **THEN** the player advances to that item instead of looping the completed video

### Requirement: Feed-equivalent playback surface
The collection queue SHALL reuse GCFeed's typed player adapters, truthful media controls, player preferences, author metadata, interaction controls, comments, and bounded adjacent-resource lifecycle. It MUST NOT use a separate raw-video implementation that omits the active player state.

#### Scenario: Queue item has adaptive sources
- **WHEN** a collection item exposes ready playback sources
- **THEN** the same source selection, fallback, buffering, quality, speed, seek, mute, fullscreen, and retry behavior as the Feed is available

#### Scenario: User opens comments
- **WHEN** the user activates comments for the current collection item
- **THEN** the existing threaded comment surface opens for that video without changing the queue index

#### Scenario: Queue item changes
- **WHEN** navigation makes another collection item active
- **THEN** media, author, counts, viewer actions, comments, and diagnostics all bind to the new video and stale state from the prior item is not displayed

### Requirement: Accessible overlay lifecycle
The collection player SHALL trap dialog focus where appropriate, expose an accessible close action, prevent background interaction while open, and honor reduced-motion behavior.

#### Scenario: Keyboard user opens and closes the queue
- **WHEN** a keyboard user opens collection playback and activates Escape or the close control
- **THEN** the overlay closes and focus returns to the originating card without moving the profile scroll position
