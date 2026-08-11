## Why

RabbitMQ queue-head inspection, destructive acknowledgement, `x-death` provenance, and quorum delivery limits do not map cleanly to Kafka's immutable partition log. Frux needs recovery behavior based on consumer groups, topic-partition-offset identity, retained failure records, and non-destructive replay.

## What Changes

- Define consumer-specific retry topics only for workflows where a failed record must not block its source partition; bounded local retry remains available for short transient failures.
- Publish exhausted or terminal records to immutable consumer-specific DLQ topics with bounded failure metadata and original topic, partition, offset, key, event ID, schema version, and payload hash.
- Commit a source offset only after the handler's durable success or confirmed publication to the next retry/DLQ topic.
- Replace queue summaries and head previews with authorized topic/partition/offset browsing and bounded redacted record diagnostics.
- Replace destructive queue replay with audited non-destructive replay of one DLQ record identified by topic, partition, and offset.
- Persist replay claims/results so concurrent or repeated operator requests are idempotent even though the original DLQ record remains retained.
- Expose consumer lag, retry ingress, DLQ end offsets, replay outcomes, publication failures, and retention risk using bounded metrics and alerts.
- Introduce Kafka-native admin endpoints; the RabbitMQ endpoints remain temporarily available until final retirement.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `message-dead-letter-recovery`: Replace RabbitMQ delivery-limit, DLX/DLQ, queue-head preview, `x-death`, confirm-before-ack, and depth requirements with Kafka retry topics, immutable DLQ records, topic-partition-offset inspection, audited replay, lag, and retention requirements.

## Impact

- Depends on `add-kafka-event-backbone`.
- Affects dead-letter domain/application/HTTP contracts, Kafka administration and consumers, PostgreSQL replay ledger/audit persistence, permissions, metrics, alerts, dashboards, tests, and documentation.
- Adds new Kafka-oriented admin API shapes and leaves the old RabbitMQ API available only for the migration window.
- Does not require every consumer to use retry topics; database-owned jobs continue to retry through their own durable state.
