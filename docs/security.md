# 媒体安全

## 资产边界

- `media_asset.owner_id` 是认证上传者，创建后不可转移。
- 上传会话绑定 owner、kind、对象键、大小、Content-Type、SHA-256 和过期时间；完成接口重新 HEAD 校验。
- 对象键由服务端生成，客户端不能选择 bucket 或跨 owner 前缀。
- 同一 `Idempotency-Key` 只能重放相同规范化上传意图。

## 访问策略

| 资产 | 访问方式 | 缓存 |
| --- | --- | --- |
| 已就绪公开 MP4/segment/cover | v3 虚拟 URL 校验后签名 protected 对象 | 307 为 25 分钟，媒体响应为 30 分钟并 `must-revalidate` |
| DASH manifest | v3 虚拟 URL 校验后由 Frux 返回 | 30 分钟并 `must-revalidate` |
| 原始上传、处理中资产 | owner 获取短期签名 URL | `private, no-store` |
| 私密作品 | API 不返回公共播放源；owner 按当前引用状态获取签名访问 | `private, no-store` |
| 删除作品 | 立即移除 API 发现并拒绝 owner 访问，物理对象延迟清理 | 不新增缓存 |

生产对象存储 bucket 始终保持私有，公开播放也只使用短期签名 GET。API/Worker 通过
`http://minio:9000` 使用 Bucket-scoped 应用凭据；MinIO Root 凭据只供服务和初始化器使用，不能
注入应用容器。浏览器只接收 `https://FRUX_S3_DOMAIN:<public-port>` 的对象级预签名 URL，签名 URL
不包含 JWT、Cookie 或长期凭据。下架立即拒绝新签名，但已缓存 307 或已签发 URL 最多可继续使用
30 分钟。

MinIO CORS 只允许精确 Origin `https://FRUX_DOMAIN:<public-port>` 和上传/播放所需方法、头部；不能
使用通配 Origin。主机 Caddy 不改写签名请求的 Host、path、query、method 或 Range。MinIO Console
只绑定 `127.0.0.1:19001` 并通过 SSH 隧道访问；DNS-01 API 凭据使用最小权限并保存在仓库之外。

## 处理与清理

- Worker 对输出计算 SHA-256，直接写确定性的 `processed/*` 最终键并 HEAD 校验；匹配对象复用，冲突
  不覆盖。公开状态由 PostgreSQL `public/exposure_generation` 控制，不复制或移动正文。
- 处理任务按资产和 profile 版本幂等，租约过期后可重试。
- 删除请求只写清理任务，不同步批量删除对象。
- Reconciler 检查缺失源、缺失变体、过期租约和孤儿对象。
- `minio_data` 与 PostgreSQL 备份位于同一单机故障域；媒体恢复还需要提供商磁盘快照或外部 MinIO/S3
  镜像。一个数据库不得同时连接两个活动 Bucket。

## 播放遥测隐私

- 遥测请求使用严格 JSON schema，只允许版本、稳定事件 ID、技术事件和低基数环境维度。
- 不接受完整媒体 URL、签名参数、JWT、Cookie、标题、描述或任意 metadata map；CDN 只保存校验后的 hostname。
- PostgreSQL 可以保存认证用户 ID、视频 ID、request ID 和播放会话用于受控诊断，但 Prometheus/Grafana 标签禁止包含这些标识符。
- browser、OS、network、viewport、codec、quality、source 和 scene 都折叠到固定枚举；未知值归入 `unknown`/`other`。
- 原始遥测默认保留 168 小时并由有界清理任务删除；行为历史和推荐事实使用独立保留策略。

## 私信数据与内容安全

- 私信只允许正常消费端账号之间的当前互关关系；每次创建会话和发送都重新检查关系与账号状态。取关保留历史但禁止继续发送，避免把历史内容误删或让旧资格永久有效。
- 发送接口使用严格 JSON schema 和 `Idempotency-Key`，只接受 trim 后的文本或正数 `video_id`；客户端不能提交媒体 URL、封面、标题、作者快照或任意 metadata。服务端先按 sender+key 和完整 payload 解析已提交消息，再检查当前账号、成员、互关和视频资格；因此精确重试返回 `created=false`，同键不同 payload 才冲突。发送受用户维度 `chat_send` 限流，Redis 协调不可用时 fail-closed。
- `chat_message` 只保存视频 ID。读取历史时按当前 `published + public + media-ready` 资格批量 hydration；视频删除、下架、私密或处理中时返回 `available=false` tombstone，不返回保护 URL、封面、标题或作者隐私数据。
- `chat_conversation`、`chat_conversation_member` 和 `chat_message` 是独立于 `user_message` 的 PostgreSQL 表；私信正文不复制到 Kafka、搜索索引、Redis 内容缓存或 telemetry。备份中仍可能存在正文，留存、导出和删除政策需后续 change 明确。
- 日志、trace、Prometheus 标签和错误诊断不得包含正文、昵称、用户/会话/消息/视频 ID 或媒体 URL；chat metrics 仅使用封闭 operation、`TEXT`/`VIDEO`、outcome、error class 和延迟。
- 当前协议为前端路由、去重和卡片 hydration 携带正数 `user_id`/`video_id`；这些标识只用于请求响应，不得进入观测或二次数据集。不可用 participant/video 使用最小字段和 safe fallback。
- 初版没有陌生人消息、block/report、群聊、附件、在线状态、撤回、端到端加密或 WebSocket。未来增加这些能力前，需单独定义滥用治理、留存、删除、导出和密钥管理边界。
