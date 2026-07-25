# 视频 Feed 系统架构图（MVP）

本文按 `mermaid-diagrams` skill 重构：每张图只表达一个概念，节点保持克制，连接线带语义标签，图前给出用途说明。当前实现由 Go API 与 Worker 共同承载账户、视频、Feed、互动、曝光、个人内容库和消息能力。

## 1. 系统上下文

这张图展示 GCFeed 与客户端、存储和演进型基础设施的边界。

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
  %% GCFeed MVP system context
  classDef client fill:#EFF6FF,stroke:#60A5FA,color:#0F172A,stroke-width:1px;
  classDef system fill:#DCFCE7,stroke:#22C55E,color:#0F172A,stroke-width:1px;
  classDef store fill:#F1F5F9,stroke:#64748B,color:#0F172A,stroke-width:1px;
  classDef service fill:#FAE8FF,stroke:#C084FC,color:#0F172A,stroke-width:1px;
  classDef future fill:#FFFFFF,stroke:#94A3B8,color:#475569,stroke-width:1px,stroke-dasharray:5 5;

  Web["Web App<br/>React + Vite"]
  Client["移动端 / API 调用方"]
  API["GCFeed API<br/>Go + Hertz"]
  PostgreSQL[("PostgreSQL<br/>业务数据")]
  Uploads[("uploads<br/>视频 / 封面 / 头像")]
  Redis[("Redis<br/>缓存与计数")]
  MQ[("MQ<br/>异步事件")]
  ObjectStorage[("对象存储<br/>媒体文件")]

  Web -->|"调用管理与浏览接口"| API
  Client -->|"调用公开 API"| API
  API -->|"读写业务事实、投影和聚合"| PostgreSQL
  API -->|"保存和读取本地文件"| Uploads
  API -->|"缓存 Feed、互动状态与计数"| Redis
  API -->|"投递互动、发布和曝光事件"| MQ
  API -.->|"迁移媒体文件"| ObjectStorage

  class Web,Client client;
  class API system;
  class PostgreSQL,Uploads store;
  class Redis,MQ service;
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
  Auth["JWT Middleware<br/>鉴权上下文"]
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
  Router -->|"分发业务请求"| Handlers
  Handlers -->|"调用用例服务"| Services
  Services -->|"执行领域规则"| Domains
  Services -->|"读写仓储"| Repo
  Repo -->|"复用连接池"| SQL
  SQL -->|"持久化数据"| PostgreSQL
  Handlers -->|"保存上传文件"| Uploads
  Router -->|"授权后交给 Range 文件服务"| Uploads

  class Entry entry;
  class Router,Auth,Handlers http;
  class Services service;
  class Domains domain;
  class Config,JWT,Repo,SQL infra;
  class PostgreSQL,Uploads store;
  linkStyle default stroke:#94A3B8,stroke-width:1.4px
```

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
  alt 已发布且公开
    R->>FS: 匿名 Range/HEAD 读取
  else 作者读取非删除作品
    R->>FS: 私有 Range/HEAD 读取
  else 删除、他人私密或未引用保护文件
    R-->>C: 404
  end

  C->>R: POST /api/videos
  R->>H: Video.Create
  H->>S: CreatePublished
  S->>Repo: Save video and video_stat
  Repo->>DB: INSERT video, video_stat
  DB-->>Repo: 返回 video id
  Repo-->>S: 返回视频实体
  S-->>H: 返回视频详情
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
  S->>MQ: 发布 ActionChangedEvent 并等待 publisher confirm
  alt 发布失败或确认不确定
    S->>Repo: 同步持久化同一版本事件
    alt 同步持久化也失败
      S->>Redis: 仅当前版本未被更新时条件回滚
    end
  end
  MQ->>Worker: 投递已接收互动事件
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

  class Account,Video,Feed,Upload,Recommendation,Interaction,Exposure,Library,Message current;
  class Review,Admin growth;
  class Governance,Observability platform;
  class AsyncStore data;
  linkStyle default stroke:#94A3B8,stroke-width:1.4px
```

## 6. 说明

- 当前代码以 Go API 承载同步 HTTP，用 Worker 消费互动、发布、曝光预热和嵌入事件；内部按接口层、应用层、领域层、基础设施层组织。
- 对外接口统一挂载在 `/api/*`；`/uploads/*` 中头像/普通文件公开读取，视频/封面按不可变上传所有权、同作者视频引用、数据库状态和可选 Authorization 或“HttpOnly JWT 资产 Cookie + Web 活跃标记”授权后提供 Range/HEAD；资产 Cookie 只在登录时写入，离线退出也会同步移除活跃标记；健康检查使用 `/health`。
- 数据持久化使用 PostgreSQL；API 和 Worker 通过 advisory transaction lock 串行执行完整 GORM 自动迁移。内容统计校正采用快照差量叠加，既修复旧偏差也保留其他在线实例已提交的并发增量；观看历史聚合修复只修正仍存在的投影，不会从原始事件恢复用户已删除的历史。
- Feed 通过 `scene` 分发策略：`timeline` 按 `published_at DESC, id DESC` 排序，`hot` 按最近 60 分钟互动热度排序，并通过 Base64 游标分页。
- 公开读取统一要求视频已发布且公开；Feed 缓存命中后仍通过数据库批量验证公开可读性，避免旧缓存泄露私密内容。
- Web 为每次激活视频建立播放会话，按 10 秒边界和暂停、seek、切换、隐藏、退出上报曝光、播放、进度、完播和跳过。观看事实按 `(user_id, event_id)` 幂等，历史投影按有界 `(occurred_at, event_id)` 单调更新；事实、历史/曝光投影和 `view_event_outbox` 同事务提交，Worker 通过租约、重试与 publisher confirm 将反馈可靠送入推荐链路。
- 新互动请求只接受当前已发布公开视频；Redis 在状态/计数事务内为每个行为事实分配单调版本。RabbitMQ 使用 publisher confirm；失败或确认不确定时同步落库，双失败时只条件回滚仍由该版本拥有的 Redis 状态，相同幂等重试会重发原事件。Redis 提交后若计数读取失败，应用使用脱离请求取消且有超时的上下文条件回滚；回滚报错时重新确认投递原事件并以同步回执持久化兜底，并发更高版本不会被旧请求覆盖。事件回执按 `event_id` 去重，行为行优先按 `version` 拒绝延迟旧事件，同版本才用 `(occurred_at, event_id)` 兼容定序；重复和旧事件成功确认且不改变统计。缺失/删除视频和无效载荷终止消费而不无限重入队，所有内容读取仍按当前可见性过滤。
- 个人主页本人能力包括作品、推荐、喜欢、收藏、观看历史、稍后再看；公开主页仅含公开作品、公开合集和隐私允许的喜欢。“短剧”和“我的预约”没有领域模型或接口，明确不在架构范围内。
