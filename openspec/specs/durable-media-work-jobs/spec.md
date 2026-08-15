## Purpose
Define durable, database-owned media processing jobs, Kafka wakeups, fenced execution, lifecycle
intents, recovery, and operational observability.

## Requirements

### Requirement: Database-Owned Media Processing
PostgreSQL SHALL remain the authoritative state machine for media-processing identity, state,
attempts, retry time, claim token, lease, terminal outcome, and reconciliation.

#### Scenario: Processing wakeup is lost
- **WHEN** a durable media job commits but its Kafka wakeup is not published or consumed
- **THEN** polling or reconciliation claims and processes the job without message recovery

#### Scenario: Processing command is duplicated
- **WHEN** Kafka delivers the same asset and profile-version wakeup more than once
- **THEN** the database job identity and claim fence prevent duplicate concurrent processing and outputs

### Requirement: Non-Authoritative Kafka Wakeups
Media-processing Kafka commands SHALL be short-retention wakeup hints keyed by asset ID and SHALL NOT
remain uncommitted for the duration of transcoding.

#### Scenario: Wakeup command is consumed
- **WHEN** the referenced durable job exists
- **THEN** the consumer signals bounded local scheduling and commits independently of the later ffmpeg outcome

#### Scenario: Local processing capacity is full
- **WHEN** a valid wakeup arrives while no local execution slot is available
- **THEN** the consumer commits because polling can rediscover the durable job

### Requirement: Slot-Bounded Fenced Media Claims
Kafka wakeups and PostgreSQL polling SHALL use one bounded scheduler. A worker SHALL reserve a slot
before claiming one job. Every claim SHALL have a unique token, and heartbeat, finalization, retry,
and terminal transitions SHALL require that token and a current unexpired lease.

#### Scenario: Wakeups and polling overlap
- **WHEN** Kafka signals jobs while the recovery poller also finds work
- **THEN** their combined claims never exceed available execution slots

#### Scenario: Lease expires and is reclaimed
- **WHEN** another token reclaims a job before the old ffmpeg attempt returns
- **THEN** the stale attempt cannot mutate assets, variants, cleanup tasks, job state, publication state, or notifications

#### Scenario: Current attempt completes
- **WHEN** the current token owns an unexpired lease and valid outputs are ready
- **THEN** asset metadata, variants, cleanup records, and job completion commit atomically before public projection or notifications

#### Scenario: Heartbeat storage stalls
- **WHEN** lease extension exceeds its bounded processing-derived context
- **THEN** processing is canceled without a stale finalization or retry transition

### Requirement: Durable Media Lifecycle Intents
Any transaction that makes media private, deletes its owner video, or otherwise requires object
protection/cleanup SHALL persist a durable idempotent lifecycle intent before commit.

#### Scenario: Private transition commits and the process exits
- **WHEN** a video becomes private after public objects were promoted and the process stops before object protection
- **THEN** a worker later claims the durable intent and protects the objects

#### Scenario: Delete transition commits and cleanup fails
- **WHEN** a video is deleted and physical object cleanup fails
- **THEN** the cleanup intent remains retryable without restoring public discovery

#### Scenario: Lifecycle intent is delivered twice
- **WHEN** the same protection or cleanup intent is claimed more than once
- **THEN** object state and intent completion remain idempotent

### Requirement: Durable Media Recovery and Observability
Media workers SHALL expose bounded job, wakeup, polling-recovery, lifecycle-intent, lease-loss, and
terminal-outcome metrics without asset, video, token, object-key, URL, or raw-error labels. Durable
retry state SHALL retain a bounded safe reason that distinguishes command timeout, ordinary command
failure, and expired-lease recovery.

#### Scenario: Worker exits with a leased job
- **WHEN** the worker stops and the lease expires
- **THEN** another worker reclaims the job without resetting Kafka offsets

#### Scenario: Expired lease returns to the queue
- **WHEN** reconciliation releases a processing lease that expired before finalization
- **THEN** the job becomes retryable with a safe `lease_expired` reason and no stale owner

#### Scenario: Processing command exceeds its configured deadline
- **WHEN** ffmpeg remains active beyond the validated command timeout
- **THEN** the attempt is canceled and persisted with a stable timeout reason rather than an ambiguous process-killed message

#### Scenario: Processing command emits long diagnostics
- **WHEN** ffmpeg fails with stderr longer than the durable error-message limit
- **THEN** the retained message contains the bounded diagnostic tail where ffmpeg reports the actionable terminal error

#### Scenario: Wakeups fail while jobs remain pending
- **WHEN** the Kafka transport is unavailable
- **THEN** backlog and polling-recovery signals remain observable while durable jobs continue
