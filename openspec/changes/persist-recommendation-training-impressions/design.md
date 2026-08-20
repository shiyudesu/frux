## Context

Frux currently has three different recommendation records with deliberately different purposes:

- `recommendation_served_candidate` is the trusted delivery and attribution boundary. It contains only delivered membership and expires after the existing snapshot/retry window plus delivery grace.
- `recommendation_request_log` can retain the complete ranked pool and explanations, but it is sampled at 1% and permits payloads up to 1 MiB. Making it unsampled would create an unsuitable high-volume diagnostic log.
- Outcomes and behavior facts are durable and request-ID linked, but they do not establish which exact final cards were delivered or their ranking features.

The Feed already calls `RecordDeliveredCandidates` only after card hydration and current readability filtering. The recommendation result still contains the server-derived candidate reasons and score components at that point, so this boundary can create an exact compact impression without trusting a new client report.

This change creates only the trusted diagnostic fact and its lifecycle. Future export or training work may consume it only after its own explicit activation and privacy gates; the current low-data roadmap does not depend on such consumption.

## Goals / Non-Goals

**Goals:**

- Record 100% of final recommendation cards actually returned, with stable user/request/generation/video identity, absolute generation position, author/publication metadata, policy/scene, bounded explanations, degraded metadata, trusted delivery time, durable recording time, and explicit schema versions.
- Make accepted delivery durable through the same transaction as served-candidate evidence, while moving long-retention fact materialization to a bounded leased worker.
- Preserve the existing evidence expiry and attribution interval.
- Provide idempotent replay, retention cleanup, privacy deletion/training-opt-out boundaries, migration safety, reconciliation, and bounded metrics.
- Meet explicit acceptance thresholds for row/index storage, Feed p99 overhead, backlog recovery, and fact completeness before rollout.
- Keep public APIs compatible.

**Non-Goals:**

- Dataset export, training job orchestration, offline evaluation, feature/label joins, or any claim that the retained facts are sufficient for training.
- Semantic embeddings, pgvector, learned weights, propensity logging, exploration, experimentation changes, or model serving.
- Backfilling historical impressions from sampled request logs, outcomes, or expiring evidence.
- Using diagnostic impressions to authorize feedback, interaction, follow, exposure, or playback attribution.

## Decisions

### 1. Carry trusted delivery metadata to final Feed assembly

Recommendation candidates will gain an immutable delivery generation and internal zero-based absolute rank position assigned immediately after ranking and diversity, before cursor filtering or page slicing. Snapshot candidates retain both values. The deterministic degraded path creates a new generation for a new complete recomputation and assigns positions before applying its score cursor, so later pages retain the same generation-relative positions without changing the opaque cursor contract.

`CandidateResult` will expose an internal delivery projection containing video ID, generation, absolute rank position, trusted author ID and publication time, bounded normalized recall reasons, bounded score components, scene, policy/version, degraded state and bounded degraded providers, record schema version, and feature schema version. Feed will intersect this projection with the video IDs that survived final card assembly and pass only those records to the delivery recorder. Missing/unreadable cards leave rank gaps rather than renumbering the delivered page.

Alternative considered: derive position from the final Feed slice. Rejected because page-local, compacted positions lose the original ranking needed for diagnosis and any future reproducible replay.

Alternative considered: join against `recommendation_request_log`. Rejected because the log is sampled, contains the pre-delivery pool, and is intentionally much larger.

### 2. Extend the trusted delivery transaction with an outbox

The recommendation persistence boundary will accept one validated delivery aggregate containing:

- the unchanged served-candidate evidence fields and expiry;
- one training payload per final delivered candidate.

Under the existing `(user_id, request_id)` advisory transaction lock, the repository will append only new evidence rows and capture their database IDs. It will insert one `recommendation_training_impression_outbox` row per new evidence row in the same transaction. A unique `source_served_candidate_id` binds each handoff to the exact trusted evidence insertion, while `(user_id, request_id, generation, video_id)` is the stable downstream identity. Replayed pages that add no evidence add no duplicate handoff.

If evidence or outbox insertion fails, the transaction rolls back and Feed keeps its current fail-closed delivery behavior. Once the transaction commits, worker lag does not affect the response.

Alternative considered: write the final training row synchronously. Rejected because long-retention analytics persistence and indexes would remain on the Feed critical path and would not provide an explicit replay backlog.

Alternative considered: publish directly to Kafka. Rejected because broker availability must not become a precondition for recording trusted delivery, and the database transaction cannot atomically commit with Kafka.

### 3. Use compact typed outbox and fact tables

Both the handoff and final `recommendation_training_impression` row will contain only:

- `source_served_candidate_id`;
- `user_id`, `request_id`, `generation`, `video_id`;
- `rank_position`, `author_id`, `published_at`, `scene`, `policy_version`;
- bounded `recall_reasons_json` and `score_components_json`;
- bounded `degraded_state` and `degraded_providers_json`;
- trusted `served_at` and database `recorded_at`;
- numeric `record_schema_version` and bounded `feature_schema_version`.

Version 1 will reuse the existing request-log bounds: at most 8 reasons, each at most 64 characters, at most 8 supported finite score components, and a bounded sorted degraded-provider list. The initial feature schema token will be a constant such as `ranking-components-v1`. Domain constructors will normalize and validate the payload before persistence; arbitrary JSON, context, tokens, URLs, device data, embeddings, and outcomes are excluded.

The fact table will have a unique index on `source_served_candidate_id`, a downstream identity index on `(user_id, request_id, generation, video_id)`, and cleanup/watermark indexes on `(served_at, id)` and `(recorded_at, id)`. The outbox will have a unique source index plus a claim index beginning with `dispatched_at`, `available_at`, and `leased_until`.

Alternative considered: store one page-sized JSON blob. Rejected because row-per-impression gives compact idempotency, bounded cleanup, direct outcome joins, and future schema evolution without decoding an entire page.

### 4. Persist through a bounded leased database worker

The worker will claim a bounded ordered batch using `FOR UPDATE SKIP LOCKED`, set a lease and increment attempts, then insert facts with conflict-safe idempotency on `source_served_candidate_id`. Fact insertion and marking the outbox row dispatched will occur in one database transaction. A crash before completion leaves a reclaimable lease; replay after an already committed insert is treated as success.

Failures retain the outbox row, cap `last_error`, clear/expire the lease, and set a capped exponential-backoff `available_at`. Runs are limited by batch count and wall-clock duration so a poison row or backlog cannot monopolize the worker. Pending rows are not age-deleted.

The worker will run in `cmd/worker` without Kafka because the durable source is PostgreSQL. Defaults will be small and operationally configurable under a `recommendation.training_impressions` config block: dispatch interval/batch/lease/run bound, retention, cleanup interval/batch/run bound, and completed-outbox replay retention. The initial fact retention will default to 180 days with validated bounds; completed handoffs will remain for a short replay/diagnostic period such as 7 days.

### 5. Keep diagnostic retention independent from security and sampled-log retention

Cleanup will delete final facts by trusted `served_at` in stable `(served_at, id)` batches after the diagnostic retention cutoff. A separate pass removes only dispatched outbox rows after their operational replay period. It will never delete pending rows.

Diagnostic retention will not change:

- `recommendation_served_candidate.expires_at`;
- the five-minute delivery grace;
- `served_at <= recorded_at < expires_at` attribution checks;
- request-log policy sampling or retention;
- outcome or behavior-event retention.

There will be no foreign key from the long-lived fact/outbox to served-candidate rows because evidence cleanup must remain independent and pending handoffs must survive evidence expiry.

### 6. Preserve delivery, exposure, and security boundaries

No handler will accept a diagnostic impression and no public DTO will expose internal reasons, score components, schema versions, or rank metadata. Feedback and outcome code will continue to query only `recommendation_served_candidate`; the diagnostic repository will not implement the evidence-verifier interface.

`delivered` means only that Feed returned the card. Exposure remains a separately validated outcome. Diagnostic facts never synthesize exposure, and any future label consumer must treat delivered-without-exposure as unobserved rather than negative.

The Feed response, feedback body, exposure/playback contracts, and existing cursor formats remain unchanged. The only response-path behavior change is that the already-required trusted delivery transaction now also guarantees a durable training handoff.

### 7. Apply privacy deletion and training-opt-out boundaries

Account deletion will enqueue or directly perform bounded deletion of all account-linked pending handoffs and materialized diagnostic facts, with idempotent retries and reconciliation. Training opt-out does not suppress the operational delivery record while it is needed for diagnostics, but it creates a durable exclusion boundary that every future export or learner must check at its own captured privacy watermark; opted-out facts cannot enter future training artifacts. A later opt-out or deletion supersedes an earlier export eligibility decision.

No active exporter or learner is added here. The privacy contract is nevertheless frozen now so future consumers cannot infer eligibility merely from row presence.

### 8. Unify downstream identity and time semantics

The immutable impression identity is `(user_id, request_id, generation, video_id)` and the source evidence ID remains the persistence idempotency key. `rank_position` is zero-based within `generation`. `served_at` is the trusted occurrence time of delivery; `recorded_at` is the database acceptance/materialization time used for watermarks and late-arrival accounting. Future outcome joins must retain both `occurred_at` and `recorded_at`: occurrence time determines behavioral ordering and label windows, while recording time determines snapshot completeness.

### 9. Add bounded operational metrics and acceptance gates

Metrics will include:

- handoff/dispatch results with bounded result labels;
- pending outbox count and oldest pending age;
- persisted/replayed/retried totals;
- training fact and completed-outbox cleanup deletion totals;
- worker duration and success through `ObserveWorkerJob`.

No metric label will include user ID, request ID, video ID, policy version, raw error text, feature name, or other high-cardinality data. Monitoring documentation will define alertable backlog and failure signals.

Rollout is blocked until representative load tests and reconciliation prove all of the following:

- compact payload p95 is at most 2 KiB and measured table-plus-index storage is at most 4 KiB per fact;
- Feed delivery p99 increases by no more than both 5 ms and 5% versus the same build with diagnostic handoff disabled;
- under steady load, 99.99% of committed handoffs materialize within 5 minutes, oldest pending age stays below 15 minutes, and a simulated 10-minute worker outage drains within 60 minutes;
- reconciliation finds exactly one fact or pending handoff for 100% of committed delivered evidence within 24 hours, with zero unexplained missing or duplicate identities.

## Risks / Trade-offs

- [The atomic outbox adds writes to the Feed delivery transaction] → Keep each row compact, batch inserts, index only claim/idempotency/join/cleanup paths, and block rollout if the storage or Feed p99 gates fail.
- [A worker outage grows the outbox] → Retain pending rows, expose count/oldest-age gauges, use bounded catch-up batches, and never make Kafka part of recovery.
- [Long retention increases storage and privacy exposure] → Default to 180 days with validated bounds, retain only enumerated server-derived fields, honor deletion/opt-out boundaries, and use bounded cleanup.
- [Generation or absolute rank can be assigned incorrectly on cursor paths] → Assign both before all slicing/filtering, persist them in snapshots, and test first, later, filtered, replayed, recomputed, and degraded pages with intentional rank gaps.
- [Schema evolution can mix incompatible feature meanings] → Store record and feature-schema versions on every row and require any future activated consumer to select supported versions explicitly.
- [Rolling back code leaves additive tables and backlog] → Old binaries ignore the tables; stop the new worker and leave tables intact for forward recovery instead of dropping durable facts.

## Migration Plan

1. Add both GORM models and their indexes to the shared advisory-locked migration used by API and worker.
2. Deploy code that writes evidence and outbox atomically; do not backfill historical deliveries because existing records cannot reconstruct exact final cards plus bounded ranking metadata.
3. Start the leased persistence, privacy, reconciliation, and cleanup workers and verify pending age, dispatch results, fact counts, deletion behavior, and delivery failures.
4. Run the storage, Feed p99, outage-recovery, and 24-hour reconciliation acceptance gates before broad rollout.
5. Update recommendation, monitoring, architecture/engineering, and configuration documentation for the diagnostic fact and its future-consumer boundary.
6. On rollback, stop the new workers and deploy the prior API/worker binaries. Preserve the additive tables and rows; do not shorten evidence retention or delete pending handoffs.

## Open Questions

None. Implementation should use the validated default retention and worker bounds documented above, while keeping them configurable for operations.
