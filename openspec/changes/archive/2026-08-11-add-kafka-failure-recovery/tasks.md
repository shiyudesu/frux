## 1. Recovery Registry and Contracts

- [x] 1.1 Add closed recovery policies for block-and-retry, retry-topics, and durable-job consumer groups.
- [x] 1.2 Register allowed retry tiers, retry topics, DLQ topics, retention, failure classes, and replay destinations per consumer group.
- [x] 1.3 Implement bounded retry/DLQ metadata codecs that preserve unchanged Kafka keys and business payloads.
- [x] 1.4 Add tests for invalid source metadata, unsupported groups, oversized headers, payload hashes, attempt bounds, and topic allowlists.

## 2. Consumer Retry Routing

- [x] 2.1 Add bounded local retry with cancellation and registered total-delay limits.
- [x] 2.2 Implement acknowledged publication from source to the registered retry tier or DLQ before source-offset commit.
- [x] 2.3 Implement retry-tier consumers with `not_before` enforcement through partition pause/resume rather than busy polling.
- [x] 2.4 Route retryable, terminal, and exhausted outcomes according to the consumer-group policy.
- [x] 2.5 Add crash-window and duplicate tests for publication before commit, commit failure, restart, rebalance, and tier progression.
- [x] 2.6 Prove durable-job consumers commit after job handoff and do not create Kafka retry records for later job failures.

## 3. Kafka DLQ Inspection Adapter

- [x] 3.1 Implement allowlisted DLQ topic listing with partitions, retained offset ranges, end-offset growth, recent ingress, and oldest-age summaries.
- [x] 3.2 Implement isolated non-group record reads by topic, partition, starting offset, and bounded limit.
- [x] 3.3 Return only redacted coordinates, source metadata, registered failure fields, sizes, hashes, and bounded JSON diagnostics.
- [x] 3.4 Add adapter tests for unauthorized topics, invalid partitions/offsets, compacted or expired ranges, cancellation, and broker failures.

## 4. Replay Persistence and Service

- [x] 4.1 Add replay-attempt persistence with idempotency key fingerprint, source coordinates, actor, replay ID, reason, status, and bounded failure code.
- [x] 4.2 Register the replay model and indexes in the shared migration path.
- [x] 4.3 Serialize concurrent replay for one DLQ coordinate and return stored results for repeated idempotency keys.
- [x] 4.4 Validate retained record provenance, event contract, source group, original route, and payload hash before replay.
- [x] 4.5 Republish unchanged key/value to the registered source or first retry topic, wait for acknowledgement, and commit immutable audit plus replay result.
- [x] 4.6 Add service tests for success, timeout, broker rejection, missing/expired record, invalid provenance, concurrent requests, idempotent repeats, and later intentional replay.

## 5. HTTP and Authorization

- [x] 5.1 Add governance-protected Kafka DLQ summary endpoints.
- [x] 5.2 Add bounded topic/partition/offset record inspection endpoints.
- [x] 5.3 Add the single-record replay endpoint with strict JSON, `Idempotency-Key`, registered reason, and safe error mapping.
- [x] 5.4 Keep RabbitMQ dead-letter endpoints available during the migration window and clearly separate response types.
- [x] 5.5 Add HTTP tests for permission denial, allowlists, bounds, redaction, idempotency conflicts, replay results, and audit attribution.

## 6. Observability and Operations

- [x] 6.1 Add bounded retry/DLQ publication, lag, retained-offset growth, oldest-age, no-progress, replay, and retention-risk metrics.
- [x] 6.2 Add Prometheus alerts and a Kafka failure-recovery Grafana dashboard without high-cardinality labels.
- [x] 6.3 Document topic counts, retention sizing, delay-tier behavior, ordering trade-offs, incident inspection, replay, and expiry procedures.
- [x] 6.4 Update engineering and module documentation to reserve Kafka retry topics for event-delivery failures and PostgreSQL jobs for long-running work.

## 7. Validation

- [x] 7.1 Run targeted recovery domain/application/HTTP, Kafka adapter, migration, metrics, and audit tests.
- [x] 7.2 Run Kafka integration tests for delay tiers, poison records, acknowledged next-hop routing, crash duplicates, retention, and exact-offset replay.
- [x] 7.3 Run full Go tests, both Go builds, Compose configuration validation, and strict OpenSpec validation.

## 8. Code Review Corrections

- [x] 8.1 Route malformed-key terminal contract records directly to the owning DLQ without weakening retry or replay validation.
- [x] 8.2 Cancel and discard delayed partition ownership on revoke, loss, reassignment, and session shutdown.
- [x] 8.3 Reconcile pending acknowledged replay claims from bounded immutable Kafka evidence without republishing.
- [x] 8.4 Replace ingress-only no-progress state with bounded window signals for end-offset, oldest-record, durable recovery, and replay progress.
- [x] 8.5 Apply each resolved source, retry, and DLQ Topic's broker maximum to franz-go batches while retaining exact per-record validation.
- [x] 8.6 Preserve Kafka recovery-publication error chains behind bounded sanitized display errors.
