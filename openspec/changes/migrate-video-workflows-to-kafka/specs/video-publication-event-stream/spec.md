## ADDED Requirements

### Requirement: Durable Publication Event Handoff
The first transition that establishes public publication SHALL create one immutable video-owned
publication fact and one operational publication outbox row in the same durable transaction.

#### Scenario: Public gates first become complete
- **WHEN** review, media readiness, visibility, restore, administration, or reconciliation first makes a video publicly eligible
- **THEN** Frux creates one stable publication fact and one idempotent broker handoff

#### Scenario: Broker is unavailable
- **WHEN** publication transport is unavailable after the fact commits
- **THEN** the operational row remains retryable without reverting truthful public eligibility

#### Scenario: Operational row is cleaned
- **WHEN** a dispatched row ages past its replay window
- **THEN** bounded cleanup removes it while retaining the immutable fact so reconciliation cannot republish it

#### Scenario: Public media becomes ready after review
- **WHEN** an existing blocked publication fact becomes media-ready
- **THEN** the undispatched operational payload refreshes current public media fields without changing event identity or original publication time

### Requirement: Retained Video Publication Topic
Frux SHALL publish stable first-publication facts to `frux.video.published.v1`, keyed by video ID,
using broker append time, exact registered partitions, bounded payloads, and retained delete policy.

#### Scenario: Publication is acknowledged
- **WHEN** the required transition transports acknowledge the record
- **THEN** Frux marks the operational handoff dispatched

#### Scenario: Duplicate publication edge is observed
- **WHEN** another module or reconciliation observes the same stable publication event
- **THEN** no second fact or logically distinct event is created

### Requirement: Independent Feed and Hash Embedding Consumers
Feed fanout and `hash-ngram-v1` embedding SHALL consume the publication topic through independent
registered groups and commit offsets only after their own idempotent boundary.

#### Scenario: Hash embedding is unavailable
- **WHEN** the hash embedding group is delayed or failing
- **THEN** Feed fanout continues consuming and committing its own offsets

#### Scenario: Feed record is replayed
- **WHEN** Feed receives the same event again
- **THEN** preheat and following-index effects remain idempotent and publication time is unchanged

#### Scenario: Hash embedding record is replayed
- **WHEN** the hash group receives the same event again
- **THEN** conditional `(video_id, hash-ngram-v1)` persistence does not create a duplicate fact

#### Scenario: Semantic capability is absent
- **WHEN** a publication record is consumed under this change
- **THEN** no semantic service call, semantic vector, semantic job, semantic retry, or semantic coverage state is created

### Requirement: Publication Stream Cutover
Each Feed or hash-embedding responsibility SHALL validate mirror production and non-mutating shadow
parity before activating its Kafka group.

#### Scenario: One consumer cuts over first
- **WHEN** Feed meets its parity and drain gates before hash embedding
- **THEN** Feed may activate Kafka while hash embedding continues on RabbitMQ

#### Scenario: First cutover initializes offsets
- **WHEN** the Kafka group has no committed offsets
- **THEN** an advisory-locked initializer verifies legacy queue, quorum source queue, unacknowledged deliveries, and DLQ are drained before committing the past millisecond-aligned boundary

#### Scenario: Initialized worker restarts
- **WHEN** committed offsets already exist
- **THEN** restart preserves them without rewinding or requiring mirrored Rabbit queues to remain empty

#### Scenario: Consumer rolls back
- **WHEN** a Kafka group exceeds correctness or latency thresholds
- **THEN** only that responsibility restores its RabbitMQ consumer and stable IDs absorb boundary duplicates

### Requirement: Non-Mutating Workflow Parity
Feed and hash-embedding shadow groups SHALL use non-mutating parity readers and SHALL distinguish
propagation-pending state from conflicting durable state.

#### Scenario: Durable effect has not propagated
- **WHEN** a Kafka mirror record arrives before the RabbitMQ active path creates its effect
- **THEN** parity retries inline with a bounded delay rather than committing a mismatch

#### Scenario: Durable effect conflicts
- **WHEN** an existing Feed index or hash embedding does not match the publication record
- **THEN** Frux records a bounded mismatch without mutating business state
