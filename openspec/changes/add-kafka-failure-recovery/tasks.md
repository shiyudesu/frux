## 1. Recovery Registry and Contracts

- [ ] 1.1 Add closed recovery policies for block-and-retry, retry-topics, and durable-job consumer groups.
- [ ] 1.2 Register allowed retry tiers, retry topics, DLQ topics, retention, failure classes, and replay destinations per consumer group.
- [ ] 1.3 Implement bounded retry/DLQ metadata codecs that preserve unchanged Kafka keys and business payloads.
- [ ] 1.4 Add tests for invalid source metadata, unsupported groups, oversized headers, payload hashes, attempt bounds, and topic allowlists.

## 2. Consumer Retry Routing

- [ ] 2.1 Add bounded local retry with cancellation and registered total-delay limits.
- [ ] 2.2 Implement acknowledged publication from source to the registered retry tier or DLQ before source-offset commit.
- [ ] 2.3 Implement retry-tier consumers with `not_before` enforcement through partition pause/resume rather than busy polling.
- [ ] 2.4 Route retryable, terminal, and exhausted outcomes according to the consumer-group policy.
- [ ] 2.5 Add crash-window and duplicate tests for publication before commit, commit failure, restart, rebalance, and tier progression.
- [ ] 2.6 Prove durable-job consumers commit after job handoff and do not create Kafka retry records for later job failures.

## 3. Kafka DLQ Inspection Adapter

- [ ] 3.1 Implement allowlisted DLQ topic listing with partitions, retained offset ranges, end-offset growth, recent ingress, and oldest-age summaries.
- [ ] 3.2 Implement isolated non-group record reads by topic, partition, starting offset, and bounded limit.
- [ ] 3.3 Return only redacted coordinates, source metadata, registered failure fields, sizes, hashes, and bounded JSON diagnostics.
- [ ] 3.4 Add adapter tests for unauthorized topics, invalid partitions/offsets, compacted or expired ranges, cancellation, and broker failures.

## 4. Replay Persistence and Service

- [ ] 4.1 Add replay-attempt persistence with idempotency key fingerprint, source coordinates, actor, replay ID, reason, status, and bounded failure code.
- [ ] 4.2 Register the replay model and indexes in the shared migration path.
- [ ] 4.3 Serialize concurrent replay for one DLQ coordinate and return stored results for repeated idempotency keys.
- [ ] 4.4 Validate retained record provenance, event contract, source group, original route, and payload hash before replay.
- [ ] 4.5 Republish unchanged key/value to the registered source or first retry topic, wait for acknowledgement, and commit immutable audit plus replay result.
- [ ] 4.6 Add service tests for success, timeout, broker rejection, missing/expired record, invalid provenance, concurrent requests, idempotent repeats, and later intentional replay.

## 5. HTTP and Authorization

- [ ] 5.1 Add governance-protected Kafka DLQ summary endpoints.
- [ ] 5.2 Add bounded topic/partition/offset record inspection endpoints.
- [ ] 5.3 Add the single-record replay endpoint with strict JSON, `Idempotency-Key`, registered reason, and safe error mapping.
- [ ] 5.4 Keep RabbitMQ dead-letter endpoints available during the migration window and clearly separate response types.
- [ ] 5.5 Add HTTP tests for permission denial, allowlists, bounds, redaction, idempotency conflicts, replay results, and audit attribution.

## 6. Observability and Operations

- [ ] 6.1 Add bounded retry/DLQ publication, lag, retained-offset growth, oldest-age, no-progress, replay, and retention-risk metrics.
- [ ] 6.2 Add Prometheus alerts and a Kafka failure-recovery Grafana dashboard without high-cardinality labels.
- [ ] 6.3 Document topic counts, retention sizing, delay-tier behavior, ordering trade-offs, incident inspection, replay, and expiry procedures.
- [ ] 6.4 Update engineering and module documentation to reserve Kafka retry topics for event-delivery failures and PostgreSQL jobs for long-running work.

## 7. Validation

- [ ] 7.1 Run targeted recovery domain/application/HTTP, Kafka adapter, migration, metrics, and audit tests.
- [ ] 7.2 Run Kafka integration tests for delay tiers, poison records, acknowledged next-hop routing, crash duplicates, retention, and exact-offset replay.
- [ ] 7.3 Run full Go tests, both Go builds, Compose configuration validation, and strict OpenSpec validation.
