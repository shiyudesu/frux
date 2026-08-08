## ADDED Requirements

### Requirement: Scene-Scoped Feed Snapshots
The Frux Web client SHALL maintain an independent in-memory snapshot for each of the Timeline, Recommendation, Following, and Hot Feed scenes while the Feed page remains mounted. A valid snapshot SHALL preserve the ordered retained cards, active video identity, usable pagination state, Feed request identity, and viewer-action state needed to resume that scene.

#### Scenario: User opens a scene for the first time
- **WHEN** the user opens a Feed scene that has no valid snapshot
- **THEN** the client loads the scene from its first page and makes the returned active item the initial retained position

#### Scenario: User returns to a retained scene
- **WHEN** the user advances to another video, visits a different Feed scene, and returns while the original snapshot remains valid
- **THEN** the client restores the original active video and retained ordering without issuing another first-page request for that scene

#### Scenario: Browser Back returns between Feed scenes
- **WHEN** browser Back activates a Feed route that has a valid snapshot
- **THEN** the same restoration behavior applies and the active video is not reset to the first retained card

### Requirement: Scene Request Isolation
Feed loading and pagination results SHALL commit only to the scene and activation generation that issued them. Inactive or obsolete responses MUST NOT replace the visible scene or corrupt a retained snapshot.

#### Scenario: First-page response arrives after scene switch
- **WHEN** a first-page request remains in flight while the user activates another Feed scene
- **THEN** its later response does not change the active scene, active video, or destination snapshot

#### Scenario: Pagination is interrupted by scene switch
- **WHEN** a load-more request is interrupted after the scene already has a committed snapshot
- **THEN** returning restores the committed cards and cursor state and permits pagination to be retried safely

#### Scenario: Scene has no committed response
- **WHEN** the user leaves a scene before its first page commits and later returns
- **THEN** the client starts a new valid first-page request instead of restoring an incomplete loading state

### Requirement: Bounded Snapshot Retention
Inactive Feed snapshots SHALL retain card and request metadata only, SHALL release active media and transient UI resources, and SHALL enforce a fixed per-scene card bound. The client MUST reload rather than restore a compacted snapshot when it cannot preserve both the active video and valid forward-pagination continuity.

#### Scenario: Inactive scene exceeds the retention bound
- **WHEN** a scene becomes inactive with more cards than the configured retention limit
- **THEN** the client retains a safe contiguous bounded snapshot containing the active video and pagination tail or marks the snapshot unusable

#### Scenario: Scene snapshot is unusable
- **WHEN** the destination snapshot is malformed, lacks its active video, or cannot preserve its cursor continuity after compaction
- **THEN** the client discards that snapshot and performs a clean first-page load

#### Scenario: Scene releases playback resources
- **WHEN** the user leaves a Feed scene
- **THEN** player adapters, prepared media, buffering state, gestures, menus, and open comments are not kept alive solely for snapshot restoration

### Requirement: Snapshot Invalidation Boundaries
The Web client SHALL invalidate Feed snapshots when authenticated identity changes and SHALL replace only the current scene snapshot when the user intentionally refreshes or retries that scene. Snapshots SHALL NOT persist across a full page reload or Feed-page unmount.

#### Scenario: Authenticated identity changes
- **WHEN** the active token or authenticated user changes through logout, login, or account replacement
- **THEN** snapshots created for the previous identity are cleared before another scene can restore them

#### Scenario: User intentionally refreshes one scene
- **WHEN** the user activates the current scene's refresh or retry action
- **THEN** that scene loads a new first page from index zero without clearing valid snapshots for other Feed scenes

#### Scenario: Browser reloads the application
- **WHEN** the browser performs a full page reload
- **THEN** Feed restoration starts without using a snapshot from the previous page lifetime

### Requirement: Cross-Scene Mutation Coherence
Successful video mutations SHALL update every retained scene snapshot containing the affected video. Recommendation feedback removal and suppression SHALL remain exclusive to the Recommendation snapshot.

#### Scenario: Same video exists in two scenes
- **WHEN** a like, favorite, or comment-count mutation succeeds for a video retained in multiple scenes
- **THEN** returning to any retained scene shows the updated viewer state and count without requiring a first-page reload

#### Scenario: Recommendation feedback removes a video
- **WHEN** accepted recommendation feedback removes or suppresses a video or author
- **THEN** only the Recommendation snapshot applies that removal or suppression and unrelated Feed scenes retain their own ordered cards

### Requirement: Transient Feed UI Is Not Restored
Scene restoration SHALL restore Feed data position but SHALL NOT restore media playback time, an in-progress swipe, open comments, open player menus, fullscreen state, or keyboard focus from the inactive scene.

#### Scenario: User leaves with comments open
- **WHEN** the user changes Feed scenes while comments are open
- **THEN** returning restores the active video with comments closed and allows comments to be opened normally

#### Scenario: Restored video becomes active
- **WHEN** a retained scene is restored
- **THEN** the active card creates a fresh visible playback lifecycle while retaining its original Feed request attribution

### Requirement: Explicit Active-Scene Refresh Control
The left navigation SHALL expose a separately focusable and accessibly named refresh control only inside the highlighted row of the currently active Timeline, Recommendation, Following, or Hot destination. The refresh control SHALL use a single-direction refresh glyph without a separate raised tile presentation. Activating the control SHALL intentionally replace only that scene from its first page while preserving other valid scene snapshots.

#### Scenario: User refreshes the active scene
- **WHEN** the user activates the refresh control beside the currently active Feed destination
- **THEN** that scene discards its retained ordering, closes transient Feed UI, loads a new first page, and starts at its first returned card

#### Scenario: User uses the main destination control
- **WHEN** the user activates the Feed destination itself rather than its refresh control
- **THEN** normal scene restoration remains in effect and no intentional refresh is requested

#### Scenario: Inactive destinations render
- **WHEN** the left navigation displays Feed destinations other than the active scene
- **THEN** those inactive destinations do not display refresh controls

#### Scenario: Active scene is refreshed
- **WHEN** the current Feed scene is intentionally refreshed
- **THEN** valid snapshots for the other Feed scenes remain available
