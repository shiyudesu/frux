# Kafka Event Backbone

## Scope

Kafka is a retained event-stream foundation alongside RabbitMQ. This foundation owns configuration,
registries, strict JSON contracts, franz-go clients, explicit offset commits, migration controls,
metrics, diagnostics, and local KRaft provisioning.

Business streams include behavior events, the retained video-publication fact, and media-processing
wakeup commands. RabbitMQ remains available as primary/mirror and rollback transport. Kafka now has
a separate native failure-recovery surface with registered retry tiers, immutable consumer-specific
DLQs, offset inspection, and audited non-destructive single-record replay. It does **not** remove
AMQP, provide cross-system exactly-once semantics, arbitrary Kafka browsing, Kafka Connect, CDC,
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

Checked-in modes remain `rabbit`; action, view, video publication, Feed, embedding, and media wakeup
responsibilities select registered modes independently. Feed and embedding share the retained
publication producer but own different active/shadow groups. A single-transport mode succeeds after that transport acknowledges. Either
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

Registered video workflow contracts:

| Topic | Semantics | Key | Producer | Groups | Retention |
| --- | --- | --- | --- | --- | --- |
| `frux.video.published.v1` | Retained first-publication fact | `video:{video_id}` | `video_worker` | `frux.feed.video-published.v1`, `frux.embedding.video-published.v1`, and deployment-specific shadows | 30 days |
| `frux.media.processing-requested.v1` | Non-authoritative wakeup command | `asset:{asset_id}` | `media_api` | `frux.media.processing-requested.v1` and shadow | 6 hours |

The video transaction/durable publication boundary writes
`video_publication_event_outbox`; its dispatcher waits for the selected transport acknowledgement.
Feed commits after idempotent preheat/index work. Embedding commits after conditional
`hash-ngram-v1` persistence. Media wakeup consumers only validate the durable
`media_processing_job`, signal bounded local scheduling, and commit without waiting for ffmpeg.
PostgreSQL polling and reconciliation remain the media correctness path.

## Failure recovery

Only registered event-delivery consumers may use Kafka retry Topics. Feed and embedding each own
fixed 5s, 30s, 2m, 10m, and 30m tiers plus a 30-day DLQ. A Consumer acknowledges the current Record
only after durable handler success or acknowledged publication to its registered next hop. Moving a
Record to retry breaks source Partition ordering; stable Event IDs and business versions make
duplicates and late delivery safe.

The producer uses franz-go `ProducerBatchMaxBytesFn` to apply each resolved registered Topic's broker
limit; unknown Topics receive the smallest registered conservative bound. It also checks key, value,
and headers against the exact destination `MaxRecordBytes` before every publish.
One shared calculation adds 64 KiB of broker batch/protocol headroom to an application Topic limit.
Recovery Topics reserve the full source broker allowance plus bounded recovery headers, then add the
same broker headroom to their own `max.message.bytes`. Recovery publication independently rejects
source key/value bytes above the source broker maximum. Thus an application-oversized poison record
that the source broker accepted can reach DLQ unchanged, while a record above that broker limit cannot
borrow unused header capacity. The calculation applies equally to video and smaller registered Topics.

A brand-new retry consumer group is initialized before joining. A PostgreSQL advisory lock protects a
non-expiring marker keyed by environment, prefix, resolved group, versioned Topic, and marker version.
The inactive-group plan is durable before Kafka commits; partitions commit in deterministic order,
per-partition responses are checked, and acknowledged partitions are persisted so partial initialization
resumes only missing work. A fresh Kafka snapshot completes the marker. Retry consumers then use
committed-only `NoResetOffset`. Once complete, a dead group or missing/deleted/expired/out-of-range
established offset fails visibly as data loss and is never treated as a new group. Only new trailing
partitions extend the marker.

Media processing and future semantic long-running work remain PostgreSQL jobs. Kafka may wake a
durable job, but failures after handoff use job lease/retry/reconciliation rather than Kafka retry
Topics.

A handler dependency deadline is a retryable failure while the Consumer context remains active; only
the Consumer context's own cancellation bypasses recovery routing. Invalid or obsolete retry metadata
is quarantined to the owning DLQ with unchanged key/value, consumed retry coordinates, bounded hashes,
`failure_class=recovery_metadata_invalid`, and `non_replayable=true`. The retry offset advances only
after that quarantine publication is acknowledged, and operator replay rejects the record.

Operators use `/api/admin/kafka-dead-letters` and exact Topic/Partition/Offset reads. Replay keeps
the DLQ Record retained, validates registry provenance, event contract and payload SHA-256, then
commits a pending claim and republishes unchanged key/value outside that transaction with a new
Replay ID to the owning group's first retry tier. The replay ledger stores only the idempotency-key
SHA-256 fingerprint; possibly acknowledged publications and acknowledged publications whose
finalize/audit transaction fails remain pending/unknown. Repeating the identical authorized request
verifies the stable Replay ID only in the registry destination's retained window, finalizes success
plus audit when found, or records a bounded absence failure only after the producer uncertainty window
and repeated complete scans observe stable retained bounds. Any growth restarts scanning and settlement.
Malformed, expired, canceled, unstable, incomplete, or unavailable evidence remains pending; reconciliation never republishes the
claim. Detailed topology sizing, incident and expiry procedures are in
[Kafka failure recovery](modules/kafka-failure-recovery.md).

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
- Consumers disable automatic offset reset; retention loss and out-of-range committed offsets are fatal rather than silently skipping behavior facts.
- Rebalance timeout covers handler cancellation and offset commit; a blocked rebalance cancels the current batch before partition ownership is released.
- franz-go data-loss notifications are recorded as bounded metrics and stop the active consumer before any accompanying records are processed or committed.
- Worker processes continuously probe Kafka health so broker failure and recovery update the exported health gauge after startup.
- Consumer supervisors export bounded stage (`source` or registered retry tier) lifecycle, lag, and
  health series plus owning-workflow lag/health aggregation. An idle retry tier cannot overwrite source
  lag or source health, and one failed tier does not affect unrelated groups. Transient broker/session
  failures restart with bounded backoff; authentication,
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

## Video workflow correctness refinements

每个首次公开 review/media/visibility/admin/restore edge 在状态事务内同时写 notification 与
immutable publication fact、operational publication outbox。媒体-backed outbox 可先 blocked，
公共交付完成后解除 dispatch readiness；notification ready/delivered 从不作为 handoff 证明。
Dispatcher 异步启动并以 5×100/10 秒为单次上限，broker outage 不影响其他 worker。30 天 replay
window 后只清理已有 fact 的 dispatched outbox；Reconciliation 按 fact 缺失修复，因此清理不会
重新发出事件，并继续排除 private/deleted 和无 lifecycle 追踪的历史视频。

Feed fanout、hash embedding intake 和 media wakeup shadow 使用非变更 parity reader，传播缺失最多
三次有界内联重试；配置 shadow/active Kafka gate 时 nil parity 使 Worker 启动失败。
