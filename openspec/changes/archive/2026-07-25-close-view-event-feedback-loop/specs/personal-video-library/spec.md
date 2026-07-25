## MODIFIED Requirements

### Requirement: Durable Watch History Projection
GCFeed SHALL maintain one latest watch-history record per user and video from play, progress, complete, and skip events, including the most recent scene, event type, media position, effective watch duration, completion state, and watch time. Exposure-only events SHALL NOT create watch-history entries.

#### Scenario: Play event updates history
- **WHEN** an authenticated user's play event is stored
- **THEN** the corresponding user-video history record is created or updated in the same persistence transaction

#### Scenario: Progress event advances history
- **WHEN** an authenticated user's deterministically newer progress event is stored
- **THEN** the history record advances its latest media position, effective watch duration, event type, and watch time

#### Scenario: Exposure event does not update history
- **WHEN** an exposed event is stored without a play, progress, complete, or skip event
- **THEN** no watch-history entry is created

#### Scenario: Older watch event commits last
- **WHEN** concurrent or delayed watch events reach the projection out of order
- **THEN** the projection keeps the deterministic newest `(occurred_at, event_id)` state and does not regress position, completion, or latest watch time

#### Scenario: Completion is followed by an older progress event
- **WHEN** a completed history record later receives an older progress or skip event
- **THEN** the completed state and newer position remain unchanged
