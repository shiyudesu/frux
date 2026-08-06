## Context

Frux declares durable classic queues and supervises consumers, but it has no dead-letter exchanges or bounded poison-message policy. RabbitMQ queue type cannot be changed in place. The management plugin is already present in Compose and can support bounded operator inspection without copying every dead-letter payload into PostgreSQL.

## Goals / Non-Goals

**Goals:**

- Bound retry attempts and isolate terminal messages per consumer.
- Use broker-native durable dead-letter storage and safe republishing.
- Preserve event identity and application idempotency during replay.
- Provide permission-protected, audited inspection and single-message replay.

**Non-Goals:**

- Arbitrary payload editing, bulk replay, cross-environment transfer, or replacing RabbitMQ with a database queue.
- Guaranteeing original ordering after retries or replay.
- A Web UI.

## Decisions

### Introduce versioned quorum source and dead-letter queues

Critical consumers move to new queue names declared as quorum queues with delivery limits, `overflow=reject-publish`, a DLX, and at-least-once dead-lettering. Less critical queues may use documented at-most-once DLX behavior. Existing queues are not redeclared with a different type.

### Classify errors before acknowledgement

Malformed, unsupported, or terminal domain messages reject without requeue. Retryable infrastructure failures nack for broker redelivery until the delivery limit moves them to the DLQ. Consumers remain idempotent by stable event ID.
Every versioned quorum consumer owns a supervised channel, including the secondary
consumer in `dual` mode. Retryable failures close that channel and reconsume with
capped backoff so requeue cannot become a hot loop.

### Inspect through a broker adapter

An internal adapter uses RabbitMQ management APIs for queue summaries and a bounded head preview with requeue semantics. Credentials stay server-side. PostgreSQL stores only audit facts, not a second copy of every dead-letter payload.

Alternative: consume DLQs into a `governance_dead_letter` table. Rejected because it duplicates broker ownership and creates a second recovery state machine.

### Replay with publish-confirm-before-ack

Replay reads one selected dead-letter envelope and requires broker `x-death`
provenance matching the configured source queue, exchange, and `routing-keys`;
direct DLQ publications are rejected. It validates bounded headers, prepares a
valid immutable audit fact before publishing, republishes the unchanged business
payload with original event ID plus a new replay ID, waits for publisher
confirmation, appends the prepared fact, and only then acknowledges the DLQ
message. Event IDs that cannot be represented directly in bounded audit fields
use a stable SHA-256 reference. Publish failure leaves the message dead-lettered.

### Cut over queue topology explicitly

Publishers bind both old and new consumers only during a controlled drain window where duplicate delivery is safe. Metrics determine when old queues are empty before their bindings are removed.

## Risks / Trade-offs

- [Quorum queues consume more resources] -> Apply them only to durable business workflows and set length limits.
- [At-least-once DLX can duplicate messages] -> Keep all consumers idempotent and preserve original event IDs.
- [Management API inspection perturbs queue order] -> Bound previews, requeue immediately, and document that DLQ order is not a business guarantee.
- [Replay repeats a still-broken message] -> Require one-message replay, reason, permission, and audit; no bulk endpoint.

## Migration Plan

1. Add DLX exchanges, versioned quorum queues, policies, metrics, and integration tests.
2. Bind new queues while old consumers remain active and idempotent.
3. Start new consumers, observe parity, then stop old bindings after drain.
4. Enable protected inspection and replay last.
5. Roll back publishers to old bindings while retaining new DLQs for investigation.

## Open Questions

- Which current queues require at-least-once dead-lettering versus cheaper at-most-once isolation.
