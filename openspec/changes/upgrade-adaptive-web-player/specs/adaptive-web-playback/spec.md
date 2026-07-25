## ADDED Requirements

### Requirement: Typed Player Adapter Fallback
The Web Feed SHALL play media through a typed adapter boundary supporting native MP4 and adaptive manifests with graceful fallback.

#### Scenario: Adaptive source is supported
- **WHEN** the item has a ready adaptive source and the browser supports its required APIs and codecs
- **THEN** the adaptive adapter loads the source and exposes normalized player state

#### Scenario: Adaptive initialization fails
- **WHEN** the adaptive adapter cannot initialize or reaches a recoverable source error
- **THEN** the controller falls back to the best compatible MP4 source without changing the active Feed item

#### Scenario: Legacy item has one media URL
- **WHEN** the Feed item has no additive playback sources
- **THEN** the native adapter plays the legacy `media_url`

### Requirement: Bounded Feed Player Pool
The Feed SHALL retain at most a bounded previous, current, and next player set keyed by Feed generation, video ID, and source revision.

#### Scenario: User advances one item
- **WHEN** the next prepared item becomes active
- **THEN** its player slot becomes current and the old current slot becomes previous without rebuilding all media state

#### Scenario: Item leaves the pool
- **WHEN** an item is no longer previous, current, or next
- **THEN** its adapter, listeners, media source, timers, and object URLs are destroyed

### Requirement: Capability-Aware Source Selection
The player SHALL select sources using server policy, browser codec and MediaSource support, network conditions, save-data state, viewport needs, and validated user quality preference.

#### Scenario: Auto quality on constrained network
- **WHEN** auto quality is active and the network is constrained
- **THEN** the player starts from a compatible lower rendition within server policy

#### Scenario: User locks a quality
- **WHEN** the user selects an available manual quality
- **THEN** the player preserves that preference and attempts the selected rendition until it becomes unavailable or incompatible

#### Scenario: Requested codec is unsupported
- **WHEN** a source codec is not supported by the browser
- **THEN** the source is skipped rather than attempted as the only playback path

### Requirement: Buffer-Based Transition Readiness
The player SHALL use the effective `buffer_ms` target to represent next-item readiness and active buffering state.

#### Scenario: Prepared next item meets target
- **WHEN** the next item has playable buffered media at least equal to the effective target
- **THEN** the transition treats the item as prepared

#### Scenario: Swipe commits before target
- **WHEN** the user commits a transition before the next item is prepared
- **THEN** navigation succeeds and the stage shows a truthful buffering state until playback can begin

### Requirement: Extended Playback Controls
The Feed player SHALL expose truthful quality, playback-rate, retry, buffering, and continuous-play controls in addition to existing play, seek, mute, time, and fullscreen controls.

#### Scenario: User changes playback rate
- **WHEN** the user selects a supported playback rate
- **THEN** the active adapter applies it and the control reflects the effective rate

#### Scenario: Recoverable playback error occurs
- **WHEN** playback fails with a recoverable source or network error
- **THEN** the UI exposes retry or fallback state rather than reporting normal playback

#### Scenario: Continuous play is enabled
- **WHEN** the active item completes and a next item exists
- **THEN** the Feed advances to the next item instead of looping the completed item

### Requirement: Accessible Player State
All added player controls and states SHALL expose accessible names, visible focus, keyboard operation, and reduced-motion-compatible transitions.

#### Scenario: Keyboard user opens quality controls
- **WHEN** a keyboard user focuses and activates the quality control
- **THEN** available choices are reachable in logical order and the selected quality is announced

#### Scenario: Buffering state changes
- **WHEN** playback enters or exits a sustained buffering state
- **THEN** the status is represented without trapping focus or relying only on animation
