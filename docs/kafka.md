# Kafka Event Backbone

## Scope

Kafka is a retained event-stream foundation alongside RabbitMQ. This foundation owns configuration,
registries, strict JSON contracts, franz-go clients, explicit offset commits, migration controls,
metrics, diagnostics, and local KRaft provisioning.

It does **not** migrate a business event, remove AMQP, provide cross-system exactly-once semantics,
or add retry topics, DLQ inspection, replay, Kafka Connect, CDC, Flink, or a schema registry.

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
| `allow_local_provisioning` | Allowed only in `local`/`test`; production is validation-only. |
| `authentication.mechanism` | `none`, `plain`, `scram-sha-256`, or `scram-sha-512`. |
| `tls` | TLS 1.2+, optional CA and paired client certificate/key; insecure verification only in local/test. |
| `timeouts` | Bounded dial, request, produce, admin, and shutdown deadlines. |
| `consumer` | Bounded poll records/bytes, partition concurrency, and drain deadline. |
| `production_validation` | Required replication factor and minimum ISR; optional mandatory auth/TLS. |

Migration modes are closed:

- Producer: `rabbit`, `rabbit_with_kafka_mirror`, `kafka_with_rabbit_mirror`, `kafka`.
- Consumer: `rabbit`, `kafka_shadow`, `kafka`.

All checked-in business stream modes are `rabbit`. A shadow consumer has a distinct Group ID and
never invokes a mutating handler or commits the future active Group's offsets.

## Contracts and topology

Every record uses a registered Topic, Producer, Consumer Group, Event Type, Schema Version, and Key
Codec. The v1 JSON envelope contains event identity, event/schema versions, occurrence/production
timestamps, producer, optional correlation identity, and typed payload. Unknown fields, trailing
JSON, unknown versions/types, invalid keys/timestamps/IDs, and oversized records are terminal
contract failures.

Local startup creates missing topics with registered partitions, retention, cleanup policy, maximum
record size, replication, and minimum ISR. Production startup never creates or changes topics; it
fails on missing or incompatible topology.

## Production baseline

- At least 3 brokers across failure domains; replication factor >= 3 and minimum ISR >= 2.
- TLS 1.2+ and authenticated clients; mount secrets/keys instead of embedding them in YAML.
- Broker auto topic creation disabled.
- `acks=all`, idempotent producer permissions, and reviewed retention/cleanup/max-record settings.
- Controller listeners isolated from application networks.
- Alerts for broker health, topology failures, commit uncertainty, contract failures, lag, and delay.

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
