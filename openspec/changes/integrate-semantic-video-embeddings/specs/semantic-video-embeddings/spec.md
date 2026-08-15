## ADDED Requirements

### Requirement: Recommendation Roadmap and Kafka Prerequisite Gate
Implementation of this capability SHALL begin only after recommendation-roadmap steps 1-5 and
`migrate-video-workflows-to-kafka` are completed and archived. The implementation SHALL consume the
fixed semantic-service contract and the retained `frux.video.published.v1` Kafka contract supplied
by those prerequisites rather than implementing them early or preserving a retired broker publication
path.

#### Scenario: A recommendation prerequisite is still active
- **WHEN** any of steps 1-5 is not completed and archived
- **THEN** no implementation task in this change is treated as started or complete

#### Scenario: The video Kafka migration is incomplete
- **WHEN** `migrate-video-workflows-to-kafka` is not completed and archived
- **THEN** semantic video integration does not add an alternative publication transport

#### Scenario: All prerequisites are archived
- **WHEN** steps 1-5 and the video Kafka migration are archived
- **THEN** implementation may consume their accepted service and publication contracts without expanding their scope

### Requirement: Fixed Semantic Model Integration
Frux SHALL accept only model `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2` at
revision `e8f8c211226b894fcb81acc59f3b34ba3efd5f42`, dimension 384, `float32`, CPU, and
L2-normalized output. Persisted vectors SHALL use model key
`semantic-minilm-l12-v2@e8f8c211226b894f`; another model or revision MUST use a different future
key.

#### Scenario: Service metadata matches
- **WHEN** a worker validates the model, revision, dimension, dtype, device, normalization, and service limits
- **THEN** its local semantic claim gate may open

#### Scenario: Service metadata differs
- **WHEN** any fixed metadata field differs
- **THEN** the local semantic claim gate remains closed and no response from that service is persisted

### Requirement: Bounded Authenticated Semantic Client
The Go client SHALL authenticate only with the configured `X-Internal-Token`, impose bounded
connections, concurrency, deadlines, batches, and response sizes, and perform no automatic
retries. It SHALL reject invalid local URLs, weak or non-ASCII tokens, and timeout or capacity
values outside configured safe bounds.

#### Scenario: A valid bounded request succeeds
- **WHEN** a conforming service returns an embedding batch before the deadline
- **THEN** the client returns only fully validated vectors and releases its bounded capacity

#### Scenario: A request exceeds a bound
- **WHEN** the deadline, response-size limit, batch limit, or concurrency limit is exceeded
- **THEN** the client cancels or rejects the operation with a bounded result and exposes no input text or token

#### Scenario: Remote validation fails during startup
- **WHEN** the configured service is unavailable, unauthenticated, unready, or metadata-incompatible
- **THEN** unrelated worker startup continues while the local semantic claim gate stays closed

### Requirement: Strict Semantic Response Validation
The client SHALL verify exact model metadata, item count, item IDs, zero-based indexes, request
order, component count, finiteness, and unit norm within `1e-4`. It SHALL reject the complete batch
when any item is missing, partial, reordered, duplicated, unknown, non-finite, wrongly dimensioned,
wrongly versioned, or non-normalized.

#### Scenario: Ordered valid output is returned
- **WHEN** every requested `video:<id>` has one correctly ordered 384-component finite unit vector
- **THEN** the vectors may be normalized defensively and passed to conditional persistence

#### Scenario: One item violates the contract
- **WHEN** any output identity, index, order, dimension, value, norm, model, or revision is invalid
- **THEN** no item from that response is persisted and the affected replica closes its local claim gate

### Requirement: Hash-First Kafka Publication Handoff
The existing hash-embedding consumer of `frux.video.published.v1` SHALL validate the versioned
envelope and video-ID key, canonicalize bounded title and description, persist or confirm
`hash-ngram-v1`, and durably upsert the fixed semantic job before the Kafka record is eligible for
offset commit. Semantic inference SHALL NOT run in the Kafka handler.

#### Scenario: A new publication is processed
- **WHEN** a valid publication has no current hash fact or semantic job
- **THEN** hash persistence completes first and the semantic job becomes durable before offset commit

#### Scenario: Hash persistence fails
- **WHEN** `hash-ngram-v1` cannot be durably saved or confirmed
- **THEN** no semantic handoff is accepted and the Kafka record remains uncommitted

#### Scenario: Semantic job persistence fails
- **WHEN** hash persistence succeeds but semantic job upsert fails
- **THEN** the Kafka record remains uncommitted and redelivery reuses the existing hash fact safely

#### Scenario: Offset commit is uncertain
- **WHEN** both durable writes complete but the Kafka offset commit fails or is uncertain
- **THEN** the consumer session ends and redelivery remains idempotent

#### Scenario: The publication contract is invalid
- **WHEN** the envelope, event identity, key, timestamp, payload, or canonical text violates the accepted contract
- **THEN** the embedding handler terminally classifies the record according to the Kafka backbone policy without creating model work

### Requirement: PostgreSQL-Owned Semantic Job State
Semantic work SHALL be represented by one PostgreSQL job per `(video_id, model)` with canonical
text hash, bounded state, attempts, `available_at`, lease owner and expiry, bounded error class,
completion metadata, and timestamps. Claims SHALL be bounded, stably ordered, and use
`SKIP LOCKED`; expired processing leases SHALL be reclaimable.

#### Scenario: Semantic execution succeeds
- **WHEN** a lease owner validates and conditionally persists the expected semantic fact
- **THEN** the same fenced operation marks the expected job complete

#### Scenario: A retryable failure occurs
- **WHEN** inference returns timeout, overload, unavailable, authentication, readiness, or service-contract failure
- **THEN** the job is released retryably after 5 seconds, 30 seconds, 2 minutes, 10 minutes, then at most 30 minutes

#### Scenario: One replica is unhealthy
- **WHEN** one replica cannot validate or reach the service
- **THEN** only that replica stops claiming while another healthy replica may process shared pending or retry jobs

#### Scenario: A lease expires
- **WHEN** a worker exits or loses ownership before completion
- **THEN** another worker may reclaim the job without resetting a Kafka offset

#### Scenario: Canonical text changes
- **WHEN** a later accepted publication carries a different canonical text hash
- **THEN** the same model job resets to pending and stale completion for the prior hash cannot overwrite it

### Requirement: Side-by-Side Conditional Persistence
Frux SHALL store the semantic vector in the existing video embedding store beside
`hash-ngram-v1`, using unique `(video_id, model)` identity. The semantic row SHALL contain dimension
384, the fixed revision-bearing model key, canonical text hash, finite L2-normalized bounded JSON,
and timestamps. This capability SHALL add no pgvector column, ANN index, or recommendation vector
table.

#### Scenario: Both representations exist
- **WHEN** hash and semantic generation succeed for one video
- **THEN** exactly one hash row and one fixed semantic-model row coexist for that video

#### Scenario: The same semantic fact is repeated
- **WHEN** duplicate work presents the same video, model, and text hash
- **THEN** conditional persistence keeps one fact without churning an unchanged row

#### Scenario: A stale worker attempts completion
- **WHEN** the worker no longer owns the lease or its expected text hash is obsolete
- **THEN** neither the semantic row nor job completion state is changed

### Requirement: Failure-Isolated Worker Composition
Once this integration is deployed, Kafka intake SHALL create semantic jobs independently of
semantic execution readiness. Each replica SHALL claim jobs only while its local validated gate is
open. Semantic unavailability MUST NOT prevent Kafka hash processing, Feed, fanout, media,
moderation, action, view-event, or other database workers from starting or progressing.

#### Scenario: Semantic execution is disabled
- **WHEN** a replica is configured not to execute semantic jobs
- **THEN** it refrains from claims while hash-first intake continues to persist shared durable jobs

#### Scenario: The semantic service is unavailable
- **WHEN** readiness or inference capacity is lost
- **THEN** semantic jobs remain pending or retryable and unrelated workers continue

#### Scenario: Compose is rendered
- **WHEN** Compose uses a strong shared internal token
- **THEN** the worker points to the internal-only semantic service with bounded settings and a `service_started` dependency

#### Scenario: The service recovers
- **WHEN** metadata validation later succeeds
- **THEN** the local gate reopens and available jobs resume without duplicating hash or semantic facts

### Requirement: Bounded Semantic Observability
Frux SHALL expose bounded-cardinality metrics for metadata and embedding requests, local semantic
gate readiness, hash and semantic intake outcomes, job count and oldest age by bounded state,
leases, retries, and readable-video semantic coverage. Metrics and logs MUST NOT include video IDs,
text, URLs, tokens, vectors, raw errors, retry numbers, or arbitrary model strings.

#### Scenario: A semantic request completes
- **WHEN** metadata or embedding HTTP work finishes
- **THEN** count and latency are recorded with only fixed operation and bounded result labels

#### Scenario: Backlog sampling succeeds
- **WHEN** semantic job state is sampled
- **THEN** count and oldest age are reported by bounded state without per-video labels

#### Scenario: Coverage sampling succeeds
- **WHEN** readable published videos are sampled
- **THEN** gauges report fixed-model coverage without changing recommendation behavior

### Requirement: Live-Only Future Boundary
This capability SHALL process semantic embeddings only through normal new-video publication
delivery. It SHALL NOT add a historical scan, backfill command or job, cursor, checkpoint, dry-run,
re-embedding mode, semantic user profile, pgvector/ANN query, recall provider, ranking feature,
policy change, online request-path inference, media lifecycle behavior, or retired broker semantic route.

#### Scenario: Historical videos lack semantic rows
- **WHEN** existing videos emit no new accepted publication event
- **THEN** this change does not scan, enqueue, or generate semantic vectors for them

#### Scenario: Semantic rows are present
- **WHEN** one or more videos have the fixed semantic model row
- **THEN** current recommendation recall, ranking, policies, APIs, and fallbacks behave exactly as before

#### Scenario: Future backfill is proposed
- **WHEN** `backfill-semantic-video-embeddings` is implemented
- **THEN** it depends on this capability while owning historical selection, resumability, operator controls, and backfill-specific retry behavior

### Requirement: Verification and Documentation
The implementation SHALL include model/canonicalization unit tests, semantic HTTP contract tests,
Kafka intake and offset-boundary tests, PostgreSQL job and persistence tests, worker failure-isolation
tests, Compose assertions, outage/recovery tests, and migration assessment. Documentation SHALL
cover prerequisites, fixed model identity, hash-first Kafka intake, database retries and leases,
metrics, failure modes, rollout, rollback, and future boundaries.

#### Scenario: Outage recovery is tested
- **WHEN** a publication is consumed while semantic inference is unavailable and the service later recovers
- **THEN** hash coverage and a durable job exist during the outage and exactly one semantic fact appears after retry

#### Scenario: Strict validation runs
- **WHEN** the corrected planning and implementation artifacts are validated
- **THEN** `openspec validate --all --strict` succeeds without adding an extra broker semantic route or recommendation consumption
