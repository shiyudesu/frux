# 部署与媒体交付

## Compose

`apps/docker-compose.yml` 提供 PostgreSQL、Redis、RabbitMQ、MinIO、API、Worker、Web、Prometheus 和 Grafana。MinIO 地址：

| 服务 | 地址 |
| --- | --- |
| S3 API | `http://127.0.0.1:9000` |
| Console | `http://127.0.0.1:9001` |
| Bucket | `gcfeed-media` |

`minio-init` 创建 bucket、设置浏览器直传 CORS，并只允许匿名下载 `media/` 公共输出前缀。

## 生产配置

设置 `media.backend=s3`，并配置：

- API/Worker 可访问的内部 `s3.endpoint`。
- 浏览器可访问且参与签名的 `s3.presign_endpoint`。
- region、bucket 和最小权限凭据。
- 指向 CDN 或公共媒体域名的 `public_base_url`。
- 上传会话、签名 URL、处理租约和删除延迟。

API 凭据需要 Put/Head/Get，Worker 另需 List/Delete；浏览器只获得单对象、短时 PUT 签名。生产 bucket 不应整体公开，CDN origin 只读取内容寻址公共前缀。

## 灰度与回滚

1. 先部署新增表、配置和本地适配器，旧视频自动标记 `legacy_ready`，响应字段保持兼容。
2. 启动 Worker 和对象存储，但保持 Web 使用 multipart；验证队列、ffmpeg、对象指标和 Reconciler。
3. 对小流量用户开启上传会话。新视频在基线完成前保持 `media_status=processing`，不进入公开读模型。
4. 验证基线 MP4、DASH、Range、ETag、CDN 缓存和删除清理后，再全量开启。
5. 回滚时把 `media.backend` 切回 `local` 并让 Web 根据 `mode=multipart` 自动回退。已生成的 `ready` 记录、`media_url` 和 `cover_url` 继续可读，不删除新增表或对象。
6. 若 Worker 异常，停止新直传并保留任务表；修复后由数据库 pending/retryable 任务和 Reconciler 恢复，不需要重放用户请求。

