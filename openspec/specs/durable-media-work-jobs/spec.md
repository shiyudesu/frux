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

### Requirement: Fenced Durable Processing Progress
An active media-processing attempt SHALL persist a registered current step, optional step progress
from zero through ten thousand basis points, and progress update time under the same claim-token and
unexpired-lease fence as execution heartbeats.

#### Scenario: Source download advances
- **WHEN** the current Worker downloads known source bytes
- **THEN** bounded progress updates identify the downloading step and transferred-byte percentage

#### Scenario: FFmpeg advances
- **WHEN** remuxing or transcoding reports processed media time
- **THEN** bounded progress updates derive step percentage from processed time and known source
  duration

#### Scenario: Output upload advances
- **WHEN** the Worker uploads a known output size
- **THEN** bounded progress updates identify the uploading step and transferred-byte percentage

#### Scenario: Step is indeterminate
- **WHEN** inspection or finalization has no truthful percentage
- **THEN** the registered step is persisted with no percentage rather than a fabricated value

#### Scenario: Progress updates are frequent
- **WHEN** underlying byte or media-time progress changes rapidly
- **THEN** PostgreSQL updates are throttled to a bounded cadence while step transitions and terminal
  state remain promptly visible

#### Scenario: Old attempt reports progress
- **WHEN** a claim has expired or another Worker has reclaimed the task
- **THEN** the stale attempt cannot update progress, heartbeat, job state, assets, or outputs

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

### Requirement: Durable Operator Retry
An authorized operator retry SHALL reset only an eligible failed media job and its failed source
state, SHALL remain idempotent, SHALL preserve job fencing, and SHALL durably request video-facing
processing-state repair.

#### Scenario: Eligible failed task is retried
- **WHEN** a failed task with an available non-deleted source is retried
- **THEN** attempts and terminal fields reset, next attempt becomes immediately eligible, and normal
  Worker polling can claim it

#### Scenario: Retry request is replayed
- **WHEN** the same operator, idempotency key, and normalized retry request are submitted again
- **THEN** the stored result is returned without another reset or audit fact

#### Scenario: Idempotency key is reused differently
- **WHEN** the same operator and idempotency key are submitted for a different task or payload
- **THEN** the retry is rejected as an idempotency conflict

#### Scenario: Retry projection update is interrupted
- **WHEN** the durable job reset commits but video-facing processing-state repair fails
- **THEN** a durable outbox retains the repair until it succeeds without resetting the job again

#### Scenario: Ineligible task is retried
- **WHEN** the task is waiting, processing, completed, already requeued, or its source is deleted
- **THEN** the durable state is unchanged and the API returns a stable conflict or rejection

### Requirement: Direct Deterministic Output Publication
A media-processing attempt SHALL publish a completed local output directly to its deterministic
final protected object key and SHALL NOT upload, download, or copy an object-store temporary body.

#### Scenario: Final key is absent
- **WHEN** processing has a valid local output and the deterministic key does not exist
- **THEN** Worker performs one PUT from the local file and verifies final size and checksum before PostgreSQL finalization

#### Scenario: Final key already matches
- **WHEN** a retry observes the expected final size and checksum
- **THEN** Worker reuses the existing output without transferring the body again

#### Scenario: Final key conflicts
- **WHEN** the deterministic key exists with unexpected metadata
- **THEN** Worker records an explicit retryable or terminal failure and does not overwrite the conflicting file

#### Scenario: Database finalization is interrupted
- **WHEN** object PUT succeeds but fenced PostgreSQL finalization does not
- **THEN** the unreferenced protected object is left for delayed orphan reconciliation and is never advertised as ready

#### Scenario: Attempt is reclaimed during PUT
- **WHEN** the job lease is lost while final output is being uploaded
- **THEN** the stale attempt cannot finalize the job, and any unreferenced deterministic object is handled by reconciliation
