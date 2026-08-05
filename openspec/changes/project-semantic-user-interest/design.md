## Context

Frux currently materializes `user_interest_profile` from durable playback behavior, accepted LIKE/FAVORITE actions, follow changes, and recommendation feedback. That profile uses the local `hash-ngram-v1` video embedding, stores content vectors together with author affinities, and deduplicates through `recommendation_applied_profile_event(user_id, source_event_id)`. Source owners retain projection work in leased PostgreSQL outboxes so projection failure does not change the originating HTTP result.

This change depends on:

- `add-semantic-embedding-service`, which fixes the immutable MiniLM service contract.
- `integrate-semantic-video-embeddings`, which persists a normalized 384-dimensional video vector under `semantic-minilm-l12-v2@e8f8c211226b894f`, keyed by `(video_id, model)`.

The semantic user profile must be a new live projection. Reusing the hash profile would mix incompatible dimensions and incorrectly turn author affinity into content vectors. Reusing the existing applied-event ledger would make an event applied to one profile appear applied to every semantic model.

This revision deliberately removes historical reconstruction. Only eligible source events processed through live handoff after this capability is enabled can populate semantic profiles. Facts already dispatched before deployment, or accepted while handoff is disabled, are not scanned or recovered here. Existing users may therefore remain without semantic profiles until the future `rebuild-semantic-user-interest` change.

No semantic profile consumer is introduced. The stored profile is an inert prerequisite for a later semantic recall change.

## Goals / Non-Goals

**Goals:**

- Persist one semantic profile per exact `(user_id, model)` with long-term, recent, and negative vectors plus explicit schema/dimension metadata.
- Project eligible live content-bearing positive and negative video facts while preserving the existing hash profile and author-affinity ownership.
- Deduplicate independently for each embedding model and source namespace.
- Hand semantic work off durably from existing profile outboxes and retry missing video embeddings through a bounded leased queue.
- Preserve deterministic event-time decay and safe concurrent application.
- Define migrations, worker composition, bounded observability, tests, and documentation.

**Non-Goals:**

- Replaying, rebuilding, backfilling, or repairing semantic profiles from historical durable facts.
- Adding a rebuild/backfill executable, checkpoint/run state, staging tables, high-water fences, dry-run reports, or purge/repair operator commands. Those belong to `rebuild-semantic-user-interest`.
- Guaranteeing semantic-profile coverage for existing users or for intervals when live handoff is disabled.
- Reading semantic profiles during online recall, ranking, policy evaluation, or Feed requests.
- Adding pgvector, vector indexes, ANN queries, a semantic recall provider, or semantic ranking features.
- Replacing, deleting, or changing the meaning of `user_interest_profile`, its hash vectors, or its author affinities.
- Projecting follow/unfollow or `reduce_author` as semantic content.
- Calling the Python embedding service from the profile worker; this change reads only persisted semantic video embeddings.
- Training, fine-tuning, dynamic model selection, or arbitrary runtime model/dimension configuration.

## Decisions

### 1. Add a separate model-versioned semantic profile

The recommendation domain will add `SemanticUserInterestProfile` with:

- `user_id`;
- exact revision-bearing `model`;
- `profile_schema`, initially `semantic-interest-v1`;
- fixed `dimension`, initially 384;
- `long_term_vector`;
- `recent_vector`;
- `negative_vector`;
- monotonic `version`;
- `materialized_at` and database `updated_at`.

PostgreSQL table `recommendation_semantic_user_interest_profile` uses `(user_id, model)` as its primary key and JSONB arrays for the vectors. Vectors must have exactly the declared dimension and finite bounded components. Aggregate vectors are weighted sums and are not required to have unit norm; source video embeddings must satisfy the exact model's dimension and normalization contract.

A schema mismatch is never silently reinterpreted. Incremental projection leaves the event unapplied, reports a bounded invalid-profile result, and retains the work for later correction. Historical reconstruction or schema conversion belongs to `rebuild-semantic-user-interest`.

Alternative considered: add semantic columns to `user_interest_profile`. Rejected because that table is one hash-profile aggregate with author affinities and no model key.

### 2. Use a separate model-scoped applied-event ledger

`recommendation_semantic_applied_profile_event` identifies an application by:

`(user_id, model, source_kind, source_event_id)`.

`source_kind` is one of `behavior`, `action`, or `feedback`, preventing unrelated durable ID namespaces from colliding. The row also stores `profile_schema`, a stable source payload hash, source occurrence time, and applied time. The payload hash excludes lease attempts, processing time, and embedding availability.

Applying an event and updating or creating its profile occur in one transaction. A duplicate with the same payload hash succeeds without changing profile version or timestamps. A duplicate identity with another payload hash is a terminal conflict and changes neither row.

Alternative considered: prefix semantic IDs and reuse `recommendation_applied_profile_event`. Rejected because it couples incompatible profile and model lifecycles.

### 3. Keep signal eligibility explicit and exclude author-only facts

The initial `semantic-interest-v1` rules are:

| Source fact | Eligibility | Semantic contribution |
| --- | --- | --- |
| completion | `completed=true` or event type `complete` | long-term `1.0`, recent `1.0` |
| sustained progress | progress ratio at least `0.5` | long-term `0.5`, recent `0.5` |
| early skip | skip ratio at most `0.2` | negative `0.8` |
| LIKE | accepted active LIKE event | long-term `1.0`, recent `1.0` |
| FAVORITE | accepted active FAVORITE event | long-term `1.25`, recent `1.25` |
| `not_interested` | durable video-scoped feedback | negative `1.5` |
| `already_seen` | durable video-scoped feedback | negative `1.5` |

Completion takes precedence over progress or skip classification. Unsupported, inactive, zero-weight, malformed, or author-only events create no semantic handoff.

Follow/unfollow remains an author relation feature in the existing profile. `reduce_author` remains a negative author affinity there. Neither is expanded to an author's catalog or represented by a synthetic content vector.

Alternative considered: average an author's videos. Rejected because it invents a content preference and creates unstable fan-out.

### 4. Preserve event-time decay

The semantic aggregate uses:

- a 30-day long-term half-life;
- a 24-hour recent and negative half-life;
- materialization at `max(profile.materialized_at, event.occurred_at)`;
- decay of existing components to that time;
- decay of delayed event contributions from occurrence time to that same time before addition;
- deterministic component clamping.

Weights and half-lives are fixed by `semantic-interest-v1`. Processing time, lease attempts, and delivery order do not enter source identity. A future rules change requires a new profile schema rather than mutating existing rows in place.

Alternative considered: normalize aggregate profiles after every event. Rejected because normalization destroys magnitude and age confidence.

### 5. Add durable live handoff after existing profile work

Existing source-owned profile outboxes remain the first handoff. For an eligible live video signal, the current profile worker will:

1. persist recommendation outcome attribution as it does today;
2. apply existing hash/author profile behavior;
3. upsert one `recommendation_semantic_profile_outbox` row for each enabled statically supported semantic model;
4. mark the source-owned outbox dispatched only after the semantic row is durable.

The semantic row contains bounded projection fields: model, profile schema, source kind/ID, user/video IDs, event type, occurrence time, signal weight/destination, stable payload hash, attempts, availability time, lease, last result, and dispatch time. Its unique key is `(user_id, model, source_kind, source_event_id)`.

If semantic handoff insertion fails, the source row remains pending. Reprocessing is safe because existing hash projection and semantic handoff are independently idempotent. The originating API has already committed and remains unaffected.

When projection is disabled, existing source processing completes without a semantic row. This change does not recover that interval. Follow and `reduce_author` never create semantic rows.

Alternative considered: make each source owner write semantic rows in its HTTP transaction. Rejected because it duplicates model knowledge and expands request-path work.

### 6. Process semantic rows with bounded leases and missing-embedding retry

A dedicated worker claims bounded batches using `FOR UPDATE SKIP LOCKED`, a bounded lease, stable owner identity, and cancellation-aware polling. Models are statically registered; configuration may enable supported descriptors but cannot define arbitrary model keys or dimensions.

For each row, the worker loads `video_embedding(video_id, model)`. A missing exact-model row is a retryable `missing_embedding` result. The event is not marked applied or dispatched. Retry delays are 5 seconds, 30 seconds, 2 minutes, 10 minutes, and then a repeating 30-minute cap.

Invalid embedding/model/dimension/profile data also changes no profile or ledger row and emits only a bounded result class. Once a valid vector exists, repository application and applied-ledger insertion are transactional. The semantic outbox is marked dispatched only after that transaction commits. A crash before dispatch marking causes a duplicate application attempt, which the ledger absorbs.

Pending rows are not age-deleted. Dispatched rows may be cleaned after seven days in stable bounded batches. Last-result text is bounded and excludes vectors, titles, URLs, arbitrary database errors, and tokens.

Alternative considered: call the embedding HTTP service when a vector is missing. Rejected because video embedding generation has its own owner and retry lifecycle.

### 7. Serialize live apply by `(user_id, model)`

Repository application takes a PostgreSQL transaction-scoped advisory lock derived from a fixed namespace plus `(user_id, model)`, then checks the applied ledger and locks or upserts the profile row. Multiple workers may process unrelated users/models concurrently. Unique constraints and payload-hash checks remain authoritative if advisory-lock keys collide.

The lock contract is intentionally reusable by the future `rebuild-semantic-user-interest` change, but this change adds no rebuild finalization or historical race protocol.

Alternative considered: rely only on row locking. Rejected because the first concurrent insert for a previously absent profile needs serialization before a row exists.

### 8. Expose bounded live-projection metrics

Metrics use a fixed model alias such as `minilm_l12_v2`, never the persisted model string. Bounded labels include:

- source: `behavior`, `action`, `feedback`;
- result: `applied`, `duplicate`, `deferred`, `missing_embedding`, `invalid_embedding`, `invalid_profile`, `conflict`, `error`;
- queue state: `pending`, `retry`, `dispatched`.

The implementation exposes projection outcome and duration, event occurrence-to-application lag, pending/retrying counts, oldest pending age, and missing-embedding deferrals. It does not claim historical user coverage or profile completeness because pre-existing users may legitimately have no row.

Metrics and normal logs do not label or print user/video/event IDs, arbitrary model strings, vectors, titles, URLs, or raw database errors.

### 9. Compose an inert live shadow projection

API and worker startup register the profile, ledger, and semantic outbox migrations through the existing migration lock. No rebuild/staging tables are added.

Configuration defaults the projection off for backward-compatible startup and validates fixed model descriptors plus bounded batch, poll, lease, retry, and cleanup settings. When enabled, the existing profile worker writes semantic handoffs and `cmd/worker` runs the semantic claimant and cleanup loop. A missing video embedding is a runtime deferred condition, not a startup failure.

Because no online code reads the profile, rollout is:

1. deploy migrations and code with projection disabled;
2. verify the two dependency changes and configuration;
3. enable live handoff and the semantic worker in shadow mode;
4. monitor queue, lag, retry, and missing-embedding metrics.

Rollback disables new handoff and claiming. Already persisted profiles remain inert, and pending rows remain durable. Eligible facts accepted while handoff is disabled are a known gap that this change does not recover.

## Risks / Trade-offs

- [Existing users may remain unprofiled] → Make live-only population explicit and defer completeness to `rebuild-semantic-user-interest`.
- [Disabled intervals create durable-history gaps] → Document the gap; do not imply automatic recovery or completeness metrics.
- [Long embedding outages grow pending rows] → Use capped retry, bounded claims, pending count/age metrics, and no age deletion.
- [Semantic outbox duplicates source metadata] → Store only bounded projection fields and clean dispatched rows after seven days.
- [Current video embedding may change before a delayed retry] → The exact persisted `(video_id, model)` row is the available contract; historical embedding-version replay is outside this change.
- [JSONB aggregate vectors are unsuitable for ANN] → This change only materializes profiles; a later recall change owns query storage/index decisions.
- [Profile schema mismatch blocks live work] → Reject mutation, retain the row, emit bounded diagnostics, and handle conversion in the future rebuild change.
- [Multiple future models multiply queue/storage cost] → Permit only a small statically supported configured set.

## Migration Plan

1. Complete and validate `add-semantic-embedding-service` and `integrate-semantic-video-embeddings`.
2. Add domain/application types, static model registry, persistence models, indexes, and advisory-locked migrations without enabling projection.
3. Add live semantic handoff after existing hash projection and deploy disabled; verify existing outcome/profile behavior is unchanged.
4. Add the leased semantic worker, retry, cleanup, metrics, and worker composition.
5. Enable the fixed model in shadow mode and monitor live projection. Do not assert historical completeness.
6. Keep semantic profiles unconsumed until a later accepted change adds semantic recall/ranking.

Rollback disables handoff and claiming without dropping tables. Pending rows and profiles remain inert. Re-enabling resumes already queued rows only; reconstruction of missed historical intervals requires `rebuild-semantic-user-interest`.

## Open Questions

None. Historical rebuild/backfill and operator-command behavior are explicitly deferred to `rebuild-semantic-user-interest`.
