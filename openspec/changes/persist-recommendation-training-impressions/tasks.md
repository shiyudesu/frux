## 1. Domain and Ranking Metadata

- [ ] 1.1 Add diagnostic-impression domain types, version constants, privacy/size bounds, normalization, and constructors for facts and outbox work; cover invalid user/request/generation/video identity, rank, author/publication metadata, degraded metadata, reason, component, timestamp, and schema-version inputs.
- [ ] 1.2 Add immutable delivery generation and zero-based generation-relative absolute rank position to recommendation candidates, assign them after final ranking/diversity and before cursor filtering/page slicing, and preserve them through cloning and Redis snapshots.
- [ ] 1.3 Extend delivery projections with trusted author ID, publication time, bounded reasons/components, scene, policy/version, degraded state/providers, served/recorded time semantics, and schema versions; test first-page, later snapshot, filtered-gap, recomputed-generation, deterministic degraded-cursor, and replay positions.

## 2. Final Feed Delivery Boundary

- [ ] 2.1 Update Feed recommendation assembly to match final hydrated/readable items back to trusted delivery projections and pass only actually returned cards, without exposing new fields in public DTOs.
- [ ] 2.2 Replace the evidence-only delivery call with a validated aggregate contract that carries unchanged served-candidate evidence plus matching compact training payloads.
- [ ] 2.3 Add Feed/recommendation unit and API-flow coverage proving missing, unreadable, or suppressed cards create neither evidence nor training handoffs, while a handoff failure still fails the page before HTTP success.

## 3. Persistence and Migration

- [ ] 3.1 Add GORM models for `recommendation_training_impression_outbox` and `recommendation_training_impression` with compact typed fields, unique `source_served_candidate_id`, `(user_id, request_id, generation, video_id)` identity, bounded error storage, claim, `served_at` cleanup, and `recorded_at` watermark indexes.
- [ ] 3.2 Register both models in the shared advisory-locked migration and extend PostgreSQL migration integration tests for table and index creation under API/worker-compatible startup.
- [ ] 3.3 Extend the existing served-candidate append transaction to capture newly inserted evidence IDs and batch-create exactly matching outbox rows atomically under the current request lock; preserve evidence expiry, delivery grace, replacement, and replay behavior.
- [ ] 3.4 Add PostgreSQL repository tests for atomic rollback, first/later page insertion, duplicate delivery replay, same request/video in a later generation, frozen author/publication/policy/degraded metadata, payload bounds, and independence from sampled request logs.

## 4. Leased Persistence and Cleanup Workers

- [ ] 4.1 Implement bounded `FOR UPDATE SKIP LOCKED` claim, lease, attempt, capped-error/backoff, fact upsert, and dispatched-mark operations; make fact insertion plus dispatch completion transactional and idempotent on the source evidence ID.
- [ ] 4.2 Implement the training-impression worker with bounded batch count/runtime, stale-lease recovery, graceful shutdown, replay success, and retained pending work after failures; add focused worker tests for crashes, retries, duplicates, and poison/backlog isolation.
- [ ] 4.3 Implement stable batched cleanup for expired facts by `(served_at, id)` and for old dispatched outbox rows, while never deleting pending rows; test cutoffs, batch bounds, and independence from evidence, request-log, outcome, and behavior retention.
- [ ] 4.4 Add and validate `recommendation.training_impressions` configuration for dispatch/lease/run bounds, 180-day default fact retention, cleanup bounds, and completed-outbox replay retention.
- [ ] 4.5 Wire persistence and cleanup workers into `cmd/worker` using PostgreSQL only, and verify startup does not add a Kafka dependency to this capability.
- [ ] 4.6 Add bounded account-deletion processing and a durable training-opt-out exclusion boundary; test deletion retries/reconciliation and prove future-consumer eligibility cannot be inferred from row presence.

## 5. Security and Compatibility Verification

- [ ] 5.1 Add regression tests proving feedback and all outcome attribution paths continue to query only unexpired `recommendation_served_candidate` evidence and reject attribution based solely on a retained training fact.
- [ ] 5.2 Add replay and expiry tests proving diagnostic worker delay does not affect attribution, diagnostic retention does not extend `served_at <= recorded_at < expires_at`, and evidence cleanup can proceed while a handoff remains recoverable.
- [ ] 5.3 Verify existing Feed, feedback, exposure, playback, and opaque cursor request/response contracts remain unchanged and that no client-facing training-impression endpoint or arbitrary metadata input is introduced.
- [ ] 5.4 Add explicit tests proving delivered does not imply exposed and delivered-without-validated-exposure cannot be interpreted as a negative fact.

## 6. Metrics and Documentation

- [ ] 6.1 Add bounded metrics for handoff/dispatch results, persisted/replayed/retried work, pending count, oldest pending age, privacy deletion, reconciliation, cleanup deletions, and worker duration/success; register them and test that labels exclude identities, feature names, and raw errors.
- [ ] 6.2 Update recommendation, monitoring, architecture/engineering, and configuration documentation with the compact diagnostic schema, generation/position and occurred/recorded time contract, trusted handoff, worker/replay behavior, retention/privacy bounds, unchanged security window, rollback procedure, and operational signals.
- [ ] 6.3 Document that future export/training is conditional and inactive, while the diagnostic fact remains useful for low-data replay, reconciliation, and human evaluation.

## 7. Acceptance Gates

- [ ] 7.1 Measure representative payload and PostgreSQL table-plus-index amplification and block rollout unless p95 payload is at most 2 KiB and storage is at most 4 KiB per fact.
- [ ] 7.2 Run paired Feed load tests and block rollout unless p99 overhead is no more than both 5 ms and 5%.
- [ ] 7.3 Exercise steady load and a 10-minute worker outage; verify 99.99% five-minute materialization, oldest pending below 15 minutes, and backlog drain within 60 minutes.
- [ ] 7.4 Implement 24-hour reconciliation and verify every committed delivery has exactly one pending handoff or fact with zero unexplained missing or duplicate identities.

## 8. Validation

- [ ] 8.1 Run targeted recommendation domain/application, Feed API-flow, persistence, worker, privacy, reconciliation, configuration, metrics, and PostgreSQL migration tests covering the new behavior.
- [ ] 8.2 Run `cd apps/api && go test ./...` and `cd apps/api && go build ./cmd/feed ./cmd/worker`.
- [ ] 8.3 Run `openspec validate --all --strict` and confirm the change remains consistent with its proposal, design, and `recommendation-training-impressions` delta spec.
