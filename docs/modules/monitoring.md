# 监控告警模块设计

## 1. 模块职责

监控告警模块负责业务指标采集、核心看板、告警规则和告警事件查询。

## 2. 接口设计

| 方法 | 接口路径 | 作用 | 鉴权 | 幂等键 |
| --- | --- | --- | --- | --- |
| POST | `/internal/metric-points` | 写入业务指标点 | 服务鉴权 | 支持 |
| GET | `/api/admin/metric-dashboard` | 查询核心看板数据 | Bearer JWT(运营角色) | 无 |
| POST | `/api/admin/alerts/rules` | 新建告警规则 | Bearer JWT(管理员) | 支持 |
| GET | `/api/admin/alerts/events` | 查询告警事件 | Bearer JWT(运营角色) | 无 |

## 3. 数据表设计

### 3.1 `monitor_metric_point`

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | BIGINT | PK | 记录 ID |
| `metric_name` | VARCHAR(128) | NOT NULL | 指标名 |
| `labels` | JSON | NULLABLE | 标签 |
| `value` | DOUBLE | NOT NULL | 指标值 |
| `ts` | DATETIME | NOT NULL | 指标时间 |

索引建议：`idx_metric_ts(metric_name, ts)`。

### 3.2 `monitor_alert_rule`

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | BIGINT | PK | 规则 ID |
| `rule_name` | VARCHAR(128) | UNIQUE, NOT NULL | 规则名 |
| `metric_name` | VARCHAR(128) | NOT NULL | 指标名 |
| `condition_expr` | VARCHAR(255) | NOT NULL | 告警条件 |
| `enabled` | TINYINT | NOT NULL, DEFAULT 1 | 是否启用 |
| `updated_at` | DATETIME | NOT NULL | 更新时间 |

索引建议：`idx_metric_enabled(metric_name, enabled)`。

### 3.3 `monitor_alert_event`

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | BIGINT | PK | 事件 ID |
| `rule_id` | BIGINT | NOT NULL | 规则 ID |
| `status` | VARCHAR(16) | NOT NULL | `FIRING` / `RESOLVED` |
| `trigger_value` | DOUBLE | NOT NULL | 触发值 |
| `triggered_at` | DATETIME | NOT NULL | 触发时间 |
| `resolved_at` | DATETIME | NULLABLE | 恢复时间 |

索引建议：`idx_status_triggered(status, triggered_at)`。

## 4. 业务规则

| 规则 | 说明 |
| --- | --- |
| 指标点支持标签 | 标签保存接口、场景、实例等维度 |
| 看板按时间窗口聚合 | 支持最近分钟、小时和天级查询 |
| 告警规则可启停 | 停用规则不产生新事件 |
| 告警事件保留状态 | 触发和恢复都可查询 |
| 内部写入保持幂等 | 同一批指标重复写入结果稳定 |

## 5. 测试建议

| 场景 | 期望 |
| --- | --- |
| 写入指标点 | 数据入库 |
| 查询看板 | 返回聚合指标 |
| 创建告警规则 | 规则启用 |
| 查询告警事件 | 按状态和时间返回 |
| 非管理员创建规则 | 返回 403 |

## 6. 前端接入点

| 页面 | 接入能力 |
| --- | --- |
| 监控看板 | Feed、互动、播放、队列和数据库指标 |
| 告警规则页 | 创建和启停规则 |
| 告警事件页 | 查看触发和恢复记录 |

## 7. 播放观测

Grafana 的 `Frux Playback Observability` 看板提供：

- 启动耗时 p50/p95/p99，以及首帧耗时按 scene、network、player 的 p95 拆分。
- 聚合卡顿时长占比、播放失败率、清晰度和媒体源分布。
- 遥测 batch/event 吞吐、拒绝率、重复率和客户端投递延迟。

首帧和卡顿优先按 network、player 判断是否为特定网络或播放器回归，再按 scene
判断是否局限于某一入口。清晰度或 source 分布突变通常表示选源、转码或降级策略变化。
拒绝率上升表示客户端契约或服务端校验不一致；重复率上升通常表示 keepalive/retry 增多；
投递延迟上升表示客户端积压、弱网或入口处理变慢。

Prometheus 标签仅允许固定低基数集合。播放指标不包含 `user_id`、`video_id`、
`request_id`、`session_id` 或 `playback_session_id`；未知 scene、network、player、
quality、source、错误和恢复结果会折叠到 `unknown` 或 `other`。

后端 ingestion 可使用 `internal/infra/metrics.PlaybackMetrics`：

```go
inframetrics.PlaybackMetrics.ObserveFirstFrame(inframetrics.PlaybackTimingObservation{...})
inframetrics.PlaybackMetrics.ObserveStartup(inframetrics.PlaybackTimingObservation{...})
inframetrics.PlaybackMetrics.ObserveRebuffer(inframetrics.PlaybackRebufferObservation{...})
inframetrics.PlaybackMetrics.ObserveRebufferSummary(inframetrics.PlaybackRebufferSummaryObservation{...})
inframetrics.PlaybackMetrics.ObserveAttempt(inframetrics.PlaybackAttemptObservation{...})
inframetrics.PlaybackMetrics.ObserveRecovery(inframetrics.PlaybackRecoveryObservation{...})
inframetrics.PlaybackMetrics.ObserveSelection(inframetrics.PlaybackSelectionObservation{...})
inframetrics.PlaybackMetrics.ObserveTelemetryIngestion(inframetrics.TelemetryIngestionObservation{...})
inframetrics.PlaybackMetrics.ObserveTelemetryCleanup(inframetrics.TelemetryCleanupObservation{...})
```

调用方只传归一化技术维度和聚合值，不传任何用户、视频、请求或播放会话标识。
清理失败或删除量长期为零时，检查 retention 配置、数据库权限和 `created_at` 索引。

## 8. Playback alert investigation

告警规则位于 `apps/monitoring/alerts/playback.yml`，均要求持续窗口；质量告警还要求每
10 分钟至少 100 个样本，避免稀疏流量误报。

1. **Startup regression**：在看板按 scene/network/player 定位；检查 CDN/源站可用性、
   最近播放器或选源发布，并比较 measurement method，确认是否只是 fallback 比例变化。
2. **Rebuffering high**：确认卡顿时长和样本量同步上升；检查特定网络、source 和 quality，
   再检查媒体分片、带宽和转码产物。
3. **Playback failure high**：查看失败率和 error category；区分 network、decode、
   unsupported、autoplay、timeout，并检查对应 source 的近期变化。
4. **Telemetry outage**：先确认 API `/metrics` 可抓取和整体 HTTP 流量存在，再检查
   `/api/playback-telemetry-batches` 路由、客户端发布开关、拒绝计数和服务日志。该规则应在
   遥测客户端开始稳定上报后启用。

处置后保持观察至少一个完整告警窗口。若只有遥测指标异常而实际播放成功率正常，优先回滚
遥测客户端或 ingestion 变更；遥测失败不得影响播放。

## 9. Recommendation observability and rollout gates

推荐指标为 `frux_recommendation_recall_candidates_total{provider}`、
`frux_recommendation_degraded_requests_total{provider,reason}`、
`frux_recommendation_snapshot_operations_total{result}`、
`frux_recommendation_request_log_failures_total{stage}`、
`frux_recommendation_active_policy_version{scene}`、
`frux_recommendation_outcomes_total{outcome}`，以及 profile Worker lag/events。标签只允许
固定 provider、reason、scene、result 和 outcome；不得使用 user、video、request、session 或
policy cohort 标识。snapshot `result=maintenance_failure` 表示 Lua 已提交权威 snapshot 后的
请求索引 TTL 或用户索引维护失败；该错误不应把本次 Feed 响应降级为本地重排。

v2 扩量前比较同一 24h 窗口的 API error、degraded/timeout、snapshot hit、profile lag、曝光到
play/complete 和反馈率；任何错误或降级恶化、snapshot hit 明显下降、lag 持续积压或负反馈
恶化时停止扩量并回滚到 v1。请求日志保存用于离线归因，不作为 Prometheus 标签；日志落库失败
应记录有界 failure metric 但不能影响 Feed 响应。snapshot 的 `hit/miss` 只表示读取结果；
`write_success/write_failure` 单独表示写入，避免以写入量污染命中率。

## 10. Automated review observability

自动审核使用 `frux_review_events_total{stage,result}`。stage 仅允许 `intake`、
`provider_result`、`routing`、`reconciliation`；result 仅允许 created、existing、accepted、
approve、reject、human、duplicate、invalid、conflict、retry、success 或 unknown。不得把
provider、model version、policy version、video、case、result identity 或证据引用放入标签。

`retry` 持续增长时先区分数据库/媒体发布失败和 reconciliation 失败；`invalid` 增长表示生产者
契约或边界不一致；`duplicate` 可随至少一次投递正常增长，但同身份异载荷会记为 conflict。
任何 provider 不可用都必须让视频保持 pending-review，不能通过降级路径发布。

## 11. Human review observability

人工复审暴露：

- `frux_human_review_queue_available`
- `frux_human_review_queue_oldest_age_seconds`
- `frux_human_review_operations_total{operation,result}`
- `frux_human_review_notifications_total{result}`

operation 仅为 queue、detail、claim、renew、release、lease_expiry、decision；result 仅为
success、approve、reject、invalid、conflict、retry、recovered、unknown。通知 result 仅为
delivered、retry、terminal、unknown。不得使用 reviewer、case、video、reason、token 或
idempotency key 标签。

queue oldest age 持续上升时先检查可领取量、lease expiry recovery 和 reviewer 吞吐；claim
conflict 上升通常表示并发领取或陈旧页面；decision conflict 按服务日志区分 lease ownership、
expiry、case version、review version 和 idempotency payload。notification retry 上升不影响已
提交决定，检查 message 数据库和 Worker；terminal 表示违反封闭消息契约，需要修复生产代码。

## 11.1 Production moderation provider observability

生产推理链路使用 `frux_moderation_operations_total{operation,result}`：

- operation：`loop`、`claim`、`extraction`、`provider_call`、`result_submission`、`retry`、
  `fallback`、`cancellation`、`reconciliation`、`unknown`。
- result：`success`、`retry`、`terminal`、`human`、`stale`、`created`、`cancelled`、
  `recovered`、`noop`、`unknown`。

标签不得包含 provider、model、provider config version、job/case/video/request ID、错误正文、
签名或样本 URL。`provider_call/retry` 上升时检查网关延迟、429/5xx、签名和响应契约；
`extraction/terminal` 上升时检查 ffmpeg、源对象完整性和输入预算；`fallback/human` 上升时确认
视频仍为 pending-review 并评估人工队列容量。observe 阶段应结合人工最终决定离线计算一致率，
不得把 provider/model 放进 Prometheus 标签。

## 12. Runtime degradation control observability

运行时降级控制暴露 active revision、poll result、snapshot age、invalid snapshot 和 evaluation
fallback 指标。标签只允许 `api/worker`、代码注册 key 和封闭 result/reason；不得使用 actor、
request、任意动态 key 或 revision 作为标签。revision 是 gauge value。

告警规则位于 `apps/monitoring/alerts/governance.yml`：

1. `FruxGovernanceSnapshotStale`：snapshot age 持续超过 120 秒；
2. `FruxGovernancePollingFailures`：5 分钟内至少 3 次 poll failure 且持续 2 分钟。

告警时比较 PostgreSQL active pointer 与
`frux_governance_active_revision{process,key}`，再检查数据库连接、poll timeout 和非法持久化
值。snapshot 超过 key 的 max staleness 后会使用 failure default，因此先确认可选能力已安全
关闭，再恢复控制面；不要直接修改历史 revision。

## 13. Rate-limit investigation

分层限流只暴露
`frux_rate_limit_decisions_total{endpoint_group,layer,result}`。endpoint group 仅为
`playback_telemetry`、`public_search`、`upload_session`；layer 仅为 local、distributed、
fallback；result 仅为 allow、reject、fallback、saturation、backend_error。identity、IP、
user、Redis key 和 route parameter 均不得进入标签。

告警位于 `apps/monitoring/alerts/rate_limit.yml`：

1. rejection spike：先按 endpoint group 和 layer 判断是 local burst 还是 distributed quota；
2. Redis fallback：检查 API 到 Redis 的连接、deadline 和 governance distributed control；
3. local saturation：检查异常 identity 扩散、trusted proxy 配置和 `max_entries`，不得先移除
   map bound。

`public_search` Redis 故障时仍由更严格 local fallback 保护；`upload_session` 明确 fail
closed。Grafana `Frux Layered Rate Limits` 看板展示三类信号。恢复 Redis 后确认
backend_error/fallback 停止增长，再观察一个完整告警窗口。

## 14. Kafka event backbone observability

Kafka 基础暴露：

- `frux_kafka_produce_total{topic,producer,result}` 与 produce duration；
- `frux_kafka_consumed_total{topic,group,outcome}` 与 consume duration；
- `frux_kafka_commit_total{topic,group,result}`、`frux_kafka_rebalance_total{group,result}`；
- `frux_kafka_consumer_lag{topic,group}`、delivery delay；
- `frux_kafka_contract_failures_total{topic,group,code}`；
- `frux_kafka_topology_validation_total{topic,result}` 和 broker health。
- `frux_kafka_consumer_session_total{group,result}` 与
  `frux_kafka_consumer_session_healthy{group}`。

Topic、Producer、Group、Outcome、Contract Code 和 Topology Result 均来自封闭集合。禁止使用
event/user/video/key/partition/offset/payload/raw error 作为标签。`commit result=uncertain` 表示
当前 Consumer Session 已停止，后续可能重投；先检查 Consumer 幂等边界，再检查 Group Coordinator。
`topology result=invalid/missing` 在生产会阻止启动，不能临时开启 auto creation。Delivery delay 或
lag 增长时按注册 Topic/Group 定位，不要添加动态 Partition/Offset 标签。
`consumer_session result=fatal_failure` 表示认证、配置或 Handler 契约错误；active Group 会让
Worker 明确失败，不能只观察 broker health。`retryable_failure` 表示暂时 Broker/DB/Parity
依赖失败并按有界退避重建 Session。

## 15. Behavior stream migration observability

行为迁移额外暴露：

- `frux_behavior_publication_total{stream,role,transport,result}`；
- `frux_behavior_action_fallback_total{result}` 与 conditional rollback；
- `frux_behavior_shadow_total{stream,result}`；
- `frux_behavior_consumption_total{stream,result}`。

`stream` 仅为 action/view，`role` 仅为 primary/mirror，`transport` 仅为 rabbit/kafka；
result 使用封闭 success/failure/uncertain、parity_match/parity_mismatch/parity_pending/
parity_pending_exhausted、
duplicate/superseded 等集合。不得加入
event、user、video、key、partition、offset 或错误正文。View 先 cutover；任一 stream 出现持续
primary failure、fallback/rollback 增长、shadow mismatch、lag 或 delivery age 超门槛时按该
stream 独立回滚。
