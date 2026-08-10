## 1. Publication Fact and Outbox

- [x] 1.1 Add immutable publication-fact and operational outbox models with stable event identity, bounded payload, delivery readiness, lease, attempts, dispatch time, and replay-window indexes.
- [x] 1.2 Register the models in the shared migration path and add PostgreSQL tests for exact schema/index behavior.
- [x] 1.3 Insert publication fact and outbox atomically in every transaction that first establishes public eligibility: review, media readiness, restore, administration, batch management, and reconciliation.
- [x] 1.4 Refresh only undispatched operational payloads when current public media URLs become ready, preserving immutable fact payload, event ID, and original publication time.
- [x] 1.5 Implement bounded leased dispatch, async outage-safe startup, fenced mark-success/failure, statistics, immutable-fact-aware cleanup, and non-reemitting reconciliation.
- [x] 1.6 Add crash, race, duplicate, readiness, cleanup, retry, broker-outage, and reconciliation tests.

## 2. Video Publication Kafka Contract

- [x] 2.1 Register `frux.video.published.v1` with exact partitions, broker append time, 30-day retention, video-ID key, bounded payload, allowed producer, and Feed/hash consumer groups.
- [x] 2.2 Add strict publication envelope/key/payload validation without semantic model or remote-inference constraints.
- [x] 2.3 Implement Rabbit/Kafka transition publishing with concurrent dual attempts, structured acknowledgement state, bounded metrics, and stable event identity.
- [x] 2.4 Add contract and producer tests for malformed records, publication-time preservation, definite/uncertain transport results, duplicate events, and broker outage.

## 3. Feed and Hash Embedding Consumers

- [x] 3.1 Wire Feed fanout/preheat to its active Kafka group and commit only after idempotent Redis/repository effects.
- [x] 3.2 Wire the existing `hash-ngram-v1` embedding worker to an independent Kafka group and commit only after conditional hash persistence.
- [x] 3.3 Ensure duplicate or replayed publication events preserve Feed effects, hash facts, and original publication time.
- [x] 3.4 Implement non-mutating Feed and hash parity readers with pending/mismatch classification and bounded inline retries.
- [x] 3.5 Add group-isolation, commit/redelivery, duplicate, parity, lag, and rollback tests.
- [x] 3.6 Prove the publication consumer creates no semantic service call, semantic vector, semantic job, semantic retry, or semantic coverage state.

## 4. PostgreSQL Media Jobs and Kafka Wakeups

- [x] 4.1 Register `frux.media.processing-requested.v1` as a short-retention command keyed by asset ID.
- [x] 4.2 Publish wakeups only after the durable PostgreSQL media job commits and keep wakeup failure non-fatal.
- [x] 4.3 Route Kafka wakeups and database polling through one bounded scheduler that reserves a slot before claiming one job.
- [x] 4.4 Use unique per-claim tokens and current unexpired leases for heartbeat, retry, terminal transition, and finalization.
- [x] 4.5 Atomically finalize asset metadata, variants, cleanup tasks, and job completion before public projection/notification effects.
- [x] 4.6 Commit Kafka wakeups after validating/signalling the durable job, never after ffmpeg completion.
- [x] 4.7 Add lost, duplicate, delayed, capacity, stale-claim, lease-expiry, heartbeat-stall, restart, and polling-recovery tests.

## 5. Durable Media Lifecycle Intents

- [x] 5.1 Add durable idempotent protection/cleanup intents for private, delete, offline, and related media-lifecycle transitions.
- [x] 5.2 Persist lifecycle intents in the same transaction as visibility/deletion state changes.
- [x] 5.3 Implement bounded claim, retry, completion, reconciliation, and cleanup behavior for lifecycle intents.
- [x] 5.4 Ensure private/deleted content disappears from discovery immediately while physical protection/cleanup remains recoverable.
- [x] 5.5 Add crash-after-commit, duplicate, retry, private, delete, cleanup-race, and stale-worker tests.

## 6. Migration Controls and Operations

- [x] 6.1 Add independent producer/consumer modes for publication, Feed, hash embedding, and media wakeups with invalid dual-active rejection.
- [x] 6.2 Serialize first Kafka cutover by PostgreSQL advisory lock and require past millisecond-aligned boundaries.
- [x] 6.3 Before first cutover, verify RabbitMQ legacy queue, quorum source queue, unacknowledged deliveries, and DLQ are drained; preserve initialized offsets on restart.
- [x] 6.4 Supervise Kafka/Rabbit transport connections without terminating unrelated durable database workers.
- [x] 6.5 Add bounded publication, fanout, hash embedding, media wakeup, polling recovery, lifecycle-intent, lease, parity, and backlog metrics/alerts.
- [x] 6.6 Update video, feed, media, embedding, monitoring, architecture, engineering, deployment, Kafka, and optimization documentation.
- [x] 6.7 Explicitly document that semantic service/model/job/backfill/profile/pgvector/ANN work remains in the recommendation roadmap and is not implemented here.

## 7. Validation

- [x] 7.1 Run targeted video publication, Feed, hash embedding, media job, lifecycle-intent, Kafka contract, migration, and metrics tests.
- [x] 7.2 Run live PostgreSQL and Kafka integration tests for atomic publication, independent groups, cutover, media claims, and wakeup recovery.
- [x] 7.3 Run full Go tests, both Go builds, Compose config, strict OpenSpec validation, and diff checks.
- [x] 7.4 Verify no semantic service source, semantic HTTP client, semantic job/worker/config, semantic Compose service, semantic vector generation, or recommendation semantic behavior is introduced by this change.
