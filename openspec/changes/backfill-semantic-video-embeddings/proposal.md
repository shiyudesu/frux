## Why

Historical public videos need the same managed-provider semantic contract as newly published
videos. A catalog repair can be expensive and can stress the provider, PostgreSQL, WAL, and
replicas, so it must estimate work before execution, yield to real-time semantic jobs, pause
automatically under safety thresholds, and resume without changing `hash-ngram-v1`.

## What Changes

- Reuse the exact provider/model/revision/dimension/`semantic-text-v1` contract, privacy boundary,
  validation, cache, cost/quota, and conditional persistence from the two predecessor changes.
- Require a dry-run estimate before execution, reporting eligible candidates, unique text hashes,
  cache reuse, expected provider items/API calls, billable units, and estimated cost under a fixed
  pricing revision and frozen catalog horizon.
- Add a one-shot, cancellable/resumable operator backfill protected by an
  environment-and-model-scoped PostgreSQL advisory lock.
- Bind checkpoints to environment, full provider contract, canonicalizer, pricing revision,
  refresh mode, approved estimate, frozen horizon, and completed tuple.
- Default to concurrency 1 and a small provider batch/QPS share. Real-time durable semantic jobs
  always receive provider and database capacity before backfill.
- Automatically pause or stop on provider QPS/`Retry-After`, budget, database latency, WAL rate,
  replication lag, or replication byte backlog thresholds, and resume only after bounded healthy
  hysteresis where safe.
- Deterministically quarantine bad source rows by bounded reason and source version without sending
  them to the provider; allow repair/requeue when the source changes or an operator clears them.
- Preserve atomic page-prefix checkpointing so cancellation, pause, failure, and restart replay at
  most one bounded page.
- Require acceptance proof that `hash-ngram-v1` rows changed zero times and that exact-contract
  historical coverage reaches at least 99.5%, with every remaining eligible row accounted for by a
  deterministic quarantine.

## Capabilities

### New Capabilities

- `semantic-embedding-backfill`: Defines estimated, model-locked, resource-aware, resumable
  historical generation using the shared external Embedding API contract and strict hash/coverage
  acceptance.

### Modified Capabilities

None.

## Impact

- Adds a Go operator command/runner, stable historical scan, advisory lock, estimate/checkpoint and
  quarantine persistence, shared capacity coordination, metrics, tests, container entrypoint, and
  runbook updates.
- Depends on completed `add-semantic-embedding-service` and
  `integrate-semantic-video-embeddings`.
- Reads PostgreSQL and calls the configured managed Embedding API only from the backfill process.
- Adds no public API, Web behavior, Kafka consumer, Redis dependency, local model runtime,
  vector retrieval, recommendation consumption, or training.
