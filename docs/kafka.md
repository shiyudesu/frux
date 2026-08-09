# Kafka Event Backbone

## Scope

Kafka is a retained event-stream foundation alongside RabbitMQ. This foundation owns configuration,
registries, strict JSON contracts, franz-go clients, explicit offset commits, migration controls,
metrics, diagnostics, and local KRaft provisioning.

The first business streams are `action_changed` and `view_event_recorded`. RabbitMQ remains available
as primary/mirror and rollback transport. This foundation does **not** remove AMQP, provide
cross-system exactly-once semantics, or add retry topics, DLQ inspection, replay, Kafka Connect, CDC,
Flink, or a schema registry.

## Configuration

`configs/config.yaml` keeps Kafka disabled for host development. Compose mounts
`config.docker.yaml`, enables Kafka, connects to `kafka:9092`, and provisions missing registered
topics because `environment=local`.

Key fields:

| Field | Rule |
| --- | --- |
| `enabled` | Disabled permits only RabbitMQ producer/consumer migration modes. |
| `environment` | `local`, `test`, `staging`, or `production`. |
| `brokers` | 1–16 explicit `host:port` seeds; no URL scheme or credentials. |
| `client_id`, `topic_prefix` | Bounded names; prefix isolates integration environments. |
| `shadow_deployment` | Bounded deployment suffix used in the exact registered shadow Group name. |
| `allow_local_provisioning` | Allowed only in `local`/`test`; production is validation-only. |
| `authentication.mechanism` | `none`, `plain`, `scram-sha-256`, or `scram-sha-512`. |
| `tls` | TLS 1.2+, optional CA and paired client certificate/key; insecure verification only in local/test. |
| `timeouts` | Bounded dial, request, produce, admin, and shutdown deadlines. |
| `consumer` | Bounded poll records/bytes, partition concurrency, and drain grace before handler cancellation. Handlers must honor context cancellation and bound external calls. |
| `production_validation` | Required replication factor and minimum ISR; optional mandatory auth/TLS. |

Migration modes are closed:

- Producer: `rabbit`, `rabbit_with_kafka_mirror`, `kafka_with_rabbit_mirror`, `kafka`.
- Consumer: `rabbit`, `kafka_shadow`, `kafka`.

Checked-in modes remain `rabbit`; action and view may independently select the four producer modes
and three consumer modes. A single-transport mode succeeds after that transport acknowledges. Either
dual transition mode succeeds only after both RabbitMQ and Kafka acknowledge; a partial or uncertain
result enters the action PostgreSQL fallback/conditional rollback path or leaves the view outbox
pending. Stable event IDs absorb duplicates from retrying the transport that already acknowledged.
The configured primary must still match the active mutating consumer.

An active Kafka consumer requires an RFC3339 `cutover_boundary`. Before the group starts, the worker
uses kadm to resolve the broker append timestamp to every partition offset and commits the boundary
while the group is inactive. Registered retained event topics require
`message.timestamp.type=LogAppendTime`; envelope `produced_at` is not used for offset resolution.
The boundary must be millisecond-aligned and no later than worker startup time.
Existing group commits are preserved on restart; the boundary is not reapplied. The
explicit `Backbone.ApplyConsumerCutover(..., CutoverForceReset)` operation may reset only an inactive group.
The action boundary must be strictly later than the view boundary, and each worker initializes/starts
the view active/shadow group before the action active/shadow group. Rollback reverses that dependency. A shadow
consumer uses `<active-group>.shadow.<deployment>`, never invokes a mutating handler, and never
commits the future active Group's offsets.

Registered behavior contracts:

| Topic | Key | Producer | Active group |
| --- | --- | --- | --- |
| `frux.interaction.action-changed.v1` | `action:{user}:{video}:{LIKE\|FAVORITE}` | `interaction_api` | `frux.interaction.persist-action.v1` |
| `frux.exposure.view-event-recorded.v1` | `user:{user}` | `exposure_worker` | `frux.recommendation.consume-view.v1` |

Both retain immutable records for seven days with delete cleanup and broker-assigned append timestamps.

## Contracts and topology

Every record uses a registered Topic, Producer, Consumer Group, Event Type, Schema Version, and Key
Codec. The v1 JSON envelope contains event identity, event/schema versions, occurrence/production
timestamps, producer, optional correlation identity, and typed payload. Unknown fields, trailing
JSON, unknown versions/types, invalid keys/timestamps/IDs, and oversized records are terminal
contract failures. Action and user keys must also equal their decode/re-encode canonical bytes;
leading-zero, signed, and action-case aliases are rejected.

Local startup creates missing topics with registered partitions, retention, cleanup policy,
`message.timestamp.type=LogAppendTime`, maximum record size, replication, and minimum ISR.
Production startup never creates or changes topics; it fails on missing or incompatible topology,
including `CreateTime`.

## Production baseline

- At least 3 brokers across failure domains; replication factor >= 3 and minimum ISR >= 2.
- TLS 1.2+ and authenticated clients; mount secrets/keys instead of embedding them in YAML.
- Broker auto topic creation disabled.
- Versioned topics require the exact registered partition count; partition-count changes require a new topic version to preserve keyed ordering.
- `acks=all`, idempotent producer permissions, and reviewed retention/cleanup/append-time/max-record settings.
- In-flight produce cancellation is enabled to enforce deadlines; every error after submission is classified as uncertain and must be handled through stable event IDs and application fallback.
- Controller listeners isolated from application networks.
- Alerts for broker health, topology failures, commit uncertainty, contract failures, lag, and delay.
- Rebalance timeout covers handler cancellation and offset commit; a blocked rebalance cancels the current batch before partition ownership is released.
- franz-go data-loss notifications are recorded as bounded metrics and consumption continues from the client's recovered cursor instead of restarting the group.
- Worker processes continuously probe Kafka health so broker failure and recovery update the exported health gauge after startup.
- Consumer supervisors export bounded session lifecycle counters and a per-registered-group health
  gauge. Transient broker/session failures restart with bounded backoff; authentication,
  configuration, and handler-contract failures stop a required active consumer and fail the worker
  runtime visibly.
- Shadow parity reports a missing durable fact as pending, retries it three times with a delayed
  supervisor restart, and records pending exhaustion separately from a true conflicting-fact mismatch.

## Validation

```bash
cd apps/api
go test ./internal/infra/config ./internal/infra/kafka ./internal/infra/metrics
go test ./...
go build ./cmd/feed ./cmd/worker

cd ../
FRUX_INTERNAL_TOKEN='replace-with-a-strong-test-token-123A!' docker compose config
```

The opt-in integration test requires a running broker:

```bash
cd apps/api
FRUX_KAFKA_TEST_BROKERS=127.0.0.1:29092 \
  go test ./internal/infra/kafka -run '^TestKafkaBackboneProvisionsProducesAndConsumesAfterClientRestart$'
```
