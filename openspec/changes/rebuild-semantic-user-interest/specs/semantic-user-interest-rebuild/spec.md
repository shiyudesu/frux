## ADDED Requirements

### Requirement: Explicit Dependency and Exact-Model Gate
Semantic user-interest rebuild SHALL require the implemented contracts of `integrate-semantic-video-embeddings`, `backfill-semantic-video-embeddings`, and the narrowed `project-semantic-user-interest`. It SHALL accept only a statically supported exact semantic model/profile-schema descriptor. The initial descriptor SHALL be model `semantic-minilm-l12-v2@e8f8c211226b894f`, profile schema `semantic-interest-v1`, and dimension 384.

#### Scenario: All dependency contracts are available
- **WHEN** the rebuild command validates its dependencies before creating or resuming a run
- **THEN** the exact persisted model, dimension, vector-validation, semantic-event, profile, ledger, and advisory-lock contracts match the three dependent changes

#### Scenario: Arbitrary model is requested
- **WHEN** an operator supplies an unknown model key, upstream model name, revision alias, schema, or dimension
- **THEN** the command fails before scanning or writing any run, profile, ledger, or coverage state

#### Scenario: Video backfill is incomplete
- **WHEN** the dependent video backfill has not produced every exact-model vector needed by selected facts
- **THEN** the rebuild may start but affected users are handled by the missing-vector deferral requirement rather than by inference

### Requirement: Stable Snapshot-Fenced User and Fact Selection
A fresh non-dry rebuild SHALL capture, in one repeatable-read transaction, a versioned high-water fence over immutable durable `behavior`, `action`, and `feedback` source IDs plus the greatest candidate user ID. Candidate users SHALL be selected as distinct positive user IDs at or below those fences, ordered by `user_id ASC`, and read in bounded keyset pages. One user's normalized facts SHALL be read in bounded pages ordered by `(occurred_at ASC, source_kind_rank ASC, source_event_id ASC)` using the fixed `semantic-interest-v1` source-kind rank.

#### Scenario: Facts are added after run creation
- **WHEN** new source rows or users are committed beyond the captured source or user fence
- **THEN** they do not extend the historical scan and are handled only by bounded live catch-up or a later run

#### Scenario: Facts share an occurrence time
- **WHEN** behavior, action, or feedback facts have equal `occurred_at` values
- **THEN** fixed source-kind rank and source-event ID provide a total deterministic order with no duplicate or omitted fact across pages

#### Scenario: A fact is recorded late
- **WHEN** a durable fact has an old occurrence time but its immutable source ID is within the captured fence
- **THEN** it is selected according to its source ID fence and decayed according to its occurrence time

#### Scenario: Run restarts after a completed user page
- **WHEN** the command resumes from its stored user cursor
- **THEN** it selects users strictly after the last atomically completed user ID under the original source and user fences

### Requirement: Live-Equivalent Eligibility, Identity, Weights, Ordering, and Decay
Rebuild SHALL call the same semantic classifier, stable payload-hash function, applied-event identity, vector destinations, fixed signal weights, canonical ordering, half-lives, delayed-event decay, monotonic materialization, component clamping, and dimension validation used by live `semantic-interest-v1` projection. It MUST NOT maintain an independently configurable reconstruction rule set.

#### Scenario: Positive historical facts are reconstructed
- **WHEN** selected facts contain eligible completion, sustained progress, active LIKE, or active FAVORITE events
- **THEN** their exact-model vectors contribute to long-term and recent vectors with the same completion precedence and weights as live projection

#### Scenario: Negative historical facts are reconstructed
- **WHEN** selected facts contain eligible early skip, `not_interested`, or `already_seen` events
- **THEN** their exact-model vectors contribute to the negative vector with the same thresholds and weights as live projection

#### Scenario: Author-only facts are selected from durable history
- **WHEN** history contains follow, unfollow, `reduce_author`, unsupported, inactive, malformed, or below-threshold facts
- **THEN** they create no semantic contribution, semantic applied-event row, or synthetic author vector

#### Scenario: Events arrive out of order
- **WHEN** the same eligible facts are reconstructed in canonical order and applied live in delayed or shuffled delivery order
- **THEN** both paths produce equivalent bounded long-term, recent, and negative vectors, materialization time, and applied identities

#### Scenario: Command restarts later
- **WHEN** an interrupted run resumes at a later processing time
- **THEN** processing time and retry age do not alter event identity, occurrence-time decay, or reconstructed vector values

### Requirement: Complete Exact-Model Vector Validation and Deferral
Rebuild SHALL load persisted video embeddings only for the selected exact model and SHALL validate model identity, dimension, component finiteness, and normalization before reduction. If any contributing fact for a user lacks a valid exact-model embedding, the user SHALL be durably deferred without replacing the profile, advancing rebuild coverage, or inserting an applied-event identity for any newly reconstructed fact.

#### Scenario: All contributing embeddings are valid
- **WHEN** every eligible contributing fact for a user resolves to a valid dimension-384 exact-model vector
- **THEN** the user may proceed to transactional live catch-up and finalization

#### Scenario: Exact-model embedding is missing
- **WHEN** at least one contributing fact references a video without the selected exact-model row
- **THEN** the user is recorded with bounded `missing_embedding` status, missing-vector metrics increase, and no partial semantic profile or new ledger row is committed

#### Scenario: Exact-model embedding is malformed
- **WHEN** a selected row has the correct model key but a wrong dimension, non-finite component, or invalid norm
- **THEN** the user is deferred as `invalid_embedding` and existing semantic profile, ledger, and coverage rows remain unchanged

#### Scenario: Missing embedding appears later
- **WHEN** `backfill-semantic-video-embeddings` later persists the valid exact-model vector and the user is retried within a run or after restart
- **THEN** the full user reconstruction can commit once without counting the previously missing fact twice

#### Scenario: Another model has a vector
- **WHEN** the required video has a hash embedding or a semantic row for another model but lacks the selected exact model
- **THEN** those other rows are ignored and the selected user remains deferred

### Requirement: Bounded Live Catch-Up and Newer-Version Protection
Finalization SHALL use the shared transaction-scoped `(user_id, model)` advisory lock. After acquiring it, rebuild SHALL lock and inspect the current exact-model profile and ledger, capture current per-source catch-up maxima, and include every resolvable eligible user fact after the run fence through those maxima plus every existing exact-model applied identity not represented by the baseline. Catch-up SHALL be bounded and SHALL abort without mutation when completeness cannot be proven.

#### Scenario: Live event committed before rebuild lock
- **WHEN** live projection applied an eligible exact-model event before rebuild acquires the shared lock
- **THEN** rebuild includes the matching durable fact and ledger in the recomputed profile or aborts without overwriting that live version

#### Scenario: Live event waits behind rebuild
- **WHEN** a source fact commits after the catch-up maxima and its live projector waits for the rebuild-held lock
- **THEN** rebuild commits first and the live projector subsequently applies the event once to the rebuilt profile

#### Scenario: Catch-up exceeds its bound
- **WHEN** facts or unresolved applied identities after the run fence exceed the configured per-user catch-up limit or remaining event budget
- **THEN** the user is durably deferred as `catch_up_limit` and the current profile version is not changed

#### Scenario: Existing applied identity cannot be resolved
- **WHEN** the current exact-model ledger contains an identity whose durable source fact cannot be loaded and validated
- **THEN** finalization fails closed for that user and does not replace the current profile

#### Scenario: Current version advanced after baseline computation
- **WHEN** the profile version observed under the advisory lock is newer than any version observed while scanning
- **THEN** rebuild computes from durable baseline plus bounded catch-up and writes only from the locked current version rather than restoring an older version

### Requirement: One-User Atomic Replace, Ledger Upsert, and Embedding Revalidation
For one user/model, final profile replace or insert, matching applied-event ledger upserts, rebuild coverage, deferral clearing, and resulting profile version SHALL commit in one PostgreSQL transaction. The transaction SHALL stable-order and lock every referenced exact-model embedding row, verify the vectors used for reduction remain current, validate existing ledger payload hashes, and conditionally write profile version `locked_current_version + 1`. It SHALL never delete applied-event rows.

#### Scenario: User has no semantic profile
- **WHEN** a complete valid user is finalized for the first time
- **THEN** one exact-model profile, all matching semantic ledger identities, and rebuild coverage commit atomically

#### Scenario: User has an existing semantic profile
- **WHEN** a complete reconstruction includes historical and live catch-up facts
- **THEN** the exact-model row is transactionally replaced at one version above the locked version while other models and the hash profile remain unchanged

#### Scenario: Applied identity already matches
- **WHEN** a historical fact already has an exact-model ledger row with the same stable payload hash
- **THEN** ledger upsert is idempotent and the fact contributes exactly once to the complete reconstructed profile

#### Scenario: Applied identity conflicts
- **WHEN** an existing exact-model identity has another payload hash
- **THEN** the complete user transaction rolls back with bounded `conflict` status

#### Scenario: Embedding changes during computation
- **WHEN** an exact-model embedding digest differs when stable-ordered rows are locked for finalization
- **THEN** the transaction rolls back and the user is retried or deferred without committing a mixed-version profile

#### Scenario: Transaction fails after profile write begins
- **WHEN** ledger, coverage, or commit persistence fails
- **THEN** profile replacement and every associated rebuild write roll back together

### Requirement: Idempotent Replay and Exact-Model Rebuild Coverage
Rebuild SHALL persist compact coverage per `(user_id, model, profile_schema)` containing the successfully included run and catch-up fences, committed profile version, and completion time. Default mode SHALL skip replacement only when coverage proves the requested fence is already included and current profile state is not older than that coverage. Replaying completed work MUST NOT churn profile version or timestamps.

#### Scenario: Crash occurs after user commit before page checkpoint
- **WHEN** restart reselects a user whose profile, ledgers, and coverage already committed
- **THEN** the replay is classified `already_current` without changing profile version, vectors, ledger rows, or timestamps

#### Scenario: Coverage exists but live profile is newer
- **WHEN** the current profile version exceeds the coverage version because later live events were applied
- **THEN** rebuild does not restore the covered version and uses the bounded catch-up protocol if reconstruction is still required

#### Scenario: Coverage fence is older
- **WHEN** a later run captures source maxima beyond the user's stored coverage
- **THEN** the user is eligible for complete reconstruction through the newer fence

#### Scenario: Another model has coverage
- **WHEN** a user is complete for one semantic model but selected for another supported model
- **THEN** coverage and idempotency decisions are independent for the selected exact model

### Requirement: Atomic Checkpoint, Lease, Cancellation, and Restart
A non-dry rebuild SHALL store a versioned run checkpoint in PostgreSQL and SHALL require a single renewable run lease. The checkpoint SHALL bind run ID, exact model/schema, mode, semantic-rules identity, source fences, user horizon, compatible option hash, last completed user cursor, and bounded counters. User cursor advancement SHALL occur atomically only after every user in the page has a durable terminal page outcome.

#### Scenario: Process stops in the middle of a user
- **WHEN** cancellation, runtime expiry, or failure interrupts scanning or finalization
- **THEN** the in-flight transaction rolls back and the last committed user-page checkpoint remains authoritative

#### Scenario: Process stops after user commits
- **WHEN** one or more users commit but the page checkpoint has not advanced
- **THEN** restart replays at most that bounded page and idempotency prevents duplicate contributions

#### Scenario: Checkpoint is incompatible
- **WHEN** restart supplies another model, schema, mode, semantic-rules identity, or incompatible source fence
- **THEN** the command fails closed before scanning or mutating profiles

#### Scenario: Two operators resume one run
- **WHEN** another process holds the unexpired run lease
- **THEN** the second process cannot advance the run

#### Scenario: Operator cancels the command
- **WHEN** SIGINT, SIGTERM, or context cancellation occurs
- **THEN** new work stops, the current transaction rolls back if incomplete, the lease is released or allowed to expire safely, and restart can continue from the committed checkpoint

### Requirement: Bounded Execution and Resumable Stop Reasons
The command SHALL enforce finite positive bounds with no unlimited mode. It SHALL validate user page size 1–1,000, fact page size 1–5,000, maximum users 1–1,000,000, maximum inspected events 1–100,000,000, per-user events 1–1,000,000, catch-up events 1–10,000, maximum runtime 1 minute–24 hours, and deferred retry passes 0–10. Defaults SHALL be finite and conservative.

#### Scenario: Maximum users is reached
- **WHEN** the run exhausts its user budget after committed page progress
- **THEN** it stops with resumable `max_users_reached` and preserves its checkpoint

#### Scenario: Maximum events is reached
- **WHEN** baseline or catch-up inspection would exceed the remaining event budget
- **THEN** no unbounded user transaction starts and the run stops or defers the user with resumable `max_events_reached`

#### Scenario: Maximum runtime expires
- **WHEN** the shared deadline is reached
- **THEN** in-flight work is cancelled, incomplete mutation rolls back, and the command reports resumable `max_runtime_reached`

#### Scenario: User exceeds the per-user event bound
- **WHEN** a selected user has more normalized facts than the configured limit
- **THEN** that user is durably deferred as `event_limit` without a partial profile

#### Scenario: Horizon is complete
- **WHEN** the primary user horizon and permitted deferred passes have no remaining runnable user
- **THEN** the run stops with `horizon_complete` and reports whether coverage is complete or still deferred

### Requirement: Dry-Run and Guarded Force Rebuild
Dry-run SHALL perform bounded selection, normalization, exact-model vector validation, reconstruction calculation, catch-up estimation, and reporting without creating or changing run checkpoints, leases, deferrals, coverage, profiles, or ledgers. Force mode SHALL ignore prior exact-model rebuild coverage only after `--confirm-model` exactly equals the complete selected persistence key.

#### Scenario: Dry-run finds complete users
- **WHEN** users can be safely reconstructed within configured bounds
- **THEN** the command reports `would_rebuild` counts and computed coverage without changing database state

#### Scenario: Dry-run finds missing vectors
- **WHEN** selected facts lack valid exact-model embeddings
- **THEN** missing and deferred counts are reported without writing deferral or applied-event rows

#### Scenario: Force confirmation is absent or imprecise
- **WHEN** `--force` is supplied without the complete exact model key, or with a prefix, alias, wildcard, upstream model name, or another model
- **THEN** the command fails before run creation or profile mutation

#### Scenario: Confirmed force rebuild runs
- **WHEN** `--force` and `--confirm-model=semantic-minilm-l12-v2@e8f8c211226b894f` are supplied with finite limits
- **THEN** covered candidate users may be fully reconstructed while profile version safety, idempotency, live catch-up, and model isolation remain enforced

### Requirement: Progress, Coverage, Missing-Vector Metrics, and Safe Summaries
Rebuild SHALL expose bounded-cardinality metrics for users and facts scanned, committed, already current, dry-run eligible, deferred, conflicted, missing or invalid vectors, baseline and catch-up work, durations, checkpoint results, run coverage, last progress time, and lease state. Periodic and final summaries SHALL report bounded progress and SHALL distinguish complete coverage from deferred coverage.

#### Scenario: User commits successfully
- **WHEN** one-user finalization commits
- **THEN** committed-user, fact, duration, checkpoint, and coverage metrics update using only fixed model aliases and allowlisted outcomes

#### Scenario: Missing vectors defer users
- **WHEN** one or more users cannot be rebuilt because exact-model vectors are absent
- **THEN** metrics and summaries report missing-vector facts and deferred users and do not claim full run coverage

#### Scenario: Run stops at a configured limit
- **WHEN** users, events, runtime, cancellation, or horizon completion stops execution
- **THEN** exactly one final summary includes safe stop reason, elapsed time, completed pages, bounded counts, and coverage numerator/denominator

#### Scenario: Sensitive or high-cardinality data is present
- **WHEN** processing involves user, video, event, run, cursor, fence, path, hash, vector, title, URL, model database string, token, or raw database error values
- **THEN** those values do not appear in metric labels or normal periodic/final summaries

### Requirement: Dedicated Operator Composition and Model Isolation
Frux SHALL provide `cmd/rebuild-semantic-user-interest` as a one-shot operator command and a manual container/Compose entrypoint. It SHALL use PostgreSQL and the existing static semantic contracts only, SHALL NOT call the embedding service, and SHALL NOT require Redis or Kafka. Every scan and mutation SHALL be constrained to the selected exact model and profile schema.

#### Scenario: Command runs with the semantic service stopped
- **WHEN** all required exact-model vectors already exist in PostgreSQL
- **THEN** reconstruction proceeds without an inference request

#### Scenario: Another semantic model exists
- **WHEN** a user has profiles, ledgers, coverage, or video embeddings for another model
- **THEN** selected-model rebuild neither reads them as substitutes nor changes them

#### Scenario: Hash profile and author affinities exist
- **WHEN** reconstruction commits for a user with existing non-semantic recommendation state
- **THEN** `user_interest_profile`, hash vectors, author affinities, and their applied-event ledger remain unchanged

#### Scenario: Manual container entrypoint is not invoked
- **WHEN** the normal API, worker, and Compose stack starts
- **THEN** no historical semantic user-interest rebuild starts automatically

### Requirement: Verification, Documentation, and Scope Boundary
Implementation SHALL include unit, PostgreSQL, concurrency, cancellation, restart, metrics, command, container, and dependency-integration tests. Tests SHALL cover delayed and out-of-order events, live races on both sides of the shared lock, missing vectors that later appear, decay equivalence, cursor replay, force guards, exact-model isolation, and unchanged recommendation behavior. Documentation SHALL define prerequisites, bounds, dry-run, force confirmation, coverage repair, cancellation/restart, metrics, summaries, rollout, and rollback.

#### Scenario: Delayed and out-of-order equivalence suite runs
- **WHEN** identical eligible facts are reconstructed and projected live under canonical, reversed, and delayed delivery schedules
- **THEN** semantic vectors, materialization time, and ledger identities satisfy the shared deterministic contract

#### Scenario: Live-race suite runs
- **WHEN** live projection is paused before and after the shared advisory lock while rebuild finalizes
- **THEN** every eligible fact contributes exactly once and no newer live profile version is overwritten

#### Scenario: Restart suite runs
- **WHEN** execution is interrupted before user commit, after user commit, and before page checkpoint advancement
- **THEN** restart reaches the same profile and coverage without duplicate contribution or partial state

#### Scenario: Existing recommendation flow runs
- **WHEN** reconstructed profiles are present, absent, deferred, or invalid
- **THEN** current recall, ranking, policy, Feed, API, Web, hash-profile, and author-affinity behavior remains unchanged

#### Scenario: Implementation artifacts are inspected
- **WHEN** the completed change is reviewed
- **THEN** it contains no live projection behavior change, pgvector or ANN artifact, semantic recall/ranking/policy use, training behavior, author-affinity duplication, public API, Web behavior, or main-spec edit

#### Scenario: Strict validation runs
- **WHEN** proposal artifacts are complete
- **THEN** `openspec validate --all --strict` succeeds
