## MODIFIED Requirements

### Requirement: Content Statistics
Frux SHALL maintain per-user public-work, private-work, received-like, and collection counts as persistent aggregate statistics and SHALL expose non-negative values through profile responses.

#### Scenario: Visibility change updates counts
- **WHEN** a video changes between public and private visibility
- **THEN** public and private work counts are adjusted in the same transaction without becoming negative

#### Scenario: Public work lifecycle changes
- **WHEN** a public video becomes offline, returns to published, or is deleted
- **THEN** `public_work_count` counts it exactly when it is both published and public

#### Scenario: Reconciliation overlaps an online delta
- **WHEN** startup reconciliation runs while another transaction commits a newer content-stat delta
- **THEN** reconciliation repairs the prior discrepancy without overwriting the newer delta

#### Scenario: Like state updates received likes
- **WHEN** the durable like count for a user's video changes
- **THEN** the author's received-like aggregate is adjusted consistently with the durable interaction result

#### Scenario: Accepted interaction is consumed after privacy change
- **WHEN** a new interaction is accepted while a video is published and public, and the video becomes private before its asynchronous event is consumed
- **THEN** the event is durably persisted exactly once without making the private video publicly readable

#### Scenario: New interaction targets private video
- **WHEN** a user submits a new synchronous interaction request after the video becomes private
- **THEN** the request is rejected as not found and no event is accepted

#### Scenario: Accepted interaction event is redelivered
- **WHEN** Kafka redelivers or replays the same accepted action event
- **THEN** its event receipt, action fact, video count, and author received-like aggregate remain exactly-once

#### Scenario: Publish and fallback persistence both fail
- **WHEN** Redis accepts an action mutation, no broker durably acknowledges it, and synchronous PostgreSQL fallback fails
- **THEN** the API conditionally rolls back only that still-current Redis version, and a retry emits a higher persistable version instead of silently succeeding with `delta=0`

#### Scenario: Publish acknowledgement is uncertain
- **WHEN** Kafka may have accepted an event but the producer acknowledgement is unavailable or times out
- **THEN** synchronous fallback may persist the same version and any later Kafka delivery is an exactly-once duplicate

#### Scenario: One action transition transport fails
- **WHEN** either RabbitMQ or Kafka does not acknowledge an action event in a dual transition mode
- **THEN** publication exposes per-transport acknowledgement state and fails into synchronous PostgreSQL fallback

#### Scenario: Active action acknowledgement survives fallback failure
- **WHEN** fallback fails after the active primary transport durably acknowledges the stable action event
- **THEN** the API reports failure, confirms the Redis handoff, and does not roll back state that the active broker can later persist

#### Scenario: Mirror-only action acknowledgement remains retryable
- **WHEN** fallback fails after only the non-active mirror transport acknowledges the stable action event
- **THEN** the API reports failure and preserves Redis without confirming the handoff so an idempotent retry republishes to the active transport

#### Scenario: Failed mutation is superseded
- **WHEN** a newer Redis action version replaces a failed mutation before its recovery rollback
- **THEN** recovery does not roll back the newer state

#### Scenario: Older interaction event arrives after newer state
- **WHEN** a newer unlike or unfavorite event is durably applied before an older delayed like or favorite event for the same user, video, and action type
- **THEN** the older event is consumed without changing the action state, video count, or author received-like aggregate

#### Scenario: Concurrent workers receive opposing action versions
- **WHEN** workers concurrently persist like and unlike events whose timestamps conflict with their Redis-assigned versions
- **THEN** the greatest version is primary and determines the durable state

#### Scenario: Compatibility events have the same version and timestamp
- **WHEN** distinct compatibility action events for the same fact have equal versions and occurrence timestamps
- **THEN** their event IDs provide a deterministic final tie-break

#### Scenario: Accepted interaction references terminal video
- **WHEN** an action event is invalid or its video is missing or deleted
- **THEN** the Worker classifies it as terminal and the Kafka consumer completes terminal handling without retrying indefinitely or blocking the partition
