## MODIFIED Requirements

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
- **THEN** the attempt is canceled and persisted with a stable timeout reason rather than an
  ambiguous process-killed message

#### Scenario: Processing command emits long diagnostics
- **WHEN** ffmpeg fails with stderr longer than the durable error-message limit
- **THEN** the retained message contains the bounded diagnostic tail where ffmpeg reports the
  actionable terminal error

#### Scenario: Wakeups fail while jobs remain pending
- **WHEN** the Kafka transport is unavailable
- **THEN** backlog and polling-recovery signals remain observable while durable jobs continue
