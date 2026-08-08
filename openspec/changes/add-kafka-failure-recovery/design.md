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

The retry envelope preserves the original Kafka key and value and adds bounded headers:

- original topic, partition, and offset;
- original event ID and schema version;
- owning consumer group;
- attempt and next tier;
- registered failure class;
- first failure time and latest failure time;
- replay ID when operator-triggered.

Retry consumers enforce `not_before` by pausing the assigned partition rather than busy polling. On another retryable failure they publish to the next tier; terminal or exhausted records go to the group's DLQ.

Alternative: encode retries only by leaving the source offset uncommitted. Rejected for real-time groups because one poison record would block later records in that partition.

Alternative: a database scheduler for all event failures. Rejected because retained Kafka retry records already carry the immutable event, while database scheduling is reserved for workflows with independent job state.

### Confirm next-hop publication before committing source offset

After a handler failure, the consumer publishes the unchanged key/value and bounded metadata to the next retry or DLQ topic using acknowledged idempotent production. Only after acknowledgement does it mark the source offset commit-eligible.

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

Payload, unrestricted headers, keys, credentials, and raw errors are never returned.

### Make replay non-destructive and idempotent by request

The replay endpoint accepts allowlisted DLQ topic, partition, offset, registered reason, and `Idempotency-Key`. The service:

1. serializes concurrent replay for the source record;
2. fetches the exact retained record;
3. validates source metadata, consumer group, event contract, and payload hash;
4. creates a bounded audit fact and replay-attempt row;
5. republishes unchanged key/value to the original topic or registered first retry tier with a new replay ID;
6. waits for acknowledgement;
7. commits success audit and replay result.

The DLQ record remains until topic retention expires. Repeating the same idempotency key returns the prior result; a later intentional replay requires a new key and remains separately audited.

Alternative: mutate consumer-group offsets. Rejected for single-record recovery because it can replay unrelated records and cannot target one consumer failure safely.

### Replace queue depth with lag and retained-offset signals

Metrics include:

- retry and DLQ publications by registered group/result;
- retry-topic and DLQ end-offset growth;
- active-group lag;
- oldest retry/DLQ record age;
- replay outcomes;
- inspection/replay fetch failures;
- records approaching retention expiry.

Alerts combine ingress, lag, oldest age, and no-progress windows rather than treating Kafka end offset as an exact queue depth.

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
