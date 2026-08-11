# message-dead-letter-recovery Specification

## Purpose

Define bounded Kafka failure handling, durable retry and dead-letter topology, protected inspection, confirmed non-destructive replay, and recovery observability.

## Requirements

### Requirement: Bounded Consumer Delivery
Each protected Kafka consumer group SHALL classify failures and SHALL either establish its durable boundary, perform bounded local retry, publish to its registered retry/DLQ flow, or complete registered terminal handling without blocking a source partition indefinitely.

#### Scenario: Retryable infrastructure error repeats
- **WHEN** a record repeatedly fails with a classified retryable infrastructure error under a retry-topic policy
- **THEN** Frux publishes it through only the registered bounded retry tiers and then to that consumer group's DLQ

#### Scenario: Child dependency deadline expires
- **WHEN** a handler dependency returns `context.DeadlineExceeded` while the Consumer context remains active
- **THEN** Frux applies registered bounded retry and exhausted retry/DLQ routing
- **AND** only cancellation of the Consumer context itself bypasses recovery publication

#### Scenario: Consumer session closes or rebalances
- **WHEN** a protected consumer loses its assignment, connection, or process
- **THEN** Frux supervises the consumer group and relies on committed offsets plus idempotent handlers rather than hot-looping or silently skipping the record

#### Scenario: One retry partition is delayed
- **WHEN** one assigned retry-topic partition has a future `not_before`
- **THEN** Frux pauses only that partition, continues processing other assigned partitions, and resumes the delayed partition once unless shutdown or rebalance cancels it

#### Scenario: A delayed partition is revoked
- **WHEN** a paused delayed partition is revoked, lost, or its consumer session ends before `not_before`
- **THEN** the old owner cancels its timer, discards the buffered record without processing or committing it, and the new owner refetches it from Kafka

#### Scenario: A delayed record becomes ready during revocation
- **WHEN** `not_before` becomes ready while revocation races with delayed-record removal or handling
- **THEN** Frux fences the record with its assignment generation and either lets the current owner finish handling and committing before revocation completes or aborts it for the new owner
- **AND** the old owner never commits after revocation

#### Scenario: Terminal payload error occurs
- **WHEN** a record is malformed, unsupported, or references a registered terminal domain state
- **THEN** the consumer routes it to terminal handling or its DLQ and does not retry it indefinitely

#### Scenario: Terminal record has a malformed source key
- **WHEN** source contract validation classifies a malformed key as terminal
- **THEN** Frux publishes the unchanged key and value directly to the owning registered DLQ with bounded validated recovery metadata and commits the source only after acknowledgement
- **AND** the key-validation exception is unavailable to retry tiers and operator replay

#### Scenario: Retry record has invalid recovery metadata
- **WHEN** a retry-topic record has missing, malformed, obsolete, or tier-inconsistent recovery metadata
- **THEN** Frux publishes unchanged key/value to the owning registered DLQ with sanitized bounded consumed coordinates, owning group, key/payload hashes, `failure_class=recovery_metadata_invalid`, and an explicit non-replayable marker
- **AND** it commits the retry offset only after acknowledged quarantine publication and does not commit when publication fails

#### Scenario: A retry consumer group is created
- **WHEN** a registered retry tier starts with a brand-new consumer group
- **THEN** Frux serializes an idempotent admin initialization through a non-expiring PostgreSQL marker keyed by the resolved environment, prefix, group, versioned topic, and marker version while the group is inactive
- **AND** Frux durably records the partition plan before committing every partition's retained start offset in deterministic order
- **AND** the consumer then requires committed offsets with reset disabled

#### Scenario: Retry offset initialization commits partially
- **WHEN** Kafka acknowledges only a subset of retained-start offset commits
- **THEN** Frux inspects every partition response, durably records acknowledged partitions, and retries only missing partitions
- **AND** the durable marker becomes complete only after a fresh Kafka snapshot proves the complete plan

#### Scenario: Kafka forgets a durably initialized retry group
- **WHEN** the durable marker is complete but Kafka reports the group dead or an established partition offset missing, deleted, expired, or outside the retained range
- **THEN** Frux fails startup visibly as data loss and never classifies the group as brand-new

#### Scenario: Retry partitions are added
- **WHEN** an established inactive retry group has valid commits for the existing contiguous partitions and only new trailing partitions lack commits
- **THEN** Frux initializes only those new partitions to their retained start offsets and preserves every existing commit

#### Scenario: An established retry offset is no longer retained
- **WHEN** an established retry group has an interior missing commit or a committed offset below the retained start or above the end offset
- **THEN** Frux fails the retry consumer visibly with a data-loss or offset error and does not rewind it

### Requirement: Durable Dead-Letter Topology
Protected Kafka consumers SHALL use code-registered consumer-specific retry and DLQ topics with bounded retention, immutable records, registered failure metadata, and acknowledged next-hop publication before source-offset commit.

#### Scenario: Retry or DLQ publication fails
- **WHEN** Kafka does not acknowledge publication to the registered next-hop topic
- **THEN** Frux does not commit the source offset and does not report the failed record as safely isolated

#### Scenario: Retry record is published
- **WHEN** a handler moves a record to a retry tier
- **THEN** the key and business payload remain unchanged and bounded metadata records the original topic, partition, offset, event identity, owning consumer group, attempt, and failure class

#### Scenario: Registered topics have different record limits
- **WHEN** a record is published to a source, retry, or DLQ Topic
- **THEN** Frux enforces that destination Topic's exact registered record allowance rather than the smallest registered Topic allowance
- **AND** one shared reviewed headroom calculation derives source `max.message.bytes`, recovery record limits, recovery `max.message.bytes`, and each resolved Topic's franz-go producer batch ceiling
- **AND** recovery Topics allow the maximum broker-accepted source key/value/envelope plus bounded recovery headers
- **AND** recovery publication rejects source bytes above the source Topic's broker maximum even when the destination has unused header capacity

#### Scenario: Broker accepts an application-oversized poison record
- **WHEN** a malformed source record exceeds the application `MaxRecordBytes` but remains within the source Topic's broker `max.message.bytes`
- **THEN** Frux can publish its unchanged key/value to the owning DLQ with bounded recovery headers
- **AND** a record above the source broker maximum remains rejected

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
Operator replay SHALL identify one retained DLQ record by topic, partition, and offset, validate its registered provenance, commit a durable pending claim, republish its unchanged key and business payload outside that transaction to the owning group's registered first retry topic with a new replay ID, wait for Kafka acknowledgement, and record an idempotent audited result in a second transaction without deleting the DLQ record.

#### Scenario: Record lacks valid source provenance
- **WHEN** the selected DLQ record does not identify the registered source topic, source offset, consumer group, event contract, and payload hash
- **THEN** Frux rejects replay without publishing it

#### Scenario: Quarantine record is selected for replay
- **WHEN** the selected DLQ record is marked non-replayable with `failure_class=recovery_metadata_invalid`
- **THEN** Frux rejects replay before creating a publish attempt

#### Scenario: Replay publication succeeds
- **WHEN** Kafka acknowledges the replay record
- **THEN** Frux commits the prepared success audit and replay result while the immutable DLQ record remains retained

#### Scenario: Shared source topic has sibling groups
- **WHEN** Feed or embedding replays a failure from their shared `video.published` source
- **THEN** the replay enters only the owning group's first retry topic and cannot block the sibling source consumer

#### Scenario: Replay publication outcome is uncertain
- **WHEN** the producer reports that Kafka may have acknowledged the replay, or Kafka acknowledges it but the result or audit transaction does not commit
- **THEN** the durable claim remains pending/unknown, the same idempotency key never publishes again, and Frux does not report success without broker evidence
- **AND** a repeated identical authorized request may reconcile the stable Replay ID at the registered destination, atomically finalize success plus audit when found, or finalize failure when a complete retained scan proves absence
- **AND** expired or unavailable evidence leaves the claim pending

#### Scenario: Replay absence is settling
- **WHEN** a complete destination scan does not find the stable Replay ID
- **THEN** Frux repeats end-offset snapshots and complete scans through a bounded settlement window after the producer uncertainty window
- **AND** any retained-bound growth restarts the scan and stability requirement
- **AND** only repeated stable bounds with complete clean scans prove absence; cancellation, malformed evidence, unavailable evidence, or bounds that never stabilize leave the claim pending

#### Scenario: Replay publication is definitively rejected
- **WHEN** Kafka definitively rejects the replay without a possible acknowledgement
- **THEN** Frux records a failed replay outcome and does not report success

#### Scenario: Replay request is repeated
- **WHEN** the same actor repeats the same replay payload with the same idempotency key
- **THEN** Frux returns the original replay result without producing another record

### Requirement: Dead-Letter Observability
Frux SHALL expose bounded metrics and alerts for active-group lag, retry/DLQ ingress, retained offset growth, oldest failed-record age, next-hop publication failure, replay attempts, replay failures, and retention-expiry risk.

#### Scenario: Source and retry tiers report concurrently
- **WHEN** source and retry-tier consumers for one owning group report lag or session health
- **THEN** each uses a bounded registered stage series and Frux explicitly aggregates workflow lag and worst-stage health
- **AND** an idle healthy retry tier cannot mask source lag or source failure, while an unhealthy tier does not mark unrelated groups

#### Scenario: Failed-record backlog grows
- **WHEN** retry or DLQ ingress continues while the corresponding recovery consumer makes no progress
- **THEN** Prometheus evaluates a time-window alert from end-offset movement, retained backlog, oldest-record movement, and durable recovery/replay progress without event IDs, payload fields, keys, partitions, offsets, operators, reasons, or raw errors as labels

#### Scenario: Non-destructive replay succeeds during ingress
- **WHEN** DLQ end offsets continue to advance but a replay succeeds in the same alert window
- **THEN** replay counts as recovery progress and the no-progress alert does not fire solely because retained records remain immutable

#### Scenario: DLQ record approaches expiry
- **WHEN** the oldest retained DLQ record approaches the registered retention boundary
- **THEN** Frux exposes an alertable retention-risk signal before the record expires

#### Scenario: No operator requests summaries
- **WHEN** no admin caller invokes the DLQ summary endpoint
- **THEN** a supervised bounded periodic collector still refreshes registered summary gauges and marks them stale during broker outages without preventing process startup
