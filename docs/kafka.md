# Kafka event backbone

## Scope

Kafka is Frux's only message broker. It owns retained domain and behavior events plus short-lived
wakeup commands. PostgreSQL remains the source of truth for business facts, outboxes, long-running
jobs, leases, delayed retries, and reconciliation.

The Kafka infrastructure owns:

- closed Topic, Producer, Consumer Group, key, retention, and recovery registries;
- strict versioned JSON envelopes and typed payload contracts;
- idempotent `acks=all` production with bounded deadlines;
- explicit offset commits after durable success or acknowledged recovery routing;
- fixed retry tiers, immutable Consumer Group-specific DLQs, inspection, and audited replay;
- metrics, diagnostics, and local KRaft provisioning.

## Configuration

Both host and Compose configurations enable Kafka. Key fields:

| Field | Rule |
| --- | --- |
| `enabled` | Must be `true` for the supported API and Worker runtime. |
| `environment` | `local`, `test`, `staging`, or `production`. |
| `brokers` | 1-16 explicit `host:port` seeds; no URL scheme or embedded credentials. |
| `client_id`, `topic_prefix` | Bounded names; the prefix isolates integration environments. |
| `allow_local_provisioning` | Allowed only in `local`/`test`; production is validation-only. |
| `authentication.mechanism` | `none`, `plain`, `scram-sha-256`, or `scram-sha-512`. |
| `tls` | TLS 1.2+, optional CA and paired client certificate/key. |
| `timeouts` | Bounded dial, request, produce, admin, and shutdown deadlines. |
| `consumer` | Bounded poll records/bytes, assignment timeout, partition concurrency, and drain grace. |
| `production_validation` | Required replication factor, minimum ISR, authentication, and TLS policy. |

There are no broker migration modes, mirror producers, shadow consumers, or runtime rollback toggles.

## Registered business contracts

| Topic | Semantics | Key | Producer | Active groups | Retention |
| --- | --- | --- | --- | --- | --- |
| `frux.interaction.action-changed.v1` | Accepted action state event | `action:{user}:{video}:{LIKE\|FAVORITE}` | `interaction_api` | `frux.interaction.persist-action.v1` | 7 days |
| `frux.exposure.view-event-recorded.v1` | Committed viewing feedback | `user:{user}` | `exposure_worker` | `frux.recommendation.consume-view.v1` | 7 days |
| `frux.video.published.v1` | Retained first-publication fact | `video:{video_id}` | `video_worker` | Feed and hash-embedding groups | 30 days |
| `frux.media.processing-requested.v1` | Non-authoritative media wakeup | `asset:{asset_id}` | `media_api` | media-processing group | 6 hours |

Feed and hash embedding use independent groups on the retained publication topic. Media wakeup only
signals bounded scheduling; `media_processing_job` polling and reconciliation remain authoritative.

## Consumer offsets

Every Worker source and retry consumer receives the PostgreSQL-backed durable offset-initialization
store before joining its group. Existing committed offsets are preserved and recorded. A genuinely
new group starts at the retained beginning. Once the durable marker is complete, a dead group,
missing/expired offset, or out-of-range offset is treated as explicit data loss rather than silently
reset.

The marker is keyed by environment, prefix, resolved group, topic, and marker version. Initialization
uses an advisory lock, persists the plan before Kafka commits, checks each partition response, resumes
partial commits, and extends only newly added trailing partitions.

## Failure recovery

The registry assigns:

- `block-and-retry` to action, view, and backbone-probe groups;
- fixed retry Topics to Feed and hash-embedding groups;
- `durable-job` to media wakeup.

Retry Topics use 5s, 30s, 2m, 10m, and 30m delays followed by a 30-day DLQ. A source offset advances
only after durable handler success, registered terminal handling, or acknowledged next-hop
publication. Recovery metadata preserves the original key/value and source coordinates without
modifying the business payload.

Operators use:

- `GET /api/admin/kafka-dead-letters`
- `GET /api/admin/kafka-dead-letters/:topic/records`
- `POST /api/admin/kafka-dead-letters/:topic/records/:partition/:offset/replay`

Replay is non-destructive. The ledger persists a pending claim before publication, uses a stable
Replay ID, stores only the idempotency-key SHA-256, and reconciles uncertain publication through Kafka
evidence before finalizing.

Detailed incident and retention procedures are in
[Kafka failure recovery](modules/kafka-failure-recovery.md).

## Contracts and topology

Every record uses a registered Topic, Producer, Consumer Group, Event Type, Schema Version, and key
codec. Unknown fields, trailing JSON, unknown versions/types, invalid canonical keys, invalid
timestamps/IDs, and oversized records are terminal contract failures.

Local startup creates missing registered topics with the configured partitions, retention, cleanup,
`LogAppendTime`, maximum record size, replication, and minimum ISR. Production startup validates
topology and never silently changes it.

## Production baseline

- At least three brokers across failure domains, replication factor at least 3, and minimum ISR at least 2.
- TLS 1.2+ and authenticated clients with secrets mounted outside YAML.
- Broker auto topic creation disabled.
- Exact registered partition counts for versioned keyed topics.
- `acks=all`, idempotent producer permissions, and reviewed retention/max-record settings.
- Automatic offset reset disabled after durable initialization.
- Rebalance timeout covers handler cancellation and offset commit.
- Alerts cover broker health, topology failures, commit uncertainty, contract failures, lag, retry/DLQ growth, and retention risk.

## Validation

```bash
cd apps/api
go test ./internal/infra/config ./internal/infra/kafka ./internal/infra/metrics
go test ./...
go build ./cmd/feed ./cmd/worker

cd ../
FRUX_INTERNAL_TOKEN='replace-with-a-strong-test-token-123A!' docker compose config
```

The opt-in live test requires a running broker:

```bash
cd apps/api
FRUX_KAFKA_TEST_BROKERS=127.0.0.1:29092 \
  go test ./internal/infra/kafka -run '^TestKafkaBackboneProvisionsProducesAndConsumesAfterClientRestart$'
```
