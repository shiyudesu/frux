# 私信（Chat）模块设计

## 1. 模块职责和边界

`chat` 是用户主动发送内容的私信域，负责一对一会话、会话成员、文本/视频卡片消息、已读进度和私信未读数。它与 `message` 的事件通知域分开：

| 域 | 持久化 | 内容来源 | 入口 |
| --- | --- | --- | --- |
| `chat` | `chat_conversation`、`chat_conversation_member`、`chat_message` | 用户主动发送的文本或 Frux 视频引用 | `/api/chat/*` |
| `message` | `user_message` | 点赞、评论、关注、账号和视频生命周期事件 | `/api/messages` |

私信不会写入 `user_message`，通知也不会创建聊天会话。`/messages` 仍以通知视图为默认入口，私信视图在同一工作区中独立呈现。

当前版本只支持正常消费端账号之间的 1:1 私信。陌生人消息、群聊、附件、语音、贴纸、反应、输入状态、在线状态、撤回、编辑、消息搜索、端到端加密和 WebSocket 推送不在本模块范围内。

## 2. 资格与授权

新建会话和每次发送都重新检查当前事实：

1. 请求必须有认证的 consumer JWT。
2. 发送者和目标必须是不同的正常 `role=user` 账号；冻结、删除、非消费端或不存在的账号按不可用处理。
3. 发送者和目标必须互相关注。关系模块通过一次互关查询提供授权能力，客户端不能通过拼接关注/粉丝列表自行推断资格。
4. 发送者必须是会话成员。

取关不会删除既有会话或历史。双方仍可读取已有历史，但后续创建/发送返回 `CHAT_NOT_ELIGIBLE`；恢复互关后可继续使用同一会话。自己的用户 ID、单向关注、不可用账号分别返回稳定的 eligibility reason 或 API error code。

## 3. HTTP API

所有接口均要求 consumer JWT；分页 `limit` 默认 20、最大 100，超限会裁剪，非正值由 Handler 拒绝或使用默认值。

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| GET | `/api/chat/users/{targetUserId}/eligibility` | 返回 `eligible`、封闭 `reason` 和已存在的会话 ID |
| GET | `/api/chat/recipients` | 查询当前互关、正常账号的可私信收件人 |
| GET | `/api/chat/conversations` | 查询当前用户的非空会话 |
| POST | `/api/chat/conversations` | 校验资格并创建或返回规范化用户对会话 |
| GET | `/api/chat/conversations/{conversationId}/messages` | 读取历史；可带 `cursor` 或 `after_message_id` |
| POST | `/api/chat/conversations/{conversationId}/messages` | 发送 `TEXT` 或 `VIDEO` 消息 |
| PATCH | `/api/chat/conversations/{conversationId}/read` | 提交单调的 `through_message_id` |
| GET | `/api/inbox-stats/unread` | 返回通知、私信和总未读数 |

历史响应始终包含当前认证成员可访问的 `conversation` 快照和当前 `eligibility`，即使会话为空、普通会话列表不会返回该会话。这两个字段让 `/messages/{conversationId}` 在空会话或直接 reload 时仍能显示对方、未读数和 composer 是否可用；`items` 仍是消息数组，增量请求也返回同一元数据。发送响应形如 `{ "message": { ... }, "created": true|false }`，重放时 `created=false`。

空会话历史的最小响应仍保持完整外层结构：

```json
{
  "items": [],
  "has_more": false,
  "conversation": {
    "id": 8,
    "counterpart": { "user_id": 2, "nickname": "Alice", "available": true },
    "last_message_id": 0,
    "unread_count": 0
  },
  "eligibility": {
    "eligible": true,
    "reason": "ELIGIBLE",
    "conversation_id": 8
  }
}
```

创建会话依靠用户对唯一约束天然幂等；前端可以发送操作键，但服务端不把会话创建当作消息幂等写入。发送接口必须带 `Idempotency-Key`，服务端 trim 后长度上限 128 个字符，并按 `(sender_id, idempotency_key)` 唯一。发送服务先按发送者和键查找已提交消息，再执行当前会话成员、账号、互关或视频可见性检查：相同会话、类型和规范化内容的重试返回原消息（`created=false`），即使之后账号被冻结、取关或视频不可读；视频会按当前可见性 hydration，必要时返回 unavailable tombstone。同键不同会话、类型、文本或视频 ID 返回 `CHAT_IDEMPOTENCY_CONFLICT`，不会创建新消息。

错误使用封闭的 `CHAT_*` code，包括参数校验、游标无效、自己会话、互关不足、账号不可用、会话不存在、消息不存在、视频不可用和幂等冲突。发送路径受 `chat_send` 分层限流保护：按用户识别，正常 profile 为本地 token bucket 30、分布式 Redis 60（每秒补充 0.5/1），紧急 profile 为 10/20；Redis 不可用时 fail-closed。

## 4. 消息规则

### 4.1 文本

- `kind` 固定为 `TEXT`，请求只接受 `text`，拒绝未知字段和尾随 JSON。
- 服务端 trim 空白；空字符串拒绝。
- 上限为 2,000 个 Unicode code point，且请求中的 UTF-8 文本不超过 8 KiB。
- 消息创建、会话最后消息摘要、接收方 unread 增量在同一个 PostgreSQL 事务提交。

### 4.2 视频卡片

- `kind` 固定为 `VIDEO`，请求只接受正数 `video_id`。
- 客户端不能提交或覆盖标题、作者、封面、媒体 URL 或任意媒体 metadata。
- 发送时要求视频已发布、公开且 media-ready；消息只保存 `video_id`。
- 读取历史时按页去重视频 ID 并批量读取当前公开视频卡片。
- 视频变私密、删除、下架、处理中或不存在时返回 `available=false` 的 tombstone，不返回受保护 URL 或旧的卡片快照。

### 4.3 会话摘要

空会话可以通过 profile 操作或分享流程被寻址，但 `last_message_id IS NULL` 的会话不会出现在普通会话列表。列表摘要只显示当前消息类型的安全预览：文本为规范化文本，视频显示“视频”。

## 5. 数据模型和迁移

### 5.1 `chat_conversation`

| 字段 | 说明 |
| --- | --- |
| `id` | 全局递增会话 ID |
| `lower_user_id`, `higher_user_id` | 两个用户 ID 的规范化顺序，唯一约束 |
| `last_message_id`, `last_message_at` | 会话排序和摘要；空会话为空 |
| `created_at`, `updated_at` | 服务端时间 |

### 5.2 `chat_conversation_member`

主键为 `(conversation_id, user_id)`，每个会话恰好创建两行。字段包括 `last_read_message_id`、`last_read_at`、`unread_count`，以及未来 mute/hide 用的 nullable 保留字段。当前版本不提供 mute/hide 控制。

### 5.3 `chat_message`

字段包括全局递增 `id`、`conversation_id`、`sender_id`、封闭 `kind`（`TEXT`/`VIDEO`）、规范化 `text`、nullable `video_id`、必填 `idempotency_key`、未来撤回用 `revoked_at` 和 `created_at`。文本消息不保存视频 ID，视频消息不保存文本。

迁移在共享 advisory-lock 事务中 `AutoMigrate` 三张表并显式确保以下索引：

- `(lower_user_id, higher_user_id)` 唯一 pair；
- 会话 `(last_message_id DESC, id DESC)`；
- 成员 `(conversation_id, user_id)` 唯一以及 `(user_id, unread_count)`；
- `(sender_id, idempotency_key)` 唯一；
- `(conversation_id, id DESC)` 历史顺序；
- `video_id` 查询索引。

本 change 不修改、不回填 `user_message`，也不从历史通知自动合成聊天会话。PostgreSQL 是私信事实、未读和恢复的唯一权威；Redis、Kafka 和对象存储都不是发送提交依赖。

## 6. 分页、已读和未读

- 会话列表按 `(last_message_id DESC, conversation_id DESC)`，游标绑定两个字段。
- 历史按 `message_id DESC` 返回，游标绑定会话 ID；Web 合并后按 ID 正序展示。
- `after_message_id` 只用于活动会话增量刷新，不能和历史 cursor 同时使用。
- 收件人按 `followed_at DESC, user_id DESC` 稳定分页；`q` trim/lowercase 后最多 64 个 Unicode code point，cursor 绑定规范化 query。
- 所有 cursor 使用版本字段和 URL-safe Base64；跨会话、跨 query 或版本不匹配会返回 `CHAT_CURSOR_INVALID`。

发送只增加接收成员的 `unread_count`。`PATCH /read` 锁定成员，校验 through message 属于当前会话，拒绝倒退，并按“当前用户收到且 ID 大于 read boundary 的消息”重算剩余未读。重复或较旧的 read 请求不改变状态。

`/api/message-stats/unread` 继续只统计通知。`/api/inbox-stats/unread` 返回：

```json
{
  "notification_unread_count": 2,
  "chat_unread_count": 3,
  "total_unread_count": 5
}
```

导航徽标使用 total；通知页和私信页分别使用各自计数。通知全部已读不会清除私信未读。

## 7. Web 工作区、轮询和分享

`/messages` 默认打开通知页，页内 Tab 切换“通知”和“私信”；`/messages/{conversationId}` 通过手写 History router 校验正整数并直接打开指定会话，非法路径进入安全 not-found，不发起无效请求。

私信工作区包括：

- 可搜索、可分页的会话列：当前对方资料、最后消息摘要、时间和 unread badge；
- 会话头部、按日期分组的历史、较早消息加载和文本 composer；
- 发送忙碌、错误、空状态、同步降级、账号不可用和互关失效状态；
- 视频消息使用当前公开视频卡片；不可读视频显示 tombstone；
- 宽屏并列列表/详情；窄屏桌面在列表和详情之间切换，保留 Frux 72px rail、Escape/back 和 viewport-safe scrolling。

当前同步方式是可见性和路由感知的 HTTP polling，不是 WebSocket：

- 活动消息工作区可见时立即刷新会话列表和当前会话，随后默认每 5 秒刷新；
- 当前历史使用 `after_message_id` 增量读取；
- 连续瞬时失败按 10/20/30 秒有界退避，成功后恢复 5 秒；
- `document.visibilityState=hidden`、路由离开或账号变化时暂停并清理 timer；
- generation/request guard 防止旧账号或旧会话响应覆盖当前状态；
- 发送和已读成功后立即本地 reconcile，不等待下一次轮询。

Feed、视频详情和稍后再看队列的分享动作打开同一个 `PrivateShareDialog`。它只调用 `/api/chat/recipients`，支持昵称过滤、稳定分页和单选；确认后用稳定会话/消息操作键创建会话并发送一个 `VIDEO`。不确定响应重试复用操作键，成功后轮换；成功提示允许继续浏览或显式打开会话，不中断原播放上下文。对话框支持 Escape、关闭按钮和焦点返回；未登录用户进入登录流程。

对话框使用共享的焦点栈：嵌套时只有最上层 dialog 响应 Escape，先关闭分享窗口，再由下一次 Escape 关闭外层播放窗口；关闭后焦点返回各自的触发控件。发送进行中保持所选视频和收件人不变，成功后通过显式按钮打开会话。

公开主页独立加载 eligibility：自己不显示私信动作；互关时显示可用按钮；不符合资格时显示“需要互相关注后才能私信”，而不是把失败延迟到 composer。

## 8. 隐私、观测和安全边界

- 日志、Prometheus labels 和应用观测不写入消息正文、昵称、会话 ID、视频 ID、媒体 URL；chat metrics 只使用 operation、`TEXT`/`VIDEO`、outcome、error class 和 bounded latency。
- 当前 HTTP DTO 为前端路由、去重和卡片 hydration 携带正数 `user_id`/`video_id` 字段；这些协议字段不得复制到日志、指标、trace 或 analytics。不可用 participant/video 只返回 `available=false` 和最小 tombstone。
- 服务端始终按当前账号、关系和视频可读性重检，不能相信客户端提供的展示字段或 eligibility 快照。
- 发送请求受用户维度分层限流和严格 JSON body 上限保护；限流器不可用时明确拒绝，而不是无限放行。
- 消息正文进入 PostgreSQL 及其备份，不进入 `user_message`、Kafka event、搜索索引或 Redis 内容缓存。长期留存、导出、删除和法律保留策略尚待单独设计。

## 9. 测试边界

当前实现的验证边界包括：

- `internal/domain/chat/entity_test.go`：规范化 pair、文本限制、消息类型不变量和单调 read；
- `internal/application/chat/service_test.go`：互关授权、通知/私信未读分离、cursor 绑定、空会话历史元数据、不可用 participant，以及在可变账号/关系/视频状态变化后先解析已提交消息的幂等重放；
- `internal/infra/persistence/migration/chat_postgres_test.go`：PostgreSQL 并发建会话、同键并发发送、重复响应、冲突、未读、已读和 reconciliation（仅在显式 `FRUX_POSTGRES_TEST_DSN` 下运行；本次验证未设置 DSN，因此跳过，未启动 PostgreSQL 服务）；
- `apps/api/test/chat_api_test.go`：eligibility、会话、严格 body、发送重放、历史和 inbox summary API flow；
- Web 的 `api/chat.test.ts`、`chatOperations.test.ts`、`router.test.tsx`、`hooks/useChatHistory.test.tsx`、`components/ChatWorkspace.test.tsx`、`components/PrivateShareDialog.test.tsx`、`pages/PublicProfilePage.test.tsx`、`hooks/useDialogFocus.test.tsx` 和兼容的 `pages/MessagesPage.test.tsx`：请求 payload、响应/路由安全、空会话元数据、历史代际隔离、工作区、profile、分享、操作键和最上层 Escape 行为。完整 Vitest 与严格 TypeScript/Vite build 也已通过。

集成测试不在默认门禁中启动 PostgreSQL/Redis/Kafka；HTTP API 流程使用 Hertz in-process 测试，Web 使用 Vitest。真实运行时不依赖 WebSocket、Kafka 或 Redis 才能接受一次私信。

## 10. 延后能力

后续 change 才能增加陌生人消息请求、block/report、群聊、附件/图片/文件、语音、贴纸、反应、输入状态、在线状态、回执、撤回/编辑、本地删除、消息搜索、导出/删除/留存策略、E2E 加密和 WebSocket 唤醒。若未来增加 WebSocket，它只能作为 committed message 的唤醒提示，HTTP 仍负责权威读取、游标和恢复。
