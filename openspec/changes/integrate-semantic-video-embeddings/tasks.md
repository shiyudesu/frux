## 1. Prerequisite Gate and Fixed Contract

- [ ] 1.1 Verify recommendation-roadmap steps 1-5 and `migrate-video-workflows-to-kafka` are completed and archived before changing implementation code.
- [ ] 1.2 Reconcile the archived semantic-service metadata, authentication, canonicalization, batch, timeout, and error contracts with this change.
- [ ] 1.3 Add fixed semantic model, revision, dimension, and persistence-key domain constants while preserving `hash-ngram-v1`.
- [ ] 1.4 Add shared canonical text composition, text hashing, finite-vector validation, defensive normalization, and bounded serialization tests.

## 2. Configuration and Bounded Semantic Client

- [ ] 2.1 Add independently bounded semantic execution configuration with disabled non-Compose defaults and strict URL, token, timeout, concurrency, and sampling validation.
- [ ] 2.2 Implement the application semantic-generator port and authenticated Go HTTP client with bounded connections, in-flight requests, batches, deadlines, response bodies, and no automatic retries.
- [ ] 2.3 Validate exact service metadata and complete embedding responses for identity, order, count, dimension, finiteness, normalization, model, and revision.
- [ ] 2.4 Add configuration, authentication, metadata, timeout, cancellation, overload, response-bound, redaction, and live cross-service contract tests.

## 3. Semantic Persistence and PostgreSQL Jobs

- [ ] 3.1 Add conditional side-by-side semantic embedding persistence keyed by `(video_id, model)` and canonical text hash without pgvector or ANN schema.
- [ ] 3.2 Add PostgreSQL semantic jobs keyed by `(video_id, model)` with bounded states, attempts, availability, leases, error class, completion metadata, and text-hash fencing.
- [ ] 3.3 Implement stable bounded claims with `SKIP LOCKED`, expired-lease reclaim, fenced heartbeats/completion, and 5s, 30s, 2m, 10m, then capped 30m retries.
- [ ] 3.4 Add migration, round-trip, duplicate, changed-text, stale-lease, reclaim, retry, concurrency, and hash/semantic coexistence tests.

## 4. Hash-First Kafka Intake

- [ ] 4.1 Extend the archived `frux.video.published.v1` hash-embedding handler to validate the accepted envelope, key, identity, timestamp, payload, and canonical text contract.
- [ ] 4.2 Enforce `hash lookup/generate/save -> semantic job upsert -> Kafka offset commit eligibility` without performing inference in the Kafka handler.
- [ ] 4.3 Keep duplicate delivery and uncertain offset commits safe through stable hash facts, semantic-job identity, and changed-text reset semantics.
- [ ] 4.4 Add Kafka handler tests for valid, duplicate, malformed, hash-failure, job-failure, redelivery, uncertain-commit, and Feed-group isolation behavior.

## 5. Semantic Worker and Failure Isolation

- [ ] 5.1 Add replica-local semantic readiness gating with bounded startup validation and background metadata probes that do not block unrelated worker startup.
- [ ] 5.2 Implement the bounded leased semantic worker with claim limits, cancellation, heartbeat fencing, conditional vector persistence, retry release, and terminal invalid-input classification.
- [ ] 5.3 Wire job intake and execution so disabled or unready replicas do not claim while shared durable work and hash-first Kafka processing continue.
- [ ] 5.4 Add startup, shutdown, service-outage, metadata-mismatch, lease-loss, database-stall, multi-replica, and recovery tests.

## 6. Metrics, Compose, and Documentation

- [ ] 6.1 Add bounded metrics for semantic requests, local gate readiness, intake outcomes, job count/oldest age, leases, retries, and fixed-model coverage without high-cardinality labels.
- [ ] 6.2 Configure Compose to enable bounded semantic execution against the internal-only service with the shared strong token and a `service_started` dependency.
- [ ] 6.3 Update embedding, semantic-service, video, engineering, architecture, deployment, setup, and metrics documentation for prerequisites, Kafka intake, PostgreSQL retries, failure isolation, rollout, and rollback.
- [ ] 6.4 Document the live-only boundary and prove this change adds no historical scan, semantic profile, pgvector/ANN, recommendation consumption, media lifecycle behavior, or extra broker semantic route.

## 7. Validation

- [ ] 7.1 Run targeted Go tests for embedding domain/application/client/config/metrics, Kafka intake, PostgreSQL embedding persistence, and semantic jobs.
- [ ] 7.2 Run the semantic-service contract suite, outage/recovery integration coverage, and rendered Compose assertions with a strong test token.
- [ ] 7.3 Build `./cmd/feed` and `./cmd/worker`, then run the complete Go test suite and existing semantic-service tests.
- [ ] 7.4 Run `openspec validate --all --strict` and inspect the final diff for prerequisite, scope, task-state, broker, media-lifecycle, and recommendation-boundary violations.
