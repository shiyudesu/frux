# 视频模块设计

## 1. 模块职责

视频模块负责视频创建、审核生命周期、详情读取、作品列表、独立可见性、创作者查询、原子批量操作、上传入口和软删除。互动计数由互动模块维护；视频模块同时维护用户内容聚合统计，并为个人内容库提供可读视频批量补齐。

## 2. 接口设计

| 方法 | 接口路径 | 作用 | 鉴权 | 幂等键 |
| --- | --- | --- | --- | --- |
| POST | `/api/videos` | 创建待审核视频 | 登录 | 支持 |
| GET | `/api/videos/{videoId}` | 查询已发布公开视频详情 | 可匿名 | 无 |
| DELETE | `/api/videos/{videoId}` | 软删除自己的视频 | 登录 | 支持 |
| GET | `/api/users/{userId}/videos` | 查询用户已发布公开视频 | 可匿名 | 无 |
| GET | `/api/users/me/videos` | 兼容的我的作品列表 | 登录 | 无 |
| POST | `/api/users/me/video-queries` | 按可见性、关键词和创建日期查询自己的非删除作品 | 登录 | 无 |
| POST | `/api/users/me/video-batch-actions` | 批量公开、私密或删除作品 | 登录 | 必须 |
| POST | `/api/uploads` | 上传媒体文件 | 登录 | 支持 |
| POST | `/api/upload-sessions` | 创建对象存储直传会话；本地模式返回 multipart 回退 | 登录 | 支持 |
| POST | `/api/upload-sessions/{sessionId}/complete` | 校验对象并完成上传会话 | 登录 | 自然幂等 |
| GET | `/api/media-assets/{assetId}/access` | 获取本人原始/保护资产短期签名 URL | 登录 | 无 |
| GET | `/api/admin/review/cases/{caseId}/preview-access` | 获取当前审核主体的短期保护视频/封面预览 | `review.read` | 无 |
| GET | `/api/admin/videos` | 后台稳定游标查询非删除视频 | `content.enforce` | 无 |
| POST | `/api/admin/videos/{videoId}/enforcement` | 原因化、版本检查的下架 | `content.enforce` | 无 |
| POST | `/api/admin/videos/{videoId}/restoration` | 恢复已批准的下架视频 | `content.enforce` | 无 |

复杂作品查询请求使用 `visibility`、可选 `statuses`、`query`、`created_from`、`created_to`、`cursor`、`limit`，响应为 `items`、`next_cursor`、`has_more`。`statuses` 可筛选草稿、已发布、下架、待审和拒绝，但不查询已删除状态。日期接受 RFC 3339 或 `YYYY-MM-DD`；仅日期形式的结束日期包含当天末尾。默认 `limit=20`，范围 1–100，排序为 `created_at DESC, id DESC`。

批量接口支持 `make_public`、`make_private`、`delete`，先去重并按 ID 排序，最多 100 个正整数 ID。成功返回：

```json
{"action":"make_private","video_ids":[12,18],"replayed":false}
```

同一用户用同一 `Idempotency-Key` 重放相同规范化请求时返回 `replayed=true`；同键不同请求返回 409。任一视频不存在或不属于当前用户时整批回滚。

`GET/HEAD /uploads/*` 保留标准 Range/条件请求语义，但视频和封面不再作为无条件静态文件暴露。认证上传会把 `/uploads/video/*` 和 `/uploads/cover/*` 的不可变上传者写入 `local_upload_asset`；创建视频时只有上传者可以引用这些保护 URL。已发布公开作品只有在视频作者等于资产上传者时才可匿名读取；待审、拒绝、私密和下架作品仅资产上传者本人可读。删除作品、未引用文件、无所有权记录文件和跨作者引用都返回 404。

## 3. 数据表设计

### 3.1 `video`

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | BIGINT | PK | 视频 ID |
| `author_id` | BIGINT | NOT NULL | 作者 ID |
| `title` | VARCHAR(128) | NOT NULL | 标题 |
| `description` | VARCHAR(512) | NULLABLE | 视频简介 |
| `media_url` | VARCHAR(512) | NOT NULL | 视频地址 |
| `cover_url` | VARCHAR(512) | NOT NULL | 封面地址 |
| `media_asset_id` | BIGINT | NULLABLE | 生产视频源资产 |
| `cover_asset_id` | BIGINT | NULLABLE | 生产封面资产 |
| `media_status` | VARCHAR(24) | NOT NULL | `legacy_ready` / `processing` / `ready` / `failed` |
| `media_error_code` | VARCHAR(64) | NOT NULL | 处理失败代码 |
| `review_version` | INTEGER | NOT NULL, DEFAULT 1, CHECK > 0 | 当前审核主体版本 |
| `version` | INTEGER | NOT NULL, DEFAULT 1, CHECK > 0 | 内容运营乐观并发版本 |
| `status` | SMALLINT | NOT NULL, DEFAULT 5 | 1 草稿 / 2 已发布 / 3 下架 / 4 删除 / 5 待审核 / 6 已拒绝 |
| `visibility` | VARCHAR(16) | NOT NULL, DEFAULT `public` | `public` / `private`，独立于生命周期 |
| `published_at` | TIMESTAMPTZ | NULLABLE | 发布时间 |
| `idempotency_key` | VARCHAR(128) | NULLABLE | 发布幂等键，与作者组成唯一约束 |
| `created_at` | TIMESTAMPTZ | NOT NULL | 创建时间 |
| `updated_at` | TIMESTAMPTZ | NOT NULL | 更新时间 |

主要索引：

| 索引 | 字段 | 说明 |
| --- | --- | --- |
| `idx_video_author_visibility_created` | `author_id, visibility, created_at, id` | 创作者管理查询 |
| `idx_video_public_timeline` | `status, visibility, published_at, id` | 公开 Timeline |

### 3.2 `video_stat`

以 `video_id` 为主键，保存 `like_count`、`comment_count`、`favorite_count` 和时间字段。

### 3.3 `local_upload_asset`

| 字段 | 说明 |
| --- | --- |
| `asset_url` | 主键；保护本地视频或封面的规范化 `/uploads/{kind}/{filename}` URL |
| `owner_id` | 首次认证上传者，写入后不可转移 |
| `kind` | `video` / `cover` |
| `created_at` | 上传所有权记录时间 |

迁移从现有受保护本地 URL 引用回填唯一作者资产；同一 URL 若被多个作者引用则不猜测所有者并保持不可读。重复迁移使用 `ON CONFLICT DO NOTHING`，不会覆盖已有不可变所有权。

### 3.4 `user_content_stat`

生产媒体链路另外使用 `media_asset`、`media_variant`、`media_processing_profile`、`media_processing_job`、`media_upload_session` 和 `media_cleanup_task`。资产保存不可变 owner、对象键、大小、SHA-256 和探测元数据；变体按 `sort_order` 稳定返回；处理任务按 `(asset_id, profile_version)` 幂等并带租约、尝试次数和失败状态。

| 字段 | 说明 |
| --- | --- |
| `user_id` | 主键 |
| `public_work_count` | `status=published AND visibility=public AND media ready` 的作品数 |
| `private_work_count` | 非删除、`visibility=private` 的作品数 |
| `received_like_count` | 非删除作品当前持久化点赞数之和 |
计数通过事务增量更新，并在统一迁移中从 `video` 和 `video_stat` 事实幂等校正；更新表达式使用 `GREATEST(..., 0)` 防止负数。发布、下架、恢复、删除和可见性变化都会按旧/新状态贡献差更新。迁移校正以“事实值与语句快照基线之差”叠加到当前聚合，避免覆盖校正期间已经提交的并发增量。

### 3.5 `video_batch_operation`

以 `(user_id, idempotency_key)` 唯一，保存规范化请求指纹、动作、排序后的视频 ID JSON、结果 JSON和创建时间。

### 3.6 `video_notification_outbox`

保存视频模块拥有的结构化生命周期事实：提交审核、媒体终态失败、首次公开、下架和恢复。`event_id` 唯一，载荷包含 recipient、video、review version、stage、result、safe reason 和业务发生时间。Worker 通过 30 秒数据库租约、`FOR UPDATE SKIP LOCKED`、有界指数退避和 terminal 状态投递。

首次发布事实使用两个独立耐久边界：`video_notification_outbox` 管理创作者通知 readiness；
`video_publication_event_fact` 永久保存不可变稳定事件事实，`video_publication_event_outbox`
只保存租约、尝试、可用时间和 dispatch 等运营状态。审核、媒体 ready、恢复、运营和
reconciliation 都调用同一幂等边界；Kafka 不可用不会回滚真实公开状态。Worker 将事实发布到
30 天保留的 `frux.video.published.v1`。

Dispatcher 启动只校验本地依赖，初次 dispatch 与周期任务异步执行，因此 Kafka outage
不阻塞 Worker 或其他 consumer 启动。单次运行最多 5 个 100-row batch 且总计 10 秒；超过 replay
window 的 dispatched 运营行每次最多清理 100 行。清理要求 immutable fact 已存在，reconciliation
也按 fact 缺失判断，所以删除 outbox 不会重新发布。

publication outbox 的 pending/oldest 统计查询与 dispatch 操作错误分别观测。即使 Kafka transport
失败，只要统计查询成功，pending 与 oldest-age gauge 仍按当前数据库结果更新；统计查询自身失败时才
保留旧的 age 值。

## 4. 业务规则

| 规则 | 说明 |
| --- | --- |
| 创建进入待审 | 生产媒体和兼容 URL 创建均返回 `status=5`、`published_at=null` |
| 审核版本 | 新视频从 `review_version=1` 开始；自动审核案件与结果必须匹配当前正整数版本 |
| 媒体就绪触发审核 | legacy-ready 创建和生产媒体 ready 通知都会幂等创建当前版本案件；周期 reconciliation 修复遗漏 |
| 审核转换 | 待审可幂等批准为已发布或拒绝；批准首次设置 `published_at` |
| 处罚与恢复 | 已发布可下架；下架恢复时保留原始 `published_at`；删除状态为终态 |
| 后台查询 | 排除删除状态，按 `created_at DESC, id DESC` 分页；cursor 绑定状态、作者、视频 ID、关键词和时间窗口 |
| 运营并发 | 下架和恢复必须匹配 `expected_version`，成功后递增 version；旧版本稳定返回冲突 |
| 原因与审计 | 下架只接受 `manual_enforcement`/`policy_violation`，恢复只接受 `compliance_restored`；note 最多 1000 Unicode 字符 |
| 原子事实 | 生命周期、内容统计、不可变处罚记录、后台转换意图和成功审计同一 PostgreSQL 事务提交；审计失败整体回滚 |
| 后台副作用 | Video Worker 有界租约转换意图，重试缓存失效和按当前状态执行的媒体保护/发布；全部成功后才标记 delivered |
| 历史视频默认公开 | 迁移将空可见性补为 `public` |
| 生命周期与可见性分离 | 设为私密不会改变 `status` 或 `published_at` |
| 创建统计行 | 创建视频时同步创建 `video_stat`；只有审核通过、公开且媒体就绪后才增加公开作品计数 |
| 本地上传所有权 | 认证上传视频/封面后持久化不可变 owner；记录失败会删除已写入文件 |
| 发布 URL 规则 | `http/https` 远程 URL 可用；本地媒体只接受属于作者的 `/uploads/video/*`，本地封面只接受属于作者的 `/uploads/cover/*`；`file`、`avatar`、类型互换和无所有权路径均拒绝 |
| 公开视频可读 | 视频详情、公开作者作品、Feed、推荐和预加载只返回 `status=2 AND visibility=public AND media_status IN (legacy_ready, ready)` |
| 生产上传 | Web 创建上传会话后直传 S3 兼容存储，完成接口严格校验 owner、对象键、大小、类型、SHA-256 和过期时间；本地模式继续使用 `/api/uploads` |
| 上传会话重放 | 同一 owner 和 `Idempotency-Key` 的相同 fingerprint 返回原 session 或已完成 asset，不受本次请求新生成的候选 session ID 影响；同键异载荷返回冲突 |
| 发布前校验 | Web 在创建上传会话前校验标题必填、标题 128 UTF-8 字节和简介 512 UTF-8 字节边界，避免文件已上传后才发现作品参数无效 |
| 成对上传预检 | Web 在计算校验和或创建任一上传会话前同时校验视频和封面的存在、扩展名、MIME 和大小；视频支持 MP4/MOV/WebM 且不超过 512 MiB，封面支持 JPEG/PNG/WebP 且不超过 20 MiB |
| 单侧失败重试 | 视频与封面分别持有页面内稳定幂等身份和完成结果；一侧失败或被替换时只重试该侧，未变化且已完成的资产直接复用；最终视频创建使用独立的媒体对幂等键 |
| 上传前本地预览 | Web 为选择的视频和封面创建独立临时 object URL，视频使用 controls/muted/playsInline 和封面 poster；替换文件或离开页面立即 revoke，不创建上传会话 |
| 创作者保护预览 | 本人作品页的网格封面和查看弹窗可展示待审、处理中、拒绝、私密和下架的非删除作品；缺少公共 URL 时按 asset ID 获取短期 owner access，不改变生命周期、可见性或公共缓存 |
| 保护预览选源 | ready 视频资产优先签发受保护 baseline MP4，ready 封面优先签发受保护 cover variant；没有对应 ready variant 时回退原始上传对象，并允许客户端提示浏览器兼容限制 |
| 保护凭据禁止缓存 | owner/reviewer access JSON 与对象响应均为 private/no-store；短期 URL 只保存在当前组件内存，不进入列表响应、路由或 Web Storage |
| 双门门禁 | 审核通过和 H.264/AAC faststart 基线就绪相互独立；两者同时满足前只对作者展示真实处理/审核状态 |
| 生命周期通知 | 创建提交事实；审核拒绝/批准、终态媒体失败、首次公开、下架和恢复各使用稳定 event ID；瞬时上传进度、处理重试和转人工审核不写消息中心 |
| 首次发布唯一性 | 同一 `video_id + review_version` 最多一个 `video-published` 事实；审核、媒体 ready、可见性、恢复和 reconciliation 共享该身份 |
| 发布流消费 | Feed 与 embedding 使用独立 Kafka Group；任一侧延迟、重放或回滚不推进另一侧 Offset，也不重写 `published_at` |
| 发布恢复 | 未 ready 的发布事实由 `PublicationRecoveryService` 幂等完成媒体提升和发布事件；删除或 review version 已推进的事实转 terminal |
| 历史兼容 | 只有存在同版本 submission/publication 跟踪标记的迁移后视频才补首次发布事实，既有历史公开视频不会被合成通知 |
| 兼容与增量响应 | `media_url`、`cover_url` 继续投影可播放基线和封面；新客户端可读取有序 `playback_sources` |
| 延迟清理 | 删除视频立即移除公开发现，并为原始对象、封面和所有变体创建延迟清理任务 |
| 旧列表兼容 | `/users/me/videos` 与 `/users/{userId}/videos` 保留 offset 响应 |
| 创作者查询语义 | `/users/me/videos` 和 `/video-queries` 查询作者自己的所有非删除作品；公开/私密过滤按 `visibility`，`statuses` 可筛选待审与拒绝 |
| 关键词安全 | 标题和描述使用参数化 `ILIKE`，并转义 `\`、`%`、`_` |
| 批量原子性 | 事务内锁定全部视频并验证所有权；公开/私密动作拒绝下架或删除视频 |
| 缓存防泄露 | 可见性、删除和生命周期变化清除视频卡片/统计缓存；Feed 组装还会用数据库重新校验缓存 ID 的公开可读性 |
| 本地媒体防泄露 | `/uploads` 视频/封面同时验证不可变上传所有权、同所有者视频引用、生命周期、可见性和当前身份；他人公开重引用不能授权资产，保护资源禁用缓存 |
| 审核专用预览 | 当前 review version 且非删除的视频可由 `review.read` 获得最长 5 分钟的 S3 预签名或本地 HMAC URL；不恢复公共 `media_url`，不改变 Feed/搜索/媒体公开资格 |
| 生产媒体撤销 | 私密、下架、拒绝或删除会清空 variant 公开资格和 exposure generation；protected 对象键不变，`/media` 每次新授权仍查询当前公开资格 |
| 有界缓存撤销 | 公开 307 使用 25 分钟、签名媒体响应使用 30 分钟 `must-revalidate`；状态变化立即拒绝新授权，旧缓存最多延迟 30 分钟 |
| 公共 URL 版本 | 新发布使用不暴露存储键的 `media/v3/{generation}/{variant_id}/{filename}`；恢复生成新 generation，历史 v2 在迁移窗口内兼容 |
| 删除统计 | 删除视频扣减对应可见性计数和该视频当前获赞 |

## 5. 测试建议

| 场景 | 期望 |
| --- | --- |
| 登录用户创建视频 | 返回待审视频、空 `published_at` 并创建统计行 |
| 媒体先就绪 | 保持待审、公共 URL 为空且不发送发布事件 |
| 审核先通过 | 保持公共不可读，直到媒体基线就绪 |
| 查询私密视频详情 | 匿名接口返回 404 |
| 查询私密作品 | 创作者查询只返回自己的非删除私密作品并稳定翻页 |
| 批量混入他人视频 | 返回权限错误，所有视频保持原状 |
| 批量同键异载荷 | 返回 409 |
| 直接读取待审/拒绝/私密/下架/删除媒体 | 匿名返回 404；作者可读取非删除保护媒体，删除媒体对作者也返回 404 |
| 审核员预览待审媒体 | 授权 preview-access 可播放；未授权、过期、篡改、旧 review version 或删除主体均不可读取 |
| 他人重引用保护 URL | 发布返回 403，且伪造的公开引用不能让资产对匿名用户可读 |
| 历史资产回填 | 唯一作者引用的保护 URL 获得该作者所有权并继续按公开视频/本人规则读取 |
| 上传非法媒体 | 返回 400 并清理失败文件 |
| 未选封面后补选 | 首次提交不创建任何上传会话并提示选择封面；补选有效封面后可正常提交，不继承无效会话状态 |
| 封面超限或格式不支持 | Web 在任一网络上传前指出具体封面限制；直接调用上传会话 API 时返回稳定的类型或大小错误码 |
| 视频完成而封面失败 | 页面保留已完成视频资产；重试原封面或替换封面时只请求封面上传，视频进度直接恢复为 100% |
| 创建作品瞬时失败 | 重试复用视频、封面资产和视频创建幂等键，不再次上传对象或创建重复作品 |
| 直传对象不匹配 | owner、对象键、大小、类型或校验和不匹配时返回冲突且不创建资产 |
| 上传后作品参数失败再重试 | 重用原幂等键时直接返回已完成视频/封面资产，不重复上传对象，也不因新候选 session ID 返回 500 |
| 待审作品列表与查看 | 本人作品网格按需获取 cover 短期访问，WorkViewer 并发获取 media/cover 短期访问；ready baseline 可播放，只有封面或浏览器不支持原始编码时展示真实处理提示和重试 |
| 非本人请求保护资产 | 返回权限错误且不签发对象 URL |
| 视频仍在处理 | 作者列表返回 `media_status=processing`，公共详情、Feed、推荐和预加载均不返回 |
| 基线完成 | 新任务的 `media_url` 与 `playback_sources` 投影到同一个源分辨率 MP4；历史多源继续稳定排序 |
| 首次发布投递失败 | 视频事实保持提交；publication Outbox 延迟重试，通知 readiness 与事件 dispatch 独立，且同一 event 不重复创建 |
| 发布副作用中断 | 未 ready 的 publication 事实由 API/Worker 恢复后再投递，不提前声称视频已公开 |
| 删除生产视频 | 公开发现立即消失，物理对象在安全延迟后由 Worker 清理 |

## 6. 前端接入点

| 页面 | 接入能力 |
| --- | --- |
| 发布页 | 生产模式使用预签名直传和独立视频/封面进度，本地模式保留 multipart 回退 |
| Feed/详情 | 展示已发布公开视频 |
| 个人主页作品 Tab | “公开作品”按公开可见性查询并展示处理中、审核中、未通过、已发布和已下架标签；“私密作品”对应私密可见性 |
| 消息中心目标 | 已发布/恢复且当前可读时进入详情；其他生命周期状态进入作品页并按认证 `video_id` 跨公开/私密定位 |
| 公开主页 | 展示已发布公开作品和隐私允许的喜欢列表 |
| Admin Shell | 按 typed 筛选查询视频；下架/恢复弹窗携带原因、备注、确认和当前 version，只在服务端确认审计提交后报告成功 |

## 10. 首次公开事件原子性

审核、媒体就绪、可见性、后台恢复和 reconciliation 在首次形成公开资格的数据库事务内同时
upsert lifecycle notification、immutable publication fact 与 operational publication outbox。
媒体-backed 边界先创建不可 dispatch 的同一 outbox 行，公共 variant 就绪后再事务性解除
readiness；notification 即使已经 ready/delivered 也不能替代 publication fact 证明。已 dispatch
运营行可在 replay window 后有界清理，immutable fact 继续阻止 reconciliation 合成事件；私密、
删除或没有 lifecycle 历史的旧公开视频不会被 reconciliation 合成。

媒体处理最终写入不是分散的 `UpdateAsset` / `UpsertVariants` / `UpdateJob`：Repository 在单一事务
内先锁定并验证 processing job 的 claim token 与未过期 lease，再原子提交 asset metadata、variants、
cleanup/job transition。过期或已被回收的 worker 不能写 asset/variant、提升公共状态或发送通知；
媒体 ready/failed 通知只在 fenced commit 成功后执行，失败由现有 reconciliation 幂等修复。
删除调度同样在 Repository 事务内锁定 asset、快照当前 ready variants、创建 cleanup tasks 并写入
deleted tombstone，避免与转码 finalization 交错遗漏新输出；failed assets 继续进入 reconciliation，
用于重试提交后失败的 media-failed 投影。

创作者单条删除、批量私密/删除、审核拒绝/下线和管理员下架不再在请求提交后直接调用对象存储。
视频状态、统计和 `media_video_lifecycle_task` 在同一事务提交；因此 API 公开发现立即移除，而 API
进程在 commit 后崩溃也不会丢失媒体保护/清理。Worker 以 `SKIP LOCKED`、稳定顺序、租约、attempts
和 retryable/failed 终态领取任务：私密/下线仍成立时降回保护前缀，删除时先保护再调用现有
`media_cleanup_task` 调度。任务通过 any-status 读取删除视频并保留 media/cover asset ID；较新的
公开转换会把旧私密任务标记为 `superseded`，不会错误降级当前公开媒体。

生命周期 worker 暴露 `frux_media_video_lifecycle_tasks_total{result}`、
`frux_media_video_lifecycle_backlog` 和 oldest age。result 只允许 completed、superseded、
retryable、failed、lease_lost，不包含 video、asset、对象键或错误正文。
对象存储等基础设施错误始终保留为 retryable，并使用封顶退避持续恢复；只有目标永久缺失等确定性
终态才进入 failed。
