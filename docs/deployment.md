# 部署与媒体交付

## Compose

`apps/docker-compose.yml` 提供 PostgreSQL、Redis、RabbitMQ、单节点 KRaft Kafka、MinIO、API、
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

运行时降级控制由 API 与 Worker 使用 `governance.poll_interval` 和
`governance.poll_timeout` 轮询 PostgreSQL；默认分别为 5 秒和 2 秒。timeout 必须不大于
interval。发布时应同时确认两个进程的 `/metrics` 中 active revision 和 snapshot age 正常。

RabbitMQ 死信恢复要求 RabbitMQ 3.13+ Management 镜像，并配置 `rabbitmq.management_url`、
服务端 Management 凭据、timeout 和 `rabbitmq.dead_letter`。生产凭据必须由 Secret 注入，
不得使用 Compose 的本地 guest 配置。Quorum Source/DLQ 需要足够磁盘和节点副本；容量上限、
Delivery Limit 和 Replay timeout 必须在发布前压测。

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
- 行为迁移的 dual producer 模式必须同时取得 RabbitMQ 与 Kafka acknowledgement；任一失败都应触发
  action fallback/conditional rollback 或保留 view outbox。Action cutover boundary 必须严格晚于
  view boundary，Worker 必须先启动 view Kafka group。
- 网络策略只开放 Broker Listener；Controller Listener 不暴露给 API/Worker。滚动升级前验证 ISR
  和 under-replicated partition 为零。

完整配置字段、迁移模式和验证命令见 [Kafka event backbone](kafka.md)。

视频工作流按 publication producer、Feed consumer、embedding consumer、media wakeup 四个责任独立
切换。`frux.video.published.v1` 必须保持 30 天 delete retention 和 `LogAppendTime`；
`frux.media.processing-requested.v1` 为 6 小时 command。首次 active cutover 前必须在 advisory lock
内确认对应 Rabbit source/quorum/DLQ 全部 drain；已有 Kafka Group Offset 在重启时保留，future
boundary 或 Offset/data-loss 检测必须使 Worker 显式失败。

Compose 默认启用语义 integration；非 Compose 部署可显式关闭。覆盖配置时设置：

```bash
export FRUX_SEMANTIC_EMBEDDING_ENABLED=true
export FRUX_SEMANTIC_EMBEDDING_URL=http://semantic-embedding:8081
```

目标服务必须实现固定 metadata/embedding 契约并复用强 `FRUX_INTERNAL_TOKEN`。服务不可用不会阻止
Worker 的 hash、Feed 或媒体轮询；观察 `semantic_embedding_job` backlog，恢复 metadata 后当前副本
自动恢复 claim，不执行共享 job 的批量 suspend/resume；健康副本可直接 claim 遗留 `suspended`
行。`FRUX_EMBEDDING_MODEL_PATH` 与 `FRUX_EMBEDDING_FIXTURE_PATH` 不是生产覆盖项，出现时配置启动
失败，镜像内固定路径只能通过显式构造的测试 Settings 替换。

API/Worker 对 RabbitMQ 与 Kafka 的 Compose 依赖使用 `service_started`，不使用 broker health gate。
Kafka topology/publisher、active/shadow consumer 和 Rabbit consumer 在有界退避 supervisor 中重连；
对应 `frux_kafka_broker_healthy`、`frux_kafka_consumer_session_healthy` 与
`frux_rabbitmq_transport_healthy` 会显示故障，但 PostgreSQL outbox/job、媒体轮询、审核和 semantic
poller 继续启动。Active Kafka group 仍须在 Rabbit drain 与 cutover offset 初始化成功后才启动。

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

当前 `action_changed_mode=dual` 是首个幂等试点。上线步骤：

1. 先以 `legacy` 部署 `.q2`、DLX 和 DLQ，确认声明幂等且没有修改旧 Classic Queue 类型。
2. 改为 `dual`，同时观察旧/新 Queue、重复 Event ID、retry exhaustion 和 DLQ backlog。
3. 旧 Queue 的 ready/unacked 连续 15 分钟为零后改为 `new`；旧 Queue 至少保留一个观察窗口。
4. 依次观察 publication mirror、Feed shadow、embedding shadow 和 media-wakeup shadow；每个责任单独
   cut over/rollback，不同时切换多个 Consumer。
5. 回滚先改回 `dual`，再在旧 Consumer 健康后改为 `legacy`；保留新 DLQ 供调查。

Prometheus 加载 `apps/monitoring/alerts/rabbitmq_dead_letter.yml`，Grafana 自动加载
`frux-rabbitmq-dead-letter.json`。API 每 15 秒通过 Management API 更新 DLQ depth。

## 灰度与回滚

1. 先部署新增表、配置和本地适配器，旧视频自动标记 `legacy_ready`，响应字段保持兼容。
2. 启动 Worker 和对象存储，但保持 Web 使用 multipart；验证队列、ffmpeg、对象指标和 Reconciler。
3. 对小流量用户开启上传会话。新视频在基线完成前保持 `media_status=processing`，不进入公开读模型。
4. 验证基线 MP4、DASH、Range、ETag、CDN 缓存和删除清理后，再全量开启。
5. 回滚时把 `media.backend` 切回 `local` 并让 Web 根据 `mode=multipart` 自动回退。已生成的 `ready` 记录、`media_url` 和 `cover_url` 继续可读，不删除新增表或对象。
6. 若 Worker 异常，停止新直传并保留任务表；修复后由数据库 pending/retryable 任务和 Reconciler 恢复，不需要重放用户请求。

## Semantic embedding Compose service

Compose 包含内部 `semantic-embedding` 服务，固定 MiniLM revision，复用强
`FRUX_INTERNAL_TOKEN`，无 host port，read-only root，UID 10001、64 MiB tmpfs、2 CPU/2 GiB
limit 和 180 秒 readiness allowance。Worker 只使用 `condition: service_started`；服务启动后
不可用时 hash intake、Feed 和媒体轮询仍运行，semantic jobs 保持 pending/retry，健康副本不受其他
副本 metadata/connectivity 状态影响。

服务内部使用同一个 180 秒 deadline 完成 preload、fixture validation 和全部 inference worker
初始化。运行中 live capacity 少于配置值时 readiness 为 503；replacement 以最多 5 秒退避重试，
容量恢复后 readiness 自动恢复。请求日志仅含 route/status/duration/result/capacity，Uvicorn
access log 关闭。Kafka/RabbitMQ publication transport outage 同样不会阻塞 Worker 启动；
publication dispatcher 异步重试并按 5×100/10 秒运行，30 天后的 dispatched outbox 仅在 immutable
fact 存在时分批清理。
发布 timeout 后使用脱离 aggregate cancellation 的短 deadline 标记 retry，stats 也使用独立短
deadline；stats 失败保留上一组 gauges，并增加
`frux_video_publication_outbox_stats_total{result="failure"}`。
