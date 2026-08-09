## ADDED Requirements

### Requirement: Durable Publication Event Handoff
The first transition that establishes a stable public publication fact SHALL create one video-owned durable publication-event outbox row in the same transaction or durable publication boundary.

#### Scenario: Public gates first become complete
- **WHEN** review, media readiness, visibility, lifecycle, or reconciliation first makes a video publicly eligible
- **THEN** Frux creates one stable publication event and one idempotent Kafka handoff identified by that event ID

#### Scenario: Publication handoff is retried
- **WHEN** Kafka is unavailable after the public publication fact commits
- **THEN** the outbox retains the event for delayed retry without reverting truthful public eligibility

### Requirement: Retained Video Publication Topic
Frux SHALL publish stable first-publication facts to a registered retained Kafka topic keyed by video ID.

#### Scenario: Publication record is acknowledged
- **WHEN** the outbox dispatcher receives Kafka acknowledgement
- **THEN** it marks the handoff dispatched while the Kafka event remains available for independent consumer groups during retention

#### Scenario: Duplicate publication edge is observed
- **WHEN** review, media readiness, restore, or reconciliation observes an event ID already created
- **THEN** no second outbox row or logically distinct publication event is created

### Requirement: Independent Publication Consumers
Feed fanout and embedding intake SHALL consume the publication topic through independent registered consumer groups and SHALL commit offsets only after their own durable or idempotent boundary.

#### Scenario: Embedding intake is unavailable
- **WHEN** the embedding consumer group is delayed or failing
- **THEN** the Feed fanout group continues consuming and committing its own offsets

#### Scenario: Feed fanout record is replayed
- **WHEN** the Feed group receives the same publication event again
- **THEN** preheat and following-index effects remain idempotent and the video's publication time is unchanged

### Requirement: Publication Stream Cutover
Frux SHALL validate publication mirrors and non-mutating shadow groups before activating each Kafka publication consumer responsibility.

#### Scenario: One publication consumer cuts over
- **WHEN** Feed fanout meets its Kafka parity and lag gates before embedding does
- **THEN** Feed may activate its Kafka group while embedding continues on RabbitMQ

#### Scenario: Publication consumer rolls back
- **WHEN** one Kafka group exceeds its correctness or latency threshold
- **THEN** Frux stops that group and restores only its RabbitMQ consumer without changing the other publication consumer

### Requirement: Atomic Publication Proof and Non-Mutating Parity
Publication notification and event rows SHALL be created atomically at every first-public edge.
Notification readiness SHALL NOT substitute for event-row presence. Feed and embedding shadows SHALL
require non-mutating parity readers.

#### Scenario: Media delivery completes after review
- **WHEN** review first establishes database public eligibility before a media-backed public variant is ready
- **THEN** the same transaction creates a blocked stable event row and media readiness later makes that row dispatchable without changing event identity

#### Scenario: Delivered notification lacks event row
- **WHEN** reconciliation finds an eligible tracked video with a ready or delivered notification but no publication event row
- **THEN** it repairs the event row while preserving notification state

#### Scenario: Shadow parity is absent
- **WHEN** Feed or embedding shadow/cutover mode is configured with nil parity
- **THEN** worker startup fails
