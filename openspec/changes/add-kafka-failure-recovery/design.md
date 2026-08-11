## Context

The current recovery implementation is built around RabbitMQ quorum queues, delivery limits, DLX/DLQ routing, `x-death` headers, Management API queue inspection, queue-head `basic.get`, publisher confirmation, and acknowledgement that removes the replayed DLQ message.

Kafka records are immutable and addressed by topic, partition, and offset. Consumer groups independently track progress; a poison record can block one partition, while other groups remain unaffected. Recovery must therefore distinguish:

- short transient retry while a partition is held;
- moving a failed record to consumer-specific retry topics;
- retaining an immutable DLQ record;
- resetting or creating consumer-group offsets for broad replay;
- operator replay of one DLQ record without deleting it.

PostgreSQL jobs remain outside this mechanism because their retry state is already durable and queryable.

## Goals / Non-Goals

**Goals:**

- Prevent one failed record from blocking a real-time consumer partition indefinitely.
- Preserve source identity and unchanged business payload across retry and DLQ publication.
- Provide authorized inspection by topic, partition, and offset.
- Provide audited, idempotent, non-destructive single-record replay.
- Expose lag, retry, DLQ, replay, and retention risk.
- Replace the existing `message-dead-letter-recovery` requirements with Kafka-native behavior.

**Non-Goals:**

- Providing arbitrary payload editing or bulk replay.
- Using Kafka retry topics for media or semantic database jobs.
- Guaranteeing original partition ordering after a record moves to retry.
- Offering a general Kafka browser or exposing broker credentials.
- Using event IDs or raw errors as metric labels.

## Decisions

### Register recovery policy per consumer group

Every active consumer group declares one policy:

- `block-and-retry`: bounded local retries, then stop so the same partition record is retried after restart;
- `retry-topics`: bounded local retries, then publish through registered delay tiers and finally a DLQ;
- `durable-job`: terminal handling after durable job verification because PostgreSQL owns later retries.

Policies are closed registry entries. A consumer cannot invent topic names, delays, or retry counts from message content.

Alternative: one generic recovery policy for all consumers. Rejected because behavior persistence, Feed fanout, media wakeups, and long-running jobs have different durable boundaries.

### Use consumer-specific fixed retry tiers

For `retry-topics`, each consumer group receives fixed-delay topics such as 5 seconds, 30 seconds, 2 minutes, 10 minutes, and 30 minutes. The exact tiers are registry-controlled and a consumer may use only the subset it needs.

The retry envelope preserves the original Kafka key and value and adds bounded headers. A terminal-contract record whose source key itself is malformed has one narrow direct-DLQ codec path: it still validates the registered source, owning group, DLQ, event/schema identity when available, payload hash, and bounds, but does not reapply the failed source key-kind check. Retry tiers and replay never use this bypass.

Headers contain:

- original topic, partition, and offset;
- original event ID and schema version;
- owning consumer group;
- attempt and next tier;
- registered failure class;
- first failure time and latest failure time;
- replay ID when operator-triggered.

Retry consumers enforce `not_before` by pausing only the affected assigned partition and scheduling its resume while other assigned partitions continue polling and processing. Each delayed record is fenced by the partition assignment generation. Taking a ready record acquires a partition ownership lease that remains held through handling and offset commit; revocation waits for an active lease, while a revoke that wins before acquisition invalidates and discards the buffered record. Revocation, partition loss, reassignment, and session shutdown therefore cannot let the old owner handle or commit after ownership ends, and the new owner refetches from Kafka. On another retryable failure retry consumers publish to the next tier; terminal or exhausted records go to the group's DLQ.

A dependency-created deadline is treated as a retryable handler failure while the Consumer context remains active. Only cancellation of the Consumer context itself bypasses routing. Missing, obsolete, malformed, or tier-inconsistent retry metadata is terminally quarantined to the owning DLQ with unchanged key/value and a sanitized bounded header containing the consumed retry coordinate, owning group, key/payload hashes, `failure_class=recovery_metadata_invalid`, and `non_replayable=true`. The retry offset becomes commit-eligible only after acknowledged quarantine publication.

Retry tiers never use a permanent earliest reset. Before a retry group can join, a PostgreSQL session advisory lock serializes a durable initialization marker keyed by environment, prefix, resolved recovery group, resolved versioned topic, and marker version. The marker and its per-partition initialization rows are persisted before Kafka commits. Retained starts are committed one partition at a time in deterministic order, every partition response is inspected, and each acknowledged partition is durably recorded so a partial request or process stop resumes only missing work. The marker becomes complete only after a fresh Kafka snapshot proves every planned commit. A complete marker makes any missing, dead, deleted, expired, or out-of-range established offset a fatal data-loss error rather than a brand-new group. New trailing partitions extend the same marker while existing commits remain unchanged. Concurrent replicas serialize on PostgreSQL, and the consumer then uses committed-only `NoResetOffset`.

Alternative: encode retries only by leaving the source offset uncommitted. Rejected for real-time groups because one poison record would block later records in that partition.

Alternative: a database scheduler for all event failures. Rejected because retained Kafka retry records already carry the immutable event, while database scheduling is reserved for workflows with independent job state.

### Confirm next-hop publication before committing source offset

After a handler failure, the consumer publishes the unchanged key/value and bounded metadata to the next retry or DLQ topic using acknowledged idempotent production. Only after acknowledgement does it mark the source offset commit-eligible.

franz-go uses `ProducerBatchMaxBytesFn` so every resolved registered source, retry, and DLQ Topic receives its own broker allowance; unknown Topics receive the smallest registered conservative allowance and public publication paths reject unregistered Topic IDs. One reviewed calculation adds 64 KiB protocol/batch headroom to each application Topic allowance. A recovery Topic's application allowance is the source Topic's broker allowance plus the bounded recovery-header allowance; its broker allowance adds the same protocol headroom again. Each recovery publication separately proves that unchanged source key/value bytes do not exceed the source broker allowance, recovery headers stay bounded, and the total fits the destination. This permits an application-oversized poison record accepted by the source broker to reach DLQ while rejecting source bytes above that broker maximum.

A crash between acknowledgement and offset commit may duplicate the retry record. Stable event IDs and consumer receipts absorb the duplicate. Kafka transactions are not required because they would not cover business database writes and would add operational complexity for a duplication already handled safely.

### Identify DLQ records by topic, partition, and offset

DLQ topic names are code-owned and consumer-specific. An admin adapter lists allowlisted DLQ topics, partition end offsets, retained range, recent ingress, and estimated record counts without joining arbitrary topics.

Preview requires a topic, partition, starting offset, and bounded limit. An isolated non-group reader fetches exact immutable records. Responses contain:

- topic, partition, offset, timestamp;
- source topic/partition/offset and consumer group;
- event ID/replay ID references;
- registered failure class and attempt;
- key and payload byte counts and SHA-256;
- content type, JSON validity, and bounded top-level field names.
- consumed retry coordinates, bounded metadata code, and `replayable=false` for quarantined invalid recovery metadata.

Payload, unrestricted headers, keys, credentials, and raw errors are never returned.

### Make replay non-destructive and idempotent by request

The replay endpoint accepts allowlisted DLQ topic, partition, offset, registered reason, and `Idempotency-Key`. The service:

1. serializes concurrent replay for the source record;
2. fetches the exact retained record;
3. validates source metadata, consumer group, event contract, and payload hash;
4. commits a pending replay claim with the stable replay ID and idempotency fingerprint while holding session-scoped actor/key and coordinate serialization;
5. republishes unchanged key/value outside that transaction to the registered first retry tier with a new replay ID, so a replay for one group never reaches sibling groups sharing the source topic;
6. waits for acknowledgement;
7. finalizes the result and immutable audit in a second transaction.

Quarantine records explicitly marked non-replayable fail provenance validation before a pending claim or publication.

The DLQ record remains until topic retention expires. Repeating the same idempotency key returns the prior result; a later intentional replay requires a new key and remains separately audited. If the producer reports a possible acknowledgement, or acknowledgement succeeds but finalization fails, the committed claim remains pending/unknown and blocks another publication. A repeated identical authorized request reconciles it by scanning only the registry-defined destination and retained evidence window for the stable Replay ID. Broker evidence finalizes success plus audit without publishing again. Absence requires a bounded settlement loop: complete scans and repeated retained start/end snapshots must remain stable after the producer uncertainty window, and any growth restarts scanning and stability counting. Stable clean absence finalizes a bounded failure; growth that never settles, cancellation, expired evidence, malformed evidence, or unavailable evidence leaves the claim pending.

Alternative: mutate consumer-group offsets. Rejected for single-record recovery because it can replay unrelated records and cannot target one consumer failure safely.

### Replace queue depth with lag and retained-offset signals

Metrics include:

- retry and DLQ publications by registered group/result;
- retry-topic and DLQ end-offset growth;
- per-stage consumed-topic lag and explicitly aggregated owning-workflow lag;
- per-stage session health and worst-stage owning-workflow health;
- oldest retry/DLQ record age;
- replay outcomes;
- inspection/replay fetch failures;
- records approaching retention expiry.

Alerts combine absolute end-offset movement, retained backlog, oldest-record timestamp movement, and durable recovery/replay progress over the same bounded window rather than treating recent ingress alone or Kafka end offset as an exact queue depth. Successful non-destructive replay counts as progress.

The API supervises a bounded periodic DLQ-summary collector independently of admin HTTP requests. Broker outages mark summary gauges stale and are retried without making process startup depend on Kafka availability.

## Risks / Trade-offs

- [Retry topics can multiply topic count] -> Create them only for registered groups that need non-blocking retries and use fixed shared tier definitions per group.
- [Moving a record breaks source partition order] -> Document this explicitly and require application version/idempotency rules for retry-topic consumers.
- [Crash after retry publication can duplicate retry records] -> Preserve original event IDs and make all target consumers duplicate-safe.
- [DLQ counts are estimates rather than destructive queue depth] -> Report retained offset ranges, ingress, and consumer progress separately.
- [Retained DLQ records can be replayed repeatedly] -> Require idempotency keys, serialize concurrent attempts, and audit every replay.
- [A record can expire before investigation] -> Configure long DLQ retention and alert on oldest age approaching the retention boundary.

## Migration Plan

1. Add Kafka recovery registry, retry/DLQ codecs, routing tests, and metrics without enabling any active group.
2. Add the replay-attempt persistence model and Kafka admin adapter.
3. Add Kafka-oriented admin endpoints and permission/audit tests while RabbitMQ endpoints remain available.
4. Enable one low-risk consumer group's retry/DLQ flow and validate duplicate, delay, poison, restart, and retention behavior.
5. Enable remaining registered groups; database-job consumers use `durable-job` rather than retry topics.
6. Remove RabbitMQ recovery endpoints and implementation only in `retire-rabbitmq-infrastructure`.

Rollback disables Kafka retry routing for the affected group and restores its previous consumer transport. Retained Kafka retry/DLQ topics remain for diagnosis.

## Open Questions

None.
