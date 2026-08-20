# multimodal-video-embeddings Specification

## Purpose

Defines provider-neutral multimodal vector contracts, durable processing, authoritative persistence,
privacy boundaries, and failure isolation for newly published public videos.

## Requirements

### Requirement: Environment-neutral multimodal embedding contract
Frux SHALL access multimodal embeddings through an application-owned provider contract with one
bounded operation for public-video content and one bounded operation for normalized public search
query text. The two operations SHALL return finite L2-normalized vectors in the same immutable
model space, and no domain or application type SHALL expose a provider SDK, process model,
hardware, language, or deployment topology.

#### Scenario: Public video content is embedded
- **WHEN** an eligible job supplies normalized public title/description text and a bounded set of prepared cover or keyframe images
- **THEN** the provider returns exactly one validated vector and its complete immutable contract identity

#### Scenario: Search query text is embedded
- **WHEN** semantic public-video search supplies a valid normalized query under its configured deadline and admission bound
- **THEN** the provider returns one vector compatible with video vectors from the active contract

#### Scenario: Provider implementation changes
- **WHEN** operators replace a local, remote, or otherwise packaged provider implementation with another implementation of the same accepted contract
- **THEN** recommendation, search, video, and embedding domain/application interfaces require no provider-specific type or topology change

### Requirement: Immutable multimodal contract identity
Every accepted multimodal vector SHALL be bound to provider, model, immutable revision, dimension,
text canonicalizer, frame-sampling policy, image-preprocessing policy, fusion policy, source input
hash, and vector digest. Frux MUST NOT compare, fuse, query, or persist vectors as compatible when
any compatibility field differs.

#### Scenario: Provider returns the expected contract
- **WHEN** provider identity, revision, dimension, policies, input hash, finite components, norm, and digest match the requested contract
- **THEN** Frux may accept the vector for conditional persistence

#### Scenario: Provider returns an incompatible vector
- **WHEN** any compatibility field, dimension, input hash, component, norm, or digest is invalid or unexpected
- **THEN** Frux rejects the result with a bounded failure classification and stores no active vector

#### Scenario: Active contract changes during development
- **WHEN** configuration selects a new model, revision, dimension, canonicalizer, frame policy, preprocessing policy, or fusion policy
- **THEN** the new identity is isolated from prior rows and existing videos without that identity remain normally readable but semantically uncovered

### Requirement: Public-content and query privacy boundary
Video embedding input SHALL contain only normalized text and prepared images from a video that is
currently published, public, and media-ready. Query embedding input SHALL contain only the
normalized public search query. Neither operation SHALL receive user IDs, account data, behavior,
session/request IDs, credentials, tokens, signed URLs, comments, messages, private/draft content,
or arbitrary metadata.

#### Scenario: Eligible public video is prepared
- **WHEN** the Worker revalidates a video as published, public, media-ready, and unchanged immediately before provider access
- **THEN** only its bounded normalized content inputs and contract metadata are sent

#### Scenario: Video becomes ineligible before inference
- **WHEN** the video becomes private, deleted, down, non-published, media-unready, or source-changed before provider access
- **THEN** the provider is not called for that stale input and no active vector is written

#### Scenario: Public query is embedded
- **WHEN** a valid public video-search query uses semantic retrieval
- **THEN** the provider receives the normalized query and contract metadata without authenticated identity, browser tokens, or request/session metadata

### Requirement: Durable newly published video embedding jobs
After the existing `hash-ngram-v1` publication intake is durably safe, Frux SHALL idempotently hand
eligible newly published videos to a PostgreSQL multimodal job with explicit `pending`, `leased`,
`retry`, `succeeded`, and `terminal` states, bounded attempts/backoff, database-time lease,
heartbeat/reclaim, fencing, manual requeue, and cleanup semantics. Kafka source progress SHALL NOT
wait for provider inference after durable handoff.

#### Scenario: Newly published video is handed off
- **WHEN** the embedding publication consumer validates an eligible first-publication fact and the existing hash vector is safe
- **THEN** it creates or reuses the exact-contract multimodal job before allowing the source record to commit

#### Scenario: Provider is unavailable after handoff
- **WHEN** the job encounters a retryable timeout, admission rejection, rate limit, or provider failure
- **THEN** the job retains a bounded retry time while video publication, search fallback, Feed, and Kafka source progress remain available

#### Scenario: Worker loses its lease
- **WHEN** a Worker completes after its lease expired or was reclaimed
- **THEN** fencing prevents it from persisting a result or terminal transition over the current owner

#### Scenario: Development-era video predates the feature
- **WHEN** an existing readable video has no job or active multimodal vector because it predates this change
- **THEN** Frux treats semantic coverage as absent and does not require an automatic historical scan or backfill

### Requirement: Conditional authoritative vector persistence
Multimodal vector facts SHALL be authoritative, model-isolated records persisted only when the job
still owns its lease, the source content hash and public/media-ready state still match, and the
returned vector passes complete validation. Retrieval projections SHALL be rebuildable from these
facts and SHALL exclude ineligible or stale content.

#### Scenario: Valid result matches current source
- **WHEN** a fenced leased job returns a valid vector for the exact current source and contract
- **THEN** Frux atomically persists or confirms the authoritative fact, marks the job succeeded, and makes the matching projection eligible for reconciliation

#### Scenario: Source changes during inference
- **WHEN** title, description, selected media source, frame-policy input, visibility, publication, or media readiness changes before result persistence
- **THEN** the stale result is discarded and cannot replace the current contract fact

#### Scenario: Video becomes unavailable after projection
- **WHEN** a projected video becomes private, deleted, down, non-published, media-unready, source-mismatched, or contract-mismatched
- **THEN** reconciliation removes it from semantic retrieval without deleting unrelated historical facts

### Requirement: Multimodal failure isolation and observability
Multimodal jobs and provider calls SHALL use bounded concurrency, deadlines, input sizes, retry
classes, and fixed-cardinality metrics. Missing vectors or provider failures SHALL never remove a
readable video from lexical search or existing Feed providers, and logs/metrics SHALL NOT contain
raw images, raw vectors, arbitrary queries, credentials, signed URLs, or user/video/request IDs as
labels.

#### Scenario: Provider capacity is exhausted
- **WHEN** the configured provider admission bound has no available slot
- **THEN** work is rejected or retried without an unbounded local queue or detached inference

#### Scenario: Semantic coverage is incomplete
- **WHEN** some readable videos lack the active contract vector
- **THEN** coverage/backlog metrics report the condition while existing product paths remain available

#### Scenario: Operator investigates a terminal job
- **WHEN** an authorized operator inspects or requeues a terminal multimodal job
- **THEN** the response exposes bounded contract, state, attempt, and closed failure information without content payloads, vectors, or secrets
