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

### Requirement: Slot-Bounded Fenced Media Claims
Kafka wakeups and PostgreSQL polling SHALL submit work to one bounded scheduler/worker pool. A worker
SHALL reserve an execution slot before claiming at most one job. Every claim SHALL use a unique
per-claim token, and heartbeat, completion, retry, and terminal transitions SHALL require that token,
processing state, and a current unexpired lease. Heartbeats SHALL use bounded contexts derived from
the active processing context.

#### Scenario: Wakeups and polling are both active
- **WHEN** Kafka signals jobs while the recovery poller also finds available work
- **THEN** their combined claims never exceed the scheduler's currently available execution slots

#### Scenario: Media lease expires and is reclaimed
- **WHEN** another token reclaims a job before the old ffmpeg attempt returns
- **THEN** the stale attempt cannot extend, mutate the asset or variants, create cleanup/public state, notify, complete, retry, or terminally update the reclaimed job

#### Scenario: Reclaimed media attempt completes
- **WHEN** the current token still owns an unexpired lease and valid outputs are ready
- **THEN** asset metadata, variants, and the completed job transition commit atomically before public projection or notification effects run

#### Scenario: Deletion races media finalization
- **WHEN** cleanup scheduling and a fenced media finalization overlap
- **THEN** asset tombstoning, current variant snapshot, and cleanup-task creation are transactional so either finalization is included or its outputs are separately cleanup-scheduled

#### Scenario: Post-commit failed notification fails
- **WHEN** a terminal media transition commits but its notification/projection call fails
- **THEN** failed assets remain reconciliation candidates and the idempotent failed projection is retried

#### Scenario: Media heartbeat storage stalls
- **WHEN** a lease-extension query does not return before its bounded child-context deadline
- **THEN** processing is canceled and the worker returns without performing a stale completion or retry

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

### Requirement: Fenced Semantic Claims and Replica-Local Readiness
Each semantic processor SHALL claim one job with a unique per-claim token, heartbeat while remote
inference is active, and fence heartbeat, complete, and retry by token, text hash, and unexpired lease.
Each replica SHALL gate claims only on its own successful metadata validation. It SHALL NOT suspend or
resume shared jobs when its local service is unavailable. Pending, retry, and legacy suspended rows
SHALL remain claimable by any replica whose local gate is open.

#### Scenario: Stale semantic attempt returns
- **WHEN** a semantic lease expires, another token reclaims the job, and the old remote call returns
- **THEN** the old attempt cannot heartbeat, complete, or retry the reclaimed job

#### Scenario: One replica cannot validate metadata
- **WHEN** one replica has a local metadata or connectivity failure while another replica is healthy
- **THEN** only the unhealthy replica keeps its claim gate closed and the healthy replica continues claiming shared work

#### Scenario: Legacy suspended row remains
- **WHEN** a healthy replica claims work created by the former global suspension behavior
- **THEN** the legacy suspended row is claimable in stable order without a global resume rewrite
