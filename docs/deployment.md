# 部署与媒体交付

## Compose

`apps/docker-compose.yml` 提供 PostgreSQL、Redis、单节点 KRaft Kafka、MinIO、API、
Worker、Web、Prometheus 和 Grafana。Kafka 容器内 Listener 为 `kafka:9092`，宿主机测试 Listener
为 `127.0.0.1:29092`；`kafka_data` 卷保存日志。单节点、副本 1 和明文 Listener 仅用于本地开发。
MinIO 地址：

| 服务 | 地址 |
| --- | --- |
| S3 API | `http://127.0.0.1:9000` |
| Console | `http://127.0.0.1:9001` |
| Bucket | `frux-media` |

`minio-init` 创建 bucket、设置浏览器直传 CORS，并只允许匿名下载 `media/` 公共输出前缀。

Compose 不包含可用的内部 API token。启动 API/Worker 前必须通过环境变量或部署平台
Secret 注入 `FRUX_INTERNAL_TOKEN`；配置中的 `internal.enabled=true` 会拒绝空值、
`replace-with-internal-token` 占位值，以及少于 32 字符或字符类别不足的弱值。可用
`openssl rand -base64 48` 生成值，并在执行 `docker compose config` 时一并导出。

## 生产配置

设置 `media.backend=s3`，并配置：

- API/Worker 可访问的内部 `s3.endpoint`。
- 浏览器可访问且参与签名的 `s3.presign_endpoint`。
- region、bucket 和最小权限凭据。
- 指向 CDN 或公共媒体域名的 `public_base_url`。
- 上传会话、签名 URL、处理租约和删除延迟。

API 凭据需要 Put/Head/Get，Worker 另需 List/Delete；浏览器只获得单对象、短时 PUT 签名。生产 bucket 不应整体公开，CDN origin 只读取内容寻址公共前缀。

### 雨云生产配置

默认 `apps/docker-compose.yml` 和 `config.docker.yaml` 始终保留为本地 MinIO 开发环境。Prod
使用 `apps/docker-compose.prod.yml`、`apps/api/configs/config.prod.yaml` 和 `.env.prod`。
该简单方案运行单PostgreSQL、Redis和Kafka，不具备高可用或生产级Kafka安全。

不得让使用不同PostgreSQL数据库的两套API/Worker同时连接同一个 `frux1`，也不得把已有对象的
Bucket直接连接到空数据库后启动Worker。

Rainyun CORS、`media/*` 前缀公共策略、Secret 注入、启动命令和验证步骤见
[雨云对象存储生产接入](operations/rainyun-object-storage.md)。

完整启动步骤见[简单 Prod 部署](operations/prod.md)。

运行时降级控制由 API 与 Worker 使用 `governance.poll_interval` 和
`governance.poll_timeout` 轮询 PostgreSQL；默认分别为 5 秒和 2 秒。timeout 必须不大于
interval。发布时应同时确认两个进程的 `/metrics` 中 active revision 和 snapshot age 正常。

## Kafka 生产要求

生产环境必须关闭 Broker auto topic creation，并由平台按代码注册表预建 Topic。Frux 在
`environment=production` 只验证、不修改 Topic，且要求：

- 至少 3 个 Broker，并为 Controller quorum 提供独立故障域；业务 Topic replication factor
  至少 3，`min.insync.replicas` 至少 2。
- Producer 保持 idempotence 与 `acks=all`；Broker/ACL 必须允许幂等写，不能通过降低 Ack 绕过。
- 客户端使用 TLS 1.2+；生产通常同时启用 SASL/SCRAM（SHA-256 或 SHA-512）或平台批准的认证，
  凭据和 CA/client key 由 Secret 挂载，不能写入 YAML 或日志。
- Topic 的 partition 下限、`cleanup.policy`、`retention.ms`、`message.timestamp.type=LogAppendTime`、
  `max.message.bytes` 和 `min.insync.replicas` 与代码注册表一致。Retention 或 timestamp policy
  变更需要兼容性评审，不能依赖 Broker 默认值。
- Action publication 失败或不确定时进入 PostgreSQL fallback/conditional rollback；view publication
  失败时保留 outbox。Worker 先启动 view Group，再启动 action Group。
- 网络策略只开放 Broker Listener；Controller Listener 不暴露给 API/Worker。滚动升级前验证 ISR
  和 under-replicated partition 为零。

完整配置字段和验证命令见 [Kafka event backbone](kafka.md)。

Kafka failure recovery 额外要求平台预建每个 retry-topic Group 的固定 5s/30s/2m/10m/30m
Topic 和 30 天 DLQ。当前 Feed、embedding 共 12 个 recovery Topic。按峰值失败率、平均 Record
大小、retention、replication factor 和至少 1.5 安全系数规划磁盘；DLQ oldest age 达到 retention
80% 前必须告警。API 使用已有有界 Kafka admin/reader/publisher client，不暴露 Broker 凭据。
Prometheus 加载 `kafka_failure_recovery.yml`，Grafana 自动加载
`frux-kafka-failure-recovery.json`。详细 replay/expiry runbook 见
[Kafka 故障恢复模块](modules/kafka-failure-recovery.md)。API 每 15 秒独立刷新 Kafka DLQ 摘要；
Broker outage 不阻止启动，`frux_kafka_recovery_metrics_stale` 用于识别旧 gauge。
No-progress 告警使用 15 分钟 absolute end-offset、retained backlog、oldest timestamp 与
`frux_kafka_recovery_progress_total`；成功的非破坏 replay 或 durable retry 处理会抑制该窗口告警。

`frux.video.published.v1` 必须保持 30 天 delete retention 和 `LogAppendTime`；
`frux.media.processing-requested.v1` 为 6 小时 command。所有 source/retry Group 在加入前通过
PostgreSQL durable marker 初始化或验证 committed offsets；offset retention loss 必须使 Worker
显式报告 data loss。

API/Worker 对 Kafka 的 Compose 依赖使用 `service_started`，不使用 broker health gate。Kafka
topology、publisher 和 active consumers 在有界退避 supervisor 中重连；对应
`frux_kafka_broker_healthy`、按 `stage` 区分的 `frux_kafka_consumer_session_healthy` 和
`frux_kafka_consumer_workflow_healthy` 会显示故障，PostgreSQL outbox/job、媒体轮询和审核任务仍保留。

## 生产审核推理网关

默认 `moderation.mode` 为空并归一化为 `disabled`。Compose 可注入：

```bash
export FRUX_MODERATION_MODE=observe
export FRUX_MODERATION_ENDPOINT=https://moderation-gateway.example.com/v1/review
export FRUX_MODERATION_HMAC_SECRET="$(openssl rand -base64 48 | tr -d '\n')"
export FRUX_MODERATION_SAMPLE_PRESIGN_ENDPOINT=https://media-origin.example.com
```

Provider-enabled mode 启动时强制要求 endpoint 和 32–512 字符 HMAC Secret。生产 endpoint
必须使用 HTTPS；只有本地 loopback fixture 可配 `allow_insecure_local=true`。同时确认：

- `provider_config_version` 每次改变网关、上游模型或映射语义时递增。
- `input_profile_version` 只使用字母、数字、点、下划线和连字符。
- S3 模式必须单独配置网关可达的 HTTPS `sample_presign_endpoint`；不要复用浏览器侧
  `127.0.0.1` MinIO presign 地址。远程网关禁止接收 loopback 或明文 HTTP 样本 URL。
- timeout 为 500ms–30s；lease TTL 至少比 timeout 长 1 秒且不超过 5 分钟。
- concurrency 为 1–32，attempt 为 1–10，样本 URL 最长 5 分钟，retention 为 1 分钟–24 小时。
- 网关验证请求 HMAC，并用同一 Secret 返回响应 HMAC；不得记录或转发样本 URL。
- Worker 的对象存储权限包含私有 `moderation/` Put/Get/Delete，网关只获得短期单对象 GET。

### Rollout promotion checklist

1. **disabled**：先部署 schema/Worker，确认每个新案件稳定转人工、无外部请求、人工队列容量充足。
2. **observe**：配置真实网关和新 provider config version；确认 Admin 显示“生产模型证据”，
   对同一批样本比较模型信号与人工最终决定，持续观察 timeout、invalid、fallback 和人工 backlog。
3. **approve_only**：仅在 safe/approve 一致率、误放风险、阈值和未知 label 处理通过评审后启用；
   reject 仍必须转人工。
4. **enforce**：需要明确运营批准和 reject threshold 验证；先小流量，再逐步扩大，并保持人工抽检。
5. **rollback**：任一质量、可用性、签名或队列指标异常时立即切回 `disabled`。已提交 decision 不重算；
   与当前 provider config/mode/profile 不兼容的 active/leased job 显式写 recovery 并转人工，新案件同样安全转人工。

真实 promotion 证据至少包含样本窗口、人工一致率、假阴性/假阳性分析、未知 label 比例、P95 延迟、
fallback 率、人工队列 oldest age 和回滚演练结果。

Broker 退役观察窗口、旧队列 drain、Kafka 指标阈值和七天有界回滚流程见
[Kafka-only retirement runbook](operations/kafka-only-retirement.md)。支持的恢复接口只有
`/api/admin/kafka-dead-letters*`。

## 灰度与回滚

1. 先部署新增表、配置和本地适配器，旧视频自动标记 `legacy_ready`，响应字段保持兼容。
2. 启动 Worker 和对象存储，但保持 Web 使用 multipart；验证队列、ffmpeg、对象指标和 Reconciler。
3. 对小流量用户开启上传会话。新视频在基线完成前保持 `media_status=processing`，不进入公开读模型。
4. 验证基线 MP4、DASH、Range、ETag、CDN 缓存和删除清理后，再全量开启。
5. 回滚时把 `media.backend` 切回 `local` 并让 Web 根据 `mode=multipart` 自动回退。已生成的 `ready` 记录、`media_url` 和 `cover_url` 继续可读，不删除新增表或对象。
6. 若 Worker 异常，停止新直传并保留任务表；修复后由数据库 pending/retryable 任务和 Reconciler 恢复，不需要重放用户请求。

## 视频工作流故障隔离

Kafka publication transport outage 不会丢失 durable outbox；publication dispatcher
异步重试并按 5×100/10 秒运行，30 天后的 dispatched outbox 仅在 immutable fact 存在时分批清理。
发布 timeout 后使用脱离 aggregate cancellation 的短 deadline 标记 retry，stats 也使用独立短
deadline；stats 失败保留上一组 gauges，并增加
`frux_video_publication_outbox_stats_total{result="failure"}`。

视频私密/删除/拒绝/下架会与 `media_video_lifecycle_task` 同事务提交。Worker 即使在 API commit 后
崩溃场景下也会通过租约重试保护对象；删除任务保留 asset ID 并继续调度现有 cleanup tasks。发布
发现由视频事务立即撤销，不依赖对象存储成功。运营应同时观察 media lifecycle worker 失败和
`media_cleanup_backlog`，修复后无需重放用户请求。升级迁移会按当前 private/deleted/rejected/offline
状态幂等补齐历史 lifecycle tasks；旧 admin transition intent 在过渡期仍执行保护，避免升级窗口
遗留公开对象。
