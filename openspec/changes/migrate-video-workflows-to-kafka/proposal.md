## Why

Video publication is a durable domain event consumed independently by Feed fanout and embedding, while media processing and semantic generation are retryable jobs with database-owned state. Treating all three as RabbitMQ deliveries obscures those different semantics and would produce an awkward Kafka migration.

## What Changes

- Publish each stable first-publication fact to a retained versioned Kafka topic keyed by video ID.
- Give Feed fanout/preheating and embedding intake independent consumer groups so either workflow can lag, replay, or recover without affecting the other.
- Add a durable publication outbox/recovery boundary so committing public eligibility does not depend on Kafka availability and duplicate publication remains idempotent.
- Keep media processing jobs in PostgreSQL as the source of truth; publish a short-retention Kafka command only as a wakeup hint, while leases, retry timing, terminal classification, reconciliation, and polling remain database-owned.
- Replace the planned RabbitMQ semantic-embedding delay-queue ladder with a PostgreSQL semantic job/outbox that owns delayed retries, attempt state, leases, and terminal outcomes; Kafka publication intake only creates or refreshes the durable job.
- Preserve local hash embedding progress independently of remote semantic availability.
- Update the active `integrate-semantic-video-embeddings` planning artifacts before implementation so they no longer require RabbitMQ-specific retry queues, headers, acknowledgements, or channel isolation.
- Cut over video workflows independently and retain RabbitMQ rollback until Kafka consumer parity and durable-job recovery are proven.

## Capabilities

### New Capabilities

- `video-publication-event-stream`: Retained Kafka publication events, independent fanout and embedding consumers, durable publication handoff, replay, and migration behavior.
- `durable-media-work-jobs`: PostgreSQL-owned media and semantic processing jobs with Kafka wakeups, leases, delayed retries, polling recovery, idempotency, and terminal outcomes.

### Modified Capabilities

None.

## Impact

- Depends on `add-kafka-event-backbone`.
- Affects video publication/recovery, Feed fanout, embedding, media processing, persistence migrations, Kafka/RabbitMQ adapters, API/Worker composition, metrics, tests, Compose, and documentation.
- Requires a coordinated update to the active `integrate-semantic-video-embeddings` change before either overlapping implementation begins.
- Does not make Kafka the authoritative scheduler for long-running processing jobs and does not remove RabbitMQ globally.
