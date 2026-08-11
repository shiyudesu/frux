## 1. Retirement Readiness

- [x] 1.1 Document the required observation window and measurable Kafka producer, consumer lag, retry/DLQ, fallback, duplicate, and durable-job thresholds.
- [x] 1.2 Add an operator checklist that verifies every stream is Kafka-primary/active and every RabbitMQ consumer is stopped.
- [x] 1.3 Verify every RabbitMQ source queue is drained and every allowlisted DLQ record has an audited replay, waiver, or export decision.
- [x] 1.4 Preserve the previous release artifact and RabbitMQ deployment manifest for the bounded post-retirement rollback window.

## 2. Remove RabbitMQ Recovery Surface

- [x] 2.1 Migrate internal callers and documentation to the Kafka topic/partition/offset recovery endpoints.
- [x] 2.2 Remove RabbitMQ queue summary, head preview, and destructive replay routes, DTOs, handlers, and application adapters.
- [x] 2.3 Preserve historical admin audit facts and add compatibility tests for querying pre-retirement records.
- [x] 2.4 Remove RabbitMQ dead-letter domain/application behavior that has been replaced by the modified Kafka recovery capability.

## 3. Remove AMQP Runtime Code

- [x] 3.1 Remove RabbitMQ publishers, consumers, topology, management client, migration modes, tests, and composition wiring.
- [x] 3.2 Remove RabbitMQ configuration entities, validation, YAML fields, credentials, and startup branches.
- [x] 3.3 Remove `github.com/rabbitmq/amqp091-go` and tidy the Go module.
- [x] 3.4 Make Worker startup validate Kafka, PostgreSQL, Redis, and other real dependencies without AMQP.
- [x] 3.5 Add repository checks that fail if active code or configuration still imports AMQP or references RabbitMQ runtime fields.

## 4. Remove Deployment and Monitoring Resources

- [x] 4.1 Remove the RabbitMQ Compose service, ports, volume, health check, and API/Worker dependencies.
- [x] 4.2 Remove RabbitMQ-specific Prometheus collectors, alerts, recording rules, and Grafana dashboard.
- [x] 4.3 Remove RabbitMQ management credentials and deployment instructions from supported environments.
- [x] 4.4 Validate that Kafka is the only broker provisioned by the final Compose configuration.

## 5. Final Documentation

- [x] 5.1 Update README, architecture, engineering, deployment, optimization, monitoring, and quick-read documentation.
- [x] 5.2 Replace the RabbitMQ dead-letter module document with Kafka recovery and durable-job operational guidance.
- [x] 5.3 Update all affected business module documents to distinguish retained events, wakeup commands, and PostgreSQL jobs.
- [x] 5.4 Update current OpenSpec project context and specifications while preserving archived RabbitMQ changes as history.

## 6. Validation and Rollback Drill

- [x] 6.1 Run targeted startup, configuration, Kafka, recovery API, worker, metrics, and Compose tests.
- [x] 6.2 Run full Go tests, both Go builds, Compose configuration validation, and strict OpenSpec validation.
- [x] 6.3 Start the complete stack without RabbitMQ and verify API health, Worker consumers, media polling, hash-embedding intake, Kafka recovery, and monitoring.
- [x] 6.4 Perform a deployment rollback drill using the preserved previous release and RabbitMQ manifest before ending the rollback window.
