## ADDED Requirements

### Requirement: Prerequisite and Fixed Provider Contract Gate
Implementation SHALL begin only after recommendation-roadmap steps 1–5 and
`migrate-video-workflows-to-kafka` are complete and archived. Live semantic work SHALL consume the
provider-neutral adapter and one fixed provider/model/revision/dimension/`semantic-text-v1` tuple
without hosting or training a model.

#### Scenario: A prerequisite is incomplete
- **WHEN** any required roadmap, adapter, or Kafka change remains active
- **THEN** implementation tasks in this change do not begin

#### Scenario: The contract is available
- **WHEN** all prerequisites are archived
- **THEN** jobs and vectors use the accepted complete identity and narrow Go port

### Requirement: Hash-Safe Durable Kafka Handoff
The accepted publication/hash intake SHALL validate the record, preserve or create
`hash-ngram-v1`, compute the canonical text hash, and idempotently upsert a durable semantic job
before returning handler success. It MUST NOT call the Embedding API. Publication and Feed paths
MUST NOT wait for semantic handoff or execution.

#### Scenario: A valid new publication arrives
- **WHEN** its hash fact is durable and semantic job upsert commits
- **THEN** the Kafka handler succeeds and the record becomes eligible for offset commit

#### Scenario: Hash or job persistence fails
- **WHEN** either durable operation fails
- **THEN** handler success is not returned and no provider call occurs

#### Scenario: The same event is redelivered
- **WHEN** provider identity and text hash are unchanged
- **THEN** hash and job handoff are idempotent

#### Scenario: Public text changes
- **WHEN** a later accepted event has a different canonical text hash
- **THEN** job generation increments, state resets to `pending`, and older leases are fenced

### Requirement: Source Retry and DLQ Commit Boundaries
Kafka source, retry, and DLQ handling SHALL follow the accepted backbone. A source or retry record
may commit after durable semantic handoff. A source offset may commit after acknowledged retry
publication; a retry offset may commit after acknowledged next-retry or DLQ publication; a
deterministic poison record may commit only after acknowledged DLQ publication. Failure to publish
retry or DLQ SHALL leave the current record uncommitted. Provider execution failures MUST NOT
publish Kafka retries/DLQ records or affect prior commits.

#### Scenario: Durable handoff succeeds
- **WHEN** a source or retry record creates/confirms the job
- **THEN** that record may commit without waiting for provider execution

#### Scenario: Transient handoff failure is routed
- **WHEN** the backbone publishes a retry record successfully
- **THEN** the source may commit and the retry record owns the next handoff attempt

#### Scenario: Poison input is routed
- **WHEN** a bounded DLQ record is acknowledged
- **THEN** the source/retry record may commit without creating semantic work

#### Scenario: Provider later fails
- **WHEN** a leased job times out or is throttled
- **THEN** PostgreSQL retry state changes while Kafka offsets and topics remain unchanged

### Requirement: Explicit Durable Semantic Job States
One job per
`(video_id, provider, model, revision, dimension, canonicalizer)` SHALL store the complete identity,
text hash, `pending|leased|retry|succeeded|terminal` state, generation, attempts, availability,
lease owner/token/expiry, bounded error class, provider retry-after time, and timestamps. It SHALL
store no raw text or credentials.

#### Scenario: A job is created
- **WHEN** durable handoff first succeeds
- **THEN** the job is `pending` and immediately claimable

#### Scenario: A retryable failure occurs
- **WHEN** provider/network/circuit/quota classification is temporary
- **THEN** the job becomes `retry` with bounded `available_at`

#### Scenario: A deterministic failure occurs
- **WHEN** source input, eligibility, authentication, model, or response contract is terminal
- **THEN** the job becomes `terminal` with only a bounded class

#### Scenario: A semantic fact is durably written
- **WHEN** the active fenced lease persists the expected vector
- **THEN** the job becomes `succeeded`

### Requirement: Stable Claims Lease Fencing and Reclaim
Claims SHALL be bounded and stably ordered using `FOR UPDATE SKIP LOCKED`. Every heartbeat,
retry/terminal release, vector write, and completion SHALL compare job generation, lease token, and
expected text hash. Expired leases SHALL be reclaimable.

#### Scenario: A worker claims a job
- **WHEN** a pending/retry row is available
- **THEN** it becomes `leased` with a new owner/token and bounded expiry

#### Scenario: A lease expires
- **WHEN** the owner stops heartbeating
- **THEN** another worker may reclaim the job safely

#### Scenario: A stale worker completes
- **WHEN** generation, token, or text hash no longer matches
- **THEN** neither vector nor job state changes

### Requirement: Eligibility Revalidation and Privacy-Bounded Provider Call
A leased worker SHALL re-read the video, require current published/public eligibility, recompute
`semantic-text-v1`, and compare the job text hash before provider access. The provider SHALL receive
only canonical title/description text and fixed model selection, with no Frux IDs, request IDs,
behavior data, tokens, URLs, or private/draft content.

#### Scenario: Source remains current
- **WHEN** eligibility and text hash match the lease
- **THEN** the worker may call the provider through the narrow adapter

#### Scenario: Source changed
- **WHEN** current canonical text hash differs
- **THEN** the job generation refreshes to current text and no stale provider request/result is used

#### Scenario: Video is no longer eligible
- **WHEN** it is private, unpublished, deleted, or otherwise ineligible
- **THEN** no provider call occurs and the job is terminally classified

### Requirement: Database-Owned Retry Backoff and Manual Requeue
Retryable jobs SHALL use delays of 5 seconds, 30 seconds, 2 minutes, 10 minutes, 30 minutes, then
exponential delay capped at 2 hours. A valid provider `Retry-After` MAY raise but MUST NOT exceed
that cap. Operators SHALL be able to requeue selected `retry` or `terminal` jobs through an
authenticated non-public command. Requeue SHALL increment generation and clear active lease/error
timing without silently changing identity or text hash.

#### Scenario: Provider is unavailable
- **WHEN** a retryable result is returned
- **THEN** the job is released with deterministic jitter and bounded availability

#### Scenario: Retry-After is returned
- **WHEN** its valid bounded delay exceeds normal backoff
- **THEN** the later bounded time is used

#### Scenario: An operator repairs configuration
- **WHEN** selected terminal jobs are manually requeued
- **THEN** they return to `pending` under a new generation with audit-visible counts

### Requirement: Complete Provenance and Conditional Side-by-Side Persistence
Semantic rows SHALL coexist with `hash-ngram-v1` and store video ID, provider, model, revision,
dimension, canonicalizer, text hash, finite L2-normalized vector, and timestamps. Credentials and
raw canonical text MUST NOT be stored. Persistence SHALL require the active lease generation/token
and matching text hash; identical facts SHALL be no-op writes.

#### Scenario: Hash and semantic generation succeed
- **WHEN** one video has both representations
- **THEN** the hash row remains unchanged beside one exact-contract semantic row

#### Scenario: The same fact is repeated
- **WHEN** identity, text hash, dimension, and vector are identical
- **THEN** persistence does not churn the row timestamp

#### Scenario: A new contract is introduced
- **WHEN** provider, model, revision, dimension, or canonicalizer changes
- **THEN** it uses separate identity and requires rebuild rather than overwriting old provenance

### Requirement: Cleanup Without Data Loss
Succeeded jobs MAY be deleted after seven days only if their exact matching semantic fact remains
durable. Terminal jobs SHALL be retained at least 30 days. Pending, leased, and retry jobs MUST NOT
be age-deleted. Cleanup MUST NOT delete vectors, hash rows, or another semantic identity.

#### Scenario: A succeeded job reaches retention
- **WHEN** the exact semantic fact still exists
- **THEN** bounded cleanup may remove only the completed job row

#### Scenario: Active backlog exists
- **WHEN** jobs are pending, leased, or retrying
- **THEN** cleanup leaves them intact

### Requirement: Provider Failure Isolation and Permanent Hash Fallback
Provider unavailability, throttling, quota/budget closure, authentication failure, contract mismatch,
or semantic backlog MUST NOT block API/Feed startup, publication, Kafka hash progress, fanout,
media, actions, views, or other workers. `hash-ngram-v1` SHALL remain permanent and SHALL NOT be
rewritten or removed by semantic work.

#### Scenario: Provider gate is closed
- **WHEN** a replica cannot call the provider
- **THEN** it stops semantic claims or releases jobs while unrelated work and hash fallback continue

#### Scenario: Semantic execution is disabled
- **WHEN** claims are administratively disabled
- **THEN** durable handoff continues and jobs remain visible for later execution

### Requirement: Live Semantic SLA Coverage and Observability
For videos that remain published/public under the active contract, at least 95% SHALL succeed within
15 minutes of job creation and at least 99% within 24 hours. Hash coverage SHALL remain 100% for
accepted hash-eligible publications. Rollout acceptance SHALL require three consecutive 24-hour
windows with at least 99% exact-contract coverage, terminal rate no more than 0.1%, no unexplained
uncovered rows, and no hash/unrelated-worker regression.

Metrics SHALL cover job count/oldest age by state, claims, leases, retries, requeues, cleanup,
provider/circuit/cost/quota results, semantic coverage, SLA buckets, and hash coverage with bounded
labels and no IDs, text, hashes, provider/model strings, credentials, URLs, raw errors, or retry
numbers.

#### Scenario: New-video coverage is sampled
- **WHEN** an eligible publication cohort ages
- **THEN** 15-minute and 24-hour success coverage and uncovered bounded classes are reported

#### Scenario: The rollout gate passes
- **WHEN** all thresholds hold for three consecutive windows
- **THEN** live semantic completeness is accepted without enabling recommendation consumption

#### Scenario: Provider outage breaches SLA
- **WHEN** semantic completion falls below a threshold
- **THEN** alerts fire while publish, Feed, Kafka, and hash behavior continue

### Requirement: Live-Only Boundary
This change SHALL add no historical scan/backfill, public API, online inference, provider-hosted
model runtime, pgvector/ANN, semantic profile, retrieval, ranking/policy consumption, or training.
Historical coverage SHALL remain owned by `backfill-semantic-video-embeddings`.

#### Scenario: An old video lacks semantic output
- **WHEN** it emits no accepted new publication event
- **THEN** this change does not scan or enqueue it

#### Scenario: Strict validation runs
- **WHEN** implementation and `openspec validate --all --strict` complete
- **THEN** durable live jobs exist without any synchronous provider dependency or recommendation behavior change
