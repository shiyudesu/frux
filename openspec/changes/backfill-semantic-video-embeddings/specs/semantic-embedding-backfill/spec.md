## ADDED Requirements

### Requirement: Shared Fixed Provider Contract
Backfill SHALL depend on completed `add-semantic-embedding-service` and
`integrate-semantic-video-embeddings` and SHALL use their exact
provider/model/revision/dimension/`semantic-text-v1` identity, privacy boundary, validated adapter,
cache, cost/quota controls, and conditional persistence. It MUST NOT host/train a model or select a
different provider contract.

#### Scenario: Dependencies match
- **WHEN** the command starts
- **THEN** it validates the complete contract and pricing revision before scanning

#### Scenario: Identity differs
- **WHEN** provider, model, revision, dimension, or canonicalizer is incompatible
- **THEN** the run fails before provider access or mutation

### Requirement: Mandatory Dry-Run Estimate
Before billable execution, a dry-run SHALL freeze an eligible horizon and report candidate classes,
unique text hashes, validated cache hits, expected provider items and API calls, billable units,
estimated cost, row/runtime/QPS/cost bounds, pricing revision, environment, full identity, refresh
mode, and horizon. Dry-run SHALL make no provider call and SHALL write no vector or quarantine.
Execution SHALL require the deterministic estimate digest and SHALL recompute its bound inputs
before the first provider call.

#### Scenario: Dry-run completes
- **WHEN** an operator evaluates a bounded horizon
- **THEN** calls equal the configured batching estimate over unique cache misses and cost uses the fixed pricing revision

#### Scenario: Execution lacks approval
- **WHEN** no matching estimate digest is supplied
- **THEN** the command exits before provider access

#### Scenario: Estimate bindings changed
- **WHEN** environment, contract, canonicalizer, pricing, mode, horizon, row bound, or cost bound differs
- **THEN** execution fails closed and requires another dry-run

### Requirement: Eligible Stable Historical Selection
The repository SHALL scan only published, public, media-ready (`legacy_ready` or `ready`) videos
with non-null `published_at`. It SHALL freeze the greatest eligible `(published_at, id)` tuple and
page ascending strictly after the checkpoint and no later than that horizon. Default mode SHALL
select missing exact-contract rows; `stale` and `force` SHALL require exact full-identity
confirmation and MUST NOT select another identity or `hash-ngram-v1` for replacement.

#### Scenario: Missing-only page is read
- **WHEN** default mode requests candidates
- **THEN** only eligible rows lacking the exact full contract are returned in stable tuple order

#### Scenario: Catalog changes during the run
- **WHEN** rows are inserted or change eligibility
- **THEN** no row beyond the horizon extends the run and persistence revalidates current state

### Requirement: Environment and Model Scoped Advisory Lock
The command SHALL acquire and hold one PostgreSQL session advisory lock derived from environment,
provider, model, revision, dimension, and canonicalizer before scanning. Failure to acquire SHALL
exit before provider or mutation work. Live semantic jobs MUST NOT acquire or wait on this lock.

#### Scenario: Another identical backfill runs
- **WHEN** the advisory lock is held
- **THEN** the second command exits with a bounded lock-conflict result

#### Scenario: The run ends or is canceled
- **WHEN** its database session closes
- **THEN** the lock is released without affecting live jobs

### Requirement: Fully Bound Atomic Checkpoint
The opaque checkpoint SHALL bind format version, environment, full provider identity,
canonicalizer, pricing revision, approved estimate digest, refresh mode, frozen horizon, run ID,
completed tuple, row/cost counters, and corruption checksum. It SHALL contain no text, credential,
URL, payload, vector, or business ID beyond the ordering cursor required for resume. Replacement
SHALL use mode 0600, file flush, atomic rename, and parent-directory flush only after a complete
durable page prefix.

#### Scenario: A page completes
- **WHEN** every row has a durable terminal page outcome
- **THEN** the checkpoint advances after the page's final tuple

#### Scenario: A page is interrupted
- **WHEN** cancellation, pause, provider failure, or persistence failure occurs
- **THEN** the prior checkpoint remains authoritative and restart replays at most one page

#### Scenario: Binding is wrong or corrupt
- **WHEN** environment, identity, canonicalizer, pricing, estimate, mode, horizon, or checksum fails
- **THEN** the command fails before provider access

### Requirement: Low Concurrency and Real-Time Job Priority
Defaults SHALL be page size 128, provider batch size at most 16, concurrency 1, and backfill QPS at
most 20% of configured provider QPS; maximum concurrency SHALL be 2. A shared PostgreSQL capacity
coordinator SHALL reserve provider/database tokens for real-time jobs first. Backfill SHALL pause
before a batch when claimable live jobs exist or oldest live backlog age exceeds five minutes.

#### Scenario: Live work is available
- **WHEN** pending/retry live jobs can run
- **THEN** backfill yields provider and database capacity without advancing its incomplete page

#### Scenario: Live backlog is healthy and surplus exists
- **WHEN** reserved live tokens are unused
- **THEN** backfill may consume only bounded surplus capacity

### Requirement: Automatic Provider Budget Database WAL and Replication Pauses
The runner SHALL sample provider QPS/token availability and bounded `Retry-After`, approved spend,
database p95 latency, WAL generation, replica replay lag, and replica byte backlog before reads,
calls, and writes. Default pause thresholds SHALL be 200 ms database p95, 64 MiB/min WAL,
30 seconds replay lag, and 256 MiB byte backlog. Bounded configuration MAY lower them. Database,
WAL, and replication pressure SHALL resume only after five healthy 10-second samples and at least a
30-second cooldown. Budget exhaustion SHALL stop as `budget_reached` and require new approval.

#### Scenario: Provider QPS is exhausted
- **WHEN** no backfill token is available or bounded `Retry-After` applies
- **THEN** the run pauses without consuming live reserved capacity

#### Scenario: Database or replication is unhealthy
- **WHEN** any accepted threshold is exceeded
- **THEN** new scan/call/write work pauses until healthy hysteresis is satisfied

#### Scenario: Approved cost is reached
- **WHEN** estimated plus actual spend reaches the budget
- **THEN** the run stops cleanly without another provider call or automatic resume

### Requirement: Deterministic Bad-Row Quarantine
Canonicalization failures and structurally invalid source rows SHALL be quarantined before provider
access by video, full contract, and source version with only a bounded reason, source-version
surrogate, ordering tuple, and timestamps. Quarantine MUST NOT contain raw/canonical text,
credentials, URLs, vectors, or provider responses. Unchanged quarantined sources SHALL be skipped
deterministically; changed source or authenticated operator clear SHALL allow reevaluation.

#### Scenario: A row is deterministically invalid
- **WHEN** canonical source cannot satisfy `semantic-text-v1`
- **THEN** no provider call occurs and one idempotent quarantine record accounts for it

#### Scenario: The source is repaired
- **WHEN** source version changes or quarantine is cleared
- **THEN** the row may be scanned again

#### Scenario: Provider contract fails
- **WHEN** authentication or response validation fails
- **THEN** the run stops or retries according to provider policy and does not quarantine otherwise valid rows

### Requirement: Privacy-Bounded Batch and Conditional Persistence
Backfill SHALL send only canonical published/public title/description text through the shared
adapter. Before persistence, it SHALL lock/re-read eligibility, recompute text hash, and apply
missing/stale/force compare-and-set rules only to the exact full identity. Concurrent live writes
SHALL win safely; identical facts SHALL be no-op writes.

#### Scenario: Source remains current
- **WHEN** eligibility and hash match generation input
- **THEN** the exact semantic fact may be conditionally persisted

#### Scenario: Source or eligibility changes
- **WHEN** text changes or video becomes ineligible
- **THEN** no stale vector is written and the row receives a bounded page outcome

#### Scenario: Live worker writes first
- **WHEN** the matching/newer exact fact already exists
- **THEN** backfill does not overwrite or churn it

### Requirement: Cancellation and Resume
SIGINT, SIGTERM, runtime expiry, provider/resource pause, and operator cancellation SHALL stop new
scheduling, cancel in-flight work, wait for goroutines, and preserve the last complete-page
checkpoint. Compatible restart SHALL resume strictly after that tuple and replay at most one page.
Paused time SHALL count toward maximum runtime.

#### Scenario: Cancellation occurs mid-page
- **WHEN** some writes completed but the page did not
- **THEN** restart safely no-ops completed facts and processes the remainder

#### Scenario: A resumable bound is reached
- **WHEN** max rows, runtime, or budget ends the run
- **THEN** a bounded stop reason and usable checkpoint are emitted

### Requirement: Zero Hash Mutation
Backfill code and repositories MUST NOT insert, update, or delete `hash-ngram-v1`. Operations SHALL
capture count and deterministic aggregate digest of all hash rows, including vector content and
timestamps, before first write and after completion. Acceptance SHALL require identical values and
zero hash mutation metrics/tests.

#### Scenario: Semantic facts are written
- **WHEN** backfill completes any number of rows
- **THEN** every pre-existing hash row remains byte-and-timestamp unchanged and no new hash row is created by backfill

#### Scenario: Hash verification differs
- **WHEN** count or digest changes
- **THEN** acceptance fails regardless of semantic coverage

### Requirement: Historical Coverage Acceptance
For the active frozen horizon, acceptance SHALL require at least 99.5% exact-contract coverage,
every remaining currently eligible row represented by one deterministic quarantine so
`covered + quarantined = 100%`, actual provider cost within approved budget, zero hash changes, and
no unresolved checkpoint/lock/resource incident. Passing this gate MUST NOT enable recommendation
consumption.

#### Scenario: Coverage gate passes
- **WHEN** all thresholds and accounting hold
- **THEN** historical producer coverage is accepted

#### Scenario: An eligible row is unexplained
- **WHEN** it has neither exact semantic fact nor deterministic quarantine
- **THEN** acceptance fails

### Requirement: Safe Metrics Summary and Operator Boundary
The command SHALL expose bounded metrics and summaries for estimates, provider items/calls/units/
cost, cache, pages, outcomes, quarantine, checkpoint, advisory lock, pause reasons/duration,
resource samples, live-priority yielding, and coverage. They MUST NOT include IDs, text, hashes,
provider/model strings, credentials, URLs, paths, checkpoint tokens, payloads, raw errors, or retry
numbers. The command SHALL require PostgreSQL and the configured provider only, with no public port,
Kafka, or Redis dependency.

#### Scenario: A run pauses or ends
- **WHEN** it reaches a stop condition
- **THEN** exactly one bounded final summary reports estimate/actual counts, cost, coverage, quarantine, pause time, and safe stop class

#### Scenario: Strict validation runs
- **WHEN** tests and `openspec validate --all --strict` complete
- **THEN** no live Kafka, retrieval, profile, ranking, Web, training, or local model behavior is added
