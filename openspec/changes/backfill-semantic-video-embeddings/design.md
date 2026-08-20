## Context

The live integration creates durable semantic jobs for new public videos and persists vectors under
the complete provider/model/revision/dimension/`semantic-text-v1` identity. Historical videos and
missed cohorts still need repair. The same external API can be rate-limited and billable, while
backfill database scans and writes can increase query latency, WAL generation, and replica lag.

The backfill must use the shared canonicalizer, privacy boundary, cache, adapter, response
validation, cost/quota controls, and conditional vector repository. It must never mutate
`hash-ngram-v1`, outrank live jobs, or make recommendation components consume semantic rows.

## Goals / Non-Goals

**Goals:**

- Estimate candidate count, provider calls, billable units, and cost before execution.
- Scan a frozen eligible historical horizon deterministically.
- Enforce one active run per environment and semantic contract.
- Bind resumability to the exact environment/model/canonicalizer/pricing decision.
- Use low default concurrency and only surplus capacity after real-time jobs.
- Auto-pause on provider, budget, database, WAL, and replication pressure.
- Quarantine deterministic bad rows without provider calls or raw text persistence.
- Cancel and resume safely with bounded replay.
- Prove zero hash-row changes and accepted semantic coverage.

**Non-Goals:**

- Changing live Kafka handoff, live retry state, or publication behavior.
- Running a scheduler, distributed queue, public/admin HTTP API, or Web UI.
- Hosting/training a model or adding Python/PyTorch/model containers.
- pgvector/ANN, semantic retrieval, profiles, ranking/policy changes, or training.
- Deleting or refreshing arbitrary models or `hash-ngram-v1`.

## Decisions

### 1. Use a dedicated one-shot Go command

`cmd/backfill-semantic-video-embeddings` opens PostgreSQL, loads the same fixed provider contract,
validates the adapter and pricing revision, acquires the advisory lock, runs a dry-run or approved
execution, emits one final summary, and exits. It does not initialize Kafka or Redis and does not
run schema migration.

Provider calls occur only inside this resumable backfill process. No API/Feed/publication handler
can invoke the runner.

### 2. Require dry-run estimation before billable execution

A dry-run captures a stable eligible horizon and performs the same candidate scan, canonicalization,
text-hash deduplication, exact-contract row classification, cache lookup, and pricing estimation as
execution, but makes no provider call and writes no vector or quarantine.

It reports:

- eligible/scanned candidate count by missing, stale, current, and deterministic-bad class;
- unique canonical text hashes and validated cache hits;
- expected provider items and API calls using the configured batch size;
- estimated provider billable units and cost under the configured pricing revision;
- approved maximum rows, runtime, QPS, and cost;
- environment, complete semantic identity, refresh mode, and frozen horizon.

The summary produces a deterministic estimate digest. A non-dry run requires that digest and
recomputes the bound estimate before the first provider call. A mismatch in environment, identity,
canonicalizer, pricing revision, refresh mode, horizon, row bound, or cost bound fails closed. The
run never exceeds approved rows or cost; newly discovered cost pressure stops cleanly.

### 3. Freeze a stable eligible horizon and page by tuple

Eligible videos are published, public, media-ready (`legacy_ready` or `ready`), and have non-null
`published_at`. A fresh estimate captures the greatest eligible `(published_at, id)` tuple in the
same snapshot as its initial classification. Pages are ordered ascending by that tuple, strictly
after the checkpoint and no later than the horizon.

Default mode selects rows missing the exact full contract. `stale` includes exact-contract rows
whose stored text hash differs; `force` includes all eligible exact-contract rows. Stale/force
requires exact full-identity confirmation. Other semantic identities and `hash-ngram-v1` never
participate in replacement decisions.

### 4. Serialize runs with an environment-and-model advisory lock

Before scanning, the command obtains a PostgreSQL session advisory lock derived from
`(environment, provider, model, revision, dimension, canonicalizer)`. Only one run for that exact
scope may proceed. Failure to acquire exits before provider or mutation work. The session holds the
lock through pauses and releases it on completion, cancellation, or failure.

This prevents duplicate operator runs for one contract. Live jobs are not blocked by the advisory
lock and remain higher priority.

### 5. Bind checkpoints to the approved execution contract

The opaque versioned checkpoint contains:

- environment;
- provider, model, revision, dimension, and canonicalizer;
- pricing revision and approved estimate digest;
- refresh mode and frozen horizon;
- last fully completed `(published_at, id)` tuple;
- run ID, counters needed for row/cost limits, and corruption checksum.

The checkpoint is mode 0600 and atomically replaced only after a complete durable page prefix:
write sibling, flush file, rename, flush directory. It never contains raw/canonical text,
credentials, URLs, provider payloads, or vectors. Any wrong environment/identity/canonicalizer/
pricing/mode/horizon/estimate or corrupt checkpoint fails before provider access.

### 6. Use low defaults and give real-time jobs strict priority

Defaults are page size 128, provider batch size no greater than 16, concurrency 1, and backfill QPS
no greater than 20% of configured provider QPS. Maximum concurrency is 2.

A shared PostgreSQL capacity coordinator reserves tokens for real-time semantic jobs first.
Backfill receives only surplus provider-call and database-write tokens. Before every batch it checks
available live `pending`/`retry` jobs and oldest live backlog age. If live work is available or its
oldest age exceeds five minutes, backfill pauses without advancing the current page checkpoint.
Backfill cannot consume reserved live budget even when it could issue a provider request directly.

### 7. Auto-pause on provider, budget, database, WAL, and replication pressure

The runner samples bounded controls before page reads, provider batches, and writes:

- provider token/QPS availability and bounded `Retry-After`;
- estimated plus actual run spend against approved budget;
- database p95 operation latency, default pause threshold 200 ms;
- primary WAL generation, default pause threshold 64 MiB/min;
- replica replay lag, default pause threshold 30 seconds;
- replica byte backlog, default pause threshold 256 MiB.

Thresholds are bounded configuration and recorded in the approved estimate. QPS/`Retry-After`,
database, WAL, and replication pressure enter `paused` operation without losing lock/checkpoint.
Database/WAL/replication resume only after five consecutive healthy 10-second samples and a
minimum 30-second cooldown. Budget exhaustion stops cleanly as `budget_reached` and requires a new
dry-run/approval; it does not auto-resume. Paused time counts toward maximum runtime.

### 8. Quarantine deterministic bad rows

Canonicalization failures and structurally invalid source rows are quarantined before provider
access. The quarantine key is
`(video_id, provider, model, revision, dimension, canonicalizer, source_version)` and stores only a
bounded reason code, source `updated_at`/hash surrogate, ordering tuple, and timestamps. It stores no
raw text, credential, URL, vector, or provider response.

The same unchanged source is deterministically skipped on resume and included in coverage
accounting. A changed source version becomes eligible for reevaluation. An operator command may
clear selected quarantines after repair. Provider authentication, contract, or outage failures are
run-level/job retry concerns, not per-row quarantine reasons.

### 9. Revalidate and conditionally persist through shared primitives

Work items use the shared `semantic-text-v1` canonicalizer and cache. Provider payloads contain only
canonical public title/description text and fixed model selection. Before saving, the repository
locks/re-reads the video, rechecks published/public/media-ready eligibility, recomputes the hash,
and applies missing/stale/force compare-and-set rules to only the exact full semantic identity.

A source change or ineligibility becomes a durable page outcome without a stale write. Concurrent
live completion wins safely. Identical facts are no-op writes with unchanged timestamps. No query or
write path updates `hash-ngram-v1` or another semantic identity.

### 10. Cancel and resume at complete page prefixes

SIGINT, SIGTERM, runtime expiry, provider failure, pressure pause, or operator cancellation stops new
scheduling and cancels in-flight calls. The runner waits for goroutines, persists only already
fenced item outcomes, and leaves the previous complete-page checkpoint authoritative if the page is
incomplete. Restart replays at most one bounded page.

Normal resumable stop reasons include `horizon_complete`, `max_rows_reached`,
`max_runtime_reached`, `budget_reached`, and `canceled`. A new run can continue only with compatible
checkpoint/estimate bindings or a new dry-run and horizon.

### 11. Define acceptance around coverage and zero hash changes

Before the first write, operations capture a count and deterministic aggregate digest of all
`hash-ngram-v1` rows, including vector content and timestamps. The same query runs after completion.
Acceptance requires identical count/digest and tests/audit metrics showing zero hash insert/update/
delete operations by the backfill.

Historical acceptance for the active frozen horizon requires:

- exact-contract semantic coverage of at least 99.5% of currently eligible rows;
- every remaining eligible row represented by one deterministic quarantine, so
  `covered + quarantined = 100%` with no unexplained gap;
- actual provider cost at or below the approved budget;
- no hash digest/count change;
- no unresolved checkpoint corruption, advisory-lock conflict, or resource-pressure incident.

This gate validates stored producer coverage only and does not enable recommendation use.

Metrics and summaries cover estimates, calls/items/cost, cache, pages, outcomes, quarantine,
checkpoint, advisory lock, pause reason/duration, resource samples, live-priority yielding, and
coverage. Labels exclude IDs, text, hashes, provider/model strings, credentials, URLs, paths,
checkpoint tokens, raw errors, and retry numbers.

## Risks / Trade-offs

- [Estimate differs from actual usage] -> Recompute before calls, cap approved rows/cost, and stop
  at budget.
- [Backfill harms live freshness] -> Shared priority capacity, low defaults, and five-minute live
  backlog yielding.
- [Database load increases WAL/replica lag] -> Automatic pressure pause with healthy hysteresis.
- [Bad source rows stall completion] -> Deterministic privacy-safe quarantine and complete
  coverage accounting.
- [Cancellation repeats provider calls] -> Page-prefix checkpointing, cache reuse, and idempotent
  conditional persistence bound replay.

## Migration Plan

1. Complete and validate both predecessor changes.
2. Add estimate, advisory-lock, checkpoint, quarantine, capacity, and stable-scan contracts.
3. Add the low-concurrency runner and conditional repository integration.
4. Run dry-run, review calls/units/cost/resource thresholds, and approve the estimate digest.
5. Run a small bounded execution, cancel/resume it, then expand only while live and resource gates
   remain healthy.
6. Verify 99.5% coverage, complete quarantine accounting, approved cost, and zero hash changes.
7. Rollback by canceling the command; checkpoint, quarantine, semantic facts, and hash facts remain
   valid.

## Open Questions

None. Estimation, advisory locking, bindings, priority, pressure gates, quarantine, resumability,
hash invariance, and acceptance thresholds are fixed.
