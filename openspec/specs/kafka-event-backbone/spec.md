# kafka-event-backbone Specification

## Purpose

Defines Frux's versioned Kafka contracts, topic governance, acknowledged production, durable-boundary offset commits, supervised consumer groups, controlled transport migration, and bounded observability.

## Requirements

### Requirement: Versioned Kafka Event Contracts
Frux SHALL encode every Kafka event in a bounded versioned envelope with a stable event ID, registered event type, schema version, occurrence time, production time, registered producer, validated record key, and typed payload.

#### Scenario: Registered event is encoded
- **WHEN** an application publisher submits a valid registered event and key
- **THEN** Frux produces the registered envelope version without adding arbitrary payload or header fields

#### Scenario: Unsupported event version is consumed
- **WHEN** a consumer receives an event type or schema version it does not support
- **THEN** Frux classifies the record as a contract failure and does not pass a partially decoded payload to the business handler

### Requirement: Code-Owned Topic Governance
Frux SHALL define topic names, versions, event-or-command classification, partition-key kind, retention, cleanup policy, record-size bound, producers, and consumer groups in a closed code-owned registry.

#### Scenario: Local topic is missing
- **WHEN** local development starts with a registered topic absent
- **THEN** Frux provisions it using the registered development topology

#### Scenario: Production topic is incompatible
- **WHEN** production validation finds an unsafe replication setting or an incompatible registered topic policy
- **THEN** startup fails explicitly instead of silently accepting or mutating the topology

### Requirement: Acknowledged Idempotent Production
Frux Kafka producers SHALL use idempotent acknowledged production with bounded deadlines and SHALL report success only after the broker acknowledges the record.

#### Scenario: Broker acknowledges the record
- **WHEN** all required in-sync replicas accept a valid event within its deadline
- **THEN** the publisher returns success with no application-level duplicate retry

#### Scenario: Produce result is unavailable
- **WHEN** production times out, is rejected, or cannot determine an acknowledged result
- **THEN** the publisher returns an explicit failure and leaves fallback behavior to the owning application workflow

### Requirement: Durable-Boundary Offset Commit
Frux Kafka consumers SHALL disable automatic commits and SHALL advance offsets only after the registered handler establishes its durable success or terminal-handling boundary.

#### Scenario: Handler transaction commits
- **WHEN** a consumer durably applies or hands off a record
- **THEN** the record becomes eligible for offset commit

#### Scenario: Process stops after durable commit but before offset commit
- **WHEN** the handler commits business state and the consumer exits before Kafka records the offset
- **THEN** Kafka may redeliver the record and the stable event ID prevents duplicate business facts

### Requirement: Supervised Consumer Groups
Frux SHALL run registered Kafka consumer groups with bounded batches, partition ordering, cooperative rebalancing, supervised restart, cancellation, and shutdown draining.

#### Scenario: Consumer group rebalances
- **WHEN** an instance joins, leaves, or loses a partition assignment
- **THEN** Frux stops polling, cancels the in-flight batch as soon as the rebalance is blocked, and does not release the partition until those cancellation-aware handlers return and eligible offsets are committed

#### Scenario: Worker shuts down
- **WHEN** the process context is canceled
- **THEN** consumers stop polling, allow a bounded drain grace period, cancel handler contexts, commit eligible offsets, and close only after in-flight handlers return so another group member cannot mutate the same record concurrently

### Requirement: Controlled Transport Migration
Frux SHALL support registered primary/mirror producer modes and active/shadow consumer modes while ensuring that no migration configuration activates two uncontrolled business writers for one consumer responsibility.

#### Scenario: Kafka shadow validation runs
- **WHEN** a stream remains RabbitMQ-active and its Kafka consumer is in shadow mode
- **THEN** the registered shadow group accepts only the non-mutating shadow handler, validates contracts, and records parity metrics without mutating business state or using the future active group offset

#### Scenario: Invalid dual-active configuration is loaded
- **WHEN** configuration would let RabbitMQ and Kafka consumers both invoke the same mutating responsibility
- **THEN** Frux rejects startup

### Requirement: Bounded Kafka Observability
Frux SHALL expose bounded metrics for registered production, consumption, commits, contract failures, rebalances, lag, delivery delay, and topology validation.

#### Scenario: Consumer lag grows
- **WHEN** a registered active consumer group falls behind its topic
- **THEN** Frux exposes lag using registered topic and group labels without event, user, video, key, partition, offset, payload, or raw-error labels
