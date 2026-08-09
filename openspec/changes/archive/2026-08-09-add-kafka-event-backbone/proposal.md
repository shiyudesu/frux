## Why

Frux is moving from task-oriented RabbitMQ messaging toward retained, replayable event streams that can support recommendation, analytics, and independently evolving consumers. The foundation must expose Kafka-native concepts instead of reproducing exchanges, bindings, acknowledgements, and queue requeue behavior behind renamed abstractions.

## What Changes

- Add Apache Kafka as a supported infrastructure dependency and local Compose service.
- Define a code-owned registry for versioned topics, partition keys, retention, cleanup policy, producer policy, and consumer-group identities.
- Introduce a bounded versioned event envelope with stable event identity, event type, occurrence time, producer metadata, schema version, key, and typed payload.
- Add idempotent Kafka producer and supervised consumer-group infrastructure with explicit offset commit after the consumer's durable boundary.
- Add startup topic validation/provisioning for local development while requiring production-safe replication and in-sync replica settings outside single-node development.
- Add Kafka health, produce, consume, lag, rebalance, and delivery-delay observability using bounded labels.
- Add per-stream migration controls that allow RabbitMQ, dual-publish/shadow-consume, and Kafka modes without allowing two transports to perform uncontrolled duplicate side effects.
- Keep RabbitMQ available during this foundational change; no business event cuts over yet.

## Capabilities

### New Capabilities

- `kafka-event-backbone`: Versioned event contracts, topic governance, producer/consumer runtime behavior, partitioning, retention, observability, and safe transport migration controls.

### Modified Capabilities

None.

## Impact

- Adds a Kafka Go client dependency, Kafka configuration, infrastructure package, metrics, tests, and Compose service.
- Affects API and Worker composition roots, configuration validation, deployment documentation, engineering conventions, and monitoring.
- Establishes a prerequisite for `migrate-behavior-events-to-kafka`, `migrate-video-workflows-to-kafka`, and `add-kafka-failure-recovery`.
- Does not remove AMQP code, RabbitMQ configuration, RabbitMQ monitoring, or current dead-letter APIs.
