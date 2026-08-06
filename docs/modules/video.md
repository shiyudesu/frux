# 视频模块设计

## 1. 模块职责

视频模块负责视频创建、审核生命周期、详情读取、作品列表、独立可见性、创作者查询、原子批量操作、创作者合集、上传入口和软删除。互动计数由互动模块维护；视频模块同时维护用户内容聚合统计，并为个人内容库提供可读视频批量补齐。

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
| GET | `/api/users/me/video-collections` | 游标分页查询自己的合集 | 登录 | 无 |
| POST | `/api/users/me/video-collections` | 创建合集 | 登录 | 必须 |
| PATCH | `/api/users/me/video-collections/{collectionId}` | 部分更新合集 | 登录 | 无 |
| DELETE | `/api/users/me/video-collections/{collectionId}` | 软删除合集 | 登录 | 无 |
| PUT | `/api/users/me/video-collections/{collectionId}/videos/{videoId}` | 将自己的作品加入合集 | 登录 | 自然幂等 |
| DELETE | `/api/users/me/video-collections/{collectionId}/videos/{videoId}` | 从合集移除作品 | 登录 | 自然幂等 |
| GET | `/api/users/{userId}/video-collections` | 游标分页查询公开合集 | 可匿名 | 无 |
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

合集列表按 `updated_at DESC, id DESC` 使用稳定游标。列表响应包含 `member_count` 和有序 `items`：公开列表的 `member_count` 只统计当前已发布公开成员，`items` 只返回最多 3 张主页预览卡；本人列表返回全部未删除成员，保证合集编辑器能识别完整成员关系。合集页、成员关系和视频卡分别批量查询，不随合集数量形成 N+1；即使匿名请求 `limit=100`，最多也只补齐 300 张公开预览卡。创建首次返回 201，幂等重放返回已有合集和 200；更新返回 200 并补齐当前真实成员卡片；删除和成员增删返回 204。成员真正增加或移除时才更新合集 `updated_at`，重复 PUT/DELETE 不改变排序时间。

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
| `collection_count` | 状态为有效的合集总数，包含公开和私密合集 |

计数通过事务增量更新，并在统一迁移中从 `video`、`video_stat`、`video_collection` 事实幂等校正；更新表达式使用 `GREATEST(..., 0)` 防止负数。发布、下架、恢复、删除和可见性变化都会按旧/新状态贡献差更新。迁移校正以“事实值与语句快照基线之差”叠加到当前聚合，避免覆盖校正期间已经提交的并发增量。

### 3.5 `video_collection`

保存 `owner_id`、标题、描述、`visibility`、软删除 `status`、可选幂等键和时间字段。标题最长 128，描述最长 512；未传可见性时 Domain 默认 `private`。HTTP 创建接口要求 `Idempotency-Key`，重放不比较请求指纹。

### 3.6 `video_collection_item`

以 `(collection_id, video_id)` 为主键/唯一成员约束，保存追加生成的 `position` 和 `created_at`。读取按 `position ASC, video_id ASC`。

### 3.7 `video_batch_operation`

以 `(user_id, idempotency_key)` 唯一，保存规范化请求指纹、动作、排序后的视频 ID JSON、结果 JSON和创建时间。

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
| 公开视频可读 | 视频详情、公开作者作品、Feed、推荐、预加载和公开合集只返回 `status=2 AND visibility=public AND media_status IN (legacy_ready, ready)` |
| 生产上传 | Web 创建上传会话后直传 S3 兼容存储，完成接口严格校验 owner、对象键、大小、类型、SHA-256 和过期时间；本地模式继续使用 `/api/uploads` |
| 双门门禁 | 审核通过和 H.264/AAC faststart 基线就绪相互独立；两者同时满足前只对作者展示真实处理/审核状态 |
| 兼容与增量响应 | `media_url`、`cover_url` 继续投影可播放基线和封面；新客户端可读取有序 `playback_sources` |
| 延迟清理 | 删除视频立即移除公开发现，并为原始对象、封面和所有变体创建延迟清理任务 |
| 旧列表兼容 | `/users/me/videos` 与 `/users/{userId}/videos` 保留 offset 响应 |
| 创作者查询语义 | `/users/me/videos` 和 `/video-queries` 查询作者自己的所有非删除作品；公开/私密过滤按 `visibility`，`statuses` 可筛选待审与拒绝 |
| 关键词安全 | 标题和描述使用参数化 `ILIKE`，并转义 `\`、`%`、`_` |
| 批量原子性 | 事务内锁定全部视频并验证所有权；公开/私密动作拒绝下架或删除视频 |
| 缓存防泄露 | 可见性、删除和生命周期变化清除视频卡片/统计缓存；Feed 组装还会用数据库重新校验缓存 ID 的公开可读性 |
| 本地媒体防泄露 | `/uploads` 视频/封面同时验证不可变上传所有权、同所有者视频引用、生命周期、可见性和当前身份；他人公开重引用不能授权资产，保护资源禁用缓存 |
| 审核专用预览 | 当前 review version 且非删除的视频可由 `review.read` 获得最长 5 分钟的 S3 预签名或本地 HMAC URL；不恢复公共 `media_url`，不改变 Feed/搜索/媒体公开资格 |
| 生产媒体撤销 | 私密、下架、拒绝或删除会把已提升的 `media/` 变体降回 `processed/` 保护前缀；本地 `/media` 读取还会实时查询当前公开资格 |
| 有界缓存撤销 | 公共对象和本地 `/media` 使用 60 秒 `must-revalidate` 缓存；状态变化后旧缓存最多保留一个短窗口，撤销失败返回错误并可幂等重试 |
| 公共 URL 版本 | 新提升使用 `media/v2/{exposure-generation}/...`，恢复会产生新 URL；首次上线必须清理 CDN 中旧 `media/*` 一年缓存条目 |
| 合集所有权 | 只能管理自己的有效合集，并只能加入自己未删除的作品 |
| 合集公开读取 | 只返回有效公开合集，成员只保留已发布公开作品 |
| 合集本人读取 | 返回有效公开/私密合集，成员过滤已删除作品但可包含草稿或下架作品 |
| 合集预览边界 | 公开列表每个合集最多补齐 3 张成员卡，按 `position ASC, video_id ASC`；`member_count` 保留公开可读成员总数 |
| 删除统计 | 删除视频扣减对应可见性计数和该视频当前获赞；删除合集扣减合集数 |

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
| 公开合集读取 | 私密合集和不可公开读取的成员均不返回 |
| 100 个公开合集 | 固定批量查询次数，每个合集最多 3 张预览卡，合集与成员顺序稳定 |
| 重复添加合集成员 | 不产生重复成员 |
| 重复增删合集成员 | 不改变合集 `updated_at`；真实增删会改变并影响合集排序 |
| 更新合集响应 | PATCH 返回当前真实、已补齐视频卡的成员数组 |
| 直接读取待审/拒绝/私密/下架/删除媒体 | 匿名返回 404；作者可读取非删除保护媒体，删除媒体对作者也返回 404 |
| 审核员预览待审媒体 | 授权 preview-access 可播放；未授权、过期、篡改、旧 review version 或删除主体均不可读取 |
| 他人重引用保护 URL | 发布返回 403，且伪造的公开引用不能让资产对匿名用户可读 |
| 历史资产回填 | 唯一作者引用的保护 URL 获得该作者所有权并继续按公开视频/本人规则读取 |
| 上传非法媒体 | 返回 400 并清理失败文件 |
| 直传对象不匹配 | owner、对象键、大小、类型或校验和不匹配时返回冲突且不创建资产 |
| 视频仍在处理 | 作者列表返回 `media_status=processing`，公共详情、Feed、推荐和预加载均不返回 |
| 基线完成 | `media_url` 投影到基线，`playback_sources` 按基线、MP4 清晰度、DASH manifest 稳定排序 |
| 删除生产视频 | 公开发现立即消失，物理对象在安全延迟后由 Worker 清理 |

## 6. 前端接入点

| 页面 | 接入能力 |
| --- | --- |
| 发布页 | 生产模式使用预签名直传和独立视频/封面进度，本地模式保留 multipart 回退 |
| Feed/详情 | 展示已发布公开视频 |
| 个人主页作品 Tab | “公开作品”按公开可见性查询并展示处理中、审核中、未通过、已发布和已下架标签；“私密作品”对应私密可见性 |
| 个人主页合集 Tab | 创建、编辑、删除合集并管理成员；编辑器独立搜索和游标加载全部公开/私密候选作品 |
| 公开主页 | 展示已发布公开作品和公开合集 |
| Admin Shell | 按 typed 筛选查询视频；下架/恢复弹窗携带原因、备注、确认和当前 version，只在服务端确认审计提交后报告成功 |
