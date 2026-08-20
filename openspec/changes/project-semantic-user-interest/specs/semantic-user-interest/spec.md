## ADDED Requirements

### Requirement: Dependent Pretrained Model-Versioned Semantic Input
Semantic user-interest projection SHALL consume only persisted pretrained semantic video embeddings produced under the contracts of `add-semantic-embedding-service` and `integrate-semantic-video-embeddings`. The initial supported identity SHALL bind the configured embedding provider, model `semantic-minilm-l12-v2`, revision `e8f8c211226b894f`, dimension 384, canonical semantic text hash, and finite L2-normalized vector digest. The projector MUST NOT call the semantic embedding service directly, train a group model, or accept an arbitrary runtime provider, model, revision, or dimension.

#### Scenario: Exact semantic video embedding is available
- **WHEN** an eligible live source event references a video with a valid row for `semantic-minilm-l12-v2@e8f8c211226b894f`
- **THEN** the projector validates model, dimension, finiteness, and normalization before using that vector

#### Scenario: Another model or malformed vector is stored
- **WHEN** the available row has another model, wrong dimension, non-finite component, or invalid norm
- **THEN** it is not applied to the requested semantic profile and the attempt is classified with a bounded result

#### Scenario: Embedding service is unavailable
- **WHEN** the Python semantic service is stopped but the required persisted video embedding exists
- **THEN** semantic profile projection proceeds without making an inference request

#### Scenario: Video content changes after an event
- **WHEN** an eligible event recorded one canonical semantic text hash and the video's current embedding later has another text hash
- **THEN** the projector does not reinterpret the old event with the newer embedding and waits for or uses only the immutable event-time identity

### Requirement: Separate Semantic Interest Profile
Frux SHALL persist a semantic user-interest profile separately from `user_interest_profile`, isolated by user and exact embedding provider/model/revision. Each row SHALL contain an explicit profile schema, exact dimension, long-term vector, recent vector, negative vector, monotonic version, materialization time, and update time. The initial profile schema SHALL be `semantic-interest-v1`. All three vectors SHALL have exactly the declared dimension and finite bounded components; recent and long-term SHALL remain separately available for explicit later fusion.

#### Scenario: First live semantic event is applied
- **WHEN** a user has no semantic profile for the exact model and a valid eligible live event is applied
- **THEN** one `(user_id, model)` row is created with schema `semantic-interest-v1`, dimension 384, version 1, and three valid vectors

#### Scenario: User has two model versions
- **WHEN** the same user is projected for two statically supported model keys
- **THEN** each model has an independent profile row, vectors, version, and materialization time

#### Scenario: Existing hash profile is present
- **WHEN** a semantic profile is created or updated for a user who has `user_interest_profile`
- **THEN** the hash vectors, author affinities, version, and timestamps remain unchanged except through their existing projection path

#### Scenario: Profile metadata is inconsistent
- **WHEN** a stored semantic row has another schema, wrong dimension, or malformed vector
- **THEN** incremental projection rejects the update, leaves the source event unapplied, and records a bounded invalid-profile result

#### Scenario: Semantic profile is absent
- **WHEN** a user has no compatible semantic profile
- **THEN** current recommendation remains available through the existing hash and non-vector paths without treating absence as an error

### Requirement: Model-Scoped Immutable Semantic Event Ledger
Frux SHALL maintain a semantic event ledger scoped by user, embedding provider/model/revision, source kind, and source event ID, where source kind is bounded to `behavior`, `action`, or `feedback`. Each row SHALL record profile schema, stable source payload hash, occurrence time, canonical event-order fields, event-time semantic text hash, vector digest, and the validated normalized event-time vector snapshot. Inserting the event and canonically rematerializing the semantic profile SHALL occur atomically. The semantic ledger SHALL be independent of `recommendation_applied_profile_event`.

#### Scenario: Same event is redelivered for one model
- **WHEN** the same normalized source event and payload hash are projected more than once for one user and model
- **THEN** it changes the semantic profile at most once and duplicate delivery does not change profile version or timestamps

#### Scenario: Same event is projected for another model
- **WHEN** the same durable source event is eligible for a second supported embedding model
- **THEN** it may be applied once to that second model without colliding with the first model or the existing hash-profile ledger

#### Scenario: Source namespaces reuse an identifier
- **WHEN** a behavior event and an action event have the same textual source event ID
- **THEN** their bounded source-kind identities remain distinct

#### Scenario: Duplicate identity carries another payload
- **WHEN** the same user, exact embedding identity, source kind, and source event ID is presented with a different source payload hash, text hash, or vector digest
- **THEN** projection fails with a terminal conflict and does not alter the existing profile or ledger row

#### Scenario: Transaction fails during apply
- **WHEN** profile persistence fails after semantic event insertion is attempted
- **THEN** both changes roll back and a retry can apply the event normally

#### Scenario: Content is edited after binding
- **WHEN** a bound event's video later receives a new semantic text hash or vector digest
- **THEN** the event ledger and its contribution remain unchanged while later events may bind the new content identity

### Requirement: Semantic Positive Signal Projection
The `semantic-interest-v1` projector SHALL add completion, sustained progress, accepted active LIKE, and accepted active FAVORITE facts to both long-term and recent semantic vectors. Completion SHALL use weight `1.0`, sustained progress at ratio at least `0.5` SHALL use `0.5`, LIKE SHALL use `1.0`, and FAVORITE SHALL use `1.25`. Completion classification SHALL take precedence over progress or skip classification.

#### Scenario: Video completion is projected
- **WHEN** a durable live completion event has the required semantic video embedding
- **THEN** that embedding contributes weight `1.0` to both long-term and recent vectors

#### Scenario: Sustained progress is projected
- **WHEN** a durable live progress event reaches at least half of the bounded duration
- **THEN** that embedding contributes weight `0.5` to both long-term and recent vectors

#### Scenario: Progress is below threshold
- **WHEN** a progress event has a ratio below `0.5`
- **THEN** no semantic handoff or semantic event row is created for that fact

#### Scenario: Active like or favorite is projected
- **WHEN** an accepted active LIKE or FAVORITE event is consumed
- **THEN** its embedding contributes the documented action weight to both positive vectors

#### Scenario: Inactive action is consumed
- **WHEN** an accepted action event represents an inactive or cancelled LIKE or FAVORITE state
- **THEN** it creates no positive semantic contribution

### Requirement: Semantic Negative Signal Projection
The `semantic-interest-v1` projector SHALL add reliable early skip and explicit video-scoped `not_interested` or `already_seen` feedback to the negative semantic vector. Early skip at ratio at most `0.2` SHALL use weight `0.8`; supported explicit negative video feedback SHALL use weight `1.5`.

#### Scenario: User skips early
- **WHEN** a durable live skip event has a bounded playback ratio at or below `0.2`
- **THEN** the video's semantic embedding contributes weight `0.8` to the negative vector

#### Scenario: Skip occurs after meaningful playback
- **WHEN** a skip event has a ratio above `0.2` and is not a completion
- **THEN** it creates no semantic contribution

#### Scenario: User submits negative video feedback
- **WHEN** durable live `not_interested` or `already_seen` feedback is projected
- **THEN** the video's semantic embedding contributes weight `1.5` to the negative vector

#### Scenario: Completed event also appears terminal
- **WHEN** an event is marked complete while carrying terminal playback fields
- **THEN** it is classified as positive completion and is not also added to the negative vector

### Requirement: Author Affinity Remains Non-Semantic
Follow, unfollow, and author-scoped `reduce_author` facts SHALL remain in the existing non-semantic profile path. Semantic projection MUST NOT duplicate them, expand them across an author's catalog, average author videos, or create a synthetic content vector.

#### Scenario: User follows an author
- **WHEN** a durable follow event is consumed
- **THEN** the existing author-affinity projection may update, but no semantic outbox or semantic event row is created

#### Scenario: User selects reduce author
- **WHEN** durable feedback has author suppression scope and type `reduce_author`
- **THEN** the existing negative author affinity may update, but no semantic vector changes

#### Scenario: Author has semantic videos
- **WHEN** an author-only fact references an author whose videos have semantic embeddings
- **THEN** those embeddings are not loaded or combined for that fact

### Requirement: Canonical Single-User Reduction and Time Decay
Semantic profiles SHALL use a 30-day long-term half-life and a 24-hour recent/negative half-life under `semantic-interest-v1`. Every live materialization SHALL load only that user's exact-model semantic event rows, order them by `(occurred_at ASC, source_kind_rank ASC, source_event_id ASC)`, decay each event-time vector directly to the greatest included occurrence time, sum with the fixed signal weights, and clamp once after the complete ordered reduction. Live delivery order, processing time, lease attempts, and current video content MUST NOT change the materialized result.

#### Scenario: Newer event is applied
- **WHEN** an event occurs after all existing semantic events
- **THEN** canonical rematerialization uses that occurrence time as the common decay anchor and reduces every event in the fixed order

#### Scenario: Older live event arrives late
- **WHEN** an event occurred before the current profile materialization time
- **THEN** it is inserted at its canonical position and the entire user profile is deterministically rematerialized without moving the anchor backward

#### Scenario: Delivery order differs
- **WHEN** eligible events arrive out of occurrence order
- **THEN** the resulting vectors equal materialization from canonical order and do not depend on arrival-order per-event clamping

#### Scenario: Aggregate magnitude grows
- **WHEN** repeated valid signals would exceed the configured component magnitude bound
- **THEN** components are clamped once after the complete canonical reduction without normalizing away age or confidence magnitude

#### Scenario: Per-user event safety bound is exceeded
- **WHEN** one user's exact-model event ledger exceeds the configured finite rematerialization bound
- **THEN** the new event remains durable, profile mutation defers with a bounded result, and online recommendation preserves hash/non-vector fallback

### Requirement: Durable Live Semantic Projection Handoff
For an eligible content-bearing source event and each enabled supported semantic model, the existing profile worker SHALL durably upsert a semantic projection outbox row after existing outcome/hash-profile work succeeds and before marking the source-owned profile outbox dispatched. The handoff SHALL preserve the event-time canonical semantic text hash and target embedding provider/model/revision. Semantic handoff identity SHALL be unique by user, exact embedding identity, source kind, and source event ID.

#### Scenario: Existing profile work and semantic handoff succeed
- **WHEN** an eligible source outbox item is processed while semantic projection is enabled
- **THEN** existing outcome/hash behavior completes, one semantic handoff is durable, and only then is the source outbox marked dispatched

#### Scenario: Semantic handoff insertion fails
- **WHEN** existing hash projection succeeds but the semantic outbox cannot be durably written
- **THEN** the source outbox remains retryable and repeated hash projection is absorbed by existing idempotency

#### Scenario: Worker crashes after semantic handoff
- **WHEN** the semantic row commits but the source outbox is not marked dispatched before a crash
- **THEN** redelivery upserts the same semantic row without duplicating work

#### Scenario: Semantic projection is disabled
- **WHEN** an eligible source event is processed while its semantic model is disabled
- **THEN** existing profile and outcome processing completes without a semantic row

#### Scenario: Author-only event is processed
- **WHEN** follow, unfollow, or `reduce_author` is handled
- **THEN** the source outbox completes through its current path without semantic handoff

### Requirement: Bounded Leased Projection and Missing-Embedding Deferral
Semantic projection SHALL use a dedicated PostgreSQL leased worker with bounded batch size, poll interval, lease, run duration, concurrency, and per-user event safety limit. Missing required event-time embeddings and retryable persistence failures SHALL retain the model-specific row without binding the event or marking it dispatched. A current embedding with another text hash or digest MUST NOT be substituted. Retry delays SHALL be 5 seconds, 30 seconds, 2 minutes, 10 minutes, and a repeating 30-minute cap.

#### Scenario: Required embedding is missing
- **WHEN** a claimed semantic row references a video without the exact model embedding
- **THEN** no semantic profile or semantic event row is written, the outbox remains pending with a bounded delayed retry, and the original API/source outbox remains independent

#### Scenario: Embedding appears later
- **WHEN** `integrate-semantic-video-embeddings` later persists the exact valid video embedding
- **THEN** a retry applies the semantic event once and marks the semantic row dispatched

#### Scenario: Lease expires during processing
- **WHEN** a worker loses its lease and another worker claims the same row
- **THEN** transactional model-scoped idempotency prevents duplicate profile contribution

#### Scenario: Process crashes after profile commit
- **WHEN** the semantic profile transaction commits before outbox dispatch marking
- **THEN** redelivery observes the semantic event ledger and completes without changing the profile again

#### Scenario: Pending work is old
- **WHEN** a semantic row remains blocked by missing coverage longer than completed-row retention
- **THEN** it is retained and continues capped retries rather than being age-deleted

#### Scenario: Worker is cancelled
- **WHEN** shutdown cancels a claimed batch
- **THEN** unfinished rows remain reclaimable after lease expiry and no partial profile transaction commits

#### Scenario: Completed outbox cleanup runs
- **WHEN** dispatched semantic rows are older than seven days
- **THEN** a bounded stable-order cleanup may remove them without deleting pending rows, profiles, or semantic event identities

### Requirement: Per-User Model Concurrency Safety
Live semantic application SHALL serialize on a transaction-scoped lock for user and exact provider/model/revision. Under the lock, event insertion and canonical single-user rematerialization SHALL be atomic. Profile and ledger unique constraints SHALL remain authoritative, while unrelated users or models MAY be processed concurrently. The lock namespace SHALL be stable so the optional `rebuild-semantic-user-interest` change can coordinate with live projection.

#### Scenario: Two workers apply different events concurrently
- **WHEN** two semantic events for the same user and model are claimed concurrently
- **THEN** they materialize in a serial transaction order without lost updates

#### Scenario: Two workers apply the same event concurrently
- **WHEN** duplicate claims race for the same semantic identity
- **THEN** exactly one contribution is applied and both deliveries can finish idempotently

#### Scenario: Unrelated profiles are updated
- **WHEN** workers apply events for different users or models
- **THEN** those transactions may progress concurrently

### Requirement: Live-Only Population Boundary
This change SHALL populate semantic profiles only from eligible events that pass through live semantic handoff. It SHALL NOT scan historical behavior, action, or feedback facts; create rebuild or staging state; expose historical backfill, checkpoint, repair, or purge commands; or guarantee profile completeness. Historical reconstruction SHALL remain an optional operator capability in `rebuild-semantic-user-interest`; low-volume deployments MAY skip it and allow profiles to form naturally from new behavior.

#### Scenario: User has only pre-deployment facts
- **WHEN** an existing user has eligible durable facts but no eligible event passes through live semantic handoff
- **THEN** the user may remain without a semantic profile

#### Scenario: Projection was disabled for an interval
- **WHEN** eligible source events completed while semantic handoff was disabled
- **THEN** this change does not replay that interval after projection is enabled

#### Scenario: User later generates an eligible live event
- **WHEN** an unprofiled existing user generates an eligible event after live handoff is enabled
- **THEN** a semantic profile is created from that event without incorporating older facts

#### Scenario: Historical completeness is requested
- **WHEN** complete reconstruction from durable historical facts is required
- **THEN** it is handled by `rebuild-semantic-user-interest`, not by this capability

### Requirement: Semantic Projection Observability
Frux SHALL expose bounded-cardinality metrics for live semantic projection outcomes and duration, event occurrence-to-application lag, pending count and oldest pending age, retrying rows, and missing-embedding deferrals. Model labels SHALL use fixed aliases and SHALL NOT use arbitrary persisted strings. Metrics MUST NOT represent historical profile completeness or use historical eligible-user coverage as a denominator.

#### Scenario: Event is applied or deferred
- **WHEN** semantic work finishes an attempt
- **THEN** counters and duration observations use only bounded model, source, and result values

#### Scenario: Pending work ages
- **WHEN** semantic outbox work remains pending
- **THEN** gauges expose bounded pending/retry counts and oldest age without event, user, or video labels

#### Scenario: Missing embedding is encountered
- **WHEN** projection defers because the exact video embedding is absent
- **THEN** bounded missing-embedding counters and queue metrics are updated

#### Scenario: Existing user has no profile
- **WHEN** a user has only historical eligible facts
- **THEN** metrics do not classify the absence as a live-projection failure or claim complete user coverage

#### Scenario: Sensitive or high-cardinality data exists
- **WHEN** errors involve a user, video, source event, vector, title, URL, model database string, or raw database message
- **THEN** those values do not appear in metric labels or normal operational logs

### Requirement: Migrations, Configuration, and Worker Composition
API and worker migrations SHALL register the semantic profile, immutable semantic event, and outbox models under the existing advisory-locked migration flow. Worker configuration SHALL default semantic user projection to disabled, validate bounded batch/lease/poll/retry/cleanup/rematerialization settings, and permit only statically supported model descriptors. Invalid local configuration SHALL fail startup before semantic workers begin, while a missing video embedding SHALL remain a runtime deferred condition. No rebuild or staging tables SHALL be introduced.

#### Scenario: Migrations run concurrently
- **WHEN** API and worker start against a database without semantic profile tables
- **THEN** the existing migration lock creates the profile, ledger, and outbox schema and indexes safely without duplicate or partial DDL

#### Scenario: Projection is disabled by default
- **WHEN** existing configuration is used without semantic-profile enablement
- **THEN** current hash-profile worker behavior remains available and no semantic worker claims work

#### Scenario: Configuration names an arbitrary model
- **WHEN** configuration supplies an unknown model key, schema, dimension, or unbounded worker setting
- **THEN** startup fails with a safe configuration error

#### Scenario: Semantic video coverage is incomplete
- **WHEN** valid configuration starts the worker while some video embeddings are absent
- **THEN** startup succeeds and affected semantic rows defer through leased retry

#### Scenario: Account is erased
- **WHEN** existing account-erasure processing removes a user's recommendation data
- **THEN** that user's semantic profiles, semantic event rows, and pending semantic outbox rows are also removed

### Requirement: Verification and Documentation
Implementation SHALL include domain, persistence, handoff, worker, migration, concurrency, metrics, configuration, dependency-integration, outage, and recommendation-regression tests. Documentation SHALL describe dependencies, exact provider/model/revision/schema identity, event-time text-hash/vector-digest semantics, fixed signal rules, canonical reduction, author exclusions, decay, retry, races, live-only population, hash fallback, metrics, rollout, rollback, and optional `rebuild-semantic-user-interest`.

#### Scenario: Dependency integration is tested
- **WHEN** a real persisted semantic video row from `integrate-semantic-video-embeddings` passes through live handoff
- **THEN** the leased worker materializes the expected semantic profile and ledger row

#### Scenario: Semantic embedding is missing
- **WHEN** an eligible live event references a video without its semantic embedding
- **THEN** tests prove the originating API and existing hash-profile projection remain successful while semantic work is deferred

#### Scenario: Existing historical user is tested
- **WHEN** a user has historical eligible facts but receives no live semantic handoff
- **THEN** tests accept the absence of a semantic profile

#### Scenario: Strict validation runs
- **WHEN** planning artifacts are complete
- **THEN** `openspec validate --all --strict` succeeds without implementation-code or main-spec changes

### Requirement: No Online Semantic Recommendation Consumption
This capability SHALL NOT add pgvector, ANN queries, a semantic recall provider, ranking features, policy weights, online semantic-profile reads, request-path inference, individual or group model training, or historical rebuild commands. It SHALL NOT remove or reinterpret the local hash embedding, `user_interest_profile`, or author-affinity behavior. A later accepted change MAY consume the model-versioned semantic profile only with explicit recent/long-term fusion and safe hash/non-vector fallback when the profile is absent.

#### Scenario: Semantic profiles exist
- **WHEN** valid semantic profiles have been populated from live events
- **THEN** current recommendation candidates, scores, reasons, fallbacks, and Feed responses are unchanged

#### Scenario: Semantic profiles are absent or invalid
- **WHEN** projection is disabled, delayed, invalid, or absent for historical users
- **THEN** online recommendation continues using its existing behavior without failing

#### Scenario: Implementation schema is inspected
- **WHEN** migrations for this change are reviewed
- **THEN** they contain no pgvector type, ANN index, semantic recall table, rebuild staging table, or replacement/removal of hash-profile tables
