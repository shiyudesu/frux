## 1. Dependency Contract and Domain Foundations

- [ ] 1.1 Confirm the implemented `add-semantic-embedding-service` metadata and embedding endpoints match the accepted fixed model/revision, 384-dimension, normalization, batch, authentication, and timeout contract.
- [ ] 1.2 Add embedding-domain constants for the full semantic model, immutable revision, dimension 384, and persistence key `semantic-minilm-l12-v2@e8f8c211226b894f` while keeping `hash-ngram-v1` unchanged.
- [ ] 1.3 Implement Go canonical title/description normalization, exact model-input composition, and canonical text hashing matching the service contract.
- [ ] 1.4 Add bounded finite semantic-vector construction with dimension, unit-norm, defensive-copy, final L2-normalization, and JSON serialization tests.

## 2. Configuration and Bounded Semantic Client

- [ ] 2.1 Add independently enabled semantic worker configuration with validated service URL, strong internal token dependency, metadata/request timeout bounds, coverage interval bounds, and disabled local defaults.
- [ ] 2.2 Implement an application-owned fallible `SemanticGenerator` port and a bounded authenticated HTTP client with two connections/in-flight requests, fixed dial/TLS limits, no automatic retries, response-size limits, batch limit 32, and context cancellation.
- [ ] 2.3 Validate startup metadata and every embedding response for exact model metadata, count, IDs, indexes, order, dimensions, finiteness, and unit norm; classify failures with bounded safe result values.
- [ ] 2.4 Add configuration, metadata, embedding-contract, concurrency, timeout, cancellation, payload-bound, and secret/log-redaction tests, including shared Go/Python canonicalization fixtures and a live service contract test.

## 3. Persistence and Hash-First Orchestration

- [ ] 3.1 Extend embedding persistence with same-text lookup and conditional `(video_id, model)` upserts that preserve `updated_at` for identical facts and allow changed canonical text to replace one model row.
- [ ] 3.2 Add migration assertions and PostgreSQL tests proving the fixed key fits, 384 normalized JSON components round-trip, hash and semantic rows coexist, and no schema DDL, pgvector column, ANN index, or new vector table is required.
- [ ] 3.3 Refactor application orchestration to keep the hash vectorizer mandatory, make semantic generation optional, return bounded per-model outcomes, and enforce `hash lookup/generate/save -> semantic lookup/generate/save`.
- [ ] 3.4 Add application/worker tests for disabled semantics, new and duplicate live events, changed text, hash failure, semantic success, closed semantic gate, invalid vectors, and concurrent idempotent writes.

## 4. Dedicated Live-Event Retry Pipeline

- [ ] 4.1 Add a dedicated supervised RabbitMQ connection/channel for embedding deliveries with manual acknowledgements and prefetch one, isolated from fanout, action, view-event, and media consumers.
- [ ] 4.2 Declare durable 5s, 30s, 2m, 10m, and repeating-30m retry queues that dead-letter through the default exchange only to the primary embedding queue.
- [ ] 4.3 Implement bounded attempt-header parsing, exact delay selection, publisher-confirmed retry copies, the specified acknowledgement matrix, and embedding-channel-only reconnect backoff.
- [ ] 4.4 Add RabbitMQ topology and delivery tests for queue arguments, routing isolation, QoS, every retry tier, confirmation failure, crash/redelivery idempotency, malformed events, shutdown, reconnect, and unrelated-consumer progress.

## 5. Worker Composition and Observability

- [ ] 5.1 Add the semantic readiness gate with one bounded startup probe and background metadata validation retries; fail startup only for invalid local configuration while remote failures leave hash and unrelated workers running.
- [ ] 5.2 Add bounded Prometheus collectors and instrumentation for semantic request count/latency/result, live-event hash/semantic outcomes, coverage, and ready/retry/in-flight backlog without high-cardinality labels.
- [ ] 5.3 Update worker startup/shutdown and samplers to manage the semantic transport, validator, dedicated RabbitMQ resources, coverage counts, and queue inspection without affecting existing workers; add lifecycle and metric-label tests.

## 6. Compose, Documentation, and Future Boundary

- [ ] 6.1 Add local and Docker semantic configuration, enable Compose with `http://semantic-embedding:8081` and `FRUX_INTERNAL_TOKEN`, and use `condition: service_started` while keeping the service internal-only.
- [ ] 6.2 Add Compose assertions and an outage/recovery test proving a live published event receives hash coverage during semantic downtime and exactly one semantic row after delayed recovery.
- [ ] 6.3 Update embedding, semantic-service, video, engineering, architecture, deployment, module-index, and setup/configuration documentation for fixed model identity, hash-first live processing, retries/acknowledgements, metrics, failure modes, rollout, rollback, and no-schema/no-recommendation behavior.
- [ ] 6.4 Document and test the one-way boundary: `backfill-semantic-video-embeddings` depends on this change's model identity, canonicalization, validated client, conditional persistence, and coverage interfaces, while this change adds no historical scan, command/job, cursor/checkpoint, dry-run, re-embedding mode, or backfill-specific retry and does not backfill existing historical videos.

## 7. Validation

- [ ] 7.1 Run targeted Go tests for embedding domain/application/client/config/metrics/RabbitMQ packages, PostgreSQL persistence, the existing video embedding worker flow, and the live semantic-service contract.
- [ ] 7.2 Compile `./cmd/feed` and `./cmd/worker`, then run the complete Go test suite.
- [ ] 7.3 Run targeted Compose outage/recovery verification with a strong test token and `docker compose -f apps/docker-compose.yml config`.
- [ ] 7.4 Confirm no main OpenSpec specs, recommendation recall/ranking/policy code, backfill binary/job, or pgvector/ANN artifacts were added, then run `openspec validate --all --strict`.
