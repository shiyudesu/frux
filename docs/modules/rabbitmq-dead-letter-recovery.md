# RabbitMQ 死信恢复

## 1. 范围

Frux 使用 RabbitMQ 原生 Quorum Queue、Delivery Limit、DLX 和 DLQ 隔离毒消息。PostgreSQL
只保存不可变管理审计事实，不保存第二份消息 Payload，也不承担消息恢复队列。

受保护队列使用新名称：

```text
<legacy queue>.q2       # versioned quorum source
<legacy queue>.dlx.q2   # per-consumer dead-letter exchange
<legacy queue>.dlq.q2   # bounded quorum DLQ
```

旧 Classic Queue 不改变类型。`legacy`、`dual`、`new` 三种配置模式分别表示只消费旧队列、
新旧 Binding/Consumer 并行 Drain、只消费新队列并移除旧 Binding。`new` 拓扑声明不会先绑定
旧 Queue；所有 `.q2` Consumer（包括 `dual` 的第二 Consumer）都使用独立受监督 Channel，
基础设施失败或 Channel 关闭后按最大 30 秒的有界退避重建。

## 2. 消费者清单与失败分类

| Consumer | 业务去重依据 | Terminal | Retryable | DLX 保证 | 当前模式 |
| --- | --- | --- | --- | --- | --- |
| `action_changed` | `interaction_action_event.event_id` 和完整载荷冲突检查 | JSON/字段错误、事件冲突、视频缺失或删除 | PostgreSQL/连接等基础设施失败 | at-least-once | `dual` 试点 |
| `video_published` | 视频 ID、发布时间和缓存集合幂等写 | JSON/必填字段错误 | Repository、Redis、缓存写失败 | at-least-once | `legacy` |
| `video_embedding` | 每视频 embedding upsert | JSON/必填字段错误 | embedding 生成或存储失败 | at-most-once DLX | `legacy` |
| `view_event_recorded` | `(user_id, event_id)` 原始事实和画像 source event ID | JSON/身份/视频/事件类型错误 | PostgreSQL、画像和归因存储失败 | at-least-once | `legacy` |
| `media_processing` | `(asset_id, profile_version)` 数据库任务、租约和状态机 | JSON/必填字段错误；任务内部 terminal 状态 | MQ handler 基础设施失败；处理重试由 PostgreSQL 任务拥有 | at-most-once DLX，数据库轮询兜底 | `legacy` |

所有重投递和 Operator Replay 保持原始 RabbitMQ `message_id`/业务 `event_id`。Replay 只增加
`x-frux-replay-id` 和 `x-frux-original-event-id`，不修改 JSON 业务 Payload。

## 3. Operator API

所有接口要求当前持久化账号拥有 `governance.execute`：

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| GET | `/api/admin/dead-letter-queues` | 返回 allowlist 中 DLQ 的深度、ready、unacked、consumer 和状态 |
| GET | `/api/admin/dead-letter-queues/{queue}/messages?limit=20` | 使用 Management API `ack_requeue_true` 返回有界、脱敏的队头诊断 |
| POST | `/api/admin/dead-letter-messages/{messageId}/replay` | 重放指定 DLQ 队头消息 |

Preview 不返回 Payload，只返回 Payload 字节数、SHA-256、JSON 是否有效、最多 20 个顶层字段名、
原事件 ID、Replay ID、路由和 death count。RabbitMQ 凭据只存在于服务端配置。

Replay 请求体：

```json
{
  "queue": "frux.interaction.action_changed.dlq.q2",
  "reason": "operator_retry"
}
```

`reason` 是 1–64 位小写 reason code。服务端验证 Queue allowlist、消息必须位于队头、原 Exchange
和 Routing Key 必须同时匹配 `x-first-death-*` 与 `x-death.routing-keys` 中的代码配置，
缺少来源证明的直接 DLQ 投递拒绝重放；只复制有界安全 Header。成功 Audit Fact 必须在发布前
可完整构造，发布使用 mandatory routing 和 Publisher Confirm，Confirm 后才写 Audit 并 Ack。
不能安全放入有界审计字段的合法 Event ID 使用稳定 `sha256:<hex>` 引用，业务 Payload/Header
仍保留原 ID。超时、Nack、路由失败或审计失败均重新入队。

## 4. 迁移、Drain 与回滚

试点选择 `action_changed`，因为其 PostgreSQL receipt 对 Event ID 和 Payload 都有强幂等约束。

1. `legacy`：部署 `.q2/.dlq.q2` 拓扑但不绑定新 Source。
2. `dual`：同时绑定旧 Queue 和 `.q2`，Worker 同时消费；确认相同 Event ID 的重复消息不重复业务事实。
3. Drain 条件：旧 Queue `messages_ready=0`、`messages_unacknowledged=0` 持续 15 分钟；新 Queue
   无异常 retry exhaustion，DLQ backlog 可解释且 Publisher Confirm 错误为零。
4. `new`：移除旧 Binding，只消费 `.q2`。旧 Queue 保留至少一个发布观察窗口，不立即删除。
5. 逐个按表中顺序迁移其余 Consumer；每次只改变一个 `*_mode`。

回滚时把目标 Consumer 改回 `dual`，确认旧 Consumer 正常后再改为 `legacy`。不要删除新 DLQ；
保留现场供检查。由于 Event ID 不变，回滚窗口内的重复投递仍由业务幂等层吸收。

## 5. 指标和告警

- `frux_mq_retries_total{consumer}`
- `frux_mq_retry_exhausted_total{consumer}`
- `frux_mq_terminal_total{consumer}`
- `frux_mq_dead_letter_depth{consumer}`
- `frux_mq_routing_failures_total{consumer}`
- `frux_mq_replay_total{result}`

Prometheus 规则位于 `apps/monitoring/alerts/rabbitmq_dead_letter.yml`，Grafana 看板为
`Frux RabbitMQ Dead-letter Recovery`。标签只包含封闭 Consumer/Result，不包含 Event ID、
Queue Payload、Operator 或 Reason。
