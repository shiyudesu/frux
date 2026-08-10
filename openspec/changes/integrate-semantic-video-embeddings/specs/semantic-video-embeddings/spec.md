## ADDED Requirements

### Requirement: Fixed Semantic Video Model Integration
Frux SHALL integrate only the semantic embedding contract defined by the active `add-semantic-embedding-service` change: model `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2`, revision `e8f8c211226b894fcb81acc59f3b34ba3efd5f42`, dimension 384, `float32`, CPU, and L2-normalized output. Persisted vectors SHALL use the fixed model key `semantic-minilm-l12-v2@e8f8c211226b894f`, and a different model or revision MUST use a different future model key.

#### Scenario: Planned service contract is available
- **WHEN** semantic integration validates the internal model metadata endpoint
- **THEN** the exact model, revision, dimension, dtype, device, normalization mode, and bounded service limits match the `add-semantic-embedding-service` contract before semantic requests are accepted

#### Scenario: Service advertises another contract
- **WHEN** metadata differs in model, revision, dimension, dtype, device, normalization, or required limits
- **THEN** semantic generation remains unavailable and no response from that service is persisted

### Requirement: Bounded Authenticated Go Client
The Go semantic client SHALL use the configured strong Frux internal token only through `X-Internal-Token`, SHALL impose fixed connection, concurrency, request, and response-size bounds, and SHALL NOT automatically retry requests. Configuration SHALL reject an invalid base URL, disabled or weak internal authentication, metadata timeouts outside 500 milliseconds–5 seconds, embedding timeouts outside 1–20 seconds, or coverage intervals outside 1 minute–1 hour.

#### Scenario: Valid request succeeds
- **WHEN** an enabled client sends a bounded batch to a healthy conforming service
- **THEN** it uses at most two connections and two in-flight requests, completes within the configured deadline, and returns only validated vectors

#### Scenario: Service does not finish in time
- **WHEN** metadata or embedding processing exceeds the configured deadline
- **THEN** the client cancels the request, closes or reuses resources safely, and returns the bounded `timeout` result without a vector

#### Scenario: Response body is excessive
- **WHEN** metadata exceeds 16 KiB or an embedding response exceeds 1 MiB
- **THEN** the client rejects the response as a contract failure without reading or logging an unbounded body

#### Scenario: Authentication fails
- **WHEN** the service rejects the internal token
- **THEN** the client returns the bounded `auth` result and neither logs nor exposes the token

### Requirement: Strict Semantic Response Validation
The client SHALL verify every embedding response's model, revision, dimension, item count, IDs, zero-based indexes, request order, component count, finiteness, and unit norm within `1e-4`. It SHALL reject partial, reordered, duplicated, unknown, non-finite, wrongly dimensioned, wrongly versioned, or non-normalized output, and SHALL L2-normalize a valid vector before bounded JSON persistence.

#### Scenario: Ordered batch is returned
- **WHEN** a response contains one exact output for every requested `video:<id>` in request order
- **THEN** all 384 finite components pass validation and the normalized vectors may be persisted

#### Scenario: Output order or identity changes
- **WHEN** an item ID, index, count, or order differs from the request
- **THEN** the complete response is rejected and no item from that response is persisted

#### Scenario: Vector is invalid
- **WHEN** any component is non-finite, the dimension is not 384, or the norm is outside tolerance
- **THEN** the complete response is rejected with a safe `contract` result and no partial vector is stored

### Requirement: Independent Hash Fallback Configuration
The worker SHALL always compose and run `hash-ngram-v1` generation. Semantic generation SHALL be independently enabled by validated configuration and SHALL remain optional for non-Compose deployments. A local semantic configuration error MAY fail worker startup, but remote unavailability, authentication rejection, readiness failure, or metadata mismatch MUST NOT prevent hash generation or unrelated workers from starting.

#### Scenario: Semantic integration is disabled
- **WHEN** the worker receives a video-published event with semantic generation disabled
- **THEN** it generates or skips the current hash vector, persists a pending semantic job, and commits the Kafka record without calling the semantic service

#### Scenario: Enabled service is unavailable at startup
- **WHEN** the startup metadata probe fails within its bounded deadline
- **THEN** the worker starts with the semantic gate closed, continues hash processing, and retries metadata validation in the background

#### Scenario: Local semantic configuration is invalid
- **WHEN** enabled configuration has an invalid URL, token dependency, or timeout bound
- **THEN** worker startup fails before opening semantic consumers

### Requirement: Hash-First Idempotent Published-Event Processing
For every valid Kafka video-publication event, Frux SHALL canonicalize title and description according to the dependent service contract, persist or skip `hash-ngram-v1` first, and persist a semantic-job handoff before committing the publication offset. The fixed semantic model SHALL be attempted later by a leased PostgreSQL worker. Identical `(video_id, model, text_hash)` work SHALL not create another fact or update an unchanged row; changed canonical text MAY replace the row for that model.

#### Scenario: New publication is processed
- **WHEN** neither model exists for a published video
- **THEN** Kafka intake persists hash coverage first and then creates the semantic job before committing

#### Scenario: Duplicate event is delivered
- **WHEN** both model rows already carry the same canonical text hash
- **THEN** intake skips identical writes and commits without creating duplicate facts or jobs

#### Scenario: Hash persistence fails
- **WHEN** the hash vector cannot be durably saved
- **THEN** intake does not create semantic work and leaves the Kafka record uncommitted

#### Scenario: Published text changes
- **WHEN** a later event for the same video has a different canonical text hash
- **THEN** each enabled model may update its single `(video_id, model)` row to the new bounded vector

### Requirement: Kafka Intake and Durable Semantic Job Handoff
The registered embedding Kafka group SHALL commit a video-publication offset only after hash persistence and a PostgreSQL semantic job keyed by `(video_id, model)` commit. Semantic execution, retries, suspension, leases, and terminal outcomes SHALL NOT be represented by Kafka retry topics, RabbitMQ retry queues, broker headers, or an uncommitted publication record.

#### Scenario: Semantic handoff commits
- **WHEN** hash persistence and semantic-job upsert commit for a valid publication event
- **THEN** the publication record becomes eligible for Kafka offset commit without waiting for remote inference

#### Scenario: Offset commit is uncertain
- **WHEN** intake commits its durable boundary but Kafka offset commit fails
- **THEN** the consumer session ends and redelivery remains safe through conditional hash persistence and semantic-job identity

#### Scenario: Event contract is invalid
- **WHEN** the registered publication envelope, video-ID key, event identity, timestamp, or payload is invalid
- **THEN** the record is terminally classified without model work

#### Scenario: Publication text is incompatible only with semantic intake
- **WHEN** a publication record satisfies the video-owned envelope, identity, timestamp, key, and payload bounds but semantic canonicalization rejects its title or description
- **THEN** the shared publication decoder and Feed consumer accept the record, while only the embedding group classifies its intake result as terminal and commit-safe

### Requirement: PostgreSQL-Owned Semantic Retry State
Semantic jobs SHALL store canonical text hash, bounded state, attempts, `available_at`, lease owner/until, bounded error class, and completion metadata. Claims SHALL be bounded and stably ordered. Retry delays SHALL be 5 seconds, 30 seconds, 2 minutes, 10 minutes, then capped at 30 minutes.

#### Scenario: Semantic service is temporarily unavailable
- **WHEN** a leased request returns timeout, overload, authentication, unavailable, or retryable contract failure
- **THEN** the job releases its lease and becomes available after the capped database retry delay

#### Scenario: Semantic integration is disabled
- **WHEN** semantic execution is intentionally disabled
- **THEN** the disabled replica refrains from claims, pending/retry jobs remain shared durable work, and hash coverage continues without a cluster-wide state rewrite

#### Scenario: One replica cannot validate metadata
- **WHEN** a semantic replica has local metadata or connectivity failure while another replica is healthy
- **THEN** only the unhealthy replica keeps its claim gate closed and the healthy replica may continue claiming pending, retry, or legacy suspended jobs

#### Scenario: One replica receives an invalid service response
- **WHEN** runtime response validation fails on one replica
- **THEN** that replica closes its local gate and releases the job retryably rather than terminally poisoning shared work

#### Scenario: Worker exits with a lease
- **WHEN** a semantic worker stops before completing a job and its lease expires
- **THEN** another worker reclaims the same job without resetting a Kafka offset

#### Scenario: Heartbeat storage stalls or processing shuts down
- **WHEN** a semantic lease heartbeat blocks in PostgreSQL or the processing context is canceled
- **THEN** the heartbeat uses a bounded child context, returns promptly, cancels inference, and the uncertain attempt cannot complete or retry the job

#### Scenario: Canonical text changes
- **WHEN** intake observes a new canonical text hash for the same video and model
- **THEN** the single semantic job resets to pending for the new hash and stale completion cannot overwrite it

### Requirement: Side-by-Side Normalized Persistence
Frux SHALL store semantic vectors in the existing `video_embedding` table beside hash vectors using unique `(video_id, model)` semantics. Semantic rows SHALL contain dimension 384, the fixed revision-bearing model key, canonical text hash, finite L2-normalized bounded JSON, and timestamps. This capability SHALL require no pgvector column, ANN index, or new vector table.

#### Scenario: Both models exist
- **WHEN** hash and semantic generation succeed for one video
- **THEN** PostgreSQL contains exactly one `hash-ngram-v1` row and one fixed semantic-model row for that video

#### Scenario: Same semantic fact is written concurrently
- **WHEN** duplicate workers save the same video, model, and text hash
- **THEN** the composite identity retains one fact and an identical conflict does not churn its update timestamp

#### Scenario: Existing schema is assessed
- **WHEN** migration validation runs
- **THEN** the fixed model key fits the current column and a 384-component normalized JSON vector round-trips without schema DDL

### Requirement: Semantic Embedding Observability
Frux SHALL expose bounded-cardinality metrics for metadata and embedding request count/latency/result, hash and semantic live-event vector outcomes, retries, readable-video semantic coverage, and PostgreSQL semantic-job count and oldest age by bounded state. Metrics MUST NOT label video IDs, text, URLs, raw errors, retry numbers, tokens, vectors, or arbitrary model strings.

#### Scenario: Semantic request completes
- **WHEN** metadata or embedding HTTP work finishes
- **THEN** request count and latency are observed with only the fixed operation and bounded result labels

#### Scenario: Vector work changes state
- **WHEN** a live published event generates, skips, retries, or fails a fixed model
- **THEN** the bounded vector outcome counter is incremented

#### Scenario: Coverage sampling runs
- **WHEN** the configured 1-minute–1-hour sampling interval elapses
- **THEN** gauges report readable published videos with and without the fixed semantic model

#### Scenario: Backlog sampling runs
- **WHEN** PostgreSQL semantic-job sampling succeeds
- **THEN** gauges report count and oldest age for pending, processing, retry, suspended, completed, and failed states without per-attempt labels

### Requirement: Compose and Failure Isolation
Compose SHALL configure the worker to call the internal `semantic-embedding` service with the shared strong token and SHALL declare a `service_started` dependency rather than a health-gated dependency. The semantic service SHALL remain internal-only. A semantic outage MUST NOT prevent worker startup, hash generation, or progress by fanout, action, view-event, media, or other consumers.

#### Scenario: Compose configuration is rendered
- **WHEN** Compose is rendered with a strong `FRUX_INTERNAL_TOKEN`
- **THEN** the worker has semantic enablement on by default, the internal service URL, token, bounded configuration, and a `service_started` dependency

#### Scenario: Semantic container is unhealthy
- **WHEN** the semantic container fails readiness or becomes unavailable
- **THEN** the worker continues hash and unrelated consumer work while semantic jobs remain in capped database retries

#### Scenario: Semantic service recovers
- **WHEN** metadata validation later succeeds and pending jobs become available
- **THEN** missing semantic rows are generated without duplicating existing hash or semantic facts

#### Scenario: Publication transport is unavailable during worker startup
- **WHEN** Kafka or RabbitMQ publication dispatch cannot connect
- **THEN** reconnect supervisors expose unhealthy transport/session metrics, publication retry remains durable, and hash, semantic, Feed, media, moderation, and unrelated database workers remain eligible to start

#### Scenario: Semantic inference capacity is lost
- **WHEN** one or all required inference workers are unavailable
- **THEN** semantic readiness fails, replacements retry with bounded backoff, and readiness recovers only after full required capacity returns

#### Scenario: Completed publication handoff is cleaned
- **WHEN** a dispatched operational publication row ages beyond the replay window
- **THEN** bounded cleanup retains the immutable publication fact and reconciliation does not re-emit the event

### Requirement: Verification and Documentation
The implementation SHALL include unit, HTTP contract, Kafka intake/commit, PostgreSQL semantic-job, live semantic-service contract, Compose, outage-recovery, and migration-assessment tests. Documentation SHALL cover configuration, fixed model identity, hash-first intake, database retry/lease/suspension behavior, metrics, failure modes, rollout, rollback, the dependency on `add-semantic-embedding-service` and `migrate-video-workflows-to-kafka`, and the future backfill boundary.

#### Scenario: Client contract suite runs
- **WHEN** tests exercise timeouts, overload, auth rejection, oversized/truncated responses, metadata mismatch, wrong identity/order/dimension, non-finite values, and non-unit vectors
- **THEN** each response is classified safely and no invalid vector is persisted

#### Scenario: Outage integration test runs
- **WHEN** the semantic service is unavailable for a published event and later recovers
- **THEN** hash coverage exists during the outage and semantic coverage appears after delayed retry without duplicate facts

#### Scenario: Strict validation runs
- **WHEN** the proposal artifacts are complete
- **THEN** `openspec validate --all --strict` succeeds without modifying main specifications

### Requirement: Future Historical Backfill Boundary
This capability SHALL process semantic embeddings only from live video-published deliveries. It SHALL NOT add a historical scan, backfill command or job, cursor, checkpoint, dry-run, re-embedding mode, or backfill-specific retry loop. Existing historical videos without the fixed semantic model SHALL remain unchanged unless they later produce a normal video-published event. A future separate change named `backfill-semantic-video-embeddings` SHALL own historical selection and resumable operator behavior and MAY consume the fixed model identity, canonicalization, bounded validated client, conditional persistence, and coverage interfaces established here.

#### Scenario: Integration is deployed over an existing catalog
- **WHEN** historical published videos have no fixed semantic-model row and emit no new video-published event
- **THEN** this change does not scan, enqueue, or generate semantic vectors for them

#### Scenario: Future backfill is planned
- **WHEN** `backfill-semantic-video-embeddings` is proposed
- **THEN** it depends on this integration and the semantic service while defining its own scans, resumability, operator controls, and backfill-specific retry behavior

### Requirement: No Recommendation Consumption
This capability SHALL only generate and store semantic video embeddings. It SHALL NOT add a recommendation recall provider, ranking feature, profile input, policy field, pgvector/ANN query, online request-path inference, or training behavior, and it SHALL NOT remove the hash fallback. A later `add-pgvector-recommendation-recall` change MAY consume these stored facts.

#### Scenario: Semantic vectors are present
- **WHEN** one or more videos have the fixed semantic model row
- **THEN** current recommendation recall, ranking, policies, APIs, and fallbacks behave exactly as before

#### Scenario: Semantic vectors are absent
- **WHEN** the service is disabled or coverage is incomplete
- **THEN** current recommendation behavior continues to use its existing hash or non-vector fallback without failing the Feed
