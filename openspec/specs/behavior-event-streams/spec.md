# behavior-event-streams Specification

## Purpose

Defines retained Kafka streams, durable consumer boundaries, ordering, idempotency, fallback, and
recovery for accepted action changes and committed view events.

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
- **THEN** startup rejects the topic as incompatible with retained-event ordering and recovery

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

### Requirement: Kafka-Only Behavior Runtime
Frux SHALL publish behavior events only to Kafka and SHALL run one registered active Consumer Group per behavior responsibility.

#### Scenario: Action publication fails
- **WHEN** Kafka fails or cannot confirm action publication
- **THEN** synchronous PostgreSQL fallback may persist the stable event and conditional Redis rollback is allowed only when Kafka is known not to have acknowledged it

#### Scenario: View publication fails
- **WHEN** Kafka does not acknowledge a view event
- **THEN** the PostgreSQL outbox row remains pending and retries the stable event

#### Scenario: Worker starts behavior groups
- **WHEN** the Worker starts view and action consumers
- **THEN** it waits for a non-empty view partition assignment before starting the action group

#### Scenario: New behavior group starts
- **WHEN** a behavior group has no durable marker or committed offsets
- **THEN** Frux initializes every partition at retained start before joining

#### Scenario: Consumer client has no assignment
- **WHEN** a Kafka consumer client is constructed but has not received any non-empty partition assignment
- **THEN** its supervised session is not marked started or healthy

#### Scenario: First assignment times out
- **WHEN** the first non-empty assignment does not arrive within the configured startup timeout
- **THEN** worker startup cancels that consumer and fails visibly

#### Scenario: Consumer fails before first assignment
- **WHEN** the supervisor exits fatally during initialization or runtime before a non-empty assignment
- **THEN** worker startup cancels the consumer and returns the fatal cause

#### Scenario: Behavior group restarts
- **WHEN** the group already has committed offsets
- **THEN** Frux preserves those offsets and does not rewind them

#### Scenario: Active consumer initialization is non-retryable
- **WHEN** authentication, configuration, or registered handler-contract initialization fails
- **THEN** the required consumer is unhealthy and worker startup or runtime fails visibly instead of silently retrying forever

#### Scenario: Established behavior offset is lost
- **WHEN** the durable marker proves a behavior offset was initialized but Kafka no longer retains it
- **THEN** Frux reports explicit data loss and does not reset the group

### Requirement: Retained Behavior Availability
Kafka SHALL retain behavior events for the registered operational replay window without replacing PostgreSQL as the long-term business or training source of truth.

#### Scenario: A new independent consumer starts within retention
- **WHEN** a registered future consumer group starts from an available earlier offset
- **THEN** it can consume retained behavior events without changing the offsets of existing groups
