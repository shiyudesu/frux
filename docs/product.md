# GCFeed 产品范围

本文定义 GCFeed 的业务范围、模块边界和功能优先级。README 只保留项目入口，产品状态以本文为准。

## 1. 产品定位

GCFeed 是一个短视频 Feed 系统，目标是用最小可行架构承载完整业务闭环：

```text
注册登录 -> 发布视频 -> 审核治理 -> Feed 分发 -> 浏览互动 -> 消息通知 -> 运营监控
```

首发目标是让用户完成登录、发布、刷 Feed、点赞收藏评论和基础审核；后续补齐后台运营、治理和监控。

## 2. 模块地图

| 领域 | 模块 | 职责 |
| --- | --- | --- |
| 用户 | 账户 | 注册、登录、资料、登录态、性别和主页隐私 |
| 用户 | 关系 | 关注、取关、粉丝和关注列表 |
| 内容 | 视频 | 发布、上传、详情、独立可见性、批量管理、创作者合集 |
| 内容 | 互动 | 点赞、收藏、两级评论、回复、评论点赞、热门/最新排序、计数与个人行为索引 |
| 内容 | 个人内容库 | 喜欢、收藏、观看历史、稍后再看聚合 |
| 分发 | Feed | Timeline、Hot、游标分页、卡片组装 |
| 分发 | 曝光 | 观看事件、曝光聚合、观看历史投影 |
| 分发 | 推荐 | 召回、排序、打散、曝光去重 |
| 治理 | 审核 | Agent 初审、人审判定、违规下架 |
| 治理 | 后台运营 | 内容查询、审核分配、配置管理 |
| 体验 | 消息 | 通知、未读数、已读状态、结构化讨论目标和消息深链 |
| 体验 | 播放优化 | 播放参数、预加载、QoS 与版本化播放遥测 |
| 稳定性 | 系统治理 | 限流、降级、死信重试 |
| 稳定性 | 监控告警 | 指标采集、看板、告警事件 |

## 3. 当前实现状态

| 模块 | 状态 | 说明 |
| --- | --- | --- |
| 账户 | 已实现 | 注册、登录、登出、聚合资料、性别、资料更新、隐私设置 |
| 视频 | 已实现 | 发布、详情、公开/私密可见性、创作者查询、原子批量操作、合集、软删除、对象存储直传与异步媒体处理 |
| 个人内容库 | 已实现 | 本人喜欢/收藏、公开喜欢、观看历史、稍后再看 |
| Feed | 已实现 | Timeline、Hot、复杂 Feed 查询、公开可见性校验、观看上报 |
| 曝光 | 已实现 | 原始观看事件、曝光聚合、最新观看历史投影 |
| 推荐 | 已实现 | 多源召回、上下文排序、稳定分页、反馈抑制、采样评估 |
| 互动 | 已实现 | 点赞、收藏、两级评论、回复分页/预览、热门/最新、评论点赞、差异化删除、计数/热度与耐久通知 Outbox |
| 关系 | 已实现 | 关注、取关、关注列表、粉丝列表 |
| 消息 | 已实现 | 站内通知、未读/批量已读、评论/回复/评论获赞类型、结构化目标和视频讨论深链 |
| 审核 | 规划中 | Agent 初审和人工复审 |
| 后台运营 | 规划中 | 视频查询、审核任务、配置管理 |
| 播放优化 | 已实现 | 播放参数、预加载建议、旧 QoS、准确首帧/卡顿/错误遥测、Web Feed 接入 |
| 系统治理 | 规划中 | 限流、降级、死信重试 |
| 监控告警 | 规划中 | 指标写入、看板、告警 |

## 4. P0 首发闭环

P0 目标是完整跑通用户端主链路和基础稳定性链路。

| 状态 | 模块 | 方法 | 接口路径 | 功能 |
| --- | --- | --- | --- | --- |
| 已实现 | 账户 | POST | `/api/users` | 注册 |
| 已实现 | 账户 | POST | `/api/sessions` | 登录并获取 Token |
| 已实现 | 账户 | DELETE | `/api/sessions/current` | 无需有效 Token 也会清除资产 Cookie；Web 离线退出同步停用私有资产 |
| 已实现 | 账户 | GET | `/api/users/me` | 获取当前用户信息 |
| 已实现 | 账户 | GET/PATCH | `/api/users/me/profile-settings` | 读取或部分更新主页隐私 |
| 已实现 | 视频 | POST | `/api/videos` | 发布视频 |
| 已实现 | 视频 | GET | `/api/videos/{videoId}` | 视频详情 |
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
| 规划中 | 审核 | POST | `/internal/review/tasks` | 创建审核任务 |
| 规划中 | 审核 | PUT | `/api/review/tasks/{taskId}/decision` | 人工审核通过或驳回 |
| 规划中 | 审核 | PATCH | `/api/videos/{videoId}` | 违规内容下架 |
| 规划中 | 系统治理 | POST | `/internal/rate-limit-decisions` | 限流放行检查 |
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
| 已实现 | 视频 | POST | `/api/users/me/video-queries` | 按公开/私密、关键词、日期和游标查询作品 |
| 已实现 | 视频 | POST | `/api/users/me/video-batch-actions` | 幂等批量公开、私密或删除，最多 100 个 ID |
| 已实现 | 视频 | GET/POST | `/api/users/me/video-collections` | 查询或创建创作者合集 |
| 已实现 | 视频 | PATCH/DELETE | `/api/users/me/video-collections/{collectionId}` | 更新或软删除合集 |
| 已实现 | 视频 | PUT/DELETE | `/api/users/me/video-collections/{collectionId}/videos/{videoId}` | 增删合集成员 |
| 已实现 | 视频 | GET | `/api/users/{userId}/video-collections` | 查询公开合集及公开可读成员 |
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
| 已实现 | 消息 | GET | `/api/messages` | 消息列表 |
| 已实现 | 消息 | GET | `/api/message-stats/unread` | 未读计数 |
| 已实现 | 消息 | PATCH | `/api/messages` | 批量已读 |
| 已实现 | 消息 | POST | `/internal/messages` | 消费事件生成消息 |
| 规划中 | 审核 | PUT | `/internal/review/tasks/{taskId}/agent-result` | Agent 初审回传 |
| 规划中 | 后台运营 | GET | `/api/admin/videos` | 运营查视频 |
| 规划中 | 后台运营 | GET | `/api/admin/review/tasks` | 运营查审核任务 |
| 规划中 | 后台运营 | PUT | `/api/admin/review/tasks/{taskId}/assignee` | 分配审核员 |
| 规划中 | 后台运营 | PATCH | `/api/admin/configs/{configKey}` | 更新运营配置 |
| 已实现 | 播放优化 | GET | `/api/playback-config` | 播放参数下发 |
| 已实现 | 播放优化 | GET | `/api/preload-videos` | 兼容客户端的发布时间顺序补充资源 |
| 已实现 | 播放优化 | POST | `/api/playback-qos-reports` | Web 播放质量上报 |
| 已实现 | 播放优化 | POST | `/api/playback-telemetry-batches` | 认证 Web 客户端批量上报隐私安全播放遥测 |
| 已实现 | 播放优化 | POST | `/internal/playback-qos-reports` | 服务端播放质量上报 |
| 规划中 | 系统治理 | GET | `/internal/governance/degrade-switches` | 查询降级开关 |
| 规划中 | 系统治理 | PATCH | `/api/admin/governance/degrade-switches/{key}` | 调整降级开关 |
| 规划中 | 系统治理 | POST | `/internal/dead-letter-retries` | 死信任务重试 |
| 规划中 | 监控告警 | GET | `/api/admin/metric-dashboard` | 监控看板查询 |
| 规划中 | 监控告警 | POST | `/api/admin/alerts/rules` | 告警规则创建 |
| 规划中 | 监控告警 | GET | `/api/admin/alerts/events` | 告警事件查询 |

## 6. Web 客户端范围

| 状态 | 页面/能力 | 说明 |
| --- | --- | --- |
| 已实现 | 登录/注册页 | 对接账户和会话接口 |
| 已实现 | Feed 页 | 拉取视频流，支持上下切换、推荐上下文与不感兴趣反馈 |
| 已实现 | 互动面板 | 支持视频点赞/收藏及完整两级评论：热门/最新、分页、回复预览/展开、评论点赞、发表、权限删除和墓碑 |
| 已实现 | 视频讨论页 | typed `/videos/:videoId`，可用 `comment`/`highlight` 参数直达、展开并高亮讨论 |
| 已实现 | 关注操作 | 支持关注作者和取关 |
| 已实现 | 个人主页资料头图 | 展示账号、性别、关注、粉丝、公开作品和获赞统计，支持关系弹窗与资料/隐私编辑 |
| 已实现 | 个人主页内容中心 | 一级 Tab 为作品、喜欢、收藏、观看历史、稍后再看；个人页不混入推荐，各内容库按需加载并保留独立状态，点击卡片可从所选项进入全屏连续播放 |
| 已实现 | 创作者作品管理 | 二级视图为已发布、私密作品、合集；支持关键词/日期筛选、游标加载、批量公开/私密/删除 |
| 已实现 | 创作者合集管理 | 创建、编辑、删除公开/私密合集；编辑器可独立搜索和继续加载自己的作品成员 |
| 已实现 | 公开主页 | 共享资料头图和作品网格；展示公开作品、公开合集，并仅在隐私允许时展示喜欢 |
| 已实现 | 发布页 | 发布视频信息 |
| 已实现 | 消息页 | 展示通知、未读/已读操作；评论、回复和评论获赞消息可深链到视频讨论，legacy 消息保持只读兼容 |
| 已实现 | 播放优化接入 | Feed 页读取播放配置、预加载后续视频、准确测量首帧并上报播放结果、卡顿、seek、选源和帧质量 |
| 规划中 | 审核后台 | 内容审核和违规处理 |
| 规划中 | 监控看板 | 指标、告警和治理状态 |

个人主页只呈现个人已有真实领域能力，不展示推荐内容；推荐继续由独立推荐 Feed 承载。“短剧”和“我的预约（Appointments）”没有后端模型、接口或页面入口，明确不在当前产品范围内，也不渲染占位 Tab。收藏、观看历史、稍后再看和私密作品始终为本人能力；Web 不提供“公开收藏”控制或文案，当前仅喜欢列表可通过隐私设置公开。喜欢、收藏、观看历史和稍后再看卡片可从所选项进入全屏连续播放，Feed 和内容库播放“更多”菜单可幂等加入稍后再看。资料与喜欢隐私由一次原子保存提交。私密、下架和删除作品不仅从列表中裁剪，本地视频/封面还按不可变上传所有权和有效视频引用授权，旧 `/uploads` URL 不可绕过隐私。

## 7. 里程碑

| 阶段 | 目标 | 重点模块 |
| --- | --- | --- |
| M1 | 完成用户端闭环 | 账户、视频、Feed、推荐、互动、关系 |
| M2 | 补齐内容治理 | 审核、后台运营、消息 |
| M3 | 提升体验与稳定性 | 播放优化、系统治理、监控告警 |
| M4 | 支持规模化演进 | 服务拆分、异步链路增强、指标体系完善 |
