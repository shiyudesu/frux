## MODIFIED Requirements

### Requirement: Bounded Consumer Delivery
Each protected Kafka consumer group SHALL classify failures and SHALL either establish its durable boundary, perform bounded local retry, publish to its registered retry/DLQ flow, or complete registered terminal handling without blocking a source partition indefinitely.

#### Scenario: Retryable infrastructure error repeats
- **WHEN** a record repeatedly fails with a classified retryable infrastructure error under a retry-topic policy
- **THEN** Frux publishes it through only the registered bounded retry tiers and then to that consumer group's DLQ

#### Scenario: Consumer session closes or rebalances
- **WHEN** a protected consumer loses its assignment, connection, or process
- **THEN** Frux supervises the consumer group and relies on committed offsets plus idempotent handlers rather than hot-looping or silently skipping the record

#### Scenario: Terminal payload error occurs
- **WHEN** a record is malformed, unsupported, or references a registered terminal domain state
- **THEN** the consumer routes it to terminal handling or its DLQ and does not retry it indefinitely

### Requirement: Durable Dead-Letter Topology
Protected Kafka consumers SHALL use code-registered consumer-specific retry and DLQ topics with bounded retention, immutable records, registered failure metadata, and acknowledged next-hop publication before source-offset commit.

#### Scenario: Retry or DLQ publication fails
- **WHEN** Kafka does not acknowledge publication to the registered next-hop topic
- **THEN** Frux does not commit the source offset and does not report the failed record as safely isolated

#### Scenario: Retry record is published
- **WHEN** a handler moves a record to a retry tier
- **THEN** the key and business payload remain unchanged and bounded metadata records the original topic, partition, offset, event identity, owning consumer group, attempt, and failure class

#### Scenario: Database-owned job consumer fails later
- **WHEN** a media or semantic job fails after its durable PostgreSQL handoff
- **THEN** the database job state owns retry timing and Kafka retry topics are not required for that later work

### Requirement: Idempotent Redelivery and Replay
Consumer redelivery, retry-topic movement, and operator replay SHALL preserve the original business event ID so existing receipts, versions, and idempotency rules prevent duplicate business facts.

#### Scenario: Failed record was partially persisted
- **WHEN** a Kafka record is redelivered or replayed after its original ID was already durably applied
- **THEN** the consumer treats it as an idempotent duplicate and does not repeat the business mutation

#### Scenario: Retry publication is duplicated
- **WHEN** a process stops after Kafka acknowledges the retry record but before the source offset commit completes
- **THEN** duplicate retry records remain safe because they carry the same original event identity

### Requirement: Protected Dead-Letter Inspection
An operator with `governance.execute` SHALL be able to list allowlisted Kafka DLQ topic summaries and request bounded redacted records by topic, partition, and offset without receiving broker credentials or business payload.

#### Scenario: Unauthorized caller inspects a DLQ
- **WHEN** an authenticated principal lacks governance execution permission
- **THEN** Frux returns HTTP 403 and no payload, offset, topic, or broker metadata

#### Scenario: Record preview is requested
- **WHEN** an authorized operator supplies an allowlisted DLQ topic, valid partition, starting offset, and bounded limit
- **THEN** Frux returns immutable record coordinates, source metadata, bounded failure fields, sizes, hashes, and JSON diagnostics without advancing any consumer group

#### Scenario: Arbitrary Kafka topic is requested
- **WHEN** an operator supplies a topic outside the recovery registry
- **THEN** Frux rejects the request without reading it

### Requirement: Confirmed Single-Message Replay
Operator replay SHALL identify one retained DLQ record by topic, partition, and offset, validate its registered provenance, republish its unchanged key and business payload with a new replay ID, wait for Kafka acknowledgement, and record an idempotent audited result without deleting the DLQ record.

#### Scenario: Record lacks valid source provenance
- **WHEN** the selected DLQ record does not identify the registered source topic, source offset, consumer group, event contract, and payload hash
- **THEN** Frux rejects replay without publishing it

#### Scenario: Replay publication succeeds
- **WHEN** Kafka acknowledges the replay record
- **THEN** Frux commits the prepared success audit and replay result while the immutable DLQ record remains retained

#### Scenario: Replay publication fails or times out
- **WHEN** Kafka does not acknowledge the replay
- **THEN** Frux records a failed replay outcome and does not report success

#### Scenario: Replay request is repeated
- **WHEN** the same actor repeats the same replay payload with the same idempotency key
- **THEN** Frux returns the original replay result without producing another record

### Requirement: Dead-Letter Observability
Frux SHALL expose bounded metrics and alerts for active-group lag, retry/DLQ ingress, retained offset growth, oldest failed-record age, next-hop publication failure, replay attempts, replay failures, and retention-expiry risk.

#### Scenario: Failed-record backlog grows
- **WHEN** retry or DLQ ingress continues while the corresponding recovery consumer makes no progress
- **THEN** Prometheus evaluates a firing alert without event IDs, payload fields, keys, partitions, offsets, operators, reasons, or raw errors as labels

#### Scenario: DLQ record approaches expiry
- **WHEN** the oldest retained DLQ record approaches the registered retention boundary
- **THEN** Frux exposes an alertable retention-risk signal before the record expires
