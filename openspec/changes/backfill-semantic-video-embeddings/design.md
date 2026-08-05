## Context

The active `add-semantic-embedding-service` change defines the fixed authenticated semantic API. The narrowed `integrate-semantic-video-embeddings` change defines the Go-side model identity, canonical title/description hashing, bounded validated client, finite 384-component vector construction, conditional `(video_id, model)` persistence, and live-event coverage metrics. It intentionally excludes historical scans and names this change as their future owner.

Historical catalog repair has different failure and safety requirements from live RabbitMQ delivery. It must operate on PostgreSQL source-of-truth facts, tolerate interruption and replay, avoid unbounded service/database load, and never rewrite `hash-ngram-v1` or another model. The operator entrypoint must also handle videos whose title, description, visibility, lifecycle, or media readiness changes between candidate selection and persistence.

This proposal depends explicitly on both predecessor changes. Implementation must not begin until their fixed service and narrowed integration contracts are available and strictly validated.

## Goals / Non-Goals

**Goals:**

- Provide a one-shot operator command for bounded, deterministic, resumable historical semantic embedding coverage.
- Reuse the exact semantic model/client/canonicalization/vector/repository primitives owned by the predecessor changes.
- Select only published, public, media-ready videos and persist only when the selected source is still current and eligible.
- Make default missing-row processing and guarded stale/forced exact-model refresh safe under retries, cancellation, concurrent live processing, and multiple operator attempts.
- Provide dry-run, checkpoint, row/runtime, concurrency, batch, retry, metrics, progress, container, documentation, and test contracts that are ready to implement.

**Non-Goals:**

- Changing live video-published event handling, RabbitMQ topology, or live retry behavior.
- Adding pgvector, ANN indexes, vector retrieval, profile rebuilds, recommendation providers, ranking/policy fields, or training.
- Selecting arbitrary models, refreshing all models, deleting embeddings, or replacing `hash-ngram-v1`.
- Adding a public/admin HTTP API, scheduler, recurring job, Web UI, or distributed work queue.

## Decisions

### 1. Add a dedicated one-shot Go command over the shared integration primitives

Add `cmd/backfill-semantic-video-embeddings` as a separate binary. It loads the existing database and semantic integration configuration, opens PostgreSQL, validates the exact service metadata once, composes an application-owned backfill runner, optionally starts a bounded metrics endpoint, runs until a configured stop condition, writes a final summary, and exits.

The command does not initialize Redis or RabbitMQ and does not run schema migration. It requires the schema and semantic integration delivered by its dependencies. This keeps an operator repair tool isolated from continuously running workers and prevents a backfill failure from affecting live consumers.

The API image will build and copy the binary. Compose will add a manually invoked profile/service entrypoint with PostgreSQL and `semantic-embedding` dependencies, no public port, and a mounted checkpoint directory. Operators may also run the binary directly from `apps/api`.

Alternative considered: add a mode to `cmd/worker`. Rejected because a one-shot catalog mutation should not share lifecycle, cancellation, required Redis/RabbitMQ dependencies, or automatic startup with live workers.

### 2. Use fixed bounded options with no unlimited mode

The command accepts flags, with configuration-file defaults, for:

- `--page-size`: default 256, range 1–1,000;
- `--batch-size`: default 32, range 1–32 and no greater than the dependent service limit;
- `--concurrency`: default 2, range 1–2;
- `--max-rows`: default 10,000, range 1–1,000,000;
- `--max-runtime`: default 30 minutes, range 1 minute–24 hours;
- `--checkpoint-file`: required for non-dry runs;
- `--dry-run`;
- `--refresh`: `none` (default), `stale`, or `force`;
- `--confirm-model`: empty by default and required to equal `semantic-minilm-l12-v2@e8f8c211226b894f` for `stale` or `force`;
- progress interval: default 10 seconds, range 1 second–5 minutes;
- metrics listen address: optional, with the Compose entrypoint using internal-only `:9092`.

`max-rows` counts rows returned by the stable catalog scan, including same-hash rows inspected in refresh modes. There is no zero, negative, or “unlimited” value. Effective batch size is also clamped by remaining row budget.

Alternative considered: unbounded defaults with operator discipline. Rejected because accidental catalog-wide load is precisely the failure this command must prevent.

### 3. Freeze a stable scan horizon and use tuple keyset pagination

At a fresh non-checkpoint run, the repository captures the greatest eligible tuple `(published_at, id)` under the same transaction snapshot used to establish the first page. Candidates are ordered by `(published_at ASC, id ASC)`, constrained to tuples at or below that horizon, and paged strictly after the last completed tuple.

Eligibility is:

- lifecycle is published;
- visibility is public;
- media status is `legacy_ready` or `ready`;
- `published_at` is non-null.

Default mode adds `NOT EXISTS` for the exact semantic model. Refresh modes left-join only that exact model and return its text hash/vector metadata for application classification. `stale` scans eligible rows and calls the service only for missing rows or rows whose stored text hash differs from the current canonical source hash. `force` calls the service for every scanned eligible row. Other model rows do not participate in selection.

The frozen horizon prevents newly published rows from extending a run indefinitely. Rows that become eligible behind the cursor are handled by a later run; rows that become ineligible are rejected during final persistence.

Alternative considered: offset pagination. Rejected because inserting or removing exact-model rows while the scan runs would shift offsets and cause omissions or duplicates.

### 4. Keep refresh explicit and exact-model scoped

`refresh=none` skips every existing exact-model row, including one with an older text hash. `refresh=stale` and `refresh=force` fail before scanning unless `--confirm-model` exactly names the fixed persistence key. Unknown model keys, the full upstream model name, prefixes, wildcard values, and confirmation of `hash-ngram-v1` are rejected.

The refresh policy is carried into candidate classification, checkpoint compatibility, metrics, and conditional persistence. Persistence statements always constrain `model` to the fixed semantic key and never delete rows. A forced request may recompute a same-hash vector, but the repository remains a no-op when model, dimension, text hash, and serialized vector are already identical.

Alternative considered: a generic `--overwrite` flag. Rejected because it is too easy to misapply to another model or to interpret as permission to delete side-by-side facts.

### 5. Process complete pages with bounded concurrent service batches

Each page is normalized and classified in stable candidate order, then work items are split into consecutive batches. At most two batches execute concurrently. Request items retain stable `video:<id>` identities and request order; results are associated by the validated client contract, not by completion order.

Retryable `timeout`, `over_capacity`, and `unavailable` results receive at most three total attempts with cancellation-aware delays of 1, 5, and 15 seconds; a bounded `Retry-After` may raise a delay up to 30 seconds. Authentication, metadata, contract, invalid-input, and local configuration failures are terminal. Exhausted retries stop the run and leave the current page checkpoint unadvanced.

Cancellation stops new scheduling, cancels in-flight requests and retry waits, waits for goroutines to exit, and emits a final canceled summary. Partial successful writes from an uncheckpointed page are safe: replay uses missing/same-hash checks and conditional persistence.

Alternative considered: enqueue historical work into RabbitMQ. Rejected because it would change live event processing, weaken operator row/runtime controls, and require a second resumability protocol.

### 6. Revalidate source hash and eligibility inside the persistence transaction

Candidates carry video ID, raw source fields, canonical source hash, source `updated_at`, ordering tuple, and the observed exact-model state. After semantic generation, the extended versioned embedding repository starts a transaction and locks the current video row. It:

1. verifies published/public/media-ready eligibility;
2. canonicalizes the current title/description using the shared integration function;
3. requires the current hash to equal the generated item’s source hash;
4. locks the exact semantic embedding row, if present;
5. applies the selected missing/stale/force compare-and-set policy;
6. persists through the same finite, normalized, versioned `(video_id, model)` repository path;
7. commits before reporting the item complete.

If source or eligibility changed, the item is classified `source_changed` or `ineligible` and is not persisted. If a concurrent live worker or another backfill already stored the same fact, the outcome is `already_current`. In stale/force mode, an update proceeds only when the current exact-model row still matches the state observed or is already the generated fact; this prevents overwriting a newer concurrent semantic row. No transaction reads or writes another model.

Alternative considered: re-read and save in separate calls. Rejected because visibility or text could change in the race between validation and upsert.

### 7. Checkpoint only fully completed page prefixes

The checkpoint cursor is an opaque, versioned, URL-safe token containing a format version, run ID, fixed model key, refresh mode, frozen horizon, and last fully completed ordering tuple, plus a checksum for corruption detection. The command never logs the raw token.

For a non-dry run, the checkpoint file is replaced only after every candidate in the page has reached a durable terminal outcome (`persisted`, `already_current`, `ineligible`, or `source_changed`). Replacement writes a mode-0600 sibling file, flushes it, atomically renames it over the target, and flushes the parent directory. Invalid, truncated, unsupported-version, wrong-model, wrong-mode, or horizon-inconsistent checkpoints fail closed before scanning.

Cancellation, runtime expiry, service failure, or persistence failure in the middle of a page leaves the previous file intact. Restart replays at most one page. Page size, batch size, concurrency, progress interval, and remaining row/runtime limits may change on restart; model, refresh mode, and horizon may not.

Dry-run never creates or replaces a checkpoint because it has not completed durable work.

Alternative considered: checkpoint after each concurrent batch. Rejected because out-of-order batch completion would require a more complex contiguous-range journal and would not materially improve safety for bounded pages.

### 8. Treat limits and completion as resumable outcomes

The runner derives one context from OS cancellation and `max-runtime`. It stops before fetching another page when no row or runtime budget remains. If the budget expires during a page, in-flight work is canceled and the previous page checkpoint remains authoritative.

Normal completed states are `horizon_complete`, `max_rows_reached`, and `max_runtime_reached`; all preserve a usable checkpoint and return success after completed-page progress is flushed. Operator cancellation returns a distinct canceled exit status. Invalid configuration/checkpoint, dependency contract failure, and exhausted operational errors return distinct non-zero classes without raw infrastructure details.

A restarted command resumes strictly after the checkpoint tuple and preserves the original horizon. Starting without the checkpoint begins a new run and captures a new horizon.

### 9. Provide bounded metrics, progress, and safe summaries

The command registers:

- `frux_semantic_embedding_backfill_rows_total{outcome}`;
- `frux_semantic_embedding_backfill_batches_total{result}`;
- `frux_semantic_embedding_backfill_batch_duration_seconds{result}`;
- `frux_semantic_embedding_backfill_inflight_batches`;
- `frux_semantic_embedding_backfill_checkpoint_writes_total{result}`;
- `frux_semantic_embedding_backfill_last_progress_unixtime`.

Allowed row outcomes are `scanned`, `would_generate`, `persisted`, `already_current`, `ineligible`, and `source_changed`. Batch results are `success`, `canceled`, `timeout`, `over_capacity`, `auth`, `unavailable`, `contract`, and `internal`; checkpoint results are `success` and `failure`. Labels never contain video IDs, model strings, cursor values, paths, text, hashes, URLs, tokens, raw errors, or retry numbers.

Periodic structured progress and the final summary include only run ID, mode, elapsed time, bounded counts, completed pages, service attempts, last completed publication time, stop reason, and safe error class. They exclude title/description, vectors, video IDs, checkpoint contents, secrets, and raw database/HTTP errors. The optional metrics server exposes only health and Prometheus metrics and shuts down with the run.

### 10. Verify the operator boundary at unit, persistence, service, cancellation, and container levels

Unit tests cover option bounds, confirmation, cursor round trips/corruption, deterministic ordering, classification, retry delays, page-prefix checkpointing, cancellation, summaries, and metric label allowlists. PostgreSQL tests cover eligibility, tuple pagination/horizon behavior, concurrent inserts and visibility/lifecycle/source changes, exact-model compare-and-set outcomes, coexistence with hash/other models, and idempotent replay.

Contract tests use the semantic service from `add-semantic-embedding-service` and the client/vector/repository code from `integrate-semantic-video-embeddings`. End-to-end tests interrupt and restart the command, exercise missing/stale/force and dry-run modes, enforce row/runtime limits, and verify atomic checkpoint replacement. Container tests build the binary into the API image and run the manual Compose entrypoint with a checkpoint mount and no Redis/RabbitMQ dependency.

Documentation will add an operator runbook covering prerequisites, exact command examples, dry-run, bounded rollout, refresh confirmation, checkpoint backup/mounting, progress/metrics, cancellation/restart, failure classes, verification queries, and rollback. Rollback stops the command; already persisted exact-model facts remain valid and side-by-side with all other models.

## Risks / Trade-offs

- [The two predecessor changes are active and may evolve] → Treat their accepted fixed contracts as hard dependencies and block implementation/validation until both are available without broadening this change.
- [A failure after some writes but before page checkpoint replacement repeats service calls] → Keep pages bounded and repository writes idempotent; default and stale replay skip current facts.
- [Force mode can intentionally recompute many current rows] → Require exact-model confirmation, fixed row/runtime defaults, dry-run, and no unlimited mode.
- [Stale mode must scan eligible rows to compute canonical hashes] → Count scanned rows against `max-rows`, use stable pages, and expose would-generate/current outcomes before a larger run.
- [A video can become ineligible immediately after a committed embedding write] → Lock and revalidate before write; downstream visibility remains source-of-truth and later runs skip ineligible rows.
- [Checkpoint files can be lost with ephemeral containers] → Require a mounted checkpoint path for non-dry Compose runs and document backup/restart procedures.
- [Two operators can run overlapping backfills] → Compare-and-set persistence makes data safe, while duplicated inference remains bounded; the runbook recommends one active run per model/mode.
- [Metrics disappear when a one-shot process exits] → Emit periodic and final structured summaries in addition to scrape-time metrics.

## Migration Plan

1. Complete and strictly validate `add-semantic-embedding-service`.
2. Complete and strictly validate the narrowed `integrate-semantic-video-embeddings` change, including exact model identity, canonical hashing, client validation, and conditional persistence.
3. Add the backfill application runner, stable candidate/checkpoint contracts, and repository transaction extensions with unit and PostgreSQL tests.
4. Add the dedicated command, bounded metrics/progress, signal/runtime handling, and safe exit classes.
5. Build the binary into the API container and add the manual Compose profile/entrypoint plus persistent checkpoint mount.
6. Run dry-run against a bounded catalog slice, then a small missing-only run, cancel/restart it, and verify exact-model coverage without changes to hash or other models.
7. Enable larger bounded runs only after service/database metrics remain healthy. Use stale/force modes only with explicit exact-model confirmation.

Rollback stops or cancels the one-shot command. No live component needs rollback. Persisted semantic rows are valid versioned facts and are not deleted; a later missing-only run skips them.

## Open Questions

None. Dependencies, model scope, eligibility, ordering, bounds, refresh confirmation, retry policy, transactional freshness checks, checkpoint durability, observability, container entrypoint, and exclusions are fixed by this proposal.
