## Why

Several RabbitMQ consumers currently depend on requeue behavior without a broker-level dead-letter topology, so poison messages can retry indefinitely or require ad hoc handling. Frux needs bounded failure isolation and an auditable recovery path without duplicating the broker as a PostgreSQL task queue.

## What Changes

- Introduce bounded delivery attempts and dead-letter exchanges/queues for durable application consumers.
- Use quorum queues and at-least-once dead-lettering where message loss would violate durable business processing guarantees.
- Classify terminal payload errors separately from retryable infrastructure failures and preserve idempotent consumer behavior.
- Expose dead-letter metadata through a protected operator API and support explicit replay with a new replay identifier and audit record.
- Add queue depth, dead-letter, retry-exhaustion, replay, and replay-failure metrics and alerts.
- Exclude arbitrary message editing, bulk replay, cross-environment replay, and a Web console.

## Capabilities

### New Capabilities

- `message-dead-letter-recovery`: Broker-native failure isolation, bounded retries, inspection, and audited replay.

### Modified Capabilities

None.

## Impact

This changes RabbitMQ topology and configuration, consumer acknowledgement policy, internal operator adapters, admin APIs, audit integration, Compose definitions, metrics/alerts, integration tests, and governance documentation. It depends on admin authorization and audit.
