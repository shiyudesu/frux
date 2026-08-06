# 部署与媒体交付

## Compose

`apps/docker-compose.yml` 提供 PostgreSQL、Redis、RabbitMQ、MinIO、API、Worker、Web、Prometheus 和 Grafana。MinIO 地址：

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

当前 `action_changed_mode=dual` 是首个幂等试点。上线步骤：

1. 先以 `legacy` 部署 `.q2`、DLX 和 DLQ，确认声明幂等且没有修改旧 Classic Queue 类型。
2. 改为 `dual`，同时观察旧/新 Queue、重复 Event ID、retry exhaustion 和 DLQ backlog。
3. 旧 Queue 的 ready/unacked 连续 15 分钟为零后改为 `new`；旧 Queue 至少保留一个观察窗口。
4. 逐个迁移 video fanout、embedding、view event 和 media processing，不同时切换多个 Consumer。
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
