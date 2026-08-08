## Context

Frux currently exposes application-level publisher and consumer interfaces but implements them with RabbitMQ exchanges, queues, manual acknowledgements, publisher confirms, quorum delivery limits, and a RabbitMQ Management API adapter. PostgreSQL remains the durable source for business facts, Redis owns selected fast state, and workers already rely on stable event IDs and idempotent persistence.

The Kafka foundation must serve two different future uses:

1. retained domain and behavior events consumed by independent consumer groups;
2. short-lived wakeup commands whose correctness remains owned by a PostgreSQL job.

It must not hide Kafka behind a lowest-common-denominator queue interface. Topic, partition key, consumer group, offset, retention, and replay are part of the contract.

## Goals / Non-Goals

**Goals:**

- Provide a typed Kafka producer, consumer-group runtime, administrator, and topic registry.
- Establish stable versioned wire envelopes and topic names before business cutover.
- Make partitioning, retention, cleanup, and consumer-group ownership code-reviewed decisions.
- Support idempotent production, explicit offset commits, supervised reconnects, cancellation-aware shutdown, and safe metrics.
- Support gradual RabbitMQ-to-Kafka migration without two active consumers mutating the same business state.
- Keep local Compose reproducible while documenting stronger production settings.

**Non-Goals:**

- Migrating a business event in this change.
- Providing cross-system exactly-once semantics between Kafka and PostgreSQL, Redis, or object storage.
- Introducing Flink, Kafka Connect, a schema registry, CDC, or a data lake.
- Implementing delayed retries, DLQ inspection, or operator replay; those belong to `add-kafka-failure-recovery`.
- Removing RabbitMQ.

## Decisions

### Use Apache Kafka in KRaft mode and franz-go

Local Compose will run a single Apache Kafka broker/controller in KRaft mode with a persistent volume and internal/external listeners. Production documentation will require multiple brokers, replication, and minimum in-sync replica settings; single-node values are development-only.

The Go adapter will use `github.com/twmb/franz-go` plus its admin package. It provides idempotent production, consumer groups, manual commit control, cooperative rebalancing, protocol coverage, and a pure-Go deployment without the historical API constraints of older clients.

Alternative: Sarama. Rejected because the new foundation benefits from franz-go's current consumer-group and idempotent-producer APIs.

Alternative: Redpanda. Rejected as the canonical runtime because the selected product is Kafka; protocol-compatible alternatives may still be used by operators later.

### Use a versioned JSON event envelope for the first platform version

Every retained event will contain a bounded envelope:

- `event_id`;
- `event_type`;
- `schema_version`;
- `occurred_at`;
- `produced_at`;
- `producer`;
- `correlation_id` when a safe server-derived value exists;
- typed `payload`.

The Kafka record key remains separate and is validated by the event-specific codec. Decoders reject unknown event type/version combinations, oversized records, invalid timestamps, missing IDs, and trailing or structurally invalid JSON. Payload structs remain owned by Application packages; infrastructure only wraps and transports them.

Alternative: Avro or Protobuf with a schema registry. Deferred because Frux currently has Go JSON event contracts, one deployable worker, and no external stream processors. Explicit versions, strict codecs, fixtures, and compatibility tests provide a smaller first step. A later change may add a registry without changing topic identities.

### Keep topic policy in a code-owned registry

The registry will define for every topic:

- canonical name and version;
- event or command classification;
- expected key kind;
- partition count for local provisioning;
- retention and cleanup policy;
- maximum record size;
- allowed producer;
- allowed consumer groups;
- whether replay and retry topics are permitted.

Topic names use `frux.<domain>.<event-or-command>.v1`. Names, group IDs, and metric labels are closed constants rather than arbitrary configuration. Broker addresses, authentication, TLS, timeouts, and environment prefix are configuration.

Local development may create missing topics from the registry. Production mode validates topics and fails startup on incompatible partition count, cleanup policy, or unsafe replication rather than mutating production topology automatically.

Alternative: rely on Kafka broker auto-creation. Rejected because accidental topics inherit unsafe defaults and miss reviewed retention and partitioning.

### Use idempotent acknowledged production without Kafka transactions

Producers use `acks=all`, idempotence, bounded request timeouts, bounded retries inside the client, and per-record result inspection. The Application caller receives success only after Kafka acknowledges the record.

Kafka transactions are not introduced in the foundation. Most Frux handlers also write PostgreSQL, Redis, or object storage, so Kafka-only exactly-once transactions would not make the end-to-end workflow exactly once and would add fencing and transactional-ID operations.

### Commit offsets only after the declared durable boundary

Consumers disable automatic commits. A handler receives the decoded envelope plus topic, partition, offset, timestamp, key, and headers. It returns a classified outcome only after its business transaction, durable outbox handoff, or durable job existence is established.

Successful and terminally handled records become commit-eligible. Retryable failures remain uncommitted until a later recovery policy safely moves them away from the source partition. Commit failures stop the consumer session and rely on application idempotency after reassignment.

The consumer runtime uses cooperative rebalancing, bounded poll batches, per-partition ordering, context cancellation, and explicit draining during shutdown. A blocked rebalance immediately cancels the in-flight batch and the configured rebalance timeout covers cancellation, durable completion, and offset commit. After the shutdown drain grace period it also cancels handler contexts, but neither path releases the partition or closes the consumer until in-flight handlers return, because Go cannot safely terminate a non-cooperative goroutine. Application handlers are therefore required to honor context cancellation and bound every external call. The runtime does not start unbounded goroutines per record.

Alternative: automatic periodic commits. Rejected because they can advance offsets before PostgreSQL or Redis work commits.

### Make transport migration primary/mirror and active/shadow

Each event path will expose registered migration states rather than generic booleans:

- producer: `rabbit`, `rabbit_with_kafka_mirror`, `kafka_with_rabbit_mirror`, `kafka`;
- consumer: `rabbit`, `kafka_shadow`, `kafka`, with only one active business writer.

Mirror publication failures are observable but do not redefine the primary path's user-facing success. Shadow consumers decode, validate keys/envelopes, measure delay and duplicates, and optionally compare durable facts; they never call mutating handlers or commit offsets under the future active group ID.

Alternative: run RabbitMQ and Kafka consumers against the same handler simultaneously. Rejected because idempotency limits damage but does not make duplicate cache writes, external calls, latency, or observability desirable.

### Expose Kafka-native operational signals

Metrics use closed topic and consumer-group labels and include:

- produce attempts, result, and duration;
- consumed records and handler outcomes;
- consumer-group lag and oldest observed delivery delay;
- commit failures and rebalances;
- decode/contract failures;
- broker and topic validation failures.

Event IDs, keys, partitions, offsets, users, videos, raw errors, and payload fields are not metric labels.

## Risks / Trade-offs

- [JSON contracts are weaker than registry-enforced schemas] -> Use strict codecs, explicit versions, compatibility fixtures, payload bounds, and reject unsupported versions.
- [Kafka becomes another required local service during migration] -> Keep RabbitMQ paths operational and provision a small single-node KRaft service in Compose.
- [Manual offset management can duplicate work after crashes] -> Preserve stable event IDs and require every active consumer to establish a durable idempotency boundary before commit.
- [A poor partition key cannot be changed without reordering] -> Require event-specific key tests and version the topic when key semantics change.
- [Mirror streams can contain gaps] -> Treat mirrors as migration diagnostics, not authoritative history, and start active Kafka consumption only at an explicit cutover boundary.
- [Automatic local topic creation can hide production mistakes] -> Limit creation to development; production performs validation only.

## Migration Plan

1. Add configuration, franz-go adapters, registry, codecs, metrics, and unit tests with no business publishers enabled.
2. Add Kafka to Compose and validate broker health, topic creation, production, consumption, cancellation, and restart behavior.
3. Add migration-mode plumbing to composition roots while leaving every registered stream in RabbitMQ mode.
4. Deploy and observe connection/topic validation without producing business traffic.
5. Let dependent changes introduce one stream at a time.

Rollback disables Kafka configuration and deploys the previous binary; no business state or topic data is required by this foundation alone.

## Open Questions

None. Event-specific topic names, keys, retention, and active consumer groups are owned by dependent changes.
