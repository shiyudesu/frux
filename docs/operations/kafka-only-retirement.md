# Kafka-only retirement runbook

## Observation gate

RabbitMQ retirement is allowed only after every registered Kafka producer and active consumer group has
remained healthy for a continuous observation window:

| Environment | Minimum window | Required traffic |
| --- | ---: | --- |
| Local/development | 60 minutes | At least one accepted action, view event, video publication, Feed intake, hash-embedding intake, and media job |
| Staging/production | 24 hours | Normal traffic plus one lost-wakeup media-job exercise |

The entire window must satisfy these thresholds:

| Signal | Threshold |
| --- | --- |
| `frux_kafka_broker_healthy` | `1` |
| `frux_kafka_produce_total{result=~"failed|uncertain"}` | No increase |
| `frux_kafka_consumer_session_healthy{stage="source"}` | `1` for every active group |
| `frux_kafka_consumer_lag` | At most 100 records per group/topic |
| `frux_kafka_consumer_workflow_lag` | At most 500 records per group |
| `frux_kafka_recovery_publish_total{result=~"failed|uncertain"}` | No increase |
| `frux_kafka_recovery_retained_offset_growth` | `0` after any deliberate recovery exercise settles |
| `frux_kafka_recovery_oldest_record_age_seconds` | Less than 1800 seconds |
| `frux_behavior_action_fallback_total{result=~"failure|invalid"}` | No increase |
| Behavior duplicate ratio | Below 1% outside deliberate replay tests |
| `frux_view_event_outbox_lag_seconds` and `frux_video_publication_outbox_lag_seconds` | At most 60 seconds |
| `frux_media_video_lifecycle_backlog_oldest_seconds` | At most 300 seconds |

## Operator checklist

- [ ] Kafka is enabled and the registry provisions every business, retry, quarantine, and DLQ topic.
- [ ] The active groups `persist_action_active`, `consume_view_active`,
  `feed_video_published_active`, `embedding_video_published_active`, and
  `media_processing_active` are assigned and healthy.
- [ ] Action and view publication metrics contain only `transport="kafka"` and `role="primary"`.
- [ ] Video publication and media wakeup metrics contain only `transport="kafka"` and
  `role="primary"`.
- [ ] No legacy business consumer process is running.
- [ ] Every legacy source queue reports zero ready and zero unacknowledged records.
- [ ] Every allowlisted legacy DLQ record has a replay, waiver, or export decision linked to an
  immutable admin audit fact or incident record.
- [ ] A media processing job completes while its Kafka wakeup is deliberately withheld, proving
  PostgreSQL polling owns recovery.
- [ ] Kafka topic/partition/offset inspection and non-destructive replay are available to operators.

## Legacy drain evidence

Before deploying the Kafka-only release, query every source and DLQ in the previous environment and
attach the exported result to the deployment record. The required inventory is:

| Responsibility | Source queues to inspect | DLQ disposition |
| --- | --- | --- |
| Action persistence | Legacy and quorum action-changed queues | Replay, waive, or export each record |
| View feedback | Legacy and quorum view-event queues | Replay, waive, or export each record |
| Feed fanout | Legacy and quorum video-published Feed queues | Replay, waive, or export each record |
| Hash embedding intake | Legacy and quorum video-embedding queues | Replay, waive, or export each record |
| Media wakeup | Legacy and quorum media-processing queues | Waive only after the PostgreSQL job exists |

Frux is still in development, so the repository retirement uses a no-production-data disposition:
legacy broker volumes may be discarded only after the source queue report is empty. This does not
waive the empty-source requirement for staging or production.

Local retirement evidence recorded on 2026-08-11:

- the preserved `frux-rabbitmq` container was started against its existing development volume;
- `rabbitmqctl list_queues name messages_ready messages_unacknowledged --formatter json` returned `[]`;
- therefore no source or DLQ record required replay, waiver, or export;
- Git commit `fb29a27` and its previous Compose/config files were readable through `git archive`;
- the preserved rollback Compose manifest rendered successfully with explicit rollback inputs;
- the final Compose service list contains Kafka and no RabbitMQ service.
- the Kafka-only Compose stack reached API healthy state, Prometheus scraped Worker metrics, all five
  active business Groups reported `Stable`, and media lifecycle backlog/oldest age were zero.
- an isolated rollback drill compiled commit `fb29a27`, packaged the previous API and Worker, started
  them with the preserved broker manifest against the dependency network, and confirmed API health,
  Worker metrics, and a healthy broker.

## Supported recovery API

The queue-head preview and destructive acknowledgement API is retired. Operators use:

- `GET /api/admin/kafka-dead-letters`
- `GET /api/admin/kafka-dead-letters/:topic/records`
- `POST /api/admin/kafka-dead-letters/:topic/records/:partition/:offset/replay`

Replay is non-destructive, coordinates are exact, and historical pre-retirement admin audit facts
remain queryable through `GET /api/admin/audit-events`.

## Rollback window

The bounded rollback window is seven days after deployment. The previous source artifact is Git
commit `fb29a27`, and the preserved deployment manifest is under
`ops/rollback/rabbitmq-retirement/`.

Rollback requires deploying the previous API and Worker artifact together with the preserved broker
manifest and previous configuration. There is no runtime broker toggle in the Kafka-only release.
Before switching the previous release to RabbitMQ, operators SHALL freeze action, view, publication,
and media-command ingress; wait for every source and retry Kafka Group to reach zero lag; verify
action/view/publication outboxes have no pending rows; and record the drain evidence. If Kafka lag
cannot be proven zero, deploy the previous release with Kafka-active consumers and do not switch to
RabbitMQ.
After the seven-day window and a clean observation period, external RabbitMQ infrastructure and its
volume may be deleted.
