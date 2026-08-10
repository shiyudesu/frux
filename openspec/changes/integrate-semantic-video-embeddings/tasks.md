## 1. Dependency Contract and Domain Foundations

- [x] 1.1 Confirm the implemented `add-semantic-embedding-service` metadata and embedding endpoints match the accepted fixed model/revision, 384-dimension, normalization, batch, authentication, and timeout contract.
- [x] 1.2 Add embedding-domain constants for the full semantic model, immutable revision, dimension 384, and persistence key `semantic-minilm-l12-v2@e8f8c211226b894f` while keeping `hash-ngram-v1` unchanged.
- [x] 1.3 Implement Go canonical title/description normalization, exact model-input composition, and canonical text hashing matching the service contract.
- [x] 1.4 Add bounded finite semantic-vector construction with dimension, unit-norm, defensive-copy, final L2-normalization, and JSON serialization tests.

## 2. Configuration and Bounded Semantic Client

- [x] 2.1 Add independently enabled semantic worker configuration with validated service URL, strong internal token dependency, metadata/request timeout bounds, coverage interval bounds, and disabled local defaults.
- [x] 2.2 Implement an application-owned fallible `SemanticGenerator` port and a bounded authenticated HTTP client with two connections/in-flight requests, fixed dial/TLS limits, no automatic retries, response-size limits, batch limit 32, and context cancellation.
- [x] 2.3 Validate startup metadata and every embedding response for exact model metadata, count, IDs, indexes, order, dimensions, finiteness, and unit norm; classify failures with bounded safe result values.
- [x] 2.4 Add configuration, metadata, embedding-contract, concurrency, timeout, cancellation, payload-bound, and secret/log-redaction tests, including shared Go/Python canonicalization fixtures and a live service contract test.

## 3. Persistence and Hash-First Orchestration

- [x] 3.1 Extend embedding persistence with same-text lookup and conditional `(video_id, model)` upserts that preserve `updated_at` for identical facts and allow changed canonical text to replace one model row.
- [x] 3.2 Add migration assertions and PostgreSQL tests proving the fixed key fits, 384 normalized JSON components round-trip, hash and semantic rows coexist, and no schema DDL, pgvector column, ANN index, or new vector table is required.
- [x] 3.3 Refactor application orchestration to keep the hash vectorizer mandatory, make semantic execution optional, retain bounded per-model outcomes, and enforce `hash lookup/generate/save -> PostgreSQL semantic-job handoff -> leased semantic lookup/generate/save`.
- [x] 3.4 Add application/intake/leased-worker tests for disabled semantics, new and duplicate live events, changed text, hash or job-handoff failure, semantic success, closed semantic gate, invalid vectors, and concurrent idempotent writes.

## 4. Kafka Intake and Durable Semantic Jobs

- [x] 4.1 Consume the retained video-publication topic through the registered independent embedding group and strictly validate its envelope, key, identity, timestamps, and payload.
- [x] 4.2 Add a PostgreSQL semantic job keyed by `(video_id, model)` with canonical text hash, state, attempts, availability, lease owner/until, bounded error class, and completion metadata.
- [x] 4.3 Commit the Kafka offset only after hash persistence and semantic-job upsert/reset commit; duplicate publication records and changed text hashes remain idempotent.
- [x] 4.4 Add intake and persistence tests for group isolation, malformed events, commit failure/redelivery, duplicate publication, changed text, job leases, reclaim, retry, suspension, cleanup, and backlog ordering.

## 5. Worker Composition and Observability

- [x] 5.1 Add the semantic readiness gate with one bounded startup probe and background metadata validation retries; fail startup only for invalid local configuration while remote failures leave hash intake and unrelated workers running.
- [x] 5.2 Implement a bounded leased semantic worker with the 5s, 30s, 2m, 10m, then capped 30m retry schedule, explicit suspended state, expired-lease reclaim, and terminal contract classification.
- [x] 5.3 Add bounded Prometheus collectors and instrumentation for semantic request count/latency/result, live-event hash/semantic outcomes, coverage, and pending/retry/suspended/in-flight PostgreSQL backlog without high-cardinality labels.
- [x] 5.4 Update worker startup/shutdown and samplers to manage the semantic transport, validator, Kafka intake, semantic-job poller, coverage counts, and PostgreSQL backlog inspection without affecting existing workers; add lifecycle and metric-label tests.

## 6. Compose, Documentation, and Future Boundary

- [x] 6.1 Add local and Docker semantic configuration, enable Compose with `http://semantic-embedding:8081` and `FRUX_INTERNAL_TOKEN`, and use `condition: service_started` while keeping the service internal-only.
- [x] 6.2 Add Compose assertions and an outage/recovery test proving a live published event receives hash coverage and a durable semantic job during semantic downtime and exactly one semantic row after delayed recovery.
- [x] 6.3 Update embedding, semantic-service, video, engineering, architecture, deployment, module-index, and setup/configuration documentation for fixed model identity, hash-first Kafka intake, PostgreSQL retries/leases/suspension, metrics, failure modes, rollout, rollback, and no-vector-schema/no-recommendation behavior.
- [x] 6.4 Document and test the one-way boundary: `backfill-semantic-video-embeddings` depends on this change's model identity, canonicalization, validated client, conditional persistence, and coverage interfaces, while this change adds no historical scan, command/job, cursor/checkpoint, dry-run, re-embedding mode, or backfill-specific retry and does not backfill existing historical videos.

## 7. Validation

- [x] 7.1 Run targeted Go tests for embedding domain/application/client/config/metrics/Kafka packages, PostgreSQL persistence and semantic jobs, the video publication intake flow, and the live semantic-service contract.
- [x] 7.2 Compile `./cmd/feed` and `./cmd/worker`, then run the complete Go test suite.
- [x] 7.3 Run targeted Compose outage/recovery verification with a strong test token and `docker compose -f apps/docker-compose.yml config`.
- [x] 7.4 Confirm no main OpenSpec specs, recommendation recall/ranking/policy code, backfill binary/job, or pgvector/ANN artifacts were added, then run `openspec validate --all --strict`.

## 8. Review Remediation

- [x] 8.1 Remove semantic text compatibility from the shared `video.published` Kafka contract and prove Feed accepts video-valid records that embedding intake terminally classifies.
- [x] 8.2 Bound semantic lease-heartbeat database calls with processing-derived contexts and cover shutdown/database-stall behavior.
- [x] 8.3 Re-run Python, offline contract, targeted/full Go, binary build, Compose, strict OpenSpec, and diff validation after cross-change remediation.
- [x] 8.4 Make publication dispatcher startup asynchronous during Kafka/RabbitMQ outage and bound aggregate dispatch runs.
- [x] 8.5 Apply one semantic-service startup deadline across preload, fixture validation, and complete inference-pool initialization.
- [x] 8.6 Retry inference replacement with bounded backoff, expose live capacity, and gate readiness through all-worker loss and recovery.
- [x] 8.7 Retain immutable publication facts while bounded cleanup removes replay-expired operational outbox rows without reconciliation re-emission.
- [x] 8.8 Restrict semantic operational logs to route/status/duration/result/capacity and cover success and bounded failure classes with redaction tests.
