## ADDED Requirements

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
