## Context

The current `video.published` RabbitMQ routing key fans one stable publication event into separate
queues for Feed fanout and the existing `hash-ngram-v1` embedding worker. Publication may originate
from review completion, media readiness, administrative restore, or reconciliation.

Media processing is already a PostgreSQL state machine with durable jobs, attempts, leases,
reconciliation, and polling. RabbitMQ only provides a wakeup, but the current consumer retains the
message while the long ffmpeg job runs.

The recommendation roadmap separately defines semantic model serving and semantic embedding
integration after the trusted-measurement track. This Kafka migration must not implement those later
changes. It only establishes a stable publication stream that future consumers may reuse.

## Goals / Non-Goals

**Goals:**

- Publish each stable first-publication fact through a retained Kafka topic.
- Give Feed fanout and hash embedding independent active/shadow consumer groups.
- Persist immutable publication proof and an operational publication outbox atomically with the
  first public-eligibility transition.
- Keep media-processing correctness in PostgreSQL and use Kafka only as a low-latency wakeup hint.
- Preserve original publication time, stable event identity, idempotency, rollback, and
  reconciliation.
- Cut over Feed, hash embedding, and media wakeups independently.

**Non-Goals:**

- Implementing or deploying a semantic embedding HTTP service.
- Generating, storing, retrying, backfilling, or consuming semantic model vectors.
- Adding semantic jobs, semantic readiness gates, semantic coverage, pgvector, ANN recall, or
  semantic recommendation behavior.
- Using Kafka offsets as media job state.
- Removing PostgreSQL polling/reconciliation or removing RabbitMQ globally.

## Decisions

### Use one retained publication event topic

`frux.video.published.v1` is keyed by `video_id`, uses broker append time and delete retention, and
has a fixed reviewed partition count. It carries the stable publication event ID, video ID, author
ID, publication time, and bounded title/description fields already needed by Feed and hash
embedding.

Independent groups are:

- `frux.feed.video-published.v1`;
- `frux.embedding.video-published.v1`.

The embedding group remains model-neutral infrastructure but this change wires only the existing
deterministic `hash-ngram-v1` worker. A future semantic-integration change may add its own durable
handoff after the roadmap prerequisites are complete.

### Create immutable publication proof plus an operational outbox

Every transaction that first establishes public eligibility creates:

- one immutable `video_publication_event_fact` row keyed by stable event ID;
- one operational `video_publication_event_outbox` row keyed by the same event ID.

Review, media readiness, restore, batch management, and reconciliation use the same transaction-aware
helper. Notification readiness or delivery is never proof that the publication event exists.

The dispatcher starts asynchronously, leases bounded batches, publishes through the configured
RabbitMQ/Kafka transition mode, and marks completion only after required acknowledgements. Broker
outages never block unrelated worker startup. Dispatched operational rows may be cleaned after the
replay window only while the immutable fact remains; reconciliation keys off the fact and cannot
re-emit cleaned events.

If media promotion succeeds but the publication transaction fails, promoted objects are protected
again. An undispatched outbox payload is refreshed when current public media URLs become available,
without changing the immutable event identity or publication time.

### Keep Feed fanout idempotent and independently recoverable

The Feed group commits only after preheat/index effects finish. Duplicate deliveries and replay use
the stable publication identity and idempotent Redis operations. Shadow mode uses a non-mutating
parity reader rather than treating envelope validity as parity.

Retry/DLQ routing is owned by the separate `add-kafka-failure-recovery` change. Until that change is
implemented, active group failures remain visible and do not silently skip records.

### Keep hash embedding as a bounded publication consumer

The hash embedding consumer:

1. validates the publication envelope and video key;
2. canonicalizes the existing title/description input for `hash-ngram-v1`;
3. generates or confirms the hash vector;
4. conditionally persists `(video_id, hash-ngram-v1)`;
5. commits the Kafka offset only after persistence succeeds.

Deterministic invalid hash input is terminal and observable. There is no remote call, semantic model
identity, semantic job, or semantic retry state in this change.

### Use Kafka media commands only as wakeup hints

`frux.media.processing-requested.v1` is a short-retention command topic keyed by `asset_id`. The
upload transaction creates the PostgreSQL processing job before wakeup publication.

Kafka wakeups and database polling submit work to one slot-bounded scheduler. A slot is reserved
before claiming one job. Each claim has a unique token; heartbeat, finalization, retry, and terminal
transitions require that token and a current unexpired lease.

Asset metadata, variants, cleanup tasks, and job completion commit in one token-fenced transaction.
Public projection and notifications run only after commit. Privacy/delete transitions atomically
create durable media-protection or cleanup intents so a process crash cannot leave objects publicly
exposed or permanently retained.

The Kafka wakeup consumer commits after confirming the durable job and signalling the scheduler. It
never holds the record throughout ffmpeg processing. Lost, duplicate, or delayed wakeups remain safe
because PostgreSQL polling and reconciliation own correctness.

### Use explicit cutover and drain gates

Each active Kafka group requires a past, millisecond-aligned cutover boundary. Initial offset setup is
serialized across worker replicas with a PostgreSQL advisory lock. Before the first cutover, the
corresponding RabbitMQ legacy queue, quorum source queue, unacknowledged deliveries, and DLQ must be
empty. Existing initialized offsets are preserved on restart.

Transition publishers require the acknowledgement set appropriate to their active transport. Dual
publication is concurrent so one slow broker cannot consume the other broker's attempt deadline.

## Risks / Trade-offs

- [Publication persistence adds durable rows] -> Keep immutable facts compact and clean only
  replay-expired operational rows.
- [Feed or hash replay repeats work] -> Preserve stable IDs and conditional/idempotent writes.
- [Kafka wakeup is lost] -> PostgreSQL polling and reconciliation remain mandatory.
- [Media processing overlaps privacy/delete] -> Fence finalization by claim token and persist durable
  protection/cleanup intents in the lifecycle transaction.
- [Partition-count changes remap ordering keys] -> Require a new topic version instead of expanding a
  registered version.
- [Kafka/Rabbit outage occurs at startup] -> Supervise transports independently while durable
  database workers continue running.

## Migration Plan

1. Add immutable publication facts, operational outbox rows, transaction-aware insertion, and
   reconciliation tests while RabbitMQ remains active.
2. Register the publication topic, Feed group, hash embedding group, and their shadow groups.
3. Enable Rabbit-primary/Kafka-mirror publication and verify non-mutating parity.
4. Cut over Feed fanout after Rabbit drain and observation gates pass.
5. Cut over hash embedding independently after its parity and persistence gates pass.
6. Register Kafka media wakeups, route wakeups and polling through the shared fenced scheduler, and
   verify loss/duplicate/restart recovery.
7. Stop each corresponding RabbitMQ consumer only after its independent cutover; retain rollback
   modes until RabbitMQ retirement.

Rollback restores only the affected RabbitMQ consumer/producer. Publication facts, operational
outbox rows, hash embeddings, media jobs, and media lifecycle intents remain valid and idempotent.

## Open Questions

None.
