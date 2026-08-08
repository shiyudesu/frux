## 1. Kafka Dependency and Configuration

- [x] 1.1 Add franz-go and its required admin packages to the Go module without removing the AMQP dependency.
- [x] 1.2 Add typed Kafka broker, authentication, TLS, timeout, environment, local-provisioning, and production-validation configuration.
- [x] 1.3 Validate Kafka configuration bounds and add configuration tests for disabled, local, authenticated, TLS, and invalid combinations.
- [x] 1.4 Define the closed topic, consumer-group, producer, key-kind, retention, cleanup-policy, and migration-mode registries.

## 2. Event Contracts and Codecs

- [x] 2.1 Add the bounded versioned Kafka event envelope and registered metadata types.
- [x] 2.2 Implement strict envelope encoding/decoding with size, timestamp, event ID, producer, type, version, and trailing-data validation.
- [x] 2.3 Add event-specific key codec interfaces and fixtures that prove stable key bytes.
- [x] 2.4 Add compatibility tests for supported envelope versions and terminal classification of unknown or malformed contracts.

## 3. Kafka Administration and Production

- [x] 3.1 Implement the Kafka client factory with safe connection, authentication, TLS, and shutdown behavior.
- [x] 3.2 Implement local topic provisioning from the registry and production-only topology validation.
- [x] 3.3 Implement idempotent `acks=all` record production with bounded deadlines and per-record result handling.
- [x] 3.4 Add producer tests for acknowledged writes, timeout/uncertain results, broker errors, cancellation, and duplicate-safe client retry.

## 4. Consumer Group Runtime

- [x] 4.1 Implement registered consumer groups with automatic commits disabled, cooperative rebalancing, bounded fetches, and context cancellation.
- [x] 4.2 Define handler metadata and classified outcomes without exposing Kafka client types to Domain packages.
- [x] 4.3 Commit offsets only after durable-success or registered terminal outcomes and stop the session on commit uncertainty.
- [x] 4.4 Add bounded per-partition execution and shutdown draining without unbounded goroutines.
- [x] 4.5 Add consumer tests for ordering, duplicate delivery after commit failure, rebalance revocation, restart, cancellation, and shutdown deadlines.

## 5. Migration Controls

- [x] 5.1 Add registered primary/mirror producer modes and active/shadow consumer modes.
- [x] 5.2 Reject configuration that enables two mutating consumers for one registered responsibility.
- [x] 5.3 Implement a shadow handler path that validates envelopes, keys, age, and optional parity without business writes or active-group offsets.
- [x] 5.4 Wire Kafka lifecycle and migration controls into API and Worker composition while every business stream remains RabbitMQ-active.

## 6. Observability and Local Runtime

- [x] 6.1 Add bounded Kafka produce, consume, commit, rebalance, lag, delivery-delay, contract, and topology metrics.
- [x] 6.2 Add Kafka health and topic-validation diagnostics without logging credentials, payloads, keys, or unrestricted broker errors.
- [x] 6.3 Add a persistent single-node KRaft Kafka service and health check to Compose with correct internal and host listeners.
- [x] 6.4 Add focused integration coverage for topic provisioning, production, group consumption, restart, and persisted broker data.

## 7. Documentation and Validation

- [x] 7.1 Update architecture, engineering, deployment, monitoring, and configuration documentation for the Kafka foundation and its explicit non-goals.
- [x] 7.2 Document production broker, replication, in-sync replica, retention, authentication, TLS, and auto-creation requirements.
- [x] 7.3 Run targeted Kafka/config/metrics tests, full Go tests, both Go builds, and Compose configuration validation.
- [x] 7.4 Run `openspec validate --all --strict` and confirm no business event has been cut over by this change.
