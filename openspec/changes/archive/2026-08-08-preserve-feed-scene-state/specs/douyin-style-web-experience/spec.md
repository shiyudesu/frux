## MODIFIED Requirements

### Requirement: Immersive desktop Feed stage
Each Feed scene SHALL render one active video stage inside the application shell with a rounded dark player surface, media backdrop, author metadata, vertical action rail, bottom player controls, and truthful loading, buffering, or error state. Timeline, recommendation, following, and hot scenes SHALL retain their existing data and interaction behavior, adjacent Feed transitions SHALL reuse bounded prepared player slots when available, and switching among Feed routes SHALL restore each valid retained scene to its previous active video instead of unconditionally restarting from its first card.

#### Scenario: Feed scene uses the redesigned stage
- **WHEN** a Feed scene has an active item on wide desktop
- **THEN** the item renders inside the immersive stage with author, follow state, title, description, like, comment, favorite, share, player controls, and current playback state visible in the expected stage regions

#### Scenario: Feed scenes preserve behavior
- **WHEN** the user switches among timeline, recommendation, following, and hot routes
- **THEN** each route supports its existing swipe, wheel, keyboard, pagination, interaction, behavior-event, and QoS behavior while maintaining independent retained Feed data

#### Scenario: User returns to a previous Feed route
- **WHEN** the user advances within one Feed scene, visits another Feed route, and returns during the same mounted Feed session
- **THEN** the previous scene restores its retained active video, ordering, request identity, and forward-pagination state without an unnecessary first-page request

#### Scenario: Prepared adjacent item becomes active
- **WHEN** the user moves to an adjacent item that has a retained prepared player
- **THEN** the Feed reassigns the prepared player slot without displaying the previous item's media or rebuilding unrelated player state

#### Scenario: Restored scene rebuilds transient player state
- **WHEN** a previously inactive Feed scene is restored
- **THEN** its active card uses a fresh visible player lifecycle and does not restore stale comments, gestures, menus, buffering, fullscreen, or playback-time state

#### Scenario: User explicitly refreshes the active Feed destination
- **WHEN** the user activates the refresh control beside the active Feed destination in the left navigation
- **THEN** its first page replaces its retained snapshot, other Feed destinations keep their retained positions, and inactive destinations do not show refresh icons
