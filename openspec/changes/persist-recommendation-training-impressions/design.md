## Context

Frux currently has three different recommendation records with deliberately different purposes:

- `recommendation_served_candidate` is the trusted delivery and attribution boundary. It contains only delivered membership and expires after the existing snapshot/retry window plus delivery grace.
- `recommendation_request_log` can retain the complete ranked pool and explanations, but it is sampled at 1% and permits payloads up to 1 MiB. Making it unsampled would create an unsuitable high-volume training log.
- Outcomes and behavior facts are durable and request-ID linked, but they do not establish which exact final cards were delivered or their ranking features.

The Feed already calls `RecordDeliveredCandidates` only after card hydration and current readability filtering. The recommendation result still contains the server-derived candidate reasons and score components at that point, so this boundary can create an exact compact impression without trusting a new client report.

A later dataset-export change will consume this fact together with durable outcomes. This change creates only the source fact and its lifecycle.

## Goals / Non-Goals

**Goals:**

- Record 100% of final recommendation cards actually returned, with stable identity, absolute ranked-session position, policy/scene, bounded explanations, served time, and explicit schema versions.
- Make accepted delivery durable through the same transaction as served-candidate evidence, while moving long-retention fact materialization to a bounded leased worker.
- Preserve the existing evidence expiry and attribution interval.
- Provide idempotent replay, retention cleanup, privacy bounds, migration safety, and bounded metrics.
- Keep public APIs compatible.

**Non-Goals:**

- Dataset export, training job orchestration, offline evaluation, or feature/label joins.
- Semantic embeddings, pgvector, learned weights, propensity logging, exploration, experimentation changes, or model serving.
- Backfilling historical impressions from sampled request logs, outcomes, or expiring evidence.
- Using training impressions to authorize feedback, interaction, follow, exposure, or playback attribution.

## Decisions

### 1. Carry trusted delivery metadata to final Feed assembly

Recommendation candidates will gain an internal zero-based absolute rank position assigned immediately after ranking and diversity, before cursor filtering or page slicing. Snapshot candidates retain it. The deterministic degraded path assigns positions on the complete recomputed ordering before applying its score cursor, so later pages retain the same absolute positions without changing the opaque cursor contract.

`CandidateResult` will expose an internal delivery projection containing video ID, absolute rank position, bounded normalized recall reasons, bounded score components, scene, policy version, record schema version, and feature schema version. Feed will intersect this projection with the video IDs that survived final card assembly and pass only those records to the delivery recorder. Missing/unreadable cards leave rank gaps rather than renumbering the delivered page.

Alternative considered: derive position from the final Feed slice. Rejected because page-local, compacted positions lose the original ranking needed for training and mislabel later pages.

Alternative considered: join against `recommendation_request_log`. Rejected because the log is sampled, contains the pre-delivery pool, and is intentionally much larger.

### 2. Extend the trusted delivery transaction with an outbox

The recommendation persistence boundary will accept one validated delivery aggregate containing:

- the unchanged served-candidate evidence fields and expiry;
- one training payload per final delivered candidate.

Under the existing `(user_id, request_id)` advisory transaction lock, the repository will append only new evidence rows and capture their database IDs. It will insert one `recommendation_training_impression_outbox` row per new evidence row in the same transaction. A unique `source_served_candidate_id` binds each handoff to the exact trusted evidence insertion. Replayed pages that add no evidence add no duplicate handoff.

If evidence or outbox insertion fails, the transaction rolls back and Feed keeps its current fail-closed delivery behavior. Once the transaction commits, worker lag does not affect the response.

Alternative considered: write the final training row synchronously. Rejected because long-retention analytics persistence and indexes would remain on the Feed critical path and would not provide an explicit replay backlog.

Alternative considered: publish directly to RabbitMQ. Rejected because broker availability must not become a precondition for recording trusted delivery, and the database transaction cannot atomically commit with RabbitMQ.

### 3. Use compact typed outbox and fact tables

Both the handoff and final `recommendation_training_impression` row will contain only:

- `source_served_candidate_id`;
- `user_id`, `request_id`, `video_id`;
- `rank_position`, `scene`, `policy_version`;
- bounded `recall_reasons_json` and `score_components_json`;
- trusted `served_at`;
- numeric `record_schema_version` and bounded `feature_schema_version`.

Version 1 will reuse the existing request-log bounds: at most 8 reasons, each at most 64 characters, and at most 8 supported finite score components. The initial feature schema token will be a constant such as `ranking-components-v1`. Domain constructors will normalize and validate the payload before persistence; arbitrary JSON, context, tokens, URLs, device data, embeddings, and outcomes are excluded.

The fact table will have a unique index on `source_served_candidate_id`, a request linkage index on `(user_id, request_id, video_id)`, and a cleanup index on `(served_at, id)`. The outbox will have a unique source index plus a claim index beginning with `dispatched_at`, `available_at`, and `leased_until`.

Alternative considered: store one page-sized JSON blob. Rejected because row-per-impression gives compact idempotency, bounded cleanup, direct outcome joins, and future schema evolution without decoding an entire page.

### 4. Persist through a bounded leased database worker

The worker will claim a bounded ordered batch using `FOR UPDATE SKIP LOCKED`, set a lease and increment attempts, then insert facts with conflict-safe idempotency on `source_served_candidate_id`. Fact insertion and marking the outbox row dispatched will occur in one database transaction. A crash before completion leaves a reclaimable lease; replay after an already committed insert is treated as success.

Failures retain the outbox row, cap `last_error`, clear/expire the lease, and set a capped exponential-backoff `available_at`. Runs are limited by batch count and wall-clock duration so a poison row or backlog cannot monopolize the worker. Pending rows are not age-deleted.

The worker will run in `cmd/worker` without RabbitMQ because the durable source is PostgreSQL. Defaults will be small and operationally configurable under a `recommendation.training_impressions` config block: dispatch interval/batch/lease/run bound, retention, cleanup interval/batch/run bound, and completed-outbox replay retention. The initial fact retention will default to 180 days with validated bounds; completed handoffs will remain for a short replay/diagnostic period such as 7 days.

### 5. Keep training retention independent from security and sampled-log retention

Cleanup will delete final facts by trusted `served_at` in stable `(served_at, id)` batches after the training retention cutoff. A separate pass removes only dispatched outbox rows after their operational replay period. It will never delete pending rows.

Training retention will not change:

- `recommendation_served_candidate.expires_at`;
- the five-minute delivery grace;
- `served_at <= recorded_at < expires_at` attribution checks;
- request-log policy sampling or retention;
- outcome or behavior-event retention.

There will be no foreign key from the long-lived fact/outbox to served-candidate rows because evidence cleanup must remain independent and pending handoffs must survive evidence expiry.

### 6. Preserve the security boundary and API contracts

No handler will accept a training impression and no public DTO will expose internal reasons, score components, schema versions, or rank metadata. Feedback and outcome code will continue to query only `recommendation_served_candidate`; the training repository will not implement the evidence-verifier interface.

The Feed response, feedback body, exposure/playback contracts, and existing cursor formats remain unchanged. The only response-path behavior change is that the already-required trusted delivery transaction now also guarantees a durable training handoff.

### 7. Add bounded operational metrics

Metrics will include:

- handoff/dispatch results with bounded result labels;
- pending outbox count and oldest pending age;
- persisted/replayed/retried totals;
- training fact and completed-outbox cleanup deletion totals;
- worker duration and success through `ObserveWorkerJob`.

No metric label will include user ID, request ID, video ID, policy version, raw error text, feature name, or other high-cardinality data. Monitoring documentation will define alertable backlog and failure signals.

## Risks / Trade-offs

- [The atomic outbox adds writes to the Feed delivery transaction] → Keep each row compact, batch inserts, index only claim/idempotency/join/cleanup paths, and measure delivery failures and transaction latency.
- [A worker outage grows the outbox] → Retain pending rows, expose count/oldest-age gauges, use bounded catch-up batches, and never make RabbitMQ part of recovery.
- [Long retention increases storage and privacy exposure] → Default to 180 days with validated bounds, retain only enumerated server-derived fields, and use bounded cleanup.
- [Absolute rank can be assigned incorrectly on cursor paths] → Assign it before all slicing/filtering, persist it in snapshots, and test first, later, filtered, replayed, and degraded pages with intentional rank gaps.
- [Schema evolution can mix incompatible feature meanings] → Store record and feature-schema versions on every row and require later dataset export to select supported versions explicitly.
- [Rolling back code leaves additive tables and backlog] → Old binaries ignore the tables; stop the new worker and leave tables intact for forward recovery instead of dropping durable facts.

## Migration Plan

1. Add both GORM models and their indexes to the shared advisory-locked migration used by API and worker.
2. Deploy code that writes evidence and outbox atomically; do not backfill historical deliveries because existing records cannot reconstruct exact final cards plus bounded ranking metadata.
3. Start the leased persistence and cleanup workers and verify pending age, dispatch results, fact counts, and delivery failures.
4. Update recommendation, monitoring, architecture/engineering, and configuration documentation for the new fact and later dataset-export dependency.
5. On rollback, stop the new workers and deploy the prior API/worker binaries. Preserve the additive tables and rows; do not shorten evidence retention or delete pending handoffs.

## Open Questions

None. Implementation should use the validated default retention and worker bounds documented above, while keeping them configurable for operations.
