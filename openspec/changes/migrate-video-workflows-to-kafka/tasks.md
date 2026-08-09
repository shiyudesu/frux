## 1. Reconcile Semantic Embedding Planning

- [x] 1.1 Update `integrate-semantic-video-embeddings` to remove RabbitMQ channel, TTL queue, header-attempt, and broker-backlog requirements.
- [x] 1.2 Replace those artifacts with Kafka publication intake plus PostgreSQL semantic-job retry, lease, suspension, and backlog behavior.
- [x] 1.3 Validate the updated semantic integration change and confirm no conflicting implementation starts before this dependency is resolved.

## 2. Publication Event Persistence

- [x] 2.1 Add a compact `video_publication_event_outbox` model with stable event ID, typed payload, lease, attempts, availability, dispatched time, and bounded error class.
- [x] 2.2 Register the model and indexes in the shared migration path with PostgreSQL integration coverage.
- [x] 2.3 Create the outbox row idempotently when review, media readiness, restore, administration, or reconciliation first establishes public eligibility.
- [x] 2.4 Add repository and service tests proving publication fact and outbox consistency across duplicate and racing publication edges.

## 3. Kafka Publication Stream

- [x] 3.1 Register `frux.video.published.v1`, its video-ID key, 30-day retention, producer, Feed group, embedding group, and shadow groups.
- [x] 3.2 Implement the leased publication outbox dispatcher with acknowledged Kafka production and RabbitMQ migration modes.
- [x] 3.3 Wire Feed fanout/preheat to its Kafka group and preserve idempotent Redis index behavior.
- [x] 3.4 Wire embedding intake to its independent Kafka group.
- [x] 3.5 Add tests for group independence, duplicate events, publication-time preservation, lag isolation, commit failure, and rollback.

## 4. Durable Semantic Jobs

- [x] 4.1 Add the semantic embedding job model keyed by video and model with text hash, state, attempts, availability, lease, bounded error class, and completion metadata.
- [x] 4.2 Add repository claim, upsert/reset, complete, retry, suspend, reclaim, backlog, and cleanup operations with stable ordering.
- [x] 4.3 Make embedding intake persist or confirm `hash-ngram-v1` before creating or refreshing the semantic job.
- [x] 4.4 Commit the publication offset only after hash persistence and semantic-job handoff commit.
- [x] 4.5 Implement the bounded leased semantic worker using the existing strict HTTP client contract and capped retry schedule.
- [x] 4.6 Add tests for disabled/unavailable semantic service, text changes, duplicate publications, expired leases, terminal contracts, and hash-first progress.

## 5. Kafka Media Wakeups

- [x] 5.1 Register `frux.media.processing-requested.v1` as a short-retention command topic keyed by asset ID.
- [x] 5.2 Publish the wakeup only after the PostgreSQL media job commits and keep publication failure non-fatal to job durability.
- [x] 5.3 Refactor the Kafka command consumer to validate the durable job, signal bounded scheduling, and commit without holding the record through ffmpeg work.
- [x] 5.4 Preserve PostgreSQL leasing, heartbeat, polling, reconciliation, retry timing, and terminal notification behavior.
- [x] 5.5 Add lost, duplicate, delayed, pre-capacity, restart, and polling-recovery wakeup tests.

## 6. Migration Controls and Observability

- [x] 6.1 Add independent publication, Feed, embedding, and media-wakeup migration modes with dual-active rejection.
- [x] 6.2 Add publication outbox, fanout lag, embedding intake, semantic job backlog, media wakeup, polling recovery, and outcome metrics.
- [x] 6.3 Add per-workflow shadow validation, observation gates, and rollback procedures.

## 7. Documentation

- [x] 7.1 Update video, media, feed, recommendation, architecture, engineering, deployment, optimization, and monitoring documents.
- [x] 7.2 Document the distinction between retained publication events, non-authoritative wakeup commands, and PostgreSQL job state.
- [x] 7.3 Document semantic disablement, backlog recovery, polling guarantees, and the unchanged historical backfill boundary.

## 8. Validation

- [x] 8.1 Run targeted video, media, embedding, Feed fanout, migration, and Kafka integration tests.
- [x] 8.2 Run forced Kafka and semantic-service outage/recovery tests proving public-state truth, hash progress, polling recovery, and eventual job completion.
- [x] 8.3 Run full Go tests, both Go builds, Compose configuration validation, and strict OpenSpec validation.
