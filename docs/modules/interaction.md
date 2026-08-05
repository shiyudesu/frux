# 互动模块设计

## 1. 模块职责

互动模块负责用户对视频的点赞、收藏和两级评论讨论，同步维护视频统计、评论回复数、评论获赞数和根评论热度，并向个人内容库提供按行为更新时间排序的有效喜欢/收藏索引。点赞持久化还会维护视频作者的获赞聚合；评论、回复和新评论点赞通过互动模块自有的 PostgreSQL Outbox 可靠生成站内消息。

模块边界：

| 模块 | 职责 |
| --- | --- |
| `interaction` | 记录点赞、收藏、根评论、回复、评论点赞和通知 Outbox，处理状态、计数、排序、删除与幂等 |
| `video` | 保存视频主体信息 |
| `video_stat` | 保存 `like_count`、`favorite_count`、`comment_count` 统计字段 |
| `account` | 提供作者/直接回复目标的昵称、头像及管理员角色 |
| `library` | 通过 `ActionIndex` 读取有效喜欢/收藏视频 ID，不直接读取互动 GORM 模型 |
| `user_content_stat` | 保存作者 `received_like_count` |
| `message` | 通过窄写入接口消费互动 Outbox，持久化带结构化讨论目标的消息 |

## 2. 实现结构

```text
apps/api/internal/domain/interaction/
apps/api/internal/application/interaction/
apps/api/internal/infra/persistence/interaction/
apps/api/internal/interfaces/http/interaction/
```

## 3. 接口设计

| 方法 | 接口路径 | 作用 | 鉴权 | 幂等键 |
| --- | --- | --- | --- | --- |
| PUT | `/api/videos/{videoId}/like` | 点赞视频 | Bearer JWT | 支持 |
| DELETE | `/api/videos/{videoId}/like` | 取消点赞 | Bearer JWT | 支持 |
| PUT | `/api/videos/{videoId}/favorite` | 收藏视频 | Bearer JWT | 支持 |
| DELETE | `/api/videos/{videoId}/favorite` | 取消收藏 | Bearer JWT | 支持 |
| POST | `/api/videos/{videoId}/comments` | 创建根评论 | 登录 | 支持，绑定规范化 payload |
| GET | `/api/videos/{videoId}/comments` | 根评论页，支持热门/最新 | 可匿名，可选 JWT | - |
| POST | `/api/videos/{videoId}/comments/{commentId}/replies` | 回复根评论或回复 | 登录 | 支持，绑定规范化 payload |
| GET | `/api/comments/{commentId}/replies` | 根评论的回复页 | 可匿名，可选 JWT | - |
| GET | `/api/comments/{commentId}/thread` | 目标评论所在讨论串上下文 | 可匿名，可选 JWT | - |
| PUT | `/api/comments/{commentId}/like` | 点赞评论或回复 | 登录 | 支持，绑定目标状态 |
| DELETE | `/api/comments/{commentId}/like` | 取消评论点赞 | 登录 | 支持，绑定目标状态 |
| DELETE | `/api/comments/{commentId}` | 删除/治理评论 | 登录 | 操作状态幂等 |

### 3.1 点赞

#### PUT `/api/videos/{videoId}/like`

请求头：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `Authorization` | 是 | `Bearer <access_token>` |
| `Idempotency-Key` | 否 | 客户端幂等键，最长 128 |

请求体：空

响应：

```json
{
  "video_id": 1001,
  "action_type": "LIKE",
  "active": true,
  "like_count": 18
}
```

#### DELETE `/api/videos/{videoId}/like`

请求头：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `Authorization` | 是 | `Bearer <access_token>` |
| `Idempotency-Key` | 否 | 客户端幂等键，最长 128 |

请求体：空

响应：

```json
{
  "video_id": 1001,
  "action_type": "LIKE",
  "active": false,
  "like_count": 17
}
```

### 3.2 收藏

#### PUT `/api/videos/{videoId}/favorite`

请求头：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `Authorization` | 是 | `Bearer <access_token>` |
| `Idempotency-Key` | 否 | 客户端幂等键，最长 128 |

请求体：空

响应：

```json
{
  "video_id": 1001,
  "action_type": "FAVORITE",
  "active": true,
  "favorite_count": 7
}
```

#### DELETE `/api/videos/{videoId}/favorite`

请求头：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `Authorization` | 是 | `Bearer <access_token>` |
| `Idempotency-Key` | 否 | 客户端幂等键，最长 128 |

请求体：空

响应：

```json
{
  "video_id": 1001,
  "action_type": "FAVORITE",
  "active": false,
  "favorite_count": 6
}
```

### 3.3 异步落库

点赞和收藏启用 Redis 快速状态后，接口先校验视频状态和幂等键，再在同一个 Redis CAS 事务中写入行为状态、实时计数和该 `(user_id, video_id, action_type)` 的单调版本，随后使用 RabbitMQ publisher confirm 投递 `ActionChangedEvent`。Worker 消费事件并调用仓储写入 PostgreSQL 行为表和 `video_stat`。

推荐流操作可选传入 `X-Recommendation-Request-ID`（最长 64）。该归因字段是不可信输入，随 durable action event 传递后，Worker 仅在耐久推荐证据绑定当前用户、request 和视频时幂等保存 `like` 或 `favorite` outcome；缺失或伪造归因会跳过 outcome，不改变已接受互动或画像信号。

每个 Redis 状态版本都记录其 `handoff_confirmed` 标志。发布确认失败或结果不确定时，API 使用短时、脱离客户端取消的恢复上下文同步持久化同一事件。相同状态的无键重试、相同幂等键重放和新的 `delta=0` 幂等键若遇到未确认版本，都会重发该稳定事件（或同步持久化）并确认 handoff 后才返回成功，不能仅因状态未变化跳过耐久交接。新的请求键在确认前以有界（最多 32 条）的 `idempotency_receipts` 依赖该版本；确认后才成为普通 no-op 回执。每个键仍绑定目标 active 载荷：同键相反载荷返回冲突且不改变状态。

发布与同步持久化都失败时，回滚只可撤销仍未确认、仍匹配 `state_version + event_id`、且没有后续依赖回执的版本；版本计数器不回退，因此可重试的撤销会分配更高版本。已确认的版本、依赖该版本的并发 no-op 或更高版本都会让回滚条件不命中，避免旧失败路径撤销已报告的成功。Redis 事务提交后若响应计数读取失败，缓存层会把版本和原事件元数据一并返回给应用层，以同一条件恢复或回滚；恢复失败时未确认事件保留在 Redis，后续重试可再次交接。

同步请求和已接收事件使用不同的持久化入口：

- `SetActionWithAcceptedEvent` 服务于 Redis/RabbitMQ 不可用时的新 HTTP 请求，必须在事务内再次锁定并验证视频仍为 `published + public`。每个非空 `Idempotency-Key` 都在 `interaction_action_idempotency_receipt` 中绑定目标 active 状态和首次响应计数：同键同目标返回首次结果，同键相反目标返回 409。状态未改变（包括不存在的行为收到取消）只写该回执；只有真实状态转换才创建 action event、画像投影 handoff 和推荐 outcome handoff。
- `PersistAcceptedActionEvent` 仅供 Worker 使用，表示事件已在入队前通过公开可读校验；视频之后变为私密或下架时仍写入互动事实和统计，但已删除或不存在的视频作为终止事件丢弃。
- `interaction_action_event` 按 `event_id` 保存版本和完整已处理载荷。同一事件重复投递不再次改变 `interaction_action`、`video_stat` 或作者 `received_like_count`；相同事件 ID 携带不同载荷视为终止冲突。
- `interaction_action` 为每个 `user_id + video_id + action_type` 保存最新 `latest_event_version + latest_event_occurred_at + latest_event_id`。Worker 首先比较版本；仅在版本相同的兼容事件中使用时间和事件 ID 确定顺序。任何较新事件（即使目标 active/canceled 状态未变）都推进这组顺序字段和推荐 request 归因，而状态相同的事件不改变统计增量；延迟旧事件与精确重复事件写入/命中回执后成功确认，但不改变物化状态或聚合。
- Worker 将格式错误、无效字段、事件 ID 冲突、视频不存在和视频已删除分类为不可重试错误，RabbitMQ 不重新入队；数据库连接等瞬时错误保留给受监督的消费者重连，而不会确认尚未完成的 durable handoff。

有效 LIKE/FAVORITE 是推荐画像的正向耐久事实；推荐 Worker 通过稳定 action event ID 消费，
重复事件不重复加权。互动请求不直接信任或写入客户端推荐画像，避免异步失败扩散到点赞、
收藏的用户可见结果。

每个已接受 action receipt 同事务持有可租约重试的画像投影和 outcome 归因字段。Action Worker 在该
事务提交后确认 RabbitMQ；缺失 embedding、待到达的推荐证据和投影失败只由带指数退避的 leased outbox
重试，不会触发 MQ 热循环。发布或通道恢复失败时，HTTP 的同步持久化路径仍会留下该 durable outbox，
Worker 最终按同一 event ID 投影，且与 MQ 重投递去重。

私密或下架视频的互动事实不会放宽任何读取规则：Feed、公开视频详情、公开主页和个人内容库补齐仍按当前可读性过滤内容。

核心键和队列：

| 类型 | 名称 |
| --- | --- |
| 用户行为状态 | `interaction:action:v1:{user_id}:{video_id}:{action}` |
| 实时计数 Hash | `video:stat:counter:v1:{video_id}` |
| Feed 计数 JSON | `video:stat:v1:{video_id}` |
| Exchange | `frux.interaction` |
| Queue | `frux.interaction.action_changed` |
| Routing key | `interaction.action_changed` |

### 3.4 两级评论模型和通用响应

- 根评论的 `root_comment_id=0`（数据库为 `NULL`）。
- 回复的 `root_comment_id` 指向所属根评论，`reply_to_comment_id` 保留用户直接选择的根评论或回复。
- 回复回复时仍归入同一个根评论，只展示两层，不产生第三层嵌套。
- `reply_to_user_*` 用于展示“回复 @用户”，客户端无需从其他页反查目标作者。

评论 DTO 的核心字段如下；创建、列表、回复预览、讨论串上下文复用同一概念：

| 字段 | 说明 |
| --- | --- |
| `id`, `video_id` | 评论和父视频 ID |
| `user_*` | 作者展示信息；根评论墓碑中清空 |
| `root_comment_id` | 根评论为 `0`/省略，回复为所属根 ID |
| `reply_to_comment_id`, `reply_to_user_*` | 直接回复目标 |
| `content`, `created_at` | 正文和创建时间 |
| `status`, `deleted` | 软状态和删除投影 |
| `reply_count`, `reply_previews` | 活跃回复总数和最多 3 条预览 |
| `like_count`, `liked` | 评论获赞数和当前 viewer 状态 |
| `can_delete` | 服务端按评论作者、视频作者或管理员身份计算 |
| `hot_score` | 根评论物化热度；回复为 0 |
| `comment_count` | 创建响应中的视频最新评论总数 |

匿名读取时 `liked=false`、`can_delete=false`；有效可选 JWT 会补充 viewer 状态。无效或缺失的可选 Token 不阻断公开读取。

### 3.5 创建根评论

#### POST `/api/videos/{videoId}/comments`

请求体：

```json
{"content":"这个剪辑节奏很好"}
```

正文去除首尾空白后按 Unicode code point 校验 1–1000 个字符，而不是按 UTF-8 字节数。`Idempotency-Key` 最长 128 字符，并绑定规范化后的 `video_id + root_id + direct_target_id + content` 指纹。

成功返回 `201` 和完整评论 DTO，额外包含 `comment_count`。相同用户以同一键和相同 payload 重放时返回原评论，不重复增加计数或通知；同键改用不同视频、目标或正文返回 `409`。父视频必须为已发布、公开且媒体就绪。

### 3.6 创建回复

#### POST `/api/videos/{videoId}/comments/{commentId}/replies`

`commentId` 是直接选择的回复目标，可以是根评论或已有回复。请求体、Unicode 限制和 payload 幂等规则与根评论创建相同。服务端解析并返回 `root_comment_id`、`reply_to_comment_id`、直接目标用户信息和新的 `comment_count`。

目标缺失、已自删/治理、跨视频或所属根评论不可用时返回 404，不创建回复，也不改变任何计数。

### 3.7 根评论列表

#### GET `/api/videos/{videoId}/comments`

匿名列表仅在父视频满足 `status=published`、`visibility=public` 且媒体为 `legacy_ready/ready` 时返回；私密、下架、删除、媒体未就绪或不存在统一返回 404，不泄露历史讨论。

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `sort` | `latest` | `latest` / `hot`；Web 显式默认请求 `hot` |
| `cursor` | 空 | 上一页 opaque 游标 |
| `limit` | 20 | 最大 100 |

响应包含 `items`、`next_cursor`、`has_more`、视频 `comment_count` 和实际 `sort`。根列表只返回正常根评论，以及仍有活跃回复的作者自删根墓碑。每个根最多批量水合 3 条按时间正序的活跃回复预览；作者、直接目标、viewer 点赞和权限均采用有界批量查询，不随根数量产生 N+1。

| 模式 | 排序元组 | 游标内容 |
| --- | --- | --- |
| `latest` | `created_at DESC, id DESC` | 版本、sort、`created_at`、`comment_id` |
| `hot` | `hot_score DESC, created_at DESC, id DESC` | 版本、sort、`hot_score`、`created_at`、`comment_id` |

游标为 URL-safe opaque 字符串并带版本/排序标识；跨 sort 使用返回 400。旧版不带版本和 sort 的游标仅按 `latest` 兼容解析。热度分页期间分数可能变化，前端按评论 ID 去重。

### 3.8 回复列表和讨论串上下文

#### GET `/api/comments/{rootCommentId}/replies`

只接受可公开投影的根评论 ID。参数为 `cursor` 和 `limit`（默认 20、最大 100），按 `created_at ASC, id ASC` 最旧优先返回；回复游标包含版本、`created_at`、`comment_id`。响应包含 `root_comment_id`、`items`、`next_cursor`、`has_more`、`comment_count`。

#### GET `/api/comments/{commentId}/thread`

`commentId` 可以是根或回复。接口直接返回 `root`、首个回复页 `replies`、精确 `target`、回复 `next_cursor/has_more` 和视频 `comment_count`，供消息深链使用，无需扫描热门/最新根评论页。目标已删除、被治理或父视频不可读时返回 404。

### 3.9 评论点赞

#### PUT/DELETE `/api/comments/{commentId}/like`

仅登录用户可调用，父视频和目标评论必须仍可公开互动。`Idempotency-Key` 最长 128 字符，在 `user_id + key` 范围绑定 `comment_id + active`。响应包含 `comment_id`、`root_comment_id`、`liked`、`like_count`。

真实状态转换才增减 `like_count`；重复设置同一状态成功且计数不变。同键改绑其他评论或相反状态返回 409。根评论获赞会在同一事务重算根热度；回复获赞只更新回复自身计数。首次点赞他人评论创建稳定的 `COMMENT_LIKE` Outbox 事件，取消再点赞不会重复通知。

### 3.10 删除评论

#### DELETE `/api/comments/{commentId}`

响应包含 `comment_id`、`status`、视频 `comment_count`、`root_reply_count`、`deleted_count`、`thread_hidden`、`tombstone`。

| 用户身份 | 权限 |
| --- | --- |
| 评论作者 | 可删除自己的评论 |
| 视频作者 | 可删除自己视频下的评论 |
| 管理员 | 可删除任意评论 |

删除采用软状态且重复执行不会重复扣减：

- 根作者自删：根变为 `2/self-deleted`；有活跃回复时以无作者/无正文墓碑保留，没有回复时从公开列表省略。
- 视频作者或管理员治理根：根和所有正常回复批量变为 `3/moderated`，整个讨论串隐藏。
- 删除回复：只隐藏该回复；作者自删用状态 2，视频作者/管理员治理用状态 3。
- 父视频后来私密、下架或删除不影响已有评论的授权删除能力。

## 4. 数据表设计

### 4.1 `interaction_action`

`interaction_action` 保存点赞和收藏，这两类行为共享 `user_id + video_id + action_type` 的唯一性约束。

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | BIGINT | PK | 记录ID |
| `user_id` | BIGINT | NOT NULL | 用户ID |
| `video_id` | BIGINT | NOT NULL | 视频ID |
| `action_type` | VARCHAR(16) | NOT NULL | `LIKE` / `FAVORITE` |
| `status` | TINYINT | NOT NULL, DEFAULT 1 | 1有效/2取消 |
| `idempotency_key` | VARCHAR(128) | NULLABLE | 最近一次写入幂等键 |
| `latest_event_version` | BIGINT | NOT NULL, DEFAULT 0 | 最新已应用行为版本；新 Redis 事件从持久基线继续递增 |
| `latest_event_occurred_at` | TIMESTAMPTZ | NULLABLE（迁移后回填） | 最新已应用异步事件发生时间 |
| `latest_event_id` | VARCHAR(128) | NULLABLE（迁移后回填） | 同时间事件的确定性排序键 |
| `created_at` | DATETIME | NOT NULL | 创建时间 |
| `updated_at` | DATETIME | NOT NULL | 更新时间 |

索引建议：

| 索引 | 字段 | 说明 |
| --- | --- | --- |
| `uk_user_video_type` | `user_id, video_id, action_type` | 保证同一用户对同一视频的同类行为只有一条记录 |
| `idx_video_type_status` | `video_id, action_type, status` | 支持按视频统计有效行为 |
| `idx_user_type_status` | `user_id, action_type, status` | 支持后续我的点赞、我的收藏列表 |
| `idx_interaction_action_user_type_status_updated` | `user_id, action_type, status, updated_at, video_id` | 支持个人内容库稳定游标 |

### 4.2 `interaction_action_event`

`interaction_action_event` 保存 Worker 已处理的异步行为事件回执。`event_id` 是主键，`version` 保存 Redis 原子分配的行为版本；事件载荷和回执写入与行为事实、视频统计及作者获赞聚合位于同一事务。

### 4.3 `interaction_action_idempotency_receipt`

同步回退路径按 `user_id + video_id + action_type + idempotency_key` 唯一保存目标状态、行为 ID 和该类型的首次响应计数。它覆盖真实转换与 no-op，因此后续状态改变也不会改变同键重放结果；无幂等键请求不创建此回执。

### 4.4 `interaction_comment`

`interaction_comment` 同时保存根评论和回复，删除采用状态更新。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | BIGINT PK | 评论 ID |
| `video_id`, `user_id` | BIGINT | 父视频和作者 |
| `root_comment_id` | BIGINT NULL | 根为 NULL；回复指向所属根 |
| `reply_to_comment_id` | BIGINT NULL | 直接回复目标 |
| `content` | VARCHAR(1000) | 正文 |
| `status` | SMALLINT | 1 正常/2 作者自删/3 治理 |
| `reply_count`, `like_count` | INT | 活跃回复数、活跃点赞数 |
| `hot_score` | BIGINT | 根物化热度 |
| `request_fingerprint` | VARCHAR(64) | 创建 payload 指纹 |
| `idempotency_key` | VARCHAR(128) NULL | 创建幂等键 |
| `created_at`, `updated_at` | TIMESTAMPTZ | 时间 |

| 索引 | 用途 |
| --- | --- |
| `idx_interaction_comment_root_latest` | `video_id, status, root_comment_id, created_at DESC, id DESC` 最新根列表 |
| `idx_interaction_comment_root_hot` | 加 `hot_score DESC` 的热门根列表 |
| `idx_interaction_comment_replies` | `root_comment_id, status, created_at, id` 回复正序分页和预览 |
| `idx_interaction_comment_direct_target` | 直接目标定位 |
| `idx_interaction_comment_user_created` | 用户评论历史 |
| `uk_interaction_comment_user_idempotency` | `user_id, idempotency_key` 根/回复创建幂等 |

### 4.5 `interaction_comment_like`

保存每个 `user_id + comment_id` 的最新点赞状态，唯一索引为 `uk_interaction_comment_like_user_comment`；`idx_interaction_comment_like_comment_status(comment_id, status)` 支持计数与 reconciliation。

### 4.6 `interaction_comment_like_idempotency_receipt`

以 `user_id + idempotency_key` 为主键，保存 `comment_id`、目标 `active`、首次响应 `like_count`。真实转换和 no-op 都写回执，确保后续状态变化不改变同键重放结果。

### 4.7 `interaction_comment_notification_outbox`

互动模块拥有的耐久通知表。`event_id` 为主键，保存 recipient/actor、`COMMENT`/`COMMENT_REPLY`/`COMMENT_LIKE`、标题/正文快照、`video_id`、`root_comment_id`、`comment_id`，以及 `state`、`attempts`、`available_at`、租约、错误和送达时间。`idx_interaction_comment_notification_outbox_pending(state, available_at, lease_until)` 支持 Worker `SKIP LOCKED` 领取。

## 5. 状态枚举

### 5.1 Action Type

| 值 | 说明 |
| --- | --- |
| `LIKE` | 点赞 |
| `FAVORITE` | 收藏 |

### 5.2 Action Status

| 值 | 说明 |
| --- | --- |
| `1` | 有效 |
| `2` | 取消 |

### 5.3 Comment Status

| 值 | 说明 |
| --- | --- |
| `1` | 正常 |
| `2` | 作者自删；仅有活跃回复的根可投影为墓碑 |
| `3` | 视频作者/管理员治理；不公开 |

## 6. 核心业务规则

### 6.1 点赞和收藏状态变更

处理流程：

1. 校验登录用户和 `video_id`。
2. 查询并锁定视频，只有 `status=Published AND visibility=public` 时允许互动。
3. 按 `user_id + video_id + action_type` 查询行为记录。
4. `PUT` 请求将行为记录更新为 `status = 1`，首次生效时对应计数字段加 1。
5. `DELETE` 请求将行为记录更新为 `status = 2`，首次取消时对应计数字段减 1。
6. 记录缺失且收到 `DELETE` 请求时返回取消状态，计数保持稳定，不创建行为事实或异步 handoff。
7. 在同一事务内提交真实转换的行为记录和 `video_stat` 计数更新；有幂等键的 no-op 仅提交结果回执。
8. `LIKE` 的真实状态发生变化时，同事务增减作者 `user_content_stat.received_like_count`；Worker 异步落库与无 Redis 的同步路径都复用同一仓储逻辑。
9. Worker 事件先按 `version` 排序；版本相同才按 `occurred_at`、`event_id` 兼容排序。不大于行为行已保存顺序的事件为成功 no-op，不更新行为、视频计数、作者获赞或行为 `updated_at`。

计数字段映射：

| `action_type` | 统计字段 |
| --- | --- |
| `LIKE` | `video_stat.like_count` |
| `FAVORITE` | `video_stat.favorite_count` |

计数约束：

| 操作 | 计数变化 |
| --- | --- |
| 生效 | `count + 1` |
| 取消 | `max(count - 1, 0)` |
| 幂等命中 | 返回首次结果，不改计数 |

个人内容库读取只选择 `status=1` 的行为，按 `updated_at DESC, video_id DESC` 排序。取消后的行为事实保留，但不会出现在喜欢或收藏列表。

### 6.2 评论、计数和热度

1. 新根、回复、评论点赞和删除都在 PostgreSQL 事务中锁定所需视频/评论事实。
2. `video_stat.comment_count` 只统计状态 1 的根和回复；墓碑不计数。
3. 根 `reply_count` 只统计状态 1 的回复。
4. 根热度公式为 `hot_score = like_count * 3 + reply_count * 5`。
5. 根点赞、回复创建/删除和整串治理在同一事务更新相关计数与根热度，所有计数下限为 0。
6. 回复点赞可见但不向上影响根热度。
7. 应用层同时同步评论数缓存，并按实际视频评论增量更新现有视频热榜分数。

### 6.3 可见性和删除

- 根列表、回复列表、讨论串上下文、新建根/回复和评论点赞都要求父视频已发布、公开且媒体就绪。
- 不可读视频统一按 404 处理，缓存或消息深链不能绕过数据库可见性校验。
- viewer 的 `liked` 和 `can_delete` 由服务端计算；匿名值为 false。
- 已有评论的授权删除不依赖父视频仍公开，便于作者和治理人员处理私密、下架或删除视频上的历史评论。
- 根作者墓碑公开投影会清除作者 ID、昵称、头像、正文、点赞状态、删除权限和公开点赞数，但保留活跃回复。

### 6.4 耐久通知 Outbox

- 创建根评论、创建回复和首次点赞他人评论时，互动事实、计数和 Outbox 在同一事务提交。
- 根评论通知视频作者，回复通知直接目标作者，评论点赞通知评论作者；actor=recipient 时不创建事件。
- 根/回复事件 ID 为 `interaction:comment:{comment_id}`；评论点赞为稳定的 `interaction:comment-like:{comment_id}:{actor_id}`。
- Worker 默认每秒轮询，单轮最多处理 50 条；每条使用 30 秒租约和 5 秒投递超时。
- 领取使用行锁和 `SKIP LOCKED`；失败按 1 秒起步指数退避，最长 1 分钟。非法消息目标/类型/长度等错误进入 terminal，瞬时错误继续 pending。
- Worker 只通过 `CommentNotificationMessageWriter` 调用 message Application Service；`user_id + event_id` 和同值幂等键保证重复领取只生成一条消息。

### 6.5 迁移和 reconciliation

- 评论、评论点赞、点赞回执、通知 Outbox 和消息目标列由 API/Worker 共用的 PostgreSQL advisory-locked `AutoMigrate` 创建。
- 既有平面评论天然保持 `root_comment_id=NULL`，继续作为根评论；旧状态 2 与新的 self-deleted 值兼容，原内容、作者、时间、幂等键和总数不丢失。
- 一次性标记 `20260803_threaded_comment_backfill_v1` 补齐空计数、热度和指纹默认值，不为历史评论合成通知。
- 启动迁移随后从正常回复、有效点赞和正常评论事实推导视频评论数、根回复数、评论点赞数与根热度。
- reconciliation 使用物化快照差量叠加到当前值，不绝对覆盖并发写入后的聚合，结果保持非负；重复迁移和 API/Worker 并发启动均安全。

## 7. 幂等设计

写接口读取最长 128 字符的 `Idempotency-Key`；非账号幂等键保留大小写语义。

| 场景 | 处理方式 |
| --- | --- |
| 点赞/收藏状态变更 | 按 payload 绑定 durable receipt；同键同目标返回首次结果，同键相反目标返回 409 |
| 根/回复创建 | `user_id + idempotency_key` 唯一；SHA-256 指纹绑定规范化视频、根、直接目标和正文 |
| 评论点赞 | `user_id + idempotency_key` 回执绑定评论和目标 active 状态，并保存首次计数 |
| 评论删除 | 不使用 payload 回执；软状态转换本身幂等，重复删除返回当前结果且不再扣计数 |

## 8. 错误码

| HTTP 状态 | 场景 | 响应概念 |
| --- | --- | --- |
| 400 | ID、limit、sort、sort/cursor 组合、Unicode 内容或幂等键长度校验失败 | 具体领域错误文本 |
| 401 | 写操作登录态缺失或 Token 失效 | `invalid access token` |
| 403 | 删除权限校验失败 | `comment permission denied` |
| 404 | 视频不可读、评论/回复目标缺失、已自删或已治理 | `resource not found` |
| 409 | 视频行为、评论创建或评论点赞幂等键复用于另一 payload | `idempotency key conflicts with another payload` |
| 500 | 仓储、缓存或内部服务失败 | `internal server error` |

## 9. 测试矩阵

| 范围 | 已覆盖行为 |
| --- | --- |
| Domain/Application | Unicode 1000 code-point 边界、payload 指纹、回复根解析、回复回复扁平化、sort 游标、热度增量和所有删除模式 |
| API flow | 兼容根创建、热门/最新页、跨 sort 拒绝、回复页、3 条预览、可选 viewer、评论点赞、权限、幂等冲突、隐藏视频 |
| PostgreSQL | schema/backfill/index、稳定排序、有界水合查询数、点赞/回复/视频计数、并发差量 reconciliation、治理级联、重复迁移 |
| Message/Outbox | 根/回复/评论点赞事件、自通知抑制、瞬时重试、稳定去重、结构化目标、terminal 错误和 legacy 消息 |
| Frontend state/API | 分页合并去重、sort 切换、展开、草稿隔离、创建重放、乐观点赞回滚、删除、登录态切换和直接 thread context |
| Components/router | 桌面面板、移动 sheet、墓碑、治理确认、回复目标、字符限制、焦点恢复、typed 路由、高亮和不可用讨论 |
| Go 验证 | 定向 interaction/message/migration/Worker 测试、完整 `go test ./...`，以及真实 PostgreSQL threaded 测试均已通过 |

## 10. 前端接入点

| 页面 | 接入能力 |
| --- | --- |
| Feed 视频操作栏 | 调用视频点赞/收藏状态接口，使用返回计数刷新按钮 |
| 个人主页喜欢/收藏 Tab | library 聚合有效行为索引并补齐视频卡片 |
| Feed 详情/评论面板 | Web 默认热门，可切最新；根分页、3 条回复预览、展开/收起和回复继续加载 |
| 评论卡片 | 根/回复点赞采用局部乐观更新，确认或回滚只重绘目标卡片并保持其他线程、滚动、展开、草稿和焦点状态；直接回复标签、服务端权限删除；治理整串前确认，作者自删根显示墓碑 |
| 评论 Composer | 多行输入、每视频草稿、回复目标/取消、Unicode 计数、提交/错误独立状态 |
| 视频讨论页 | typed `/videos/{videoId}?comment={rootId}&highlight={targetId}`，复用视频和评论组件 |
| 消息深链 | 直接加载 thread context、展开根、滚动并短暂高亮目标；不可用时显示安全状态 |
| 桌面窗口适配 | ≥1280px 为 346px 推挤面板，所有更窄桌面窗口统一使用右侧抽屉；不提供移动端底部 sheet |
| 未登录用户 | 可读取公开讨论；回复、点赞和发表操作提供登录入口 |
