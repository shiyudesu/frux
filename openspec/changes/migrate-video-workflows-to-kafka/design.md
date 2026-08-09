## Context

The current `video.published` RabbitMQ routing key fans one publication event into separate queues for Feed fanout and video embedding. Publication can originate from review completion, media readiness, administrative restore, or reconciliation, and existing stable event IDs prevent duplicate publication effects.

Media processing is already a PostgreSQL state machine with jobs, leases, attempts, reconciliation, and polling. RabbitMQ only wakes the worker, but the current consumer performs the long-running job while holding the message.

The active `integrate-semantic-video-embeddings` change currently proposes a dedicated RabbitMQ channel and five TTL retry queues for remote semantic generation. That design is appropriate for RabbitMQ delivery semantics but would be an unnecessary retry-topic state machine after Kafka adoption. Semantic work has explicit model, text-hash, attempt, availability, and long-outage behavior that fits a durable PostgreSQL job.

## Goals / Non-Goals

**Goals:**

- Make first publication a retained Kafka event with independent fanout and embedding consumer groups.
- Persist a publication outbox in the same durable boundary that establishes the stable publication fact.
- Keep media processing correctness in PostgreSQL and use Kafka only to reduce wakeup latency.
- Give semantic embedding a PostgreSQL job with leases and delayed retries rather than broker-owned retry tiers.
- Preserve hash embedding progress during semantic service outages.
- Allow each video workflow to cut over and roll back independently.

**Non-Goals:**

- Moving media bytes, vectors, titles, or descriptions outside their existing bounded event contracts.
- Using Kafka offsets as media or semantic job state.
- Removing polling or reconciliation from media and semantic workers.
- Backfilling historical semantic embeddings; the existing backfill change remains separate.
- Enabling semantic recommendation recall.

## Decisions

### Use one retained publication event topic

`frux.video.published.v1` is keyed by `video_id`, uses delete retention, and initially retains 30 days because publication volume is low and replay is valuable. The event keeps the stable publication event ID, video ID, author ID, publication time, and bounded text fields already required by fanout and embedding.

Independent groups are:

- `frux.feed.video-published.v1`;
- `frux.embedding.video-published.v1`.

Kafka retains one event copy while each group owns its offset and recovery. A slow embedding dependency cannot hold Feed fanout offsets, and replaying embedding does not repeat fanout.

Alternative: separate fanout and embedding topics. Rejected because publication is one domain fact and Kafka consumer groups already provide independent delivery.

### Create a video-owned publication event outbox

The transaction that first establishes a stable public publication fact inserts one `video_publication_event_outbox` row keyed by `event_id`. Review, media readiness, restore, and reconciliation all call the same idempotent publication boundary.

The dispatcher publishes Kafka and marks the row dispatched after acknowledgement. A lease, `available_at`, attempts, bounded error class, and stable payload support crash recovery. The publication notification outbox is not reused because creator notification delivery and domain-event publication have different recipients, payloads, readiness, retention, and consumers.

Alternative: continue requiring the lifecycle transition to synchronously publish Kafka. Rejected because broker availability must not prevent a durable video from reaching its valid public state.

### Keep Feed fanout idempotent and isolate its recovery

The fanout consumer commits after preheating and the selected inbox/outbox index writes succeed. Existing Redis sorted-set semantics and publication identity must remain duplicate-safe.

Retryable Redis or repository failures use the Kafka failure policy registered for the fanout group so one failed video does not block later publication records indefinitely. Terminal envelope failures go to that group's DLQ. Replaying the fanout group or DLQ must not change the video's original publication time.

### Split embedding intake from semantic execution

The publication consumer performs only bounded intake:

1. validate the publication event;
2. generate or confirm `hash-ngram-v1`;
3. upsert one semantic job identified by `(video_id, semantic_model)` with the canonical text hash;
4. commit the Kafka offset after hash persistence and semantic-job persistence commit.

A changed text hash resets or supersedes the semantic job for that model. Duplicate publication records leave identical hash facts and jobs unchanged.

The remote semantic request runs in a separate leased database worker. The job stores state, attempts, `available_at`, lease owner/until, bounded error class, text hash, model identity, and completion metadata. Retry delay follows the existing intended sequence and then caps at 30 minutes. Disabled semantic integration leaves jobs pending or intentionally suspended according to documented configuration; it never removes hash coverage.

Alternative: Kafka retry topics for every semantic failure. Rejected because long outages, model gates, leases, coverage queries, cancellation, and exact text/model identity are job state rather than event-delivery state.

### Use Kafka media commands only as wakeup hints

`frux.media.processing-requested.v1` is a short-retention command topic keyed by `asset_id`. The upload transaction creates the PostgreSQL processing job before attempting to publish the wakeup.

The Kafka consumer validates that the durable job identity exists, signals a bounded local scheduler, and commits promptly. It does not hold the Kafka record throughout ffmpeg execution. The scheduler leases jobs from PostgreSQL, and the existing poller continues to find jobs when a command is missing, duplicated, delayed, or consumed before local capacity is available.

Alternative: consume and transcode before committing the Kafka record. Rejected because long processing would interact poorly with consumer-group liveness and duplicate the job's lease/retry state in offsets.

### Reconcile the active semantic OpenSpec before code implementation

Before implementation begins, `integrate-semantic-video-embeddings` must be updated:

- remove dedicated RabbitMQ connections/channels and TTL retry queues;
- replace attempt headers and broker retry backlog with semantic job fields and metrics;
- make Kafka intake depend on this change and the Kafka backbone;
- retain its semantic HTTP contract, strict response validation, hash-first behavior, model identity, coverage metrics, and future backfill boundary.

The changes must not be implemented concurrently from conflicting artifacts.

### Cut over publication, media wakeups, and semantic work separately

Publication producer mirror/shadow validation precedes activation of each consumer group. Media commands may switch to Kafka while RabbitMQ remains as a temporary mirror because database polling owns recovery. Semantic execution can move to database jobs before or after the publication consumer cuts over, provided only one intake path creates the same idempotent job.

## Risks / Trade-offs

- [The publication outbox adds another durable row] -> Keep it compact, unique by event ID, lease in bounded batches, and remove only completed rows after a replay window.
- [Fanout replay repeats Redis writes] -> Preserve idempotent sorted-set/index operations and test duplicate publication events.
- [Kafka wakeup can be lost] -> Treat PostgreSQL polling and reconciliation as mandatory correctness paths.
- [Semantic job backlog can grow during long outages] -> Cap retry frequency, expose pending count/oldest age, support suspension, and preserve hash processing.
- [Updating an active OpenSpec creates planning dependency] -> Update and validate `integrate-semantic-video-embeddings` before either code change is applied.
- [Publication retention eventually expires] -> Keep durable video/publication facts and outbox history sufficient for reconciliation; Kafka is not the only source of publication truth.

## Migration Plan

1. Update `integrate-semantic-video-embeddings` to the durable-job design.
2. Add publication outbox/job models, migrations, repositories, and dispatchers with RabbitMQ still active.
3. Add the publication topic, Kafka mirror, fanout/embedding shadow groups, and parity metrics.
4. Activate the Kafka Feed fanout group, then the embedding intake group, one at a time.
5. Move semantic execution to the database worker and verify hash coverage, semantic backlog, retries, and recovery.
6. Enable Kafka media wakeups while retaining RabbitMQ mirror and PostgreSQL polling.
7. Stop the corresponding RabbitMQ consumers after observation windows; retain rollback configuration until final retirement.

Rollback restores the RabbitMQ consumer or publisher for the affected workflow. Publication outbox rows, media jobs, and semantic jobs remain valid and idempotent across rollback.

## Open Questions

None.

## Review Clarifications

- The transaction that first establishes public eligibility also upserts both lifecycle notification
  and publication-event rows. Media-backed rows may remain blocked until public delivery is ready;
  notification readiness or delivery is never handoff proof.
- Feed, embedding-intake, and media-wakeup shadows use non-mutating readers, distinguish propagation
  pending from mismatch, retry pending inline with a fixed bound, and reject nil parity at configured
  shadow/cutover gates.
- Each semantic processor claims one job with a unique token, heartbeats during the remote request,
  fences complete/retry on token/hash/unexpired lease, and starts only after resume succeeds.
