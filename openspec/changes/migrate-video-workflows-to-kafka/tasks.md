## 1. Publication Fact and Outbox

- [ ] 1.1 Add immutable publication-fact and operational outbox models with stable event identity, bounded payload, delivery readiness, lease, attempts, dispatch time, and replay-window indexes.
- [ ] 1.2 Register the models in the shared migration path and add PostgreSQL tests for exact schema/index behavior.
- [ ] 1.3 Insert publication fact and outbox atomically in every transaction that first establishes public eligibility: review, media readiness, restore, administration, batch management, and reconciliation.
- [ ] 1.4 Refresh only undispatched operational payloads when current public media URLs become ready, preserving immutable fact payload, event ID, and original publication time.
- [ ] 1.5 Implement bounded leased dispatch, async outage-safe startup, fenced mark-success/failure, statistics, immutable-fact-aware cleanup, and non-reemitting reconciliation.
- [ ] 1.6 Add crash, race, duplicate, readiness, cleanup, retry, broker-outage, and reconciliation tests.

## 2. Video Publication Kafka Contract

- [ ] 2.1 Register `frux.video.published.v1` with exact partitions, broker append time, 30-day retention, video-ID key, bounded payload, allowed producer, and Feed/hash consumer groups.
- [ ] 2.2 Add strict publication envelope/key/payload validation without semantic model or remote-inference constraints.
- [ ] 2.3 Implement Rabbit/Kafka transition publishing with concurrent dual attempts, structured acknowledgement state, bounded metrics, and stable event identity.
- [ ] 2.4 Add contract and producer tests for malformed records, publication-time preservation, definite/uncertain transport results, duplicate events, and broker outage.

## 3. Feed and Hash Embedding Consumers

- [ ] 3.1 Wire Feed fanout/preheat to its active Kafka group and commit only after idempotent Redis/repository effects.
- [ ] 3.2 Wire the existing `hash-ngram-v1` embedding worker to an independent Kafka group and commit only after conditional hash persistence.
- [ ] 3.3 Ensure duplicate or replayed publication events preserve Feed effects, hash facts, and original publication time.
- [ ] 3.4 Implement non-mutating Feed and hash parity readers with pending/mismatch classification and bounded inline retries.
- [ ] 3.5 Add group-isolation, commit/redelivery, duplicate, parity, lag, and rollback tests.
- [ ] 3.6 Prove the publication consumer creates no semantic service call, semantic vector, semantic job, semantic retry, or semantic coverage state.

## 4. PostgreSQL Media Jobs and Kafka Wakeups

- [ ] 4.1 Register `frux.media.processing-requested.v1` as a short-retention command keyed by asset ID.
- [ ] 4.2 Publish wakeups only after the durable PostgreSQL media job commits and keep wakeup failure non-fatal.
- [ ] 4.3 Route Kafka wakeups and database polling through one bounded scheduler that reserves a slot before claiming one job.
- [ ] 4.4 Use unique per-claim tokens and current unexpired leases for heartbeat, retry, terminal transition, and finalization.
- [ ] 4.5 Atomically finalize asset metadata, variants, cleanup tasks, and job completion before public projection/notification effects.
- [ ] 4.6 Commit Kafka wakeups after validating/signalling the durable job, never after ffmpeg completion.
- [ ] 4.7 Add lost, duplicate, delayed, capacity, stale-claim, lease-expiry, heartbeat-stall, restart, and polling-recovery tests.

## 5. Durable Media Lifecycle Intents

- [ ] 5.1 Add durable idempotent protection/cleanup intents for private, delete, offline, and related media-lifecycle transitions.
- [ ] 5.2 Persist lifecycle intents in the same transaction as visibility/deletion state changes.
- [ ] 5.3 Implement bounded claim, retry, completion, reconciliation, and cleanup behavior for lifecycle intents.
- [ ] 5.4 Ensure private/deleted content disappears from discovery immediately while physical protection/cleanup remains recoverable.
- [ ] 5.5 Add crash-after-commit, duplicate, retry, private, delete, cleanup-race, and stale-worker tests.

## 6. Migration Controls and Operations

- [ ] 6.1 Add independent producer/consumer modes for publication, Feed, hash embedding, and media wakeups with invalid dual-active rejection.
- [ ] 6.2 Serialize first Kafka cutover by PostgreSQL advisory lock and require past millisecond-aligned boundaries.
- [ ] 6.3 Before first cutover, verify RabbitMQ legacy queue, quorum source queue, unacknowledged deliveries, and DLQ are drained; preserve initialized offsets on restart.
- [ ] 6.4 Supervise Kafka/Rabbit transport connections without terminating unrelated durable database workers.
- [ ] 6.5 Add bounded publication, fanout, hash embedding, media wakeup, polling recovery, lifecycle-intent, lease, parity, and backlog metrics/alerts.
- [ ] 6.6 Update video, feed, media, embedding, monitoring, architecture, engineering, deployment, Kafka, and optimization documentation.
- [ ] 6.7 Explicitly document that semantic service/model/job/backfill/profile/pgvector/ANN work remains in the recommendation roadmap and is not implemented here.

## 7. Validation

- [ ] 7.1 Run targeted video publication, Feed, hash embedding, media job, lifecycle-intent, Kafka contract, migration, and metrics tests.
- [ ] 7.2 Run live PostgreSQL and Kafka integration tests for atomic publication, independent groups, cutover, media claims, and wakeup recovery.
- [ ] 7.3 Run full Go tests, both Go builds, Compose config, strict OpenSpec validation, and diff checks.
- [ ] 7.4 Verify no semantic service source, semantic HTTP client, semantic job/worker/config, semantic Compose service, semantic vector generation, or recommendation semantic behavior is introduced by this change.
