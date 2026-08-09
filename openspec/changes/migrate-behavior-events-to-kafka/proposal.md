## Why

Interaction and playback behavior are Frux's highest-value replayable signals for recommendation and future analytics. They should become retained Kafka event streams with explicit ordering and consumer ownership rather than transient RabbitMQ tasks.

## What Changes

- Publish accepted action changes to a versioned Kafka topic keyed by the stable action-state identity so opposing versions for the same user, video, and action remain ordered within one partition.
- Publish committed view events from the existing PostgreSQL outbox to a versioned Kafka topic keyed by user identity so recommendation behavior processing can preserve per-user stream order.
- Run interaction persistence and recommendation behavior consumers under registered Kafka consumer groups and commit offsets only after their existing durable receipts/outboxes commit.
- Preserve stable event IDs, version ordering, payload conflict checks, synchronous PostgreSQL fallback for action publication failures, and idempotent duplicate handling.
- Introduce dual-publish plus shadow-consume validation before each active consumer cuts over; both transition transports must acknowledge each event, while shadow consumers validate envelopes and parity without writing business state.
- Resolve cutover boundaries from broker-assigned append timestamps and require the action boundary to be strictly after the view boundary.
- Retain behavior events for replay and future independent consumers instead of deleting them after successful consumption.
- Remove RabbitMQ from these behavior paths only after Kafka lag, parity, duplicate, fallback, and rollback gates pass.

## Capabilities

### New Capabilities

- `behavior-event-streams`: Kafka topic, partitioning, publication, consumption, retention, replay, migration, and correctness requirements for action and view behavior events.

### Modified Capabilities

- `view-event-feedback`: Replace the RabbitMQ-specific reliable recommendation publication requirement with a Kafka outbox stream and consumer-group delivery contract.
- `creator-content-management`: Replace RabbitMQ publication/redelivery scenarios for accepted action events with Kafka acknowledgement, fallback, ordering, and duplicate-consumption behavior.

## Impact

- Depends on `add-kafka-event-backbone`.
- Affects interaction and exposure application services/outboxes, Kafka and RabbitMQ adapters, recommendation behavior workers, API/Worker composition, configuration, metrics, tests, and module documentation.
- Keeps PostgreSQL receipts and profile/outcome outboxes as durable business boundaries; Kafka does not provide exactly-once writes to PostgreSQL.
- Does not migrate video publication, embedding, media processing, or RabbitMQ dead-letter administration.
