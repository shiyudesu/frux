# behavior-event-streams Specification

## Purpose

Defines retained Kafka streams, durable consumer boundaries, ordering, idempotency, and controlled
RabbitMQ-to-Kafka migration for accepted action changes and committed view events.

## Requirements

### Requirement: Versioned Behavior Topics
Frux SHALL publish accepted action changes and committed view events to separate registered Kafka topics with stable event IDs, versioned envelopes, bounded payloads, delete-based retention, and broker-assigned append timestamps.

#### Scenario: Action event is published
- **WHEN** Redis accepts a valid action mutation and assigns its monotonic version
- **THEN** Frux publishes the action event keyed by its user, video, and action-type identity

#### Scenario: View event is committed
- **WHEN** the view-event transaction commits its raw fact and outbox row
- **THEN** the outbox dispatcher publishes the event keyed by user identity without making Kafka availability part of the HTTP transaction

#### Scenario: Behavior key has an equivalent non-canonical spelling
- **WHEN** an action or user key contains leading zeroes, a numeric sign, or a non-canonical action-type case
- **THEN** Frux rejects the record as an invalid key instead of accepting an alias for the same identity

#### Scenario: Behavior topic uses producer create time
- **WHEN** topology validation finds `message.timestamp.type` other than `LogAppendTime`
- **THEN** startup rejects the topic as incompatible for timestamp cutover

### Requirement: Behavior Consumer Group Ownership
Each behavior responsibility SHALL use one registered active Kafka consumer group and SHALL commit its offset only after the existing durable receipt, fact, and downstream outbox boundary commits.

#### Scenario: Action persistence commits
- **WHEN** the accepted-action transaction inserts or validates the receipt, applies the winning version, updates aggregates, and creates downstream handoffs
- **THEN** the action record becomes eligible for Kafka offset commit

#### Scenario: View behavior persistence commits
- **WHEN** the recommendation behavior transaction records or recognizes the event and creates its durable profile/outcome handoff
- **THEN** the view record becomes eligible for Kafka offset commit without waiting for later projection dependencies

### Requirement: Stable Behavior Ordering and Idempotency
Frux SHALL preserve action-version ordering, deterministic compatibility tie-breaking, view-event identity, and duplicate-safe consumption independently of Kafka partition reassignment or redelivery.

#### Scenario: Older action record is consumed later
- **WHEN** a lower action version is delivered after a higher version for the same key
- **THEN** the lower version is recorded or recognized without regressing durable state or aggregates

#### Scenario: Kafka redelivers a committed behavior event
- **WHEN** a process stops after PostgreSQL commits but before the offset commit succeeds
- **THEN** the stable event identity prevents another business fact or projection handoff

### Requirement: Behavior Stream Cutover
Frux SHALL validate each Kafka behavior stream with mirror production and a non-mutating shadow group before enabling its active group, and SHALL record an explicit cutover boundary.

#### Scenario: One transition transport fails
- **WHEN** either RabbitMQ or Kafka fails or cannot confirm publication in a dual transition mode
- **THEN** publication returns an error so action fallback or view outbox retry remains responsible, even if the other transport acknowledged

#### Scenario: Shadow action consumer receives a record
- **WHEN** RabbitMQ remains the active action transport
- **THEN** the Kafka shadow consumer validates the record and optional fact parity without invoking action persistence

#### Scenario: RabbitMQ remains the active consumer
- **WHEN** a migration configuration selects Kafka primary with RabbitMQ mirror
- **THEN** Frux rejects the pair because a failed RabbitMQ mirror would bypass the active consumer

#### Scenario: Active Kafka group starts at cutover
- **WHEN** an active group has no committed offsets and starts with a cutover boundary
- **THEN** Frux resolves the broker append-time boundary per partition and durably initializes the group before consumption

#### Scenario: Producer clock differs from broker clock
- **WHEN** an event envelope carries a producer timestamp before or after the broker clock
- **THEN** timestamp cutover resolution uses the broker-assigned record append time rather than the producer timestamp

#### Scenario: Action and view boundaries are equal
- **WHEN** both Kafka active groups are configured with equal cutover boundaries
- **THEN** startup rejects the configuration because the action boundary must be strictly after the view boundary

#### Scenario: Worker starts behavior Kafka groups
- **WHEN** view and action Kafka active or shadow groups are enabled in one worker
- **THEN** the worker initializes and starts the view group before the action group

#### Scenario: Active Kafka group restarts
- **WHEN** the group already has committed offsets
- **THEN** Frux preserves those offsets and does not reapply or rewind to the configured boundary

#### Scenario: Shadow fact has not propagated yet
- **WHEN** a valid Kafka mirror record arrives before the RabbitMQ path has created its durable fact
- **THEN** parity is pending and receives bounded delayed retries rather than being committed as a mismatch

#### Scenario: Shadow fact conflicts
- **WHEN** a durable fact with the same identity exists but its payload conflicts
- **THEN** parity is recorded as a mismatch without pending retries

#### Scenario: Active consumer initialization is non-retryable
- **WHEN** authentication, configuration, or registered handler-contract initialization fails
- **THEN** the required consumer is unhealthy and worker startup or runtime fails visibly instead of silently retrying forever

#### Scenario: Behavior stream rolls back
- **WHEN** Kafka production, lag, or correctness exceeds the rollback threshold
- **THEN** Frux stops the Kafka active group and restores RabbitMQ consumption before returning RabbitMQ to primary production

### Requirement: Retained Behavior Availability
Kafka SHALL retain behavior events for the registered operational replay window without replacing PostgreSQL as the long-term business or training source of truth.

#### Scenario: A new independent consumer starts within retention
- **WHEN** a registered future consumer group starts from an available earlier offset
- **THEN** it can consume retained behavior events without changing the offsets of existing groups
