## Why

After every business path and failure-recovery surface has cut over, retaining RabbitMQ would leave two messaging stacks, duplicate operational controls, and misleading recovery behavior. The final change removes AMQP only after explicit drain and rollback gates prove Kafka and PostgreSQL jobs own all required delivery semantics.

## What Changes

- Require all behavior streams, video publication consumers, processing wakeups, and Kafka failure recovery to be in Kafka mode for an observation window before retirement.
- Drain legacy RabbitMQ queues and DLQs, preserve required audit history, and export any unresolved operator work before shutdown.
- **BREAKING** Remove RabbitMQ dead-letter queue summary, preview, and replay endpoints after their Kafka topic/offset replacements are available.
- Remove RabbitMQ topology declaration, publishers, consumers, management adapter, migration modes, configuration, credentials, AMQP dependency, and RabbitMQ-specific tests.
- Remove the RabbitMQ Compose service, volumes, health dependencies, Prometheus alerts, Grafana dashboard, metrics, and operational documentation.
- Make API startup independent of any AMQP endpoint and make Worker startup require Kafka plus the existing PostgreSQL/Redis dependencies instead of RabbitMQ.
- Update architecture, engineering, deployment, optimization, module, and OpenSpec documentation to describe the final Kafka event-stream plus PostgreSQL durable-job model.

## Capabilities

### New Capabilities

- `kafka-only-messaging-runtime`: Final runtime, deployment, startup, compatibility, and retirement requirements after RabbitMQ is removed.

### Modified Capabilities

None.

## Impact

- Depends on completed cutover of `migrate-behavior-events-to-kafka`, `migrate-video-workflows-to-kafka`, and `add-kafka-failure-recovery`.
- Removes `github.com/rabbitmq/amqp091-go`, `internal/infra/mq` RabbitMQ implementation, RabbitMQ configuration and Compose resources, and RabbitMQ-only monitoring assets.
- Breaks the internal admin RabbitMQ dead-letter HTTP contract; Kafka recovery endpoints become the supported replacement.
- No business data migration is required, but deployment rollback after the observation window requires restoring the previous binary and RabbitMQ service rather than toggling a runtime flag.
