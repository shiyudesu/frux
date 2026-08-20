## Context

`add-semantic-embedding-service` now defines a provider-neutral Go port for a managed external
Embedding API. It pins a provider/model/revision/dimension/canonicalizer tuple, sends only
`semantic-text-v1` public title/description text, validates vectors, and supplies bounded
rate/cost/quota/circuit behavior. It deliberately performs no online product-path calls.

`migrate-video-workflows-to-kafka` supplies retained `frux.video.published.v1` delivery and the
accepted source/retry/DLQ backbone. PostgreSQL remains authoritative for long-running work.
Semantic generation must therefore be durably handed off during publication/hash intake and
executed later from PostgreSQL jobs.

## Goals / Non-Goals

**Goals:**

- Generate the fixed semantic contract for newly published public videos.
- Preserve `hash-ngram-v1` before and throughout semantic handoff/execution.
- Make Kafka commit safe after durable job handoff without waiting for the provider.
- Define explicit job state, lease fencing, retries, terminal outcomes, requeue, and cleanup.
- Persist complete provider/model/revision/dimension/canonicalizer/text-hash provenance.
- Keep provider failures isolated from publish, Feed, hash, Kafka progress, and other workers.
- Establish measurable new-video availability SLA and coverage rollout gates.

**Non-Goals:**

- Historical scanning or backfill.
- Provider calls from HTTP, publication, Feed, ranking, profile, or Kafka handlers.
- Hosting/training a model or adding Python/PyTorch/model containers.
- pgvector/ANN, semantic retrieval, user profiles, ranking features, or policy changes.
- Replacing `hash-ngram-v1`.

## Decisions

### 1. Gate implementation on roadmap, adapter, and Kafka prerequisites

Implementation starts only after recommendation-roadmap steps 1–5 and
`migrate-video-workflows-to-kafka` are complete and archived. This change consumes the accepted
provider adapter and Kafka backbone; it does not recreate either contract.

### 2. Keep Kafka intake to hash safety plus durable semantic handoff

The accepted publication/hash consumer validates the event and video key, preserves or creates
`hash-ngram-v1`, computes `semantic-text-v1` and its hash from the event's bounded public text, and
idempotently upserts a semantic job. The handler never calls the Embedding API.

The success boundary is:

1. validate the accepted publication record;
2. confirm the hash fact is durable;
3. durably insert or refresh the semantic job with the complete pinned contract and text hash;
4. return handler success so the current Kafka record may commit.

A duplicate event with the same identity and text hash is a no-op. A changed text hash increments
the job generation, resets it to `pending`, and fences any older lease. If hash or job persistence
fails, handler success is not returned.

The publish transaction and Feed consumer never wait for semantic handoff or provider execution.
If the semantic consumer is delayed, published videos remain available through normal behavior and
`hash-ngram-v1`.

### 3. Make source, retry, and DLQ commit boundaries explicit

For a source or retry record that reaches durable handoff, its offset may commit immediately after
the database transaction succeeds. A transient handoff failure follows the accepted Kafka backbone:

- a source offset commits only after the retry record is durably acknowledged by Kafka;
- a retry offset commits only after semantic handoff succeeds or the next retry/DLQ publication is
  durably acknowledged;
- a deterministic invalid/poison source or retry record commits only after its bounded DLQ record is
  durably acknowledged;
- failure to publish retry or DLQ leaves the current record uncommitted.

Redelivery is safe because hash facts and semantic jobs have stable identities. Embedding API
timeout, throttling, quota, outage, or contract failure happens only after Kafka handoff; it never
publishes Kafka retries/DLQ records and never changes an already accepted source/retry commit.

### 4. Persist an explicit fenced job state machine

One job is identified by
`(video_id, provider, model, revision, dimension, canonicalizer)`. It stores the full identity,
canonical text hash, state, generation, attempts, `available_at`, lease owner/token/expiry,
provider retry-after time, bounded error class, source event metadata needed for idempotency, and
created/updated/completed timestamps. It stores no raw text or credential.

States are:

- `pending`: durable and immediately claimable;
- `leased`: owned temporarily by one worker generation/token;
- `retry`: retryable and claimable at `available_at`;
- `succeeded`: matching semantic fact is durable;
- `terminal`: deterministic input, eligibility, authentication, contract, or operator-actionable
  failure requires review/requeue.

Claims use stable priority/order and `FOR UPDATE SKIP LOCKED`. Every heartbeat, retry release,
terminal mark, semantic write, and completion compares video identity, job generation, lease token,
and expected text hash. Expired leases are reclaimable. A stale worker cannot write or complete a
newer generation.

### 5. Revalidate source and invoke the provider only under a lease

After claiming, the worker locks/re-reads the video source of truth. It verifies that the video is
still published and public, canonicalizes current title/description with `semantic-text-v1`, and
requires the current hash to match the job. Ineligible or deterministic bad source becomes
`terminal`; changed source refreshes the job generation to the current hash without calling the
provider.

Only then does the worker call the narrow `SemanticEmbedder`. The provider receives canonical text
only, with no Frux IDs or metadata. Successful output is conditionally persisted and the job marked
`succeeded` in a fenced transaction. A cache hit follows the same fencing and validation.

### 6. Use database-owned retry timing and operator requeue

Retryable network, timeout, `429`, `5xx`, open-circuit, and temporary quota failures move the job to
`retry`. Delays are 5 seconds, 30 seconds, 2 minutes, 10 minutes, 30 minutes, then exponential up to
2 hours. A valid provider `Retry-After` may raise, but never exceed, the 2-hour delay. Jitter is
deterministic per job identity and attempt to avoid synchronized retries without making tests
non-repeatable.

Authentication/authorization, unknown model/revision, response contract mismatch, deterministic
invalid text, and permanently ineligible source move to `terminal`. Operators use an authenticated
CLI/administrative command, not a public HTTP endpoint, to inspect bounded classes and requeue
selected terminal/retry jobs. Requeue increments generation, clears lease/error timing, preserves
attempt history/audit fields, and never changes the pinned contract or text hash implicitly.

### 7. Clean completed state without losing facts or evidence

`succeeded` jobs may be deleted after seven days only when the exact matching semantic row still
exists. `terminal` jobs are retained at least 30 days for bounded operational review. `pending`,
`leased`, and `retry` jobs are never age-deleted. Cleanup is bounded, leased/fenced, observable, and
does not delete semantic vectors, hash rows, or another contract identity.

### 8. Persist complete semantic provenance side by side

The existing embedding store keeps one semantic row per full identity beside
`hash-ngram-v1`. Each semantic row stores video ID, provider, model, revision, dimension,
canonicalizer, text hash, finite normalized vector, and timestamps. Credentials and raw canonical
text are absent.

Persistence requires the active job generation/token and matching text hash. An identical fact is a
no-op that does not churn timestamps. A new provider/model/revision/dimension/canonicalizer uses a
separate identity and rebuild; it cannot reinterpret an old row.

### 9. Isolate provider and semantic worker failure

Semantic execution has independent claim concurrency, QPS, database batch, timeout, cost, and quota
bounds. A replica claims only while its local provider gate is usable and budget permits. Disabled,
unready, or over-budget replicas leave durable jobs for later/other replicas.

API/Feed startup, publication, Kafka hash processing, fanout, media, actions, views, and other
workers do not depend on provider readiness. `hash-ngram-v1` remains the permanent fallback and is
never deleted or rewritten by semantic execution.

### 10. Define live availability SLA and rollout gates

For videos that remain published/public under the active contract:

- at least 95% SHALL reach `succeeded` within 15 minutes of durable job creation;
- at least 99% SHALL reach `succeeded` within 24 hours;
- hash coverage SHALL remain 100% for accepted hash-eligible publications.

The SLA is measured continuously; provider outages may breach it and alert, but cannot block
product traffic. Rollout acceptance requires three consecutive 24-hour windows with at least 99%
exact-contract coverage for eligible new publications, no unexplained uncovered rows, terminal
rate at or below 0.1%, and no regression in hash coverage or unrelated worker lag. This gate
authorizes semantic production completeness only; it does not enable recommendation consumption.

Metrics cover job count/oldest age by state, claim/lease outcomes, retry delay, manual requeue,
cleanup, provider/circuit/cost/quota outcomes, semantic coverage, SLA age buckets, and hash coverage.
Labels remain bounded and exclude IDs, text, hashes, provider/model strings, credentials, URLs, raw
errors, and retry numbers.

## Risks / Trade-offs

- [Provider outage creates backlog] -> Database retries, bounded gates, SLA alerts, and permanent
  hash fallback isolate product traffic.
- [Kafka redelivery repeats handoff] -> Stable hash/job identity and changed-text generations make
  it idempotent.
- [A stale worker writes obsolete text] -> Generation, lease token, and text-hash fencing protect
  vector and completion writes.
- [Terminal misconfiguration strands jobs] -> Bounded terminal classes, alerts, and audited manual
  requeue support recovery.
- [Semantic cost grows unexpectedly] -> QPS/quota/budget gates and cost metrics stop claims without
  affecting hash/publication.

## Migration Plan

1. Verify all prerequisites are archived and the provider contract is fixed.
2. Add full-identity semantic job/vector persistence and migrations.
3. Deploy Kafka handoff with execution disabled; verify hash and job idempotency plus commit
   boundaries.
4. Enable low-concurrency semantic claims, provider gates, retries, requeue, cleanup, and metrics.
5. Observe the SLA and three-window coverage gate while recommendation behavior remains unchanged.
6. Roll back by disabling semantic claims first. Durable jobs, semantic facts, and hash rows remain
   intact; Kafka handoff may continue safely.

## Open Questions

None. Contract identity, privacy, handoff, commit boundaries, job states, fencing, retry/requeue,
cleanup, SLA, coverage, and fallback behavior are fixed.
