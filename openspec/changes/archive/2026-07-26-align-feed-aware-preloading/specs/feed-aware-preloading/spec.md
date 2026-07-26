## ADDED Requirements

### Requirement: Feed-Ordered Preload Window
The Web Feed SHALL derive preload candidates from the exact ordered items of the active scene and request generation.

#### Scenario: Recommendation item is active
- **WHEN** a recommendation item is active and later ordered items are already loaded
- **THEN** the preloader selects those later items in Feed order rather than querying global publish order

#### Scenario: Feed approaches the loaded-page boundary
- **WHEN** the active index plus preload window reaches the end of loaded items and the Feed has more data
- **THEN** the existing Feed pagination path loads the next page before those appended items become preload candidates

### Requirement: Bounded Prepared Media Resources
The preloader SHALL retain only the active, bounded adjacent, and configured forward resources and SHALL release media state outside that window.

#### Scenario: Candidate becomes active
- **WHEN** a prepared next candidate becomes the current Feed item
- **THEN** the player reuses or adopts its prepared media state instead of starting an unrelated disposable probe

#### Scenario: Candidate leaves the retained window
- **WHEN** an item is no longer active, previous, or inside the forward preload budget
- **THEN** its listeners, source, object URL, timers, and retained media resources are released

### Requirement: Network-Aware Preload Policy
The preloader SHALL apply `preload_count`, `buffer_ms`, effective network class, save-data state, and bounded device policy to decide how much media to prepare.

#### Scenario: Save-data is enabled
- **WHEN** the browser reports save-data or offline state
- **THEN** the client preloads covers or metadata only and does not intentionally preload media bytes

#### Scenario: Immediate next item reaches buffer target
- **WHEN** the next candidate exposes a playable buffered range at least as large as the effective `buffer_ms` target
- **THEN** the candidate is marked ready for transition

#### Scenario: Browser does not expose useful buffered ranges
- **WHEN** a compatible browser can play the source but does not expose a reliable buffered target
- **THEN** the preloader uses a documented metadata or `canplay` fallback

### Requirement: Preload Generation Cancellation
Preload work SHALL be scoped to scene, Feed request generation, authentication generation, video ID, and source revision.

#### Scenario: User switches Feed scenes
- **WHEN** the active scene or Feed request generation changes
- **THEN** obsolete pending preload work is aborted and cannot populate the new scene's resource registry

#### Scenario: Source revision changes
- **WHEN** a candidate receives a new playback source or signed-source revision
- **THEN** the stale resource is released and the new source is prepared under a distinct key

### Requirement: Preload Failure Isolation
Preload failure SHALL NOT fail Feed loading or prevent normal visible playback.

#### Scenario: Next media preload fails
- **WHEN** the next candidate cannot be preloaded
- **THEN** the Feed remains usable and selecting that candidate retries through the visible-player path

#### Scenario: Repeated preload failure occurs
- **WHEN** the same candidate fails repeatedly within the retry cooldown
- **THEN** the preloader suppresses immediate repeated work while preserving later visible playback
