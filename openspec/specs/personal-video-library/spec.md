# personal-video-library Specification

## Purpose

Defines authenticated liked, favorite, watch-history, and Watch Later libraries, including privacy, durability, ordering, and bounded pagination.

## Requirements

### Requirement: Liked Video Library
GCFeed SHALL provide an authenticated, cursor-paginated list of the current user's active LIKE actions, ordered by action update time descending and video ID descending.

#### Scenario: User lists liked videos
- **WHEN** an authenticated user requests liked videos with a valid cursor and limit
- **THEN** the API returns readable video cards in action order with `items`, `next_cursor`, and `has_more`

#### Scenario: Liked video becomes unreadable
- **WHEN** a liked video is deleted, down, or private to another user
- **THEN** it is omitted from the returned library without exposing its media or metadata

### Requirement: Favorite Video Library
GCFeed SHALL provide an authenticated, cursor-paginated list of the current user's active FAVORITE actions, ordered by action update time descending and video ID descending.

#### Scenario: User lists favorite videos
- **WHEN** an authenticated user requests favorite videos
- **THEN** the API returns the user's readable active favorites using stable cursor pagination

#### Scenario: Canceled favorite is absent
- **WHEN** a user has canceled a favorite action
- **THEN** that video is not returned by the favorite library

### Requirement: Public Liked Videos
GCFeed SHALL expose a public liked-video list only when the target user's profile setting explicitly permits it. Favorite videos SHALL remain owner-only.

#### Scenario: Public liked list is permitted
- **WHEN** a visitor requests liked videos for a user whose liked visibility is public
- **THEN** the API returns only publicly readable liked videos

#### Scenario: Public liked list is private
- **WHEN** a visitor requests liked videos for a user whose liked visibility is private
- **THEN** the API returns the configured privacy response without any liked-video items

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

### Requirement: Watch History Listing and Deletion
GCFeed SHALL allow an authenticated user to list watch history by `last_watched_at DESC, video_id DESC`, delete one history item, or clear the history projection without deleting raw view events.

#### Scenario: User lists watch history
- **WHEN** an authenticated user requests watch history
- **THEN** the API returns readable video cards with last-watched metadata using stable cursor pagination

#### Scenario: User clears history
- **WHEN** the user clears watch history
- **THEN** history projection rows are removed while raw view-event and recommendation exposure records remain intact

#### Scenario: History page resolves after clear
- **WHEN** a watch-history page request was active before clear and resolves afterward
- **THEN** the Web rejects the stale page and cleared entries do not reappear

#### Scenario: Startup migration reruns after history deletion
- **WHEN** raw events were backfilled once, the user deletes one history item or clears all history, and API or Worker startup migration runs again
- **THEN** the deleted projection remains absent because the raw-event backfill is durably marked complete

#### Scenario: Unreadable history candidates lead a page
- **WHEN** newer history candidates are unreadable but older readable candidates exist
- **THEN** the service replenishes for at most the configured round bound and returns a stable next cursor with `has_more=true` when older candidates remain

### Requirement: Watch Later State
GCFeed SHALL allow an authenticated user to idempotently add or remove a readable video from Watch Later and list active Watch Later entries using stable cursor pagination.

#### Scenario: User adds video to Watch Later
- **WHEN** the user sends PUT for a readable video not currently active in Watch Later
- **THEN** an active Watch Later fact is stored and returned

#### Scenario: User repeats Watch Later mutation
- **WHEN** the same active state is requested repeatedly
- **THEN** the API returns the current state without creating duplicate active facts

#### Scenario: User lists Watch Later
- **WHEN** the user requests Watch Later
- **THEN** active readable videos are returned by `updated_at DESC, video_id DESC`

#### Scenario: Unreadable Watch Later candidates lead a page
- **WHEN** newer active Watch Later facts reference unreadable videos but older readable facts exist
- **THEN** the service replenishes for at most the configured round bound and returns a stable next cursor with `has_more=true` when older candidates remain

### Requirement: Personal Library Authentication
Liked videos, favorite videos, watch history, and Watch Later owner endpoints SHALL require authentication and SHALL derive the user ID only from the authenticated request context.

#### Scenario: Anonymous request
- **WHEN** an unauthenticated caller requests an owner personal-library endpoint
- **THEN** the API returns 401 and no library facts
