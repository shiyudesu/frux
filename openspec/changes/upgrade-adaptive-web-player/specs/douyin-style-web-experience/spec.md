## MODIFIED Requirements

### Requirement: Immersive desktop Feed stage
Each Feed scene SHALL render one active video stage inside the application shell with a rounded dark player surface, media backdrop, author metadata, vertical action rail, bottom player controls, and truthful loading, buffering, or error state. Timeline, recommendation, following, and hot scenes SHALL retain their existing data and interaction behavior, and adjacent Feed transitions SHALL reuse bounded prepared player slots when available.

#### Scenario: Feed scene uses the redesigned stage
- **WHEN** a Feed scene has an active item on wide desktop
- **THEN** the item renders inside the immersive stage with author, follow state, title, description, like, comment, favorite, share, player controls, and current playback state visible in the expected stage regions

#### Scenario: Feed scenes preserve behavior
- **WHEN** the user switches among timeline, recommendation, following, and hot routes
- **THEN** each route loads its existing scene data and supports existing swipe, wheel, keyboard, pagination, interaction, behavior-event, and QoS behavior

#### Scenario: Prepared adjacent item becomes active
- **WHEN** the user moves to an adjacent item that has a retained prepared player
- **THEN** the Feed reassigns the prepared player slot without displaying the previous item's media or rebuilding unrelated player state

### Requirement: Real media controls
For video media, the Feed stage SHALL expose play/pause, elapsed and duration values, mute state, seekable progress, fullscreen, quality, playback-rate, buffering, retry, and continuous-play controls backed by the active player adapter. Image fallback stages SHALL NOT display false playback progress or enabled video-only controls.

#### Scenario: User pauses and resumes playback
- **WHEN** the active video is playing and the user activates the play control or presses Space outside an editable field
- **THEN** the media pauses, the control state updates, and the same action resumes playback

#### Scenario: User seeks the video
- **WHEN** the user selects a valid position on the progress control
- **THEN** the active video's current time moves to the corresponding position and the elapsed display updates

#### Scenario: User selects quality
- **WHEN** multiple compatible qualities are available and the user selects one
- **THEN** the active player applies or attempts that quality and reflects the effective selection or fallback

#### Scenario: Video is buffering
- **WHEN** the active player lacks enough data to continue expected playback
- **THEN** the stage displays a truthful buffering state until playback resumes or an error is surfaced

#### Scenario: Non-video item renders safely
- **WHEN** the Feed item resolves to an image rather than a playable video
- **THEN** the image remains visible and video-only controls are hidden or disabled
