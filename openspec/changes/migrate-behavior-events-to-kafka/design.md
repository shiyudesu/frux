## Context

`action_changed` currently originates from a Redis atomic mutation, publishes through RabbitMQ with publisher confirms, and falls back to synchronous PostgreSQL persistence when publication is unavailable or uncertain. The worker persists a receipt and applies only the greatest action version; delayed recommendation attribution and profile projection are already handed to PostgreSQL outboxes.

`view_event_recorded` is committed to PostgreSQL with a transactional outbox. A dispatcher publishes RabbitMQ messages, and the recommendation worker durably records each behavior event before creating its own profile/outcome handoff.

Both flows already have the stable identity and idempotency needed for Kafka. The migration should preserve those correctness boundaries while making the broker history retained and consumer-group based.

## Goals / Non-Goals

**Goals:**

- Define independent versioned Kafka topics and stable partition keys for action and view events.
- Preserve action fallback, version ordering, payload conflict detection, and conditional Redis rollback.
- Preserve the view-event transactional outbox and accepted HTTP behavior.
- Commit Kafka offsets only after existing durable receipts and downstream outboxes commit.
- Validate Kafka production and decoding before activating Kafka business consumers.
- Leave retained event history available for new consumers during the configured retention window.

**Non-Goals:**

- Treating Kafka as a replacement for PostgreSQL action receipts, behavior facts, or profile outboxes.
- Rebuilding all historical user profiles from Kafka in this change.
- Adding analytics consumers, Flink, or long-term training storage.
- Migrating video or media workflows.
- Removing the generic RabbitMQ implementation.

## Decisions

### Use one topic per behavior event contract

- `frux.interaction.action-changed.v1`, keyed by canonical `(user_id, video_id, action_type)`;
- `frux.exposure.view-event-recorded.v1`, keyed by `user_id`.

Both use delete retention rather than log compaction because each record is an immutable accepted fact. The initial retention is seven days and remains registry-controlled. PostgreSQL remains the long-term source of truth.

The action key guarantees that opposing versions of the same logical action share a partition while allowing unrelated users/videos to progress independently. The view key keeps one user's playback sequence in one partition, matching profile projection semantics.

Alternative: one shared `behavior-events` topic. Rejected because the events have different volume, payload, retention tuning, producer, and active consumer ownership.

### Preserve the action fast path and database fallback

The interaction service will publish the same typed event through a transport-aware publisher after Redis assigns its monotonic version. Kafka production uses the backbone's acknowledged idempotent producer.

If the primary Kafka result fails or is uncertain, the service calls `PersistAcceptedActionEvent` synchronously exactly as it does for RabbitMQ uncertainty. Successful fallback also creates the durable profile/outcome handoffs, so the request does not need a later Kafka repair publication for correctness. If both Kafka and fallback fail, the existing conditional Redis rollback remains.

During migration, mirror failure does not fail a request whose primary transport or PostgreSQL fallback succeeded. Mirror gaps are acceptable because the shadow stream is diagnostic and Kafka activation starts at an explicit cutover boundary.

Alternative: add a database outbox to every Redis action. Rejected for this migration because it would put a new mandatory PostgreSQL transaction on the optimized action path and duplicate the existing synchronous fallback guarantee.

### Reuse and extend the view-event outbox dispatcher

The existing outbox payload is already the authoritative Kafka event payload. The dispatcher selects its primary and optional mirror publisher from the stream migration mode. An outbox row is marked dispatched after the primary transport acknowledges it; mirror results are observed separately.

At Kafka cutover, rows not yet dispatched use Kafka as primary. Already dispatched RabbitMQ rows are not backfilled into Kafka because durable behavior facts already exist and the migration has no historical-stream requirement.

### Register one active group per durable responsibility

- `frux.interaction.persist-action.v1` invokes the accepted-action worker;
- `frux.recommendation.consume-view.v1` invokes the recommendation behavior worker.

Action offset commit follows the transaction that inserts or validates the event receipt, applies the latest version, updates counts, and creates durable downstream handoffs. View offset commit follows the transaction that stores or recognizes the behavior fact and creates its durable projection/outcome handoff.

The consumers do not keep offsets open while waiting for embeddings or attribution dependencies; those failures remain owned by the existing leased PostgreSQL outboxes.

### Use shadow groups that cannot become active accidentally

Shadow consumers use distinct group IDs suffixed with `.shadow.<deployment>` and a handler that only validates envelope, key, event fields, age, and optional durable-fact parity. They never call the mutating worker.

Cutover creates or enables the registered active group at a recorded Kafka timestamp/offset boundary after producer health has remained acceptable for an observation window. The old RabbitMQ consumer remains available but stopped; rollback stops Kafka and restores RabbitMQ before changing the primary producer.

### Measure event-specific correctness

Metrics add registered results for:

- action primary/mirror production and synchronous fallback;
- view outbox primary/mirror dispatch;
- shadow decode and fact-parity results;
- active group lag and delivery age;
- duplicate receipts, action-version supersession, and view-event duplicate application.

No event ID or business identity becomes a label.

### Require acknowledgement from the active delivery transport

Migration pairs are valid only when the producer result required for success belongs to the
transport used by the active mutating consumer. `kafka_with_rabbit_mirror` is therefore valid only
with an active Kafka consumer; Rabbit-active and Kafka-shadow phases require
`rabbit_with_kafka_mirror`. A successful Kafka primary result cannot hide a failed Rabbit mirror
while Rabbit still owns business mutation.

### Apply active cutover boundaries as durable group offsets

Before starting an active behavior group, the worker uses franz-go/kadm to resolve the configured
RFC3339 boundary to one offset per topic partition and commits those offsets while the group is
inactive. The offset metadata records the boundary. Normal startup is initialize-only: any existing
committed offset is preserved, so changing configuration or restarting cannot rewind the group.
An explicit reset operation is available only for an inactive group.

### Retry missing shadow facts without reporting false mismatches

Parity readers distinguish `pending` (no durable fact yet) from `mismatch` (a durable fact exists
with conflicting content). Pending records receive a bounded number of delayed retries; if the fact
still does not arrive, the record is completed as pending-exhausted rather than mismatch. Existing
conflicts remain immediate mismatches.

### Enforce canonical behavior keys and supervised consumer health

Action and user keys are decoded and then re-encoded; the bytes must match exactly, rejecting
leading-zero, signed, or case aliases that could split one logical identity across partitions.
Consumer supervisors expose bounded session lifecycle metrics, preserve underlying authorization
errors for classification, retry transient broker/session failures, and terminate required active
consumer runtime on non-retryable authentication, configuration, or handler-contract failures.

## Risks / Trade-offs

- [Mirror publication can miss records] -> Treat mirror data as validation only and begin active consumption at a documented cutover boundary.
- [Action Kafka outage increases synchronous PostgreSQL fallback] -> Alert on fallback rate and retain conditional Redis rollback when both durable paths fail.
- [Key changes would reorder state transitions] -> Freeze key encodings in fixtures and require a new topic version for changes.
- [A consumer crash after PostgreSQL commit duplicates delivery] -> Preserve event receipts, payload conflict checks, and monotonic version application.
- [A hot user can concentrate view traffic] -> Key by user for correctness; monitor partition skew and version the topic if a future scale requires another model.
- [Seven-day Kafka retention is not sufficient for model training] -> Continue using PostgreSQL training and behavior facts; longer analytical retention is a separate capacity decision.
- [Concurrent cutover initialization] -> Require an inactive group, recheck committed offsets before
  committing, and use deterministic timestamp offsets so concurrent initializers either preserve an
  existing commit or write the same boundary.

## Migration Plan

1. Add both topic contracts, codecs, keys, group registrations, and tests with producers/consumers disabled.
2. Enable Kafka mirrors and shadow consumers while RabbitMQ remains primary and active.
3. Observe production success, decode parity, partition distribution, delivery delay, and durable-fact matches.
4. Cut over `view_event_recorded` first because its PostgreSQL outbox already isolates HTTP success from broker availability.
5. Cut over `action_changed`, monitor Kafka failures and synchronous fallback load, then stop its RabbitMQ consumer.
6. Retain per-stream rollback modes until the RabbitMQ retirement change.

Rollback stops the Kafka active group, restores the RabbitMQ consumer, and returns the primary producer to RabbitMQ. Stable event IDs make records delivered around the boundary idempotent.

## Open Questions

None.
