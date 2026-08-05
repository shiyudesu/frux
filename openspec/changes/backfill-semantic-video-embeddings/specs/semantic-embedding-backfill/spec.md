## ADDED Requirements

### Requirement: Fixed Dependency and Model Contract
The historical backfill SHALL depend on the completed `add-semantic-embedding-service` and narrowed `integrate-semantic-video-embeddings` changes. It SHALL use only the authenticated fixed semantic service contract, canonical title/description hashing, validated client, finite 384-dimensional vector construction, and versioned persistence key `semantic-minilm-l12-v2@e8f8c211226b894f` defined by those changes.

#### Scenario: Dependency contracts are available
- **WHEN** the backfill command starts
- **THEN** it validates the exact semantic model metadata and uses the shared integration primitives before scanning any video

#### Scenario: Dependency contract is unavailable or mismatched
- **WHEN** the service metadata, model key, dimension, normalization, or required client/repository capability differs from the dependent contracts
- **THEN** the command fails before scanning or persisting and reports only a bounded dependency error class

### Requirement: Dedicated Bounded Operator Command
Frux SHALL provide a one-shot operator command separate from the API and worker. The command SHALL require positive bounded page size, service batch size, concurrency, maximum rows, and maximum runtime; SHALL reject unlimited values; and SHALL initialize PostgreSQL and the semantic client without requiring Redis or RabbitMQ.

#### Scenario: Valid bounded command starts
- **WHEN** an operator supplies valid configuration and options
- **THEN** the command starts with page size 1–1,000, batch size 1–32, concurrency 1–2, maximum rows 1–1,000,000, and maximum runtime 1 minute–24 hours

#### Scenario: Bound is invalid
- **WHEN** an option is zero, negative, above its maximum, or requests an unlimited run
- **THEN** the command exits before opening a scan or calling the semantic service

#### Scenario: Redis or RabbitMQ is absent
- **WHEN** PostgreSQL and the semantic service are available but Redis or RabbitMQ is not
- **THEN** the backfill can run because it does not initialize or use those dependencies

### Requirement: Eligible Stable Historical Selection
The repository SHALL scan only videos that are published, public, media-ready with status `legacy_ready` or `ready`, and have a non-null publication time. A new run SHALL freeze the greatest eligible `(published_at, id)` tuple as its horizon and SHALL return stable bounded pages ordered by `(published_at ASC, id ASC)`, strictly after the checkpoint tuple and no later than the frozen horizon.

#### Scenario: Missing-only page is read
- **WHEN** the default mode requests a page
- **THEN** the repository returns only eligible videos with no row for the exact semantic model in deterministic tuple order

#### Scenario: Catalog changes while scanning
- **WHEN** videos are inserted, gain embeddings, or become ineligible after the horizon is captured
- **THEN** offset shifts do not affect the run, no tuple beyond the horizon is read, and final eligibility is checked before persistence

#### Scenario: Equal publication times occur
- **WHEN** multiple eligible videos share the same `published_at`
- **THEN** ascending video ID breaks ties and the opaque cursor resumes strictly after the last completed tuple

### Requirement: Exact-Model Refresh Safeguards
The command SHALL support refresh modes `none`, `stale`, and `force`. Mode `none` SHALL skip every existing exact-model row. Modes `stale` and `force` MUST require `--confirm-model` to equal `semantic-minilm-l12-v2@e8f8c211226b894f`; no other confirmation value SHALL authorize refresh. Refresh and persistence MUST NOT delete, select for replacement, or update `hash-ngram-v1` or another model.

#### Scenario: Existing exact-model row is encountered by default
- **WHEN** refresh mode is `none`
- **THEN** the row is skipped without a semantic request even if its stored text hash is old

#### Scenario: Stale refresh is confirmed
- **WHEN** refresh mode is `stale`, exact-model confirmation matches, and the current canonical source hash differs from the stored exact-model hash
- **THEN** the row is eligible for semantic regeneration and conditional exact-model replacement

#### Scenario: Force refresh is confirmed
- **WHEN** refresh mode is `force` and exact-model confirmation matches
- **THEN** every scanned eligible row may be regenerated while identical persisted facts remain no-op writes

#### Scenario: Refresh confirmation is unsafe
- **WHEN** confirmation is missing, names another model, uses a prefix or wildcard, or names `hash-ngram-v1`
- **THEN** the command fails before scanning and no embedding is changed

### Requirement: Dry-Run and Bounded Stop Conditions
Dry-run SHALL perform the same bounded selection, canonical source hashing, refresh classification, and progress accounting without calling the semantic service, persisting an embedding, or creating or replacing a checkpoint. Non-dry runs SHALL stop on the frozen horizon, maximum scanned rows, maximum runtime, cancellation, or a terminal error.

#### Scenario: Dry-run identifies work
- **WHEN** an operator runs with `--dry-run`
- **THEN** the summary reports scanned, already-current, and would-generate counts without a service request, database mutation, or checkpoint mutation

#### Scenario: Maximum rows is reached
- **WHEN** scanned rows reach the configured maximum before the horizon
- **THEN** the command stops without fetching another row and reports `max_rows_reached`

#### Scenario: Maximum runtime is reached
- **WHEN** the runtime deadline expires
- **THEN** new work stops, in-flight work is canceled, only fully checkpointed page progress is resumable, and the command reports `max_runtime_reached`

### Requirement: Bounded Ordered Batch Processing and Retry
The runner SHALL split classified work into consecutive batches of at most the configured size, preserve candidate and request item order, and execute no more than the configured concurrency. Retryable timeout, over-capacity, and unavailable results SHALL receive at most three total attempts with cancellation-aware bounded delays; authentication, metadata, contract, invalid-input, and local configuration failures MUST NOT be retried.

#### Scenario: Multiple batches run
- **WHEN** a page contains more work than one service batch
- **THEN** batches preserve stable item identity and order while no more than two requests are in flight

#### Scenario: Service is temporarily unavailable
- **WHEN** a batch returns a retryable timeout, over-capacity, or unavailable result
- **THEN** the runner waits according to the bounded retry schedule and performs no more than three total attempts

#### Scenario: Service contract is invalid
- **WHEN** authentication, metadata, identity, order, dimension, finiteness, or normalization validation fails
- **THEN** the run stops without persisting any item from that invalid response or advancing the current page checkpoint

#### Scenario: Cancellation occurs during work
- **WHEN** SIGINT, SIGTERM, or the runtime context cancels an in-flight request or retry delay
- **THEN** no new batch is scheduled, all goroutines exit, and the current incomplete page remains replayable

### Requirement: Transactional Freshness and Eligibility Revalidation
Before writing each generated vector, the versioned embedding repository SHALL lock and re-read the current video inside the persistence transaction, recompute the canonical title/description hash, and verify published, public, media-ready eligibility. It SHALL persist only when the current hash equals the hash used for generation and the exact-model compare-and-set policy still permits the write.

#### Scenario: Source remains current
- **WHEN** the video remains eligible and its canonical source hash matches the generated item
- **THEN** the repository conditionally inserts or updates only the exact semantic model and commits before reporting `persisted`

#### Scenario: Title or description changes during inference
- **WHEN** the canonical source hash differs at persistence time
- **THEN** no embedding is written and the item is reported as `source_changed`

#### Scenario: Visibility, lifecycle, or media readiness changes
- **WHEN** the video becomes private, non-published, deleted, or non-ready before persistence
- **THEN** no embedding is written and the item is reported as `ineligible`

#### Scenario: Concurrent exact-model write wins
- **WHEN** live processing or another backfill persists a newer or identical exact-model fact first
- **THEN** the backfill does not overwrite the newer fact and reports `already_current` or a safe compare-and-set skip

### Requirement: Idempotent Side-by-Side Versioned Persistence
Backfill writes SHALL use the same `(video_id, model)` versioned embedding repository contract as live semantic integration. Replaying completed or partially completed work MUST NOT create duplicate rows or churn timestamps for an identical model, dimension, text hash, and serialized vector. The repository MUST NOT read-modify-write another model as part of a backfill save.

#### Scenario: Interrupted page is replayed
- **WHEN** some vectors were committed before failure but the page checkpoint was not advanced
- **THEN** restart safely skips or no-ops those facts and completes the remaining candidates

#### Scenario: Hash and semantic rows coexist
- **WHEN** a video has `hash-ngram-v1` and receives the fixed semantic backfill
- **THEN** both rows remain present and only the fixed semantic row is inserted or conditionally updated

#### Scenario: Identical forced result is persisted
- **WHEN** force mode regenerates a vector identical to the current exact-model fact
- **THEN** persistence is a no-op and the existing update timestamp is preserved

### Requirement: Opaque Atomic Checkpoint and Restart
A non-dry run SHALL require a checkpoint file. Its opaque cursor SHALL bind a format version, run ID, exact model key, refresh mode, frozen horizon, and last fully completed ordering tuple, and SHALL include corruption detection. The command SHALL replace the checkpoint only after every candidate in a page has a durable terminal outcome, using a mode-0600 sibling file, file flush, atomic rename, and parent-directory flush.

#### Scenario: Page completes
- **WHEN** every candidate in the page is durably persisted or classified as already current, ineligible, or source changed
- **THEN** the checkpoint is atomically replaced with a cursor after that page’s final tuple

#### Scenario: Page fails partway
- **WHEN** cancellation, service failure, persistence failure, or runtime expiry interrupts a page
- **THEN** the previous checkpoint remains intact and restart replays at most that incomplete page

#### Scenario: Checkpoint is incompatible or corrupt
- **WHEN** the file is truncated, fails its checksum, uses an unsupported version, or binds another model, mode, or inconsistent horizon
- **THEN** the command fails closed before scanning or calling the semantic service

#### Scenario: Restart changes safe execution bounds
- **WHEN** an operator resumes with a different page size, batch size, concurrency, row budget, runtime budget, or progress interval
- **THEN** the original model, mode, horizon, and completed tuple remain authoritative while the safe execution bounds may change

### Requirement: Safe Metrics, Progress, and Final Summary
The command SHALL expose bounded-cardinality metrics for row outcomes, batch results and duration, in-flight batches, checkpoint replacement results, and last progress time. It SHALL emit periodic structured progress and exactly one final summary. Metrics and output MUST NOT contain video IDs, title/description text, vectors, source hashes, tokens, URLs, checkpoint tokens or paths, arbitrary model labels, raw infrastructure errors, or retry-number labels.

#### Scenario: Work progresses
- **WHEN** pages and batches complete
- **THEN** fixed-label metrics and progress summaries report bounded counts, elapsed time, completed pages, service attempts, and last completed publication time

#### Scenario: Run stops
- **WHEN** the horizon, row limit, runtime limit, cancellation, or an error ends the command
- **THEN** the final summary reports one bounded stop reason and safe error class without exposing source or infrastructure details

#### Scenario: Metrics endpoint is enabled
- **WHEN** the configured internal metrics listener is active
- **THEN** it exposes only health and Prometheus metrics, has no public Compose port, and shuts down with the command

### Requirement: Container Entry Point and Operator Documentation
The API container build SHALL include the backfill binary. Compose SHALL provide a manually invoked backfill profile/service with PostgreSQL and semantic-service dependencies, no Redis or RabbitMQ dependency, no public port, and a persistent checkpoint mount. Operational documentation SHALL cover prerequisites, exact bounded commands, dry-run, refresh confirmation, metrics, progress, checkpoint durability, cancellation/restart, failure classes, verification, and rollback.

#### Scenario: Container image is built
- **WHEN** the API image build completes
- **THEN** it contains the API, worker, and semantic backfill binaries with the existing configuration files

#### Scenario: Manual Compose backfill runs
- **WHEN** an operator invokes the backfill profile with a mounted checkpoint directory
- **THEN** the one-shot command can reach PostgreSQL and the internal semantic service without starting a public endpoint or requiring Redis/RabbitMQ

#### Scenario: Operator follows rollback procedure
- **WHEN** a run must be stopped or rolled back
- **THEN** cancellation stops further work, the last completed-page checkpoint remains usable, and already stored versioned semantic facts are retained

### Requirement: Verification Across Unit and Integration Boundaries
The implementation SHALL include unit tests for options, confirmation, cursor integrity, ordering, classification, retries, cancellation, metrics, summaries, and checkpoint replacement; PostgreSQL tests for horizon pagination, eligibility, source changes, compare-and-set persistence, coexistence, and replay; live semantic-service contract tests; command interruption/restart tests; and container/Compose entrypoint tests.

#### Scenario: Concurrent catalog changes are tested
- **WHEN** integration tests change source text, visibility, lifecycle, media readiness, or exact-model rows during a run
- **THEN** only current eligible exact-model facts are persisted and deterministic checkpoint progress is preserved

#### Scenario: Resume matrix is tested
- **WHEN** tests interrupt missing, stale, and force runs before and after partial page writes
- **THEN** restart resumes from the last complete page, produces no duplicate facts, and never changes another model

#### Scenario: Strict validation runs
- **WHEN** proposal artifacts are complete
- **THEN** `openspec validate --all --strict` succeeds without modifying main specifications

### Requirement: No Live, Retrieval, Profile, Policy, or Training Changes
This capability SHALL NOT change live event processing, RabbitMQ queues or acknowledgements, pgvector or ANN schema, semantic retrieval, user-interest profile construction, recommendation providers, ranking or policy behavior, online request-path inference, or model training. It SHALL add no public API or Web behavior.

#### Scenario: Backfill is deployed
- **WHEN** the operator command and container entrypoint are available
- **THEN** existing API, worker, Feed, recommendation, profile, and Web behavior remain unchanged

#### Scenario: Semantic rows are backfilled
- **WHEN** historical videos receive the fixed semantic row
- **THEN** no recommendation component consumes those rows unless a separate accepted change explicitly adds that behavior
