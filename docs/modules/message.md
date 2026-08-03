# 消息模块设计

## 1. 模块职责和边界

消息模块负责站内消息持久化、列表、未读数和已读状态。它识别 `LIKE`、`COMMENT`、`COMMENT_REPLY`、`COMMENT_LIKE`、`FOLLOW`、`SYSTEM`，并保存 actor 展示信息与可选结构化讨论目标。

评论事实和通知可靠性仍归 interaction：根评论、回复和首次评论点赞在同一事务写入 `interaction_comment_notification_outbox`；Worker 通过窄 `CommentNotificationMessageWriter` 调用 message Application Service。message 不读取互动表，也不拥有互动 Outbox。

## 2. 接口设计

| 方法 | 路径 | 作用 | 鉴权/幂等 |
| --- | --- | --- | --- |
| GET | `/api/messages` | 按 `created_at DESC, id DESC` 游标分页 | 登录 |
| GET | `/api/message-stats/unread` | 当前用户未读数 | 登录 |
| PATCH | `/api/messages` | 指定 ID 或空数组全部已读 | 登录，状态幂等 |
| POST | `/internal/messages` | 内部事件创建消息 | 服务入口；event/key 去重 |

内部创建 DTO 和公开消息 DTO 均可携带：

| 字段 | 说明 |
| --- | --- |
| `actor_id`, `actor_nickname`, `actor_avatar_url` | 触发用户快照 |
| `video_id` | 讨论所属视频 |
| `root_comment_id` | 应展开的根评论 |
| `comment_id` | 应高亮的根或回复 |

`COMMENT`、`COMMENT_REPLY`、`COMMENT_LIKE` 的新写入必须有三个正 ID；非评论消息可为空。公开 DTO 使用 additive/omitempty 字段，旧客户端可忽略。

## 3. 数据表

### 3.1 `user_message`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id`, `user_id` | BIGINT | 消息和接收用户 |
| `type` | VARCHAR(16) | 六种消息类型 |
| `title`, `content` | VARCHAR(128/1024) | 展示文本 |
| `actor_*` | BIGINT/VARCHAR | 触发用户快照 |
| `video_id`, `comment_id`, `root_comment_id` | BIGINT NULL | 结构化讨论目标 |
| `event_id` | VARCHAR(64) NULL | 业务事件 ID |
| `idempotency_key` | VARCHAR(128) NULL | 内部请求键 |
| `is_read`, `read_at` | BOOL/TIMESTAMPTZ | 已读状态 |
| `created_at` | TIMESTAMPTZ | 创建时间 |

主要索引：

- `uk_user_message_user_event(user_id, event_id)`：Outbox 重投递去重。
- `uk_user_message_user_idempotency(user_id, idempotency_key)`：内部请求去重。
- `idx_user_message_user_read_created(user_id, is_read, created_at)`：未读统计和列表。

同一 event 重放时返回既有消息；若旧行缺失目标且新事件 payload 与既有 event 兼容，可增量补齐空目标列，不覆盖冲突目标。

## 4. 评论通知语义

| 类型 | 接收人 | 内容目标 |
| --- | --- | --- |
| `COMMENT` | 视频作者 | 新根评论，root/comment 均指新根 |
| `COMMENT_REPLY` | 直接回复目标作者 | root 指所属根，comment 指新回复 |
| `COMMENT_LIKE` | 被赞评论作者 | root 指所属根，comment 指被赞评论 |

actor 与 recipient 相同时 interaction 不创建 Outbox，避免自己评论自己的视频、回复自己或点赞自己产生通知。评论点赞事件 ID 固定为评论+actor，取消再点赞不会重复发消息。

## 5. 耐久投递

1. interaction 事务提交评论/点赞事实、计数和 Outbox，消息服务故障不回滚已接受互动。
2. Worker 默认每秒轮询，单轮最多 50 条；逐条使用 30 秒租约和 5 秒写入超时。
3. PostgreSQL 领取按 `available_at, created_at, event_id` 排序并使用 `FOR UPDATE SKIP LOCKED`。
4. 瞬时失败从 1 秒指数退避，最长 1 分钟；非法用户、类型、标题/正文、event/key 长度或讨论目标进入 terminal。
5. message 通过 `user_id + event_id` 及同值 idempotency key 保证重复领取、Worker 重启和响应不确定时只保存一条消息。
6. actor 资料读取失败不会阻断投递，结构化目标和正文快照仍可持久化。

迁移不为历史评论合成消息；既有无目标消息保持可读、可分页、可标已读。

## 6. Web 行为和兼容性

- 消息中心分别渲染“新评论”“新回复”“评论获赞”，使用 comment/reply/heart 图标，并保留未知类型的安全 fallback。
- 激活消息先调用已读接口；成功后，仅当三个结构化 ID 都是安全正整数时导航。
- 导航目标为 typed `/videos/{videoId}?comment={rootCommentId}&highlight={commentId}`。
- 视频讨论页直接加载 thread context、展开根、滚动并短暂高亮目标。
- 目标已删除、被治理或视频不可读时仍保持消息已读，并显示“讨论不可用”，不泄露隐藏内容。
- legacy 消息或缺少目标的消息为只读激活：仍可标已读，不尝试从文本/event ID 猜测跳转。
- 旧文本中缺少 actor 结构时，前端保留现有正文兼容解析；结构化 actor 优先。

## 7. 测试覆盖

已覆盖根评论、回复、评论点赞事件，自通知抑制，租约/瞬时重试/terminal 错误，稳定 event 去重，结构化目标，旧消息补齐与 legacy 读取，消息标签图标，先已读后导航，typed 路由、reload、高亮和不可用目标。
