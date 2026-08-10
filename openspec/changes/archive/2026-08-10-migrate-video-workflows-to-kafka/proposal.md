## Why

Video publication is a durable domain event consumed independently by Feed fanout and the existing
hash embedding pipeline, while media processing is a retryable PostgreSQL-owned job. Treating these
different responsibilities as RabbitMQ deliveries obscures their semantics and blocks the move to a
replayable Kafka event stream.

## What Changes

- Publish each stable first-publication fact to a retained versioned Kafka topic keyed by video ID.
- Give Feed fanout/preheating and `hash-ngram-v1` embedding intake independent consumer groups so
  either workflow can lag, replay, or recover without affecting the other.
- Add a durable publication outbox/recovery boundary so committing public eligibility does not depend on Kafka availability and duplicate publication remains idempotent.
- Keep media processing jobs in PostgreSQL as the source of truth; publish a short-retention Kafka command only as a wakeup hint, while leases, retry timing, terminal classification, reconciliation, and polling remain database-owned.
- Preserve the existing deterministic `hash-ngram-v1` behavior and storage contract; this migration
  does not introduce a semantic model, semantic HTTP service, semantic job, or semantic retry state.
- Cut over video workflows independently and retain RabbitMQ rollback until Kafka consumer parity and durable-job recovery are proven.
- Provide the Kafka publication stream that the future roadmap step
  `integrate-semantic-video-embeddings` may consume after its measurement and service prerequisites
  are completed.

## Capabilities

### New Capabilities

- `video-publication-event-stream`: Retained Kafka publication events, independent Feed and hash
  embedding consumers, durable publication handoff, replay, and migration behavior.
- `durable-media-work-jobs`: PostgreSQL-owned media-processing jobs with non-authoritative Kafka
  wakeups, fenced leases, polling recovery, idempotency, and terminal outcomes.

### Modified Capabilities

None.

## Impact

- Depends on `add-kafka-event-backbone`.
- Affects video publication/recovery, Feed fanout, hash embedding intake, media processing,
  persistence migrations, Kafka/RabbitMQ adapters, API/Worker composition, metrics, tests, Compose,
  and documentation.
- Does not implement or modify the recommendation roadmap changes
  `add-semantic-embedding-service`, `integrate-semantic-video-embeddings`, semantic backfill,
  semantic user profiles, pgvector, ANN recall, or semantic shadow evaluation.
- Does not make Kafka the authoritative scheduler for long-running processing jobs and does not remove RabbitMQ globally.
