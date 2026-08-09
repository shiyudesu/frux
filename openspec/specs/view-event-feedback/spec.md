# view-event-feedback Specification

## Purpose

Defines reliable, ordered playback lifecycle feedback from the Web client through durable recommendation event delivery.

## Requirements

### Requirement: Web Playback Lifecycle Events
The authenticated Web Feed SHALL emit a bounded playback lifecycle for each active video using `exposed`, `play`, `progress`, `complete`, and `skip` events with a playback session ID, event ID, sequence, occurrence time, media position, and effective watch duration.

#### Scenario: Active video begins playback
- **WHEN** a Feed video becomes active and its media first enters the playing state
- **THEN** the client emits one exposed event and one play event for that playback session

#### Scenario: Active video accumulates watch time
- **WHEN** foreground effective playback or media position crosses the configured progress interval
- **THEN** the client emits a progress event without emitting an event for every browser `timeupdate`

#### Scenario: User leaves before completion
- **WHEN** the active video is replaced, the scene changes, or the page exits before the completion threshold
- **THEN** the client emits a skip event with the latest bounded position and watch duration

#### Scenario: Video reaches completion
- **WHEN** playback ends or reaches the configured completion threshold
- **THEN** the client emits exactly one complete event for that playback session

### Requirement: Idempotent View Event Acceptance
The view-event API SHALL accept additive client event identity fields and SHALL deduplicate authenticated events by user ID and event ID.

#### Scenario: Client retries the same event
- **WHEN** the same authenticated user submits the same normalized event with the same event ID
- **THEN** the API returns the previously accepted result without inserting another event or reapplying projections

#### Scenario: Event ID is reused for another payload
- **WHEN** the same authenticated user submits a different normalized payload with an existing event ID
- **THEN** the API returns a conflict and leaves the original event unchanged

#### Scenario: Legacy client omits event identity
- **WHEN** an existing client submits the current view-event request without additive identity fields
- **THEN** the API continues to validate and store the request using legacy behavior

### Requirement: Monotonic Playback Event Ordering
The server SHALL validate bounded client occurrence times and playback sequences and SHALL expose deterministic ordering metadata to projections and consumers.

#### Scenario: Delayed older event arrives
- **WHEN** an event with an older accepted occurrence tuple arrives after a newer event
- **THEN** the raw fact is retained but no latest-state projection is regressed

#### Scenario: Client occurrence time is outside the accepted window
- **WHEN** an event occurrence time exceeds the configured clock-skew boundary
- **THEN** the server uses or rejects it according to the documented fallback without trusting the unbounded timestamp

### Requirement: Unload-Safe Event Delivery
The Web client SHALL flush terminal lifecycle events before player teardown and SHALL use bounded keepalive or beacon delivery during page shutdown.

#### Scenario: Page becomes hidden
- **WHEN** a playing page receives `visibilitychange` or `pagehide`
- **THEN** the client flushes the latest progress or terminal event without blocking navigation

#### Scenario: Shutdown delivery is retried
- **WHEN** the client cannot confirm a terminal event delivery and later retries it
- **THEN** the stable event ID prevents duplicate behavior facts

### Requirement: Reliable Recommendation Event Publication
Accepted playback behavior events SHALL be published from the transactional PostgreSQL outbox to a registered Kafka event stream through retryable, idempotent delivery that does not block the user-facing playback request on Kafka availability.

#### Scenario: Kafka is temporarily unavailable
- **WHEN** a view event is committed while Kafka publication cannot be acknowledged
- **THEN** the event remains pending in the PostgreSQL outbox for retry and the accepted HTTP result is not lost

#### Scenario: Kafka consumer receives a duplicate delivery
- **WHEN** the same playback event is delivered more than once because an offset was not committed or a consumer group replays it
- **THEN** the recommendation behavior fact and downstream projection handoff apply the event at most once by event ID

#### Scenario: Kafka delivery is durably handed off
- **WHEN** the recommendation consumer commits the raw behavior fact and its leased profile/outcome outbox
- **THEN** it may commit the Kafka offset without waiting for embedding generation or later projection work

#### Scenario: Consumer receives a duplicate delivery
- **WHEN** the same playback event is delivered more than once
- **THEN** the recommendation projection applies the event at most once by event ID
