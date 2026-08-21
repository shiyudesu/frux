# Frux 产品范围

本文定义 Frux 的业务范围、模块边界和功能优先级。README 只保留项目入口，产品状态以本文为准。

## 1. 产品定位

Frux 是一个短视频 Feed 系统，目标是用最小可行架构承载完整业务闭环：

```text
注册登录 -> 发布视频 -> 审核治理 -> Feed 分发 -> 浏览互动 -> 消息通知 -> 运营监控
```

首发目标是让用户完成登录、发布、刷 Feed、点赞收藏评论和基础审核；后续补齐后台运营、治理和监控。

## 2. 模块地图

| 领域 | 模块 | 职责 |
| --- | --- | --- |
| 用户 | 账户 | 注册、登录、资料、登录态、性别和主页隐私 |
| 用户 | 关系 | 关注、取关、粉丝和关注列表 |
| 内容 | 视频 | 发布、上传、详情、独立可见性和批量管理 |
| 内容 | 互动 | 点赞、收藏、两级评论、回复、评论点赞、热门/最新排序、计数与个人行为索引 |
| 内容 | 个人内容库 | 喜欢、收藏、观看历史、稍后再看聚合 |
| 发现 | 全局搜索 | 公开视频与正常用户的相关性搜索、可选多模态 Hybrid、Exact 相似视频和稳定分页 |
| 分发 | Feed | Timeline、Hot、游标分页、卡片组装 |
| 分发 | 曝光 | 观看事件、曝光聚合、观看历史投影 |
| 分发 | 推荐 | 召回、排序、打散、曝光去重 |
| 治理 | 审核 | 版本化机器审核、稳定人工队列、租约领取、幂等决定和不可变历史 |
| 治理 | 后台权限 | 当前账号驱动的 Reviewer、Operator 和兼容 Admin 权限边界 |
| 治理 | 操作审计 | 不可变特权操作事实、同事务成功审计和稳定查询 |
| 治理 | 后台运营 | 内容查询、审核分配、普通用户账号管理、配置管理 |
| 体验 | 消息通知 | 事件通知、未读数、已读状态、结构化讨论目标和消息深链 |
| 体验 | 私信聊天 | 互关 1:1 会话、文本/视频卡片、已读进度、私信未读和内部视频分享 |
| 体验 | 播放优化 | 播放参数、预加载、QoS 与版本化播放遥测 |
| 稳定性 | 系统治理 | 限流、降级、死信重试 |
| 稳定性 | 监控告警 | 指标采集、看板、告警事件 |

## 3. 当前实现状态

| 模块 | 状态 | 说明 |
| --- | --- | --- |
| 账户 | 已实现 | 注册、登录、登出、聚合资料、性别、资料更新、隐私设置，以及后台普通用户查询、冻结、解冻、会话撤销和原因化站内通知 |
| 视频 | 已实现 | 待审创建、批准/拒绝/下架/恢复生命周期、详情、可见性、创作者查询、批量操作、对象存储和异步媒体处理 |
| 个人内容库 | 已实现 | 本人喜欢/收藏、公开喜欢、观看历史、稍后再看 |
| Feed | 已实现 | Timeline、Hot、复杂 Feed 查询、公开可见性校验、观看上报 |
| 曝光 | 已实现 | 原始观看事件、曝光聚合、最新观看历史投影 |
| 推荐 | 已实现 | 多源召回、上下文排序、稳定分页、反馈抑制、采样评估，以及默认关闭的 active-contract Session Semantic Exact Recall/Confidence/脱敏证据；未 Rollout |
| 互动 | 已实现 | 点赞、收藏、两级评论、回复分页/预览、热门/最新、评论点赞、差异化删除、计数/热度与耐久通知 Outbox |
| 关系 | 已实现 | 关注、取关、关注列表、粉丝列表 |
| 消息 | 已实现 | 站内通知、未读/批量已读、评论/回复/评论获赞类型、结构化目标和视频讨论深链 |
| 私信聊天 | 已实现 | 互关正常消费端 1:1 会话、文本和视频卡片消息、稳定游标、幂等发送、单调已读、私信/通知未读汇总、HTTP 轮询和内部视频分享 |
| 搜索 | 已实现（多模态默认关闭） | 匿名视频/用户搜索、相关性排序、query 绑定游标；Exact/Hybrid/Similar 与 Tongyi Flash 适配器已实现，需真实 API Key 和 Golden Set 验收后再启用 |
| 审核 | 已实现 | 视频审核状态机、模型无关自动审核、人工队列/租约/决定、审计和作者通知 |
| 后台运营 | 部分实现 | typed 懒加载内容运营工作台、审核队列/详情/租约/决定、视频查询/下架/恢复和普通用户账号管理；配置管理仍规划中 |
| 播放优化 | 已实现 | 播放参数、预加载建议、旧 QoS、准确首帧/卡顿/错误遥测、Web Feed 接入 |
| 系统治理 | 已实现 | 已实现版本化运行时降级控制、typed 分层请求限流，以及 Kafka retry/DLQ inspection 和非破坏审计重放 |
| 监控告警 | 部分实现 | 已实现播放、推荐、审核、降级控制、限流和 Kafka 故障恢复 Prometheus 指标/告警/Grafana；业务指标写入仍规划中 |

## 4. P0 首发闭环

P0 目标是完整跑通用户端主链路和基础稳定性链路。

| 状态 | 模块 | 方法 | 接口路径 | 功能 |
| --- | --- | --- | --- | --- |
| 已实现 | 账户 | POST | `/api/users` | 注册 |
| 已实现 | 账户 | POST | `/api/sessions` | 登录并获取 Token |
| 已实现 | 账户 | DELETE | `/api/sessions/current` | 无需有效 Token 也会清除资产 Cookie；Web 离线退出同步停用私有资产 |
| 已实现 | 账户 | GET | `/api/users/me` | 获取当前用户信息 |
| 已实现 | 账户 | GET/PATCH | `/api/users/me/profile-settings` | 读取或部分更新主页隐私 |
| 已实现 | 视频 | POST | `/api/videos` | 创建待审核视频 |
| 已实现 | 视频 | GET | `/api/videos/{videoId}` | 视频详情 |
| 已实现（默认关闭） | 视频发现 | GET | `/api/videos/{videoId}/similar` | active-contract Exact 相似视频；无覆盖返回健康 unavailable |
| 已实现 | 视频 | GET | `/api/users/me/videos` | 我的作品列表 |
| 已实现 | Feed | GET | `/api/feed-items` | 拉取视频流，支持 scene 和游标分页 |
| 已实现 | Feed | POST | `/api/video-view-events` | 上报曝光和观看事件 |
| 已实现 | 个人内容库 | GET | `/api/users/me/watch-history` | 读取观看历史投影 |
| 已实现 | 个人内容库 | GET | `/api/users/me/watch-later` | 读取稍后再看 |
| 已实现 | 推荐 | POST | `/internal/recommendation-candidates` | 召回、排序、打散推荐候选 |
| 已实现 | 推荐 | POST | `/internal/exposures` | 写入曝光记录 |
| 已实现 | 互动 | PUT | `/api/videos/{videoId}/like` | 点赞 |
| 已实现 | 互动 | DELETE | `/api/videos/{videoId}/like` | 取消点赞 |
| 已实现 | 互动 | POST | `/api/videos/{videoId}/comments` | 创建 Unicode/payload 幂等根评论 |
| 已实现 | 互动 | POST | `/api/videos/{videoId}/comments/{commentId}/replies` | 回复根评论或回复，保持两级展示 |
| 已实现 | 互动 | GET | `/api/videos/{videoId}/comments` | 匿名可读的热门/最新根评论游标页和回复预览 |
| 已实现 | 审核 | PUT | `/internal/review/cases/{caseId}/machine-results/{resultId}` | 服务鉴权的幂等机器结果回传 |
| 已实现 | 审核 | GET/POST/DELETE | `/api/admin/review/cases*` | 人工队列、详情、领取、续租、释放和决定 |
| 已实现 | 后台运营 | POST | `/api/admin/videos/{videoId}/enforcement` | 原因化、版本检查、审计下架 |
| 已实现 | 后台运营 | POST | `/api/admin/videos/{videoId}/restoration` | 恢复已批准的下架视频 |
| 已实现 | 账户 | GET/POST | `/api/admin/accounts*` | 仅查询普通 `user`，支持版本化幂等冻结、解冻、撤销全部 Refresh Session 与冻结/解冻站内通知 |
| 已实现 | 系统治理 | Middleware | registered endpoint groups | local-first token bucket、可选 Redis 原子协调和显式 fallback/fail-closed |
| 规划中 | 监控告警 | POST | `/internal/metric-points` | 核心指标写入 |

## 5. P1 体验和运营能力

| 状态 | 模块 | 方法 | 接口路径 | 功能 |
| --- | --- | --- | --- | --- |
| 已实现 | 账户 | PATCH | `/api/users/me` | 在单事务中更新头像、昵称、简介、性别和可选主页隐私 |
| 已实现 | 账户 | GET | `/api/users/{userId}` | 查看公开用户资料 |
| 已实现 | 账户 | GET | `/api/users/{userId}/videos` | 查看用户公开视频列表 |
| 已实现 | 关系 | PUT | `/api/users/me/following/{targetUserId}` | 关注 |
| 已实现 | 关系 | DELETE | `/api/users/me/following/{targetUserId}` | 取关 |
| 已实现 | 关系 | GET | `/api/users/me/following/{targetUserId}` | 直接读取单目标关注状态 |
| 已实现 | 关系 | GET | `/api/users/me/following` | 关注列表 |
| 已实现 | 关系 | GET | `/api/users/me/followers` | 粉丝列表 |
| 已实现 | 视频 | DELETE | `/api/videos/{videoId}` | 删除视频，软删除 |
| 已实现 | 视频 | POST | `/api/users/me/video-queries` | 按公开/私密、生命周期、关键词、日期和游标查询作品 |
| 已实现 | 视频 | POST | `/api/users/me/video-batch-actions` | 幂等批量公开、私密或删除，最多 100 个 ID |
| 已实现 | 上传 | POST | `/api/uploads` | 上传媒体文件 |
| 已实现 | 上传 | POST | `/api/upload-sessions` | 创建 S3 兼容直传会话；本地模式回退 multipart |
| 已实现 | 上传 | POST | `/api/upload-sessions/{sessionId}/complete` | 校验直传对象并创建不可变媒体资产 |
| 已实现 | 媒体 | GET/HEAD | `/uploads/*` | 公开视频/封面匿名读取；作者通过受限资产 Cookie 与 Web 活跃标记读取自己的非删除私密/下架媒体 |
| 已实现 | Feed | POST | `/api/feed-queries` | 通过请求体查询复杂 Feed 场景 |
| 已实现 | 推荐 | POST | `/api/recommendation-feedback` | 提交不感兴趣、减少作者或已看反馈 |
| 已实现 | 推荐 | POST | `/internal/exposure-decisions` | 曝光去重校验 |
| 已实现 | 互动 | PUT | `/api/videos/{videoId}/favorite` | 收藏 |
| 已实现 | 互动 | DELETE | `/api/videos/{videoId}/favorite` | 取消收藏 |
| 已实现 | 互动 | GET | `/api/comments/{commentId}/replies` | 最旧优先的回复游标页 |
| 已实现 | 互动 | GET | `/api/comments/{commentId}/thread` | 消息深链使用的直接讨论串上下文 |
| 已实现 | 互动 | PUT/DELETE | `/api/comments/{commentId}/like` | 幂等点赞/取消点赞根评论或回复 |
| 已实现 | 互动 | DELETE | `/api/comments/{commentId}` | 作者墓碑、回复单删或视频作者/管理员整串治理 |
| 已实现 | 个人内容库 | GET | `/api/users/me/liked-videos` | 当前用户喜欢列表 |
| 已实现 | 个人内容库 | GET | `/api/users/me/favorite-videos` | 当前用户收藏列表 |
| 已实现 | 个人内容库 | GET | `/api/users/{userId}/liked-videos` | 隐私允许时读取公开喜欢 |
| 已实现 | 个人内容库 | DELETE | `/api/users/me/watch-history/{videoId}` | 删除一条观看历史投影 |
| 已实现 | 个人内容库 | DELETE | `/api/users/me/watch-history` | 清空观看历史投影 |
| 已实现 | 个人内容库 | PUT/DELETE | `/api/videos/{videoId}/watch-later` | 幂等设置稍后再看状态 |
| 已实现 | 搜索 | GET | `/api/search/videos` | 搜索已发布公开且媒体就绪的视频 |
| 已实现 | 搜索 | GET | `/api/search/users` | 搜索状态正常的公开用户 |
| 已实现 | 消息 | GET | `/api/messages` | 消息列表 |
| 已实现 | 消息 | GET | `/api/message-stats/unread` | 未读计数 |
| 已实现 | 消息 | PATCH | `/api/messages` | 批量已读 |
| 已实现 | 消息 | POST | `/internal/messages` | 消费事件生成消息 |
| 已实现 | 私信聊天 | GET | `/api/chat/users/{targetUserId}/eligibility` | 查询当前互关私信资格 |
| 已实现 | 私信聊天 | GET | `/api/chat/recipients` | 分页查询可私信的互关收件人 |
| 已实现 | 私信聊天 | GET/POST | `/api/chat/conversations` | 查询或创建规范化 1:1 会话 |
| 已实现 | 私信聊天 | GET | `/api/chat/conversations/{conversationId}/messages` | 会话历史与增量消息 |
| 已实现 | 私信聊天 | POST | `/api/chat/conversations/{conversationId}/messages` | 发送文本或视频卡片消息 |
| 已实现 | 私信聊天 | PATCH | `/api/chat/conversations/{conversationId}/read` | 单调推进会话已读边界 |
| 已实现 | 私信聊天 | GET | `/api/inbox-stats/unread` | 返回通知、私信和总未读数 |

私信历史响应除 `items`、`next_cursor` 和 `has_more` 外，始终返回当前成员可访问的
`conversation` 快照与 `eligibility`；空会话因此可以在 typed route 或空路由 reload 后正确显示，
但仍不会进入普通会话列表。发送响应包含 `message` 和 `created`，精确的 sender+key+payload 重试
返回 `created=false`，不因当前取关、冻结或视频失效而重复创建。
| 已实现 | 后台权限 | GET | `/api/admin/me` | 返回当前持久化角色和封闭权限集合；路由要求 `review.read` |
| 已实现 | 操作审计 | GET | `/api/admin/audit-events` | 按有界时间范围、过滤条件和稳定游标查询审计事实 |
| 已实现 | 审核 | PUT | `/internal/review/cases/{caseId}/machine-results/{resultId}` | 保存有界机器证据并路由通过、拒绝或待人审 |
| 已实现 | 后台运营 | GET | `/api/admin/videos` | 按生命周期、作者、ID、关键词和有界创建时间运营查视频 |
| 已实现 | 后台运营 | GET/POST | `/api/admin/multimodal-jobs*` | governance 权限下检查封闭 Job 状态并同事务审计人工 requeue |
| 已实现 | 后台运营 | GET | `/api/admin/review/cases` | 稳定优先级/年龄人工审核队列 |
| 已实现 | 后台运营 | POST/DELETE | `/api/admin/review/cases/{caseId}/*` | 租约领取、续租、释放和幂等决定 |
| 已实现 | 账户 | GET | `/api/admin/accounts` | 按账号、昵称、ID 和状态稳定分页查询普通用户 |
| 已实现 | 账户 | GET | `/api/admin/accounts/{userId}` | 查看普通用户资料、统计、版本和活跃会话数 |
| 已实现 | 账户 | POST | `/api/admin/accounts/{userId}/freeze` | 原因化冻结并撤销耐久会话；不自动下架作品 |
| 已实现 | 账户 | POST | `/api/admin/accounts/{userId}/unfreeze` | 解冻并保持旧会话撤销 |
| 已实现 | 账户 | POST | `/api/admin/accounts/{userId}/sessions/revoke` | 保持状态并强制退出全部耐久会话 |
| 规划中 | 后台运营 | PATCH | `/api/admin/configs/{configKey}` | 更新运营配置 |
| 已实现 | 播放优化 | GET | `/api/playback-config` | 播放参数下发 |
| 已实现 | 播放优化 | GET | `/api/preload-videos` | 兼容客户端的发布时间顺序补充资源 |
| 已实现 | 播放优化 | POST | `/api/playback-qos-reports` | Web 播放质量上报 |
| 已实现 | 播放优化 | POST | `/api/playback-telemetry-batches` | 认证 Web 客户端批量上报隐私安全播放遥测 |
| 已实现 | 播放优化 | POST | `/internal/playback-qos-reports` | 服务端播放质量上报 |
| 已实现 | 系统治理 | GET | `/api/admin/governance/controls` | 以 `governance.execute` 查询代码注册控制及 active revision |
| 已实现 | 系统治理 | GET | `/api/admin/governance/controls/{key}/revisions` | 查询不可变 revision 历史 |
| 已实现 | 系统治理 | PATCH | `/api/admin/governance/controls/{key}` | expected-revision 更新 typed value、reason 和 expiry |
| 已实现 | 系统治理 | POST | `/api/admin/governance/controls/{key}/rollback` | expected-revision 回滚到较早有效值并生成新 revision |
| 已实现 | 系统治理 | GET | `/api/admin/kafka-dead-letters` | Kafka allowlist DLQ retained range、growth、ingress 和 oldest age 摘要 |
| 已实现 | 系统治理 | GET | `/api/admin/kafka-dead-letters/{topic}/records` | 按 Partition/Offset 有界读取脱敏 immutable Record 诊断 |
| 已实现 | 系统治理 | POST | `/api/admin/kafka-dead-letters/{topic}/records/{partition}/{offset}/replay` | 必需幂等键、注册 reason、保持 key/value 且不删除 DLQ 的确认后审计重放 |
| 规划中 | 监控告警 | GET | `/api/admin/metric-dashboard` | 监控看板查询 |
| 规划中 | 监控告警 | POST | `/api/admin/alerts/rules` | 告警规则创建 |
| 规划中 | 监控告警 | GET | `/api/admin/alerts/events` | 告警事件查询 |

## 6. Web 客户端范围

| 状态 | 页面/能力 | 说明 |
| --- | --- | --- |
| 已实现 | 登录/注册页 | 对接账户和会话接口 |
| 已实现 | Feed 页 | 拉取视频流，支持上下切换、推荐上下文与不感兴趣反馈 |
| 已实现 | 互动面板 | 支持视频点赞/收藏及完整两级评论：热门/最新、分页、回复预览/展开、评论点赞、发表、权限删除和墓碑 |
| 已实现 | 视频讨论页 | typed `/videos/:videoId`，可用 `comment`/`highlight` 参数直达、展开并高亮讨论；明确展示相似视频 loading/unavailable/empty/error 状态 |
| 已实现 | 关注操作 | 支持关注作者和取关 |
| 已实现 | 个人主页资料头图 | 本人主页展示自己的登录账号；公开作者主页只展示昵称、性别、关注、粉丝、公开作品和获赞统计，支持关系弹窗与资料/隐私编辑 |
| 已实现 | 个人主页内容中心 | 一级 Tab 为作品、喜欢、收藏、观看历史、稍后再看；个人页不混入推荐，各内容库按需加载并保留独立状态，点击卡片可从所选项进入全屏连续播放 |
| 已实现 | 创作者作品管理 | 二级视图为已发布和私密作品；支持关键词/日期筛选、游标加载、批量公开/私密/删除 |
| 已实现 | 公开主页 | 共享资料头图和作品网格；展示公开作品，并仅在隐私允许时展示喜欢；作品和公开喜欢卡片从所选项进入 Feed 式全屏连续播放 |
| 已实现 | 发布页 | 发布视频信息 |
| 已实现 | 消息页 | `/messages` 默认展示通知并保留原已读/深链行为；同一工作区增加私信 Tab 和 typed `/messages/{conversationId}` 会话页，私信使用 HTTP 轮询和视频卡片分享 |
| 已实现 | 搜索页 | typed `/search?q=...&tab=videos|users`；顶部表单提交、独立分页状态、旧响应隔离，并进入现有视频/公开主页 |
| 已实现 | 播放优化接入 | Feed 页读取播放配置、预加载后续视频、准确测量首帧并上报播放结果、卡顿、seek、选源和帧质量 |
| 已实现 | 内容运营工作台 | typed `/admin/reviews`、`/admin/reviews/:reviewId`、`/admin/videos`、`/admin/accounts`；权限导航、审核租约/决定、视频下架/恢复、普通用户账号管理和冲突恢复 |
| 规划中 | 监控看板 | 指标、告警和治理状态 |

个人主页只呈现个人已有真实领域能力，不展示推荐内容；推荐继续由独立推荐 Feed 承载。“短剧”和“我的预约（Appointments）”没有后端模型、接口或页面入口，明确不在当前产品范围内，也不渲染占位 Tab。收藏、观看历史、稍后再看和私密作品始终为本人能力；Web 不提供“公开收藏”控制或文案，当前仅喜欢列表可通过隐私设置公开。喜欢、收藏、观看历史和稍后再看卡片可从所选项进入全屏连续播放，Feed 和内容库播放“更多”菜单可幂等加入稍后再看。资料与喜欢隐私由一次原子保存提交。私密、下架和删除作品不仅从列表中裁剪，本地视频/封面还按不可变上传所有权和有效视频引用授权，旧 `/uploads` URL 不可绕过隐私。

## 7. 里程碑

| 阶段 | 目标 | 重点模块 |
| --- | --- | --- |
| M1 | 完成用户端闭环 | 账户、视频、Feed、推荐、互动、关系 |
| M2 | 补齐内容治理 | 审核、后台运营、消息通知、私信聊天 |
| M3 | 提升体验与稳定性 | 播放优化、系统治理、监控告警 |
| M4 | 支持规模化演进 | 服务拆分、异步链路增强、指标体系完善 |
