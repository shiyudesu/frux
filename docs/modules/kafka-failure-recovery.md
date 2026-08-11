# Kafka 故障恢复模块

## 1. 模块职责

本模块只处理 Kafka **事件投递失败**：Consumer 先做有界本地重试，需要解除源 Partition
阻塞时才进入代码注册的固定延迟 Topic，最终进入 Consumer Group 专属、不可变的 DLQ。
RabbitMQ `/api/admin/dead-letter-*` 接口在迁移期继续保留；Kafka 使用独立 Topic/Partition/Offset
接口和 DTO，不模拟 Queue head、Ack 或 destructive replay。

媒体处理和未来语义计算等长任务仍由 PostgreSQL job、lease、heartbeat、retry 和
reconciliation 恢复。Kafka retry Topic 不承载已经完成数据库 durable handoff 后的任务执行失败。

## 2. 注册拓扑与容量

当前 Feed 与 embedding 的 `video.published` Active Group 各自注册 5s、30s、2m、10m、30m
固定延迟层和一个 30 天 DLQ。每组因此增加 6 个 Topic；当前合计 12 个 recovery Topic。
Retry Topic 保留 7 天，DLQ 保留 30 天，均使用 delete cleanup、12 Partition、
`LogAppendTime`。统一容量计算在 Source 应用 Record 上限上增加 64 KiB，得到 broker
`max.message.bytes`；video 因此为 256 KiB + 64 KiB。Recovery Topic 应用上限再容纳完整 source
broker allowance 与 8 KiB 有界 recovery/quarantine Header，并为自己的 broker batch 再增加相同
64 KiB。franz-go 按解析后的注册 Topic 设置各自 batch 上限，未知 Topic 使用最小注册上限作为保守值；
每次 publish 同时校验 source 原 key/value 不超过 source broker 上限、Header 有界、总 Record 不超过目标 Topic。这样 source broker
已接受的应用超限 poison Record 可保持原 bytes 进入 DLQ，超过 source broker 上限的 Record 仍被拒绝；
同一公式也覆盖 32 KiB 等更小 Topic。

容量按峰值失败写入率估算：

```text
bytes = peak_failed_records_per_second
      × average_record_bytes
      × retention_seconds
      × replication_factor
      × safety_factor
```

安全系数至少 1.5，并为 Segment、索引和重放增长预留空间。生产 Topic 由平台按注册表预建；
API/Worker 不接受请求体提供任意 Topic、Group、tier、retention 或 replay destination。

## 3. 投递与顺序

- Source Handler 成功或 durable-job handoff 后提交 Offset。
- Retry/DLQ policy 只有在下一跳得到 `acks=all` acknowledgement 后，当前 Offset 才可提交。
- 进程可能在 acknowledgement 后、Offset commit 前退出，因此 retry Record 可能重复；
  原 Event ID 和业务幂等边界必须吸收重复。
- Record 移入 retry Topic 后不再保证相对源 Partition 的全局顺序。消费者必须用业务版本、
  Event ID 或 receipt 拒绝陈旧副作用。
- Retry Consumer 用异步调度的 Partition pause/resume 等待 `not_before`；只暂停延迟 Partition，
  其他 assigned Partition 继续 poll/process。buffered Record 绑定 assignment generation；ready 后从
  移除、处理到 Offset commit 全程持有 ownership lease。revoke/lost 先取得所有权时会丢弃旧 Record，
  handling 先取得 lease 时 revoke 等待其提交或中止；旧 owner 绝不在 revoke 完成后提交，新 owner 从
  Kafka refetch。
- Retry Consumer 不使用永久 `AtStart`。Brand-new Group 在 join 前按 environment/prefix/resolved
  Group/versioned Topic 取得 PostgreSQL advisory lock，先写入非过期 marker 与 per-Partition plan，再按
  Partition 顺序提交 retained start、检查每个 Kafka 响应并记录成功项。partial failure 只补缺失项；
  fresh Kafka snapshot 完整后 marker 才 complete。重启保留 Commit；Partition 增加时只扩展 trailing
  新 Partition。Complete marker 对应的 Group dead、interior missing、删除/过期 Commit，或 Commit
  小于 retained start/大于 end 时，Consumer 以 data-loss/offset fatal 退出，不当作新 Group、不静默回绕。
- `terminal_contract` 若失败原因正是 source key malformed，只允许 direct-DLQ codec 跳过该 key-kind
  复验，同时继续校验注册 source/DLQ/Group、header bounds、payload hash 和可提取 Event/schema；
  key/value 字节不变。Retry tier 和 replay 不得使用该例外。
- Handler 子依赖返回 `context.DeadlineExceeded` 时，只要 Consumer context 仍有效，就继续执行有界
  retry，并在 exhausted 后进入注册 retry/DLQ；只有 Consumer context 自身已取消才按 lifecycle
  shutdown/rebalance 退出且不路由。
- Retry Topic 的 metadata 缺失、过期或与当前 tier 不一致时，不重启热循环。Consumer 清除原 Header，
  将原 key/value 发布到 owning DLQ，并写入有界 quarantine metadata：消费 Topic/Partition/Offset、
  owning Group、key/payload SHA-256、`failure_class=recovery_metadata_invalid`、
  `non_replayable=true`。只有 DLQ acknowledgement 后才提交 retry Offset；发布失败不提交。

## 4. 管理接口

全部接口要求 `governance.execute`，无权限尝试写入有界 denied audit：

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| GET | `/api/admin/kafka-dead-letters` | allowlist DLQ、retained range、growth、ingress、oldest age 摘要 |
| GET | `/api/admin/kafka-dead-letters/{topic}/records?partition=&offset=&limit=` | 独立 non-group 精确读取和脱敏诊断 |
| POST | `/api/admin/kafka-dead-letters/{topic}/records/{partition}/{offset}/replay` | 单 Record、非破坏性、审计重放 |

Inspect 只返回坐标、注册 provenance、Event/Replay ID、失败分类、attempt、字节数、SHA-256、
JSON 有效性和有界顶层字段名；quarantine Record 还显示 consumed coordinate、metadata code 和
`replayable=false`。不返回 key、value、任意 Header、Broker 地址或原始错误。

Replay Body 只有注册 reason：

```json
{"reason":"operator_retry"}
```

必须提交最长 128 字符的 `Idempotency-Key`。数据库只保存 SHA-256 fingerprint。
相同 actor + key + 规范化 topic/partition/offset/reason 返回原结果；同 key 异载荷返回 409；
使用新 key 可执行后续有意重放。若已有 pending/unknown claim，同 key 不会再次发布，而会自动检查
注册 replay destination 的稳定 Replay ID evidence；同坐标的新请求在 reconciliation 完成前仍被阻止。

## 5. Replay 原子边界

`kafka_failure_replay_attempt` 保存 DLQ/source 坐标、Group、actor、Replay ID、reason、
pending/succeeded/failed、封闭 failure code 和时间。Repository 先按 actor/key、再按 DLQ
坐标取得 PostgreSQL session advisory lock，并在 Kafka publish 全程持有，避免并发重复生产。

Service 精确读取仍在 retention 内的 Record，验证：

1. DLQ Topic 和 owning Group 来自 recovery registry；
2. source Topic、Partition、Offset 与注册 route 一致；
3. Event envelope、schema、key 和 payload contract 有效；
4. Event ID、schema version、attempt、tier、payload SHA-256 和 replay metadata 一致。

`recovery_metadata_invalid` quarantine 明确不可 replay，Service 在 pending claim 和 publish 前拒绝。

验证通过后先提交 durable pending claim，再保持 key/value 字节不变、增加新 Replay ID，在事务外
发布到 owning Group 第一 retry 层并等待 acknowledgement；Feed replay 不进入 embedding 可见的共享
source，反之亦然。DLQ Record 不删除。Acknowledgement 后，成功 replay row 与
`kafka_dead_letter.replay` immutable audit 在第二个 PostgreSQL 事务中提交。若 producer 返回
`ErrProduceUncertain`/`MayHaveAcknowledged`，或 finalize/audit 失败，pending/unknown claim 保留。
同 actor、同 key、同请求再次进入时只在注册 replay destination 的
retention 窗口内查找稳定 Replay ID：找到且 key/value hash、source、Group、Event/schema metadata
一致时原子提交 success + audit。absence 需要 producer uncertainty window 结束后，在 bounded
settlement window 内重复 end-offset snapshot 与完整扫描；bounds 增长会重启 scan/stability，只有连续
稳定 bounds 和 clean complete scans 才提交 `publication_absent` failure 并释放坐标。遇到无法稳定、
取消、无法排除的 malformed Record、证据过期或不可用时继续 pending。任何 reconciliation 都禁止再次
publish。只有可证明未确认的 timeout、明确拒绝、Record 缺失/过期、inspection failure 和 invalid
provenance 才持久化封闭失败结果，不返回伪成功。

## 6. 事件处置流程

1. 在 `Frux Kafka Failure Recovery` 看板确认 Group、Topic、lag、retained growth 和 oldest age。
2. 通过 summary 获取 retained start/end；再按精确 Partition/Offset inspect。
3. 若 offset 小于 retained start，记录已过期事件，使用 PostgreSQL 业务事实/outbox 或上游源修复，
   不移动 Group Offset 猜测恢复。
4. 修复 Consumer 或业务数据后，用注册 reason 和新的 idempotency key replay 单条 Record。
5. 确认 `replay_total{result="succeeded"}`、目标 Consumer durable receipt 和 lag 收敛。
6. Replay 失败时按 `publish_timeout`、`publish_rejected`、`publication_absent`、`record_missing`、
   `record_expired`、`invalid_provenance` 或 `inspection_failed` 处置；pending evidence expired 需要
   保留现场并使用业务事实修复，禁止编辑 payload 重试。

## 7. 监控与告警

指标只使用注册 `group/topic/stage/result/tier/state`，不使用 ID、actor、reason、Partition、Offset、
key、payload 或 raw error。告警和看板位于：

- `apps/monitoring/alerts/kafka_failure_recovery.yml`
- `apps/monitoring/grafana/dashboards/frux-kafka-failure-recovery.json`

Retention risk 在 oldest age 达到注册 retention 的 80% 时触发。No-progress 使用同一 15 分钟窗口的
absolute end-offset 增长、retained backlog、oldest-record timestamp 是否前移，以及 durable retry
处理或成功 replay counter；非破坏 replay 成功必须算 progress。不能把 Kafka end offset 当作可
destructive drain 的精确队列深度。
Source 与每个 retry tier 使用独立 `stage` lag/session-health series；owning workflow lag 为 stage
求和，health 为最差 stage，避免空闲 retry tier 最后写入 0/healthy 后掩盖 source backlog 或故障。
API 启动受监督的 15 秒周期摘要采集器（单次 5 秒 timeout），不依赖管理端 HTTP 调用；Broker
不可用不会阻止进程启动，并通过 `frux_kafka_recovery_metrics_stale` 标记现有 gauge 已过期。

## 8. 测试

```bash
cd apps/api
go test ./internal/domain/kafkafailure \
  ./internal/application/kafkafailure \
  ./internal/infra/persistence/kafkafailure \
  ./internal/interfaces/http/kafkafailure \
  ./internal/infra/kafka \
  ./internal/infra/metrics \
  ./test -run 'KafkaDeadLetter|KafkaFailure|Replay'
```

PostgreSQL concurrency/migration 测试使用 `FRUX_POSTGRES_TEST_DSN`。
