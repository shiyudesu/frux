# 视频 Feed 系统架构图（MVP）

本文按 `mermaid-diagrams` skill 重构：每张图只表达一个概念，节点保持克制，连接线带语义标签，图前给出用途说明。当前实现由 Go API 与 Worker 共同承载账户、视频、Feed、互动、曝光、个人内容库和消息能力。

## 1. 系统上下文

这张图展示 Frux 与客户端、存储和演进型基础设施的边界。

```mermaid
---
config:
  theme: base
  layout: dagre
  themeVariables:
    background: transparent
    fontFamily: Inter, PingFang SC, Microsoft YaHei, Arial
    primaryTextColor: "#0F172A"
    lineColor: "#94A3B8"
    clusterBkg: "#F8FAFC"
    clusterBorder: "#CBD5E1"
---
flowchart LR
  %% Frux MVP system context
  classDef client fill:#EFF6FF,stroke:#60A5FA,color:#0F172A,stroke-width:1px;
  classDef system fill:#DCFCE7,stroke:#22C55E,color:#0F172A,stroke-width:1px;
  classDef store fill:#F1F5F9,stroke:#64748B,color:#0F172A,stroke-width:1px;
  classDef service fill:#FAE8FF,stroke:#C084FC,color:#0F172A,stroke-width:1px;
  classDef future fill:#FFFFFF,stroke:#94A3B8,color:#475569,stroke-width:1px,stroke-dasharray:5 5;

  Web["Web App<br/>React + Vite"]
  Client["移动端 / API 调用方"]
  API["Frux API<br/>Go + Hertz"]
  PostgreSQL[("PostgreSQL<br/>业务数据")]
  Uploads[("uploads<br/>视频 / 封面 / 头像")]
  Redis[("Redis<br/>缓存与计数")]
  RabbitMQ[("RabbitMQ<br/>异步任务与行为回滚")]
  Kafka[("Kafka<br/>Action / View 保留事件流")]
  ObjectStorage[("对象存储<br/>媒体文件")]

  Web -->|"调用管理与浏览接口"| API
  Client -->|"调用公开 API"| API
  API -->|"读写业务事实、投影和聚合"| PostgreSQL
  API -->|"保存和读取本地文件"| Uploads
  API -->|"缓存 Feed、互动状态与计数；原子协调部分限流"| Redis
  API -->|"投递视频、媒体等任务；保留行为 mirror/rollback"| RabbitMQ
  API -->|"投递 action_changed；Worker 投递 view_event_recorded"| Kafka
  API -.->|"迁移媒体文件"| ObjectStorage

  class Web,Client client;
  class API system;
  class PostgreSQL,Uploads store;
  class Redis,RabbitMQ,Kafka service;
  class ObjectStorage future;
  linkStyle default stroke:#94A3B8,stroke-width:1.4px
```

## 2. API 内部分层

这张图展示 Go API 单体内部的主要代码层次和依赖方向。

```mermaid
---
config:
  theme: base
  layout: dagre
  themeVariables:
    background: transparent
    fontFamily: Inter, PingFang SC, Microsoft YaHei, Arial
    primaryTextColor: "#0F172A"
    lineColor: "#94A3B8"
    clusterBkg: "#F8FAFC"
    clusterBorder: "#CBD5E1"
---
flowchart LR
  %% Internal layering in apps/api
  classDef entry fill:#F8FAFC,stroke:#94A3B8,color:#0F172A,stroke-width:1px;
  classDef http fill:#DBEAFE,stroke:#3B82F6,color:#0F172A,stroke-width:1px;
  classDef service fill:#DCFCE7,stroke:#22C55E,color:#0F172A,stroke-width:1px;
  classDef domain fill:#CCFBF1,stroke:#14B8A6,color:#0F172A,stroke-width:1px;
  classDef infra fill:#FFEDD5,stroke:#F97316,color:#0F172A,stroke-width:1px;
  classDef store fill:#F1F5F9,stroke:#64748B,color:#0F172A,stroke-width:1px;

  Entry["cmd/feed/main.go<br/>启动装配"]
  Router["Router<br/>路由注册"]
  Auth["JWT Middleware<br/>身份上下文"]
  AdminAuth["Admin Permission Middleware<br/>当前账号状态与角色"]
  Audit["Admin Audit<br/>不可变操作事实"]
  Handlers["HTTP Handlers<br/>account / video / feed / interaction / exposure / library"]
  Services["Application Services<br/>账户资料 / 创作者管理 / 个人内容库"]
  Domains["Domain Models<br/>account / video / interaction / exposure / library"]
  Config["Config Loader<br/>configs/config.yaml"]
  JWT["JWT Manager<br/>签发访问令牌"]
  Repo["GORM Repository<br/>仓储实现"]
  SQL["database/sql<br/>PostgreSQL 连接"]
  PostgreSQL[("PostgreSQL")]
  Uploads[("uploads 目录")]

  Entry -->|"加载配置"| Config
  Entry -->|"创建数据库连接"| SQL
  Entry -->|"注册 HTTP 路由"| Router
  Router -->|"校验受保护接口"| Auth
  Auth -->|"解析和签名 Token"| JWT
  Auth -->|"/api/admin 当前身份"| AdminAuth
  AdminAuth -->|"读取当前主体"| Repo
  AdminAuth -->|"最佳努力记录拒绝"| Audit
  AdminAuth -->|"权限通过"| Handlers
  Router -->|"分发普通业务请求"| Handlers
  Handlers -->|"调用用例服务"| Services
  Services -->|"执行领域规则"| Domains
  Services -->|"读写仓储"| Repo
  Services -->|"构建成功审计事实"| Audit
  Audit -->|"与受保护变更同事务追加"| Repo
  Repo -->|"复用连接池"| SQL
  SQL -->|"持久化数据"| PostgreSQL
  Handlers -->|"保存上传文件"| Uploads
  Router -->|"授权后交给 Range 文件服务"| Uploads

  class Entry entry;
  class Router,Auth,AdminAuth,Handlers http;
  class Services,Audit service;
  class Domains domain;
  class Config,JWT,Repo,SQL infra;
  class PostgreSQL,Uploads store;
  linkStyle default stroke:#94A3B8,stroke-width:1.4px
```

请求限流是 Router 组合的横切边界：JWT middleware 先建立 server-derived user identity，
随后 registered rate-limit middleware 执行 bounded local bucket；只有声明为 distributed 的
group 才通过 infrastructure adapter 执行单次 Redis Lua。Redis 失败按 policy 使用更严格
local fallback 或 fail closed，Handler 不拥有私有限流器。

RabbitMQ 恢复同样是基础设施与控制面的组合边界：业务 Consumer 返回 terminal/retryable 分类，
Broker 的 versioned Quorum Source 用 Delivery Limit 将毒消息送入 per-consumer DLQ。API 通过
Management Adapter 提供脱敏摘要/Preview；Replay Service 验证 allowlist 路由，保持原 Event ID
和 Payload、增加 Replay ID，等待 Publisher Confirm 并提交 Audit Fact 后才 Ack DLQ。PostgreSQL
不保存 Payload Queue。

Kafka 是并行存在的事件流基础，不是 RabbitMQ 的重命名适配层。代码注册 Topic、Partition Key、
Producer 和 Consumer Group；franz-go Producer 使用 idempotence + `acks=all`，Consumer 禁用自动
提交并在耐久结果后提交 Offset。`action_changed` 与 `view_event_recorded` 已接入独立 Topic、
active/shadow Group 和 per-stream migration mode。视频首次公开事实现在通过
`video_publication_event_outbox -> frux.video.published.v1`，Feed 与 embedding 各自维护 Offset；
媒体仍由 PostgreSQL job 决定正确性，Kafka command 只负责唤醒。

## 3. 核心请求链路

这张图展示从注册、登录、上传、发布到刷 Feed 的 MVP 主链路。

```mermaid
---
config:
  theme: base
  themeVariables:
    background: transparent
    fontFamily: Inter, PingFang SC, Microsoft YaHei, Arial
    primaryTextColor: "#0F172A"
    actorBkg: "#EFF6FF"
    actorBorder: "#60A5FA"
    actorTextColor: "#0F172A"
    activationBkgColor: "#DCFCE7"
    activationBorderColor: "#22C55E"
    sequenceNumberColor: "#475569"
    lineColor: "#94A3B8"
---
sequenceDiagram
  autonumber
  participant C as Client
  participant R as Hertz Router
  participant H as Handler
  participant S as Service
  participant Repo as GORM Repository
  participant DB as PostgreSQL
  participant Redis as Redis
  participant FS as uploads

  C->>R: POST /api/users
  R->>H: Account.Register
  H->>S: 创建账户
  S->>Repo: Save account
  Repo->>DB: INSERT account
  DB-->>Repo: 返回 user id
  Repo-->>S: 返回用户实体
  S-->>H: 返回 profile
  H-->>C: 201 Created

  C->>R: POST /api/sessions
  R->>H: Account.Login
  H->>S: 校验账号密码
  S->>Repo: FindByAccount
  Repo->>DB: SELECT account
  DB-->>Repo: 返回用户记录
  S-->>H: 返回 Bearer JWT
  H-->>C: 返回 access_token

  C->>R: POST /api/uploads
  R->>H: Upload.Create
  H->>FS: 写入 video / cover / avatar
  H->>Repo: 记录保护资产不可变 owner
  Repo->>DB: INSERT local_upload_asset
  FS-->>H: 返回文件路径
  H-->>C: 返回 /uploads/{kind}/{filename}

  C->>R: GET /uploads/video/{filename}
  R->>Auth: 可选 Bearer / 资产 Cookie
  Auth->>S: 校验资产所有权、媒体引用与可读性
  Note over R,Auth: Cookie 身份同时要求 HttpOnly Token 与 Web 活跃标记；Authorization 不要求标记
  S->>Repo: 查询 local_upload_asset 和 media_url / cover_url 引用
  Repo->>DB: SELECT owner/author/status/visibility
  alt 审核已通过、已发布且公开
    R->>FS: 匿名 Range/HEAD 读取
  else 作者读取非删除作品
    R->>FS: 私有 Range/HEAD 读取
  else 删除、他人私密或未引用保护文件
    R-->>C: 404
  end

  C->>R: POST /api/videos
  R->>H: Video.Create
  H->>S: 创建 Pending Review 视频
  S->>Repo: Save video and video_stat
  Repo->>DB: INSERT video, video_stat
  DB-->>Repo: 返回 video id
  Repo-->>S: 返回视频实体
  S-->>H: 返回待审视频详情
  H-->>C: 201 Created

  C->>R: GET /api/feed-items?scene=timeline&cursor=...&limit=...
  R->>H: Feed.ListFeedItems
  H->>S: GetFeed(scene=timeline)
  S->>Repo: ListTimelinePage
  Repo->>DB: SELECT video_id, published_at
  DB-->>Repo: 返回轻量页
  S->>Redis: MGET video:card / video:stat
  S->>Repo: BatchGetFeedCards / BatchGetFeedStats
  Repo->>DB: 批量回源缺失卡片和计数
  Repo-->>S: 返回缺失数据
  S-->>H: 返回 items + next_cursor + has_more
  H-->>C: 返回 timeline feed items

  C->>R: PUT /api/videos/{videoId}/like
  R->>H: Interaction.Like
  H->>S: 校验当前 published + public
  S->>Redis: 原子更新状态、计数和单调 action version
  S->>MQ: 按迁移模式发布 ActionChangedEvent；dual 模式等待两种传输确认
  alt 任一所需传输失败或确认不确定
    S->>Repo: 同步持久化同一版本事件
    alt 同步持久化也失败
      S->>Redis: 仅当前版本未被更新时条件回滚
    end
  end
  MQ->>Worker: Kafka active Group 或 Rabbit rollback Consumer 投递
  Worker->>Repo: PersistAcceptedActionEvent
  Repo->>DB: 按 event_id 去重并优先应用最大的 version
  Repo->>DB: 同版本仅用 occurred_at + event_id 兼容定序
  Repo->>DB: 原子更新互动、video_stat、received_like_count
  S->>Redis: ZINCRBY feed:hot:minute:v1:{minute}

  C->>R: GET /api/feed-items?scene=hot&limit=10
  R->>H: Feed.ListFeedItems
  H->>S: GetFeed(scene=hot)
  S->>Redis: ZUNIONSTORE 最近 60 个分钟桶
  S->>Redis: ZREVRANGE 热榜窗口
  S-->>H: 返回一小时热榜 items + cursor
  H-->>C: 返回 hot feed items
```

### 3.1 个人主页内容链路

这张图展示个人主页如何保持领域所有权，并在 `library` Application Service 中聚合喜欢、收藏、观看历史和稍后再看。

```mermaid
sequenceDiagram
  autonumber
  participant Web as ProfilePage
  participant Router as Router
  participant Library as library Service
  participant Action as interaction ActionIndex
  participant History as exposure HistoryIndex
  participant Later as WatchLater Repository
  participant Catalog as video VideoCatalog
  participant DB as PostgreSQL

  Web->>Router: GET /api/users/me/liked-videos?cursor=...
  Router->>Library: ListLiked(userID, cursor, limit)
  Library->>Action: 按 updated_at, video_id 读取有效 LIKE
  Action->>DB: SELECT interaction_action
  Library->>Catalog: 按有序 ID 批量补齐可读视频
  Catalog->>DB: SELECT published + public/owner-private videos
  Library-->>Web: items + next_cursor + has_more

  Web->>Router: GET /api/users/me/watch-history
  Router->>Library: ListHistory(userID, cursor, limit)
  Library->>History: 按 last_watched_at, video_id 读取投影
  History->>DB: SELECT video_view_history
  Library->>Catalog: 过滤删除、下架和不可读私密视频
  Library-->>Web: 视频卡片 + history metadata

  Web->>Router: PUT /api/videos/{videoId}/watch-later
  Router->>Library: SetWatchLater(active=true)
  Library->>Catalog: 验证当前用户可读
  Library->>Later: upsert active 状态
  Later->>DB: INSERT/UPDATE user_watch_later
```

跨模块能力通过 `interfaces/http/router/library_adapters.go` 适配 Domain 窄接口；HTTP Handler 不直接组合多个仓储。

## 4. 数据模型

### 4.1 上下文推荐链路

推荐 Feed 的 `context` 经 HTTP 有界归一化后进入 Recommendation Service。服务并发调用
fresh、hot、内容相似、followed-author、session-continuation Provider，按策略 budget/deadline
合并并重新验证可见性；失败 Provider 只标记 degraded。策略和画像存于 PostgreSQL，
Redis 仅保存短期有序 snapshot，带签名的 cursor 绑定用户、scene、request ID 和 policy version。

观看/互动/关系的耐久事实分别通过 View Event、Action 和 Follow Outbox 投影为画像；投影延迟
不会阻塞原接口。采样 `recommendation_request_log` 保存有界请求和候选解释，`recommendation_outcome`
以 request ID 关联曝光、播放、进度、完播、跳过和反馈，供离线评估。Redis、embedding 或单个
Provider 不可用时保留当前可用召回或 deterministic cursor，不将外部优化组件作为 HTTP 事实提交条件。

这张图聚焦个人主页扩展涉及的实际 GORM 模型及所有权关系。统一迁移还注册 Feed Inbox、消息、关系、播放配置和嵌入等既有模型。

```mermaid
---
config:
  theme: base
  themeVariables:
    background: transparent
    fontFamily: Inter, PingFang SC, Microsoft YaHei, Arial
    primaryTextColor: "#0F172A"
    lineColor: "#94A3B8"
---
erDiagram
  ACCOUNT ||--o{ VIDEO : "发布"
  ACCOUNT ||--|| ACCOUNT_PROFILE_SETTING : "拥有隐私设置"
  ACCOUNT ||--|| USER_CONTENT_STAT : "拥有内容聚合"
  ACCOUNT ||--o{ INTERACTION_ACTION : "产生喜欢/收藏"
  ACCOUNT ||--o{ INTERACTION_ACTION_EVENT : "产生异步互动事件"
  ACCOUNT ||--o{ VIDEO_VIEW_HISTORY : "拥有观看历史"
  ACCOUNT ||--o{ USER_WATCH_LATER : "拥有稍后再看"
  ACCOUNT ||--o{ VIDEO_COLLECTION : "创建合集"
  VIDEO ||--|| VIDEO_STAT : "拥有互动计数"
  VIDEO_COLLECTION ||--o{ VIDEO_COLLECTION_ITEM : "包含"
  VIDEO ||--o{ VIDEO_COLLECTION_ITEM : "加入"
  VIDEO ||--o{ INTERACTION_ACTION : "被互动"
  VIDEO ||--o{ INTERACTION_ACTION_EVENT : "被异步互动"
  VIDEO ||--o{ VIDEO_VIEW_HISTORY : "被观看"
  VIDEO ||--o{ USER_WATCH_LATER : "稍后再看"

  ACCOUNT {
    bigint id PK
    string account UK
    string password
    string nickname
    string avatar_url
    int gender
    string role
    int status
  }

  VIDEO {
    bigint id PK
    bigint author_id FK
    string title
    string media_url
    string cover_url
    int status
    string visibility
    datetime published_at
    string idempotency_key
  }

  VIDEO_STAT {
    bigint video_id PK
    int like_count
    int comment_count
    int favorite_count
  }

  ACCOUNT_PROFILE_SETTING {
    bigint user_id PK
    string liked_visibility
    string favorite_visibility
  }

  USER_CONTENT_STAT {
    bigint user_id PK
    int public_work_count
    int private_work_count
    int received_like_count
    int collection_count
  }

  INTERACTION_ACTION {
    bigint id PK
    bigint user_id
    bigint video_id
    string action_type
    int status
    bigint latest_event_version
    datetime updated_at
  }

  INTERACTION_ACTION_EVENT {
    string event_id PK
    bigint user_id
    bigint video_id
    string action_type
    boolean active
    bigint version
    datetime occurred_at
    datetime processed_at
  }

  VIDEO_VIEW_HISTORY {
    bigint user_id PK
    bigint video_id PK
    string last_event_type
    int last_watch_ms
    boolean completed
    datetime last_watched_at
  }

  USER_WATCH_LATER {
    bigint user_id PK
    bigint video_id PK
    int status
    datetime updated_at
  }

  VIDEO_COLLECTION {
    bigint id PK
    bigint owner_id
    string visibility
    int status
    datetime updated_at
  }

  VIDEO_COLLECTION_ITEM {
    bigint collection_id PK
    bigint video_id PK
    int position
  }
```

迁移在 PostgreSQL advisory transaction lock 内执行 `AutoMigrate`，包括异步互动回执 `version` 和行为行 `latest_event_version`/兼容顺序列，随后补齐 `video_stat`、将历史视频可见性置为 `public`、补齐隐私默认行、以版本 `0` 和现有行为 `updated_at` 安全回填旧行为顺序、重建 `user_content_stat`、仅在 `app_migration` 缺少持久标记时从原始观看事件一次性回填 `video_view_history`，最后确保 Feed Timeline 索引。标记与回填处于同一事务，用户之后删除或清空的历史不会被后续 API/Worker 启动恢复。

## 5. 演进能力地图

这张图展示已落地闭环和后续模块的连接方式，虚线代表规划中的能力边界。

```mermaid
---
config:
  theme: base
  layout: dagre
  themeVariables:
    background: transparent
    fontFamily: Inter, PingFang SC, Microsoft YaHei, Arial
    primaryTextColor: "#0F172A"
    lineColor: "#94A3B8"
    clusterBkg: "#F8FAFC"
    clusterBorder: "#CBD5E1"
---
flowchart TB
  %% Roadmap map from current MVP to planned modules
  classDef current fill:#DCFCE7,stroke:#22C55E,color:#0F172A,stroke-width:1px;
  classDef growth fill:#FAE8FF,stroke:#C084FC,color:#0F172A,stroke-width:1px;
  classDef platform fill:#FFEDD5,stroke:#F97316,color:#0F172A,stroke-width:1px,stroke-dasharray:5 5;
  classDef data fill:#F1F5F9,stroke:#64748B,color:#0F172A,stroke-width:1px,stroke-dasharray:5 5;

  Account["账户"]
  Video["视频"]
  Feed["Feed"]
  Upload["上传"]
  Recommendation["推荐"]
  Interaction["互动"]
  Exposure["曝光 / 观看历史"]
  Library["个人内容库"]
  Review["审核"]
  Message["消息"]
  Admin["后台运营"]
  Governance["系统治理"]
  Observability["监控告警"]
  AsyncStore[("Redis / MQ / 对象存储")]

  Account -->|"提供资料隐私"| Library
  Video -->|"进入内容审核"| Review
  Video -->|"承载点赞评论收藏"| Interaction
  Video -->|"补齐可读卡片"| Library
  Upload -->|"迁移媒体文件"| AsyncStore
  Feed -->|"接入召回排序"| Recommendation
  Recommendation -->|"返回候选内容"| Feed
  Feed -->|"上报曝光 / 播放 / 进度 / 完播 / 跳过"| Exposure
  Exposure -->|"Outbox 可靠投递反馈"| AsyncStore
  AsyncStore -->|"更新用户兴趣信号"| Recommendation
  Interaction -->|"提供喜欢/收藏索引"| Library
  Exposure -->|"提供观看历史投影"| Library
  Interaction -->|"投递互动事件"| AsyncStore
  AsyncStore -->|"生成站内通知"| Message
  Admin -->|"处理审核任务"| Review
  Governance -->|"保护核心接口"| Feed
  Observability -->|"采集服务指标"| Governance

  class Account,Video,Feed,Upload,Recommendation,Interaction,Exposure,Library,Message,Review,Admin current;
  class Governance,Observability platform;
  class AsyncStore data;
  linkStyle default stroke:#94A3B8,stroke-width:1.4px
```

### 5.1 自动审核链路

```mermaid
sequenceDiagram
  participant Media as Media Worker / legacy create
  participant Review as Review Service
  participant DB as PostgreSQL
  participant Producer as Moderation Producer
  participant Delivery as Media Publication

  Media->>Review: ready pending video
  Review->>DB: lock video + create-or-get (video_id, review_version)
  Producer->>Review: internal-token bounded machine result
  Review->>DB: lock result identity, case, video
  Review->>DB: insert immutable signals + decision
  alt reject
    Review->>DB: close case + reject video atomically
  else human
    Review->>DB: pending_human; video remains pending
  else approve
    Review->>DB: close case + publish lifecycle atomically
    Review->>Delivery: promote ready media / publish event
  end
```

机器结果与供应商实现解耦；策略版本和证据 provenance 保存在 PostgreSQL。未知 label 保留但
保守进入人审。媒体 ready 事件重复安全，Worker reconciliation 会补建 ready pending 视频的遗漏案件。

### 5.2 人工复审链路

```mermaid
sequenceDiagram
  participant Reviewer
  participant API as Admin Review API
  participant DB as PostgreSQL
  participant Audit as Admin Audit
  participant Worker as Review Notification Worker
  participant Message

  Reviewer->>API: GET signed priority/age queue
  API->>DB: stable page including DB-time expired leases
  Reviewer->>API: claim(expected case version)
  API->>DB: lock case; store reviewer + token hash + DB-time expiry
  API-->>Reviewer: opaque lease token
  Reviewer->>API: decision(token, case/review version, idempotency key)
  API->>DB: lock idempotency, case, video
  API->>Audit: append validated success fact in same transaction
  API->>DB: decision + case + video + audit + notification outbox
  DB-->>API: atomic commit
  Worker->>DB: lease review notification outbox
  Worker->>Message: idempotent SYSTEM message
```

队列固定按 `priority DESC, created_at ASC, id ASC`，签名 cursor 绑定过滤器；查询直接纳入按
数据库时间过期的租约。pending-human priority 由触发信号确定性映射为 `1..100` 并与路由原子
提交。claim、renew、release 和 decision 都使用 case version；相关行锁先于数据库时间采样。
决定要求当前 holder、匹配 video review version、注册 reason 和 payload-bound idempotency；
旧版本决定重放不再触发媒体/发布副作用。审计失败整体回滚；通知失败只重试 durable Outbox。

内容运营接口继续由视频领域拥有，而不是建立通用 Admin Repository。`GET /api/admin/videos`
使用签名 cursor 绑定全部筛选并按 `created_at DESC, id DESC` 查询；下架/恢复锁定 video 行并校验
独立运营 version，在同一事务提交生命周期、内容统计、不可变处罚记录、成功 audit 和缓存失效
及媒体收敛意图。Video Worker 使用有界批次、`FOR UPDATE SKIP LOCKED`、租约和指数退避处理该
意图：先失效 Redis，再按当前数据库状态保护或发布媒体，全部成功后才标记 delivered；失败保留
有界错误并重试。按当前状态收敛也避免旧恢复意图在后续再次下架后错误公开内容。

### 5.3 运行时降级控制链路

```mermaid
flowchart LR
  Admin["Operator + governance.execute"] -->|"expected revision update / rollback"| API["Admin Governance API"]
  API -->|"revision + active pointer + audit 同事务"| DB[("PostgreSQL")]
  APIPoller["API Poller"] -->|"有界周期读取"| DB
  WorkerPoller["Worker Poller"] -->|"有界周期读取"| DB
  APIPoller -->|"验证后 atomic swap"| APISnapshot["API Local Snapshot"]
  WorkerPoller -->|"验证后 atomic swap"| WorkerSnapshot["Worker Local Snapshot"]
  APISnapshot -->|"纯内存求值"| Preload["兼容 preload API"]
  WorkerSnapshot -->|"纯内存求值"| Preheat["Feed cache preheat"]
```

revision 不可变，active pointer 使用 expected revision 和按 key advisory lock 串行更新；成功
审计与 pointer 切换原子提交。API/Worker poll failure 保留 last-known-good，超过注册 key 的
max staleness 后使用 failure default。请求和消费热路径不访问控制面。

## 6. 说明

- 当前代码以 Go API 承载同步 HTTP，用 Worker 消费互动、发布、曝光预热、嵌入和媒体处理事件；内部按接口层、应用层、领域层、基础设施层组织。
- 对外接口统一挂载在 `/api/*`；`/uploads/*` 中头像/普通文件公开读取，视频/封面按不可变上传所有权、同作者视频引用、数据库状态和可选 Authorization 或“HttpOnly JWT 资产 Cookie + Web 活跃标记”授权后提供 Range/HEAD；资产 Cookie 只在登录时写入，离线退出也会同步移除活跃标记；健康检查使用 `/health`。
- 数据持久化使用 PostgreSQL；API 和 Worker 通过 advisory transaction lock 串行执行完整 GORM 自动迁移。内容统计校正采用快照差量叠加，既修复旧偏差也保留其他在线实例已提交的并发增量；观看历史聚合修复只修正仍存在的投影，不会从原始事件恢复用户已删除的历史。
- Feed 通过 `scene` 分发策略：`timeline` 按 `published_at DESC, id DESC` 排序，`hot` 按最近 60 分钟互动热度排序，并通过 Base64 游标分页。
- Web 预加载直接消费活动 Feed 的有序 items，并按网络、save-data、内存和 `buffer_ms` 保留有界上一条/当前条/后续媒体资源；场景、请求、身份或源版本变化会取消旧代际。`/api/preload-videos` 仅保留为按发布时间补充资源的兼容接口。
- 新视频默认进入待审核且没有 `published_at`。批准和媒体基线就绪是独立门：只有 `status=published`、`visibility=public` 且媒体为 `legacy_ready/ready` 时才公开；Feed 缓存命中后仍通过数据库批量验证，避免旧缓存泄露待审、拒绝、私密或处理中内容。
- Web 为每次激活视频建立播放会话，按 10 秒边界和暂停、seek、切换、隐藏、退出上报曝光、播放、进度、完播和跳过。观看事实按 `(user_id, event_id)` 幂等，历史投影按有界 `(occurred_at, event_id)` 单调更新；事实、历史/曝光投影和 `view_event_outbox` 同事务提交，Worker 通过租约、重试与 publisher confirm 将反馈可靠送入推荐链路。
- 新互动请求只接受当前已发布公开视频；Redis 在状态/计数事务内为每个行为事实分配单调版本。Single 传输等待自身确认，dual/mirror 同时等待 RabbitMQ publisher confirm 与 Kafka broker acknowledgement；任一失败或不确定时同步落库，发布与 fallback 双失败时只条件回滚仍由该版本拥有的 Redis 状态，相同幂等重试会重发原事件。Redis 提交后若计数读取失败，应用使用脱离请求取消且有超时的上下文条件回滚；回滚报错时重新确认投递原事件并以同步回执持久化兜底，并发更高版本不会被旧请求覆盖。事件回执按 `event_id` 去重，行为行优先按 `version` 拒绝延迟旧事件，同版本才用 `(occurred_at, event_id)` 兼容定序；重复和旧事件成功确认且不改变统计。缺失/删除视频和无效载荷终止消费而不无限重入队，所有内容读取仍按当前可见性过滤。
- 个人主页本人能力包括作品、推荐、喜欢、收藏、观看历史、稍后再看；公开主页仅含公开作品、公开合集和隐私允许的喜欢。“短剧”和“我的预约”没有领域模型或接口，明确不在架构范围内。
- 播放技术遥测与观看行为事实分流：Web 将渲染首帧、播放结果、rebuffer/seek、选源、帧质量和终止错误组成有界版本化批次；API 严格校验并原子写入 `playback_telemetry_batch/event`，立即聚合低基数 Prometheus 指标。批次失败不影响播放，旧 QoS 端点在迁移窗口内继续兼容。
- 人工审核使用数据库时间租约和 optimistic case/review version；最终决定、视频生命周期、成功审计和作者通知 Outbox 原子提交，Review Worker 再通过 message Application 幂等生成站内通知。
- Web Admin Shell 复用 typed History router 和 SessionProvider，`AdminApp` 通过动态 import 形成独立 JS/CSS chunk；客户端权限只过滤导航，直接 URL 和所有动作仍由后台中间件授权。
- 运行时降级控制只接受代码注册 typed key；API 与 Worker 轮询 PostgreSQL 后原子替换本地
  snapshot。当前 `feed.preload.enabled` 仅影响兼容 preload 与非关键 cache preheat；poll
  failure 使用 last-known-good，过度陈旧使用 failure default，不能关闭 fanout 或耐久事实。

## 7. 生产媒体交付

```mermaid
flowchart LR
  Web["Web 上传页"] -->|"创建上传会话"| API["Hertz API"]
  API -->|"返回预签名 PUT"| Web
  Web -->|"直传原始视频/封面"| S3[("S3 / MinIO")]
  Web -->|"完成会话"| API
  API -->|"提交资产与 PostgreSQL job；尽力发布唤醒"| MQ["Kafka command / Rabbit rollback"]
  MQ --> Worker["Media Worker"]
  Worker -->|"ffprobe + FFmpeg"| Outputs["基线 MP4 / 多码率 MP4 / DASH"]
  Worker -->|"临时键校验后发布"| S3
  Worker -->|"更新 ready 与兼容 URL"| PostgreSQL[("PostgreSQL")]
  S3 -->|"不可变公共资源"| CDN["CDN / 公共前缀"]
```

- 本地开发继续支持 `/api/uploads` 和受保护 `/uploads/*`；生产模式通过 `media.backend=s3` 使用上传会话。
- `media_asset` 保存原始资产，`media_variant` 保存基线、清晰度、manifest 和 segment，`media_processing_job` 使用版本、租约和尝试次数保证重复消息安全。
- `frux.media.processing-requested.v1` Consumer 只校验 job 并有界 signal 后提交，不在转码期间持有
  Offset；轮询与 reconciliation 覆盖命令丢失、重复、延迟、满容量和重启。
- `frux.video.published.v1` 保留 30 天首次发布事实。Embedding intake 先提交 hash 与
  `semantic_embedding_job`；远端语义请求由独立数据库租约 worker 执行，长重试不占 Kafka Partition。
- Worker 只在临时对象通过大小与 SHA-256 校验后发布受保护的内容寻址输出；只有审核已通过且公开的视频才提升到 `media/` 公共前缀。基线先就绪时仅更新 `media_status=ready` 并保持公共 URL 为空；批准先发生时也等待基线，双门满足后才投影 URL 和发送发布事件。
- 公共变体使用版本化 `media/v2/{exposure-generation}/...` URL、60 秒可重验证缓存、ETag 和 Range/HEAD；私密、下架、拒绝、媒体失败或删除转换会将变体降回保护前缀，本地 `/media` 还实时校验数据库公开资格。状态撤销允许最多一个短缓存窗口，失败返回错误并由幂等请求重试；首次上线需 purge 旧 `media/*` 一年缓存条目。原始对象和未完成资产只能由不可变 owner 获取短期签名 URL。
- 删除视频立即停止 API 发现，并通过 `media_cleanup_task` 延迟删除对象；Reconciler 修复过期租约、缺失对象、不完整变体和孤儿对象。

## 8. 播放观测链路

```mermaid
flowchart LR
  Player["Web Native Player"] -->|"稳定事件 ID + 有界 batch"| API["POST /api/playback-telemetry-batches"]
  API -->|"严格校验 / 归一化"| Store[("PostgreSQL telemetry events")]
  API -->|"即时低基数聚合"| Prometheus["Prometheus"]
  Prometheus --> Grafana["Grafana Playback Dashboard"]
  Prometheus --> Alerts["Sustained Alert Rules"]
  Cleaner["Retention Cleaner"] -->|"按 created_at 有界删除"| Store
```

首帧优先使用渲染回调，卡顿排除暂停和 seek。数据库保留诊断标识，指标标签只允许固定技术维度；看板和告警无法按用户、视频、请求或播放会话展开。
