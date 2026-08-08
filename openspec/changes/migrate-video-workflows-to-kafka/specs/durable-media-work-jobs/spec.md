## ADDED Requirements

### Requirement: Database-Owned Media Processing
PostgreSQL SHALL remain the authoritative state machine for media processing identity, leases, attempts, retry time, terminal state, and reconciliation.

#### Scenario: Processing wakeup is lost
- **WHEN** a durable media job commits but its Kafka wakeup is not published or consumed
- **THEN** polling or reconciliation leases and processes the job without requiring message recovery

#### Scenario: Processing command is duplicated
- **WHEN** Kafka delivers the same asset and profile-version wakeup more than once
- **THEN** the database lease and job identity prevent duplicate concurrent processing and duplicate outputs

### Requirement: Non-Authoritative Kafka Wakeups
Media processing Kafka commands SHALL be short-retention wakeup hints keyed by asset ID and SHALL NOT remain uncommitted for the duration of transcoding.

#### Scenario: Wakeup command is consumed
- **WHEN** the referenced durable job exists
- **THEN** the consumer signals bounded local scheduling and commits the command independently of the later ffmpeg outcome

#### Scenario: Local processing capacity is full
- **WHEN** a valid wakeup arrives while no local slot is available
- **THEN** the consumer may commit the command because the durable job remains discoverable by polling

### Requirement: Durable Semantic Embedding Jobs
Remote semantic embedding work SHALL use a PostgreSQL job keyed by video and model with canonical text hash, state, attempts, availability time, lease, bounded error class, and completion metadata.

#### Scenario: Published video requires semantic generation
- **WHEN** embedding intake persists hash coverage and no current semantic vector exists for the canonical text hash
- **THEN** it creates or refreshes one durable semantic job before committing the Kafka publication offset

#### Scenario: Semantic service remains unavailable
- **WHEN** repeated remote generation attempts fail retryably
- **THEN** the job remains pending with capped delayed retries and does not repeatedly redeliver the original publication event

#### Scenario: Publication event is duplicated
- **WHEN** the same video publication event is consumed again with the same text hash and model
- **THEN** hash persistence and semantic-job creation are idempotent

### Requirement: Hash-First Embedding Progress
Hash embedding generation and persistence SHALL remain independent of semantic service enablement, readiness, latency, or failure.

#### Scenario: Semantic service gate is closed
- **WHEN** a valid publication event is consumed while semantic generation is disabled or unavailable
- **THEN** Frux persists or confirms the hash vector and retains semantic work in the durable job state

### Requirement: Durable Job Recovery and Observability
Media and semantic workers SHALL use bounded leases, polling, reconciliation, retry backoff, terminal classification, and low-cardinality backlog and outcome metrics.

#### Scenario: Worker exits with a leased job
- **WHEN** a worker stops before completing media or semantic work and its lease expires
- **THEN** another worker can reclaim the durable job without relying on Kafka offset reset

#### Scenario: Semantic backlog grows
- **WHEN** pending semantic jobs exceed the configured age or count threshold
- **THEN** Frux exposes an alertable backlog signal without video IDs, model strings outside the registry, text, vectors, or raw errors as labels
