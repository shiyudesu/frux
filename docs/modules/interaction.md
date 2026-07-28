# 互动模块设计

## 1. 模块职责

互动模块负责用户对视频的点赞、收藏和评论能力，同步维护视频统计表，并向个人内容库提供按行为更新时间排序的有效喜欢/收藏索引。点赞持久化还会维护视频作者的获赞聚合。

模块边界：

| 模块 | 职责 |
| --- | --- |
| `interaction` | 记录点赞、收藏、评论事实，处理状态变更、评论发布和评论删除 |
| `video` | 保存视频主体信息 |
| `video_stat` | 保存 `like_count`、`favorite_count`、`comment_count` 统计字段 |
| `account` | 提供登录用户身份，评论响应展示用户昵称和头像 |
| `library` | 通过 `ActionIndex` 读取有效喜欢/收藏视频 ID，不直接读取互动 GORM 模型 |
| `user_content_stat` | 保存作者 `received_like_count` |

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
| POST | `/api/videos/{videoId}/comments` | 发表评论 | Bearer JWT | 支持 |
| GET | `/api/videos/{videoId}/comments` | 获取评论列表 | 可匿名 | - |
| DELETE | `/api/comments/{commentId}` | 删除评论 | Bearer JWT | 支持 |

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
| Exchange | `gcfeed.interaction` |
| Queue | `gcfeed.interaction.action_changed` |
| Routing key | `interaction.action_changed` |

### 3.4 发表评论

#### POST `/api/videos/{videoId}/comments`

请求头：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `Authorization` | 是 | `Bearer <access_token>` |
| `Idempotency-Key` | 否 | 客户端幂等键，最长 128 |

请求体：

```json
{
  "content": "这个剪辑节奏很好"
}
```

响应：

```json
{
  "id": 3001,
  "video_id": 1001,
  "user_id": 12,
  "user_nickname": "tester",
  "user_avatar_url": "https://example.com/avatar.png",
  "content": "这个剪辑节奏很好",
  "created_at": "2026-05-04T12:00:00Z",
  "comment_count": 4
}
```

### 3.5 评论列表

#### GET `/api/videos/{videoId}/comments`

匿名评论列表仅在父视频同时满足 `status=published` 和 `visibility=public` 时返回；私密、下架、删除或不存在的视频统一返回 404，不返回历史评论内容。评论作者、视频作者和管理员原有的评论删除权限不受父视频后续状态变化影响。

请求参数：

| 参数 | 位置 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `cursor` | query | string | 否 | - | 上一页返回的游标 |
| `limit` | query | int | 否 | 20 | 返回数量，最大 100 |

响应：

```json
{
  "items": [
    {
      "id": 3001,
      "video_id": 1001,
      "user_id": 12,
      "user_nickname": "tester",
      "user_avatar_url": "https://example.com/avatar.png",
      "content": "这个剪辑节奏很好",
      "created_at": "2026-05-04T12:00:00Z"
    }
  ],
  "next_cursor": "eyJjcmVhdGVkX2F0IjoiMjAyNi0wNS0wNFQxMjowMDowMFoiLCJjb21tZW50X2lkIjozMDAxfQ",
  "has_more": true
}
```

排序规则：

| 排序字段 | 方向 | 说明 |
| --- | --- | --- |
| `created_at` | DESC | 新评论靠前 |
| `id` | DESC | 同一创建时间下按评论ID倒序 |

游标内容：

| 字段 | 说明 |
| --- | --- |
| `created_at` | 当前页最后一条评论的创建时间 |
| `comment_id` | 当前页最后一条评论ID |

### 3.6 删除评论

#### DELETE `/api/comments/{commentId}`

请求头：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `Authorization` | 是 | `Bearer <access_token>` |
| `Idempotency-Key` | 否 | 客户端幂等键，最长 128 |

响应：

```json
{
  "comment_id": 3001,
  "status": 2,
  "comment_count": 3
}
```

权限规则：

| 用户身份 | 权限 |
| --- | --- |
| 评论作者 | 可删除自己的评论 |
| 视频作者 | 可删除自己视频下的评论 |
| 运营角色 | 可删除任意评论 |

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

`interaction_comment` 保存评论内容，删除采用状态更新。

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | BIGINT | PK | 评论ID |
| `video_id` | BIGINT | NOT NULL | 视频ID |
| `user_id` | BIGINT | NOT NULL | 评论用户 |
| `content` | VARCHAR(1000) | NOT NULL | 评论内容 |
| `status` | TINYINT | NOT NULL, DEFAULT 1 | 1正常/2删除 |
| `idempotency_key` | VARCHAR(128) | NULLABLE | 创建评论幂等键 |
| `created_at` | DATETIME | NOT NULL | 创建时间 |
| `updated_at` | DATETIME | NOT NULL | 更新时间 |

索引建议：

| 索引 | 字段 | 说明 |
| --- | --- | --- |
| `idx_video_status_created` | `video_id, status, created_at, id` | 支持评论列表游标分页 |
| `idx_user_created` | `user_id, created_at` | 支持用户评论历史 |
| `uk_user_idempotency` | `user_id, idempotency_key` | 支持评论创建幂等 |

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
| `2` | 删除 |

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

### 6.2 评论创建

处理流程：

1. 校验登录用户、`video_id` 和 `content`。
2. 查询视频，只有已发布公开状态时允许评论。
3. 创建 `interaction_comment`，状态为 `1`。
4. 在同一事务内将 `video_stat.comment_count` 加 1。
5. 返回评论详情和最新评论数。

内容约束：

| 字段 | 规则 |
| --- | --- |
| `content` | 去除首尾空白后长度为 1 到 1000 |

### 6.3 评论删除

处理流程：

1. 校验登录用户和 `commentId`。
2. 查询评论和所属视频。
3. 校验删除权限。
4. 评论状态为 `1` 时更新为 `2`，并将 `video_stat.comment_count` 更新为 `max(comment_count - 1, 0)`。
5. 评论状态为 `2` 时直接返回当前结果。

## 7. 幂等设计

写接口读取请求头 `Idempotency-Key`。同一用户、同一接口、同一幂等键命中时返回首次执行结果。

当前实现建议：

| 场景 | 处理方式 |
| --- | --- |
| 点赞/收藏状态变更 | 按 payload 绑定 durable receipt；同键同目标返回首次结果，同键相反目标返回 409 |
| 评论创建 | 使用 `uk_user_idempotency(user_id, idempotency_key)` 返回已创建评论 |
| 评论删除 | 删除操作本身幂等，重复删除返回当前删除结果 |

## 8. 错误码建议

| HTTP 状态 | 场景 | 响应 |
| --- | --- | --- |
| 400 | `video_id`、`commentId`、`limit`、`cursor` 或 `content` 校验失败 | `{"error":"invalid request"}` |
| 401 | 登录态缺失或 Token 失效 | `{"error":"invalid access token"}` |
| 403 | 删除评论权限校验失败 | `{"error":"comment permission denied"}` |
| 404 | 视频或评论记录缺失 | `{"error":"resource not found"}` |
| 409 | 点赞/收藏幂等键复用于相反目标状态 | `{"error":"idempotency key conflicts with another payload"}` |
| 500 | 服务内部错误 | `{"error":"internal server error"}` |

## 9. 测试用例

| 用例 | 预期 |
| --- | --- |
| 登录用户首次点赞公开视频 | 返回 `active=true`，`like_count + 1` |
| 登录用户取消点赞同一视频 | 返回 `active=false`，`like_count - 1` |
| 登录用户首次收藏公开视频 | 返回 `active=true`，`favorite_count + 1` |
| 登录用户取消收藏同一视频 | 返回 `active=false`，`favorite_count - 1` |
| 点赞状态真实变化 | 作者 `received_like_count` 同步变化且不小于 0 |
| 查询有效喜欢/收藏索引 | 按 `updated_at, video_id` 稳定倒序，取消行为不返回 |
| 对私密视频互动 | 按视频不存在处理，不泄露内容 |
| 登录用户发表评论 | 返回评论详情，`comment_count + 1` |
| 匿名用户查询评论列表 | 返回状态为正常的评论，按创建时间倒序 |
| 视频转为私密、下架或删除后查询评论列表 | 返回 404 且不返回评论；恢复已发布公开后评论重新可见 |
| 评论作者删除评论 | 返回 `status=2`，`comment_count - 1` |
| 视频作者删除视频下评论 | 返回 `status=2`，`comment_count - 1` |
| 普通用户删除他人评论 | 返回 403 |
| 重复删除同一评论 | 返回 `status=2`，评论计数保持稳定 |
| 新取消事件先于旧点赞事件落库 | 保持取消状态，视频和作者获赞计数不回增 |
| 并发 Worker 收到点赞/取消点赞 | Redis 版本较大者确定最终状态，即使时间戳更早 |
| 相同版本和发生时间的兼容事件 | `event_id` 较大者确定最终状态 |
| 发布失败且同步落库失败后重试 | 原版本被条件回滚或由更高版本覆盖；重试仍会发布可持久化事件 |
| 发布确认不确定 | 同步落库与后续重复投递只产生一次事实和聚合变化 |
| Redis 事务提交后计数读取失败 | 脱离请求取消做有界条件回滚；回滚失败时原事件进入确认投递或同步持久化恢复路径 |
| 计数读取失败时发生并发更高版本 | 旧请求不回滚、不发布旧版本，新版本保持最终状态 |
| 旧事件或相同事件重复投递 | 成功确认，行为与所有聚合保持不变 |

## 10. 前端接入点

| 页面 | 接入能力 |
| --- | --- |
| Feed 视频右侧操作栏 | 调用点赞和收藏状态接口，使用接口返回计数刷新按钮文案 |
| 个人主页喜欢/收藏 Tab | 由 library 模块聚合本模块的有效行为索引并补齐视频卡片 |
| Feed 评论抽屉 | 按当前 `video_id` 拉取评论列表 |
| 评论输入框 | 登录用户提交评论，成功后插入列表顶部 |
| 未登录用户互动 | 引导到登录页 |
