## Context

GCFeed 已有账户资料、关系统计、公开视频/我的作品、点赞收藏事实、观看事件和推荐 Feed，但这些能力目前分散在账户、视频、互动、曝光和 Feed 模块中。Web 个人主页只消费账户、关系和我的作品接口，无法形成抖音网页版个人页所具备的个人内容中心。

现有用户端 Shell 已经采用 160px/72px 左侧栏和 56px 顶栏，暗色令牌、头像尺寸和 3:4 作品卡比例也接近目标。主要缺口是个人页的聚合读模型、作品可见性和管理、喜欢/收藏列表、观看历史、稍后再看、创作者合集，以及对应的双层标签和筛选交互。

本变更必须遵守现有 Go 四层架构、PostgreSQL 持久化、稳定游标、写接口幂等、严格 TypeScript、手写路由和 GCFeed 自有品牌资产约束。短剧和预约没有上游业务模型，按用户决定不在个人页展示，也不在本变更中引入。

## Goals / Non-Goals

**Goals:**

- 为当前用户提供真实可用的作品、推荐、喜欢、收藏、观看历史和稍后再看一级标签。
- 为作品标签提供已发布、私密作品和合集二级视图，以及搜索、日期筛选和批量管理。
- 为当前用户和公开用户提供统一的资料聚合，包括关注、粉丝、作品和获赞统计。
- 保持公开 Feed、公开主页和未登录请求不泄露私密作品或私有内容库。
- 保持现有简单作品列表、互动写入、观看上报和前端路由兼容。
- 在宽屏和紧凑桌面实现经 Chrome DevTools 测量的抖音式个人页结构，同时保留现有移动端可用性。

**Non-Goals:**

- 不复制抖音 Logo、专有 SVG、背景素材、用户文案或第三方品牌资源。
- 不实现短剧、直播、预约、二维码登录、客户端下载或抖音商业入口。
- 不新增路由库、状态管理库或 UI 组件库。
- 不将个人主页改造成独立微服务；能力继续运行在现有 Go API/Worker 进程中。
- 不保证历史观看事件在迁移前拥有完整播放进度；只回填可从现有事件确定的最近状态。

## Decisions

### 1. 保持领域所有权，通过个人内容库应用服务聚合

账户模块继续拥有用户资料和资料隐私设置；视频模块拥有作品生命周期、可见性和创作者合集；互动模块拥有喜欢/收藏事实；曝光模块拥有观看事件和观看历史投影；新 `library` 模块拥有稍后再看事实，并在 Application 层通过窄接口聚合各模块的视频 ID，再由视频目录批量补齐卡片。

`application/library.Service` 依赖：

- `ActionIndex`：按用户和 LIKE/FAVORITE 返回稳定游标页。
- `HistoryIndex`：返回最近观看视频、进度和完成状态。
- `WatchLaterRepository`：读写稍后再看事实。
- `VideoCatalog`：按有序 ID 批量读取可展示视频卡。

这些接口由 composition root 中的适配器连接现有模块，避免 library Domain 依赖 interaction、exposure 或 video 的具体实现。

替代方案是让账户 Handler 直接查询所有仓储。该方式会把跨模块排序、隐私和视频过滤规则放入 HTTP 层，无法单元测试且违反依赖方向，因此不采用。

### 2. 资料扩展使用账户字段、设置表和内容统计表

`account` 增加 `gender`，领域值限定为 `0 unspecified / 1 male / 2 female / 3 other`，`PATCH /api/users/me` 支持可选更新。

新增 `account_profile_setting`：

- `user_id` 主键。
- `liked_visibility`、`favorite_visibility`，取值 `private/public`，默认 `private`。
- `created_at`、`updated_at`。

`PATCH /api/users/me` 可携带嵌套 `profile_settings`，资料字段和隐私设置通过账户仓储的单一事务边界提交并返回完整当前资料。仓储只更新请求实际提供的列，避免事务前读取的旧资料或旧设置覆盖并发写入的其他字段。独立 profile-settings 接口继续兼容并使用相同的部分列更新语义。Web 只编辑 `liked_visibility`；收藏始终 owner-only，兼容字段由 Web 写回 `private`。

新增由视频模块维护的 `user_content_stat`：

- `user_id` 主键。
- `public_work_count`、`private_work_count`、`received_like_count`、`collection_count`。
- `created_at`、`updated_at`。

视频创建、删除、可见性变化和合集变化在各自事务中维护内容统计；互动 Worker 在持久化点赞计数变化时同步调整视频作者的 `received_like_count`。资料查询读取聚合表而不是每次对全部视频求和。

替代方案是实时 `SUM(video_stat.like_count)`。该方案实现简单，但作者作品增长后会让每次资料访问扫描大量行，因此只用于迁移校验，不用于在线路径。

### 3. 作品生命周期与可见性分离

现有 `video.status` 继续表达草稿、已发布、下架和删除。新增 `visibility` 表达 `public/private`，默认 `public`。

- Feed、公开视频详情和公开用户作品始终要求 `status=published AND visibility=public`。
- 作者本人可通过管理查询读取未删除的公开和私密作品。
- 将作品设为私密不会改变发布审核状态，但会立即从 Feed、公开主页、喜欢/收藏公共列表和合集公共读取中消失。
- 将作品重新公开后恢复公开读取资格，但不会改变原始 `published_at`。
- 新增 `local_upload_asset(asset_url, owner_id, kind, created_at)` 保存保护视频/封面的不可变认证上传者，并从现有唯一作者引用幂等回填。发布保护 URL 时必须由上传者本人创建视频；读取时只有同一 owner 的有效视频引用参与授权。已发布公开资源可匿名读取，owner 通过仅限 `/uploads` 的 HttpOnly 资产 Cookie 与 Web 活跃标记读取自己的非删除私密/下架资源，删除、未引用、无 owner 或跨作者重引用的保护资源返回 404。

该拆分避免把“私密”错误建模为审核状态，保留治理模块未来对下架和删除状态的独立控制。

### 4. 新增管理查询而不破坏旧作品列表

保留现有：

- `GET /api/users/me/videos`
- `GET /api/users/{userId}/videos`

以兼容现有调用方。

新增：

- `POST /api/users/me/video-queries`
  - 请求：`visibility`、`query`、`created_from`、`created_to`、`cursor`、`limit`。
  - 排序：`created_at DESC, id DESC`。
  - 响应：`items`、`next_cursor`、`has_more`。
- `POST /api/users/me/video-batch-actions`
  - 请求：`video_ids`（最多 100）、`action=make_public|make_private|delete`。
  - 必须支持 `Idempotency-Key`。

批量操作在事务中先锁定并验证全部视频归属，再执行全部变化；任一视频不存在、非本人或状态不允许时整体失败。新增 `video_batch_operation` 保存用户、幂等键、请求指纹和结果，重复同一请求返回原结果，同键不同请求返回 409。

### 5. 创作者合集属于视频模块

新增：

- `video_collection`：`id`、`owner_id`、`title`、`description`、`visibility`、`status`、`idempotency_key`、时间字段。
- `video_collection_item`：`collection_id`、`video_id`、`position`、`created_at`，合集与视频唯一。

接口：

- `GET/POST /api/users/me/video-collections`
- `PATCH/DELETE /api/users/me/video-collections/{collectionId}`
- `PUT/DELETE /api/users/me/video-collections/{collectionId}/videos/{videoId}`
- `GET /api/users/{userId}/video-collections`

只有视频作者可将自己的未删除视频加入自己的合集。公开合集读取时过滤私密、下架和删除视频；私密合集只对拥有者可见。列表按 `updated_at DESC, id DESC` 游标分页，合集内按 `position ASC, video_id ASC` 稳定排序。成员真实增加或移除时才触碰合集 `updated_at`；重复 PUT/DELETE 不改变排序。合集 PATCH 返回值重新读取并补齐真实成员视频卡。

合集列表不逐合集补齐成员。仓储先读取合集页，再批量读取这些合集的有序成员，最后一次批量补齐视频卡。公开主页是预览用途，每个合集最多返回领域常量规定的 3 张公开可读成员卡，并通过 `member_count` 返回公开可读成员总数；因此匿名 `limit=100` 最多补齐 300 张卡。本人合集列表继续批量返回全部未删除成员，保证现有编辑器能识别和修改完整成员关系。

### 6. 喜欢和收藏列表扩展现有互动事实

`interaction.Repository` 增加按 `user_id + action_type + status=active` 的游标查询，使用 `updated_at DESC, video_id DESC`。接口由 library Handler 暴露：

- `GET /api/users/me/liked-videos`
- `GET /api/users/me/favorite-videos`
- `GET /api/users/{userId}/liked-videos`，仅在目标用户隐私设置允许时返回。

收藏列表默认仅本人可见，不提供公开接口。列表结果通过 `VideoCatalog` 过滤不可展示内容并保持行为顺序；因视频变私密或删除造成的缺口允许少于请求 limit，但服务会在有限次数内补取候选，避免一页大量空洞。

同步互动请求和异步事件落库使用不同边界。HTTP `SetAction` 始终锁定并要求视频当前为 `published + public`；Worker 的 `PersistAcceptedActionEvent` 只接收已经在发布前通过该校验的内部事件，因此消费前发生私密或下架变化时仍保存事实和计数。Redis 在状态和计数的同一个 CAS 事务中为每个 `(user, video, action)` 分配单调版本，并把版本、事件 ID 与发生时间保存在快速状态中；缓存缺失时从 PostgreSQL 最新版本继续。RabbitMQ 行为事件使用 publisher confirm，发布失败或确认不确定时 API 在短时恢复上下文中同步持久化同一事件。若发布与同步持久化都失败，只在 Redis 当前状态仍由该版本拥有时回滚状态和计数，版本计数器不回退；并发更高版本不得被旧请求回滚。Redis 事务提交后若计数读取失败，缓存层返回已提交事件元数据，应用使用脱离请求取消/截止时间且有超时的上下文做相同版本条件回滚；回滚报错时重新确认投递原事件并以同步事件回执兜底，全部恢复尝试失败时仍保留相同幂等键重发原事件的路径。相同幂等键重试会重发原事件，避免 `delta=0` 跳过恢复。`interaction_action_event` 以 `event_id` 保存含版本的同事务回执，重复投递不会重复更新视频计数或作者获赞。`interaction_action` 保存最新 `version + occurred_at + event_id`，Worker 首先比较版本，同版本才用时间和事件 ID 兼容定序。迁移把旧行为安全回填为版本 `0`、`updated_at` 和空事件 ID。无效载荷、事件 ID 冲突、缺失视频和已删除视频分类为终止错误并停止重新入队，瞬时数据库错误继续重试。所有个人库和公开读取仍以消费时的当前可读性过滤视频，事件持久化不授予内容读取权限。

### 7. 观看历史由曝光模块同步维护投影

新增 `video_view_history`：

- `(user_id, video_id)` 唯一。
- `last_scene`、`last_event_type`、`last_watch_ms`、`completed`。
- `first_watched_at`、`last_watched_at`、时间字段。

`SaveViewEvent` 在保存 `play/complete/skip` 事件的同一数据库事务中 upsert 历史投影；纯 `exposed` 不进入观看历史。历史按 `last_watched_at DESC, video_id DESC` 查询。投影保存 `last_event_id`，仅允许更大的 `(created_at, event_id)` 覆盖最近状态；旧事件晚提交时只可补早 `first_watched_at`，不能回退进度、完播或最近时间。

接口：

- `GET /api/users/me/watch-history`
- `DELETE /api/users/me/watch-history/{videoId}`
- `DELETE /api/users/me/watch-history`

清空和单项删除只删除历史投影，不删除原始行为流水和推荐曝光事实。

### 8. 稍后再看使用独立幂等状态表

新增 `user_watch_later`：

- `(user_id, video_id)` 唯一。
- `status=active/removed`。
- `created_at`、`updated_at`。

接口：

- `GET /api/users/me/watch-later`
- `PUT /api/videos/{videoId}/watch-later`
- `DELETE /api/videos/{videoId}/watch-later`

PUT/DELETE 具有自然幂等性，返回 `video_id`、`active` 和 `updated_at`。列表按 `updated_at DESC, video_id DESC`，只返回当前仍可被用户读取的视频。

### 9. Profile API 使用加法扩展并执行隐私裁剪

`GET /api/users/me` 和 `GET /api/users/{userId}` 增加：

- `gender`
- `received_like_count`
- `public_work_count`
- 自己的响应包含 `profile_settings`

公开用户资料不返回登录账号以外的敏感字段。当前公开账号标识继续使用已有规范化 `account`，前端展示为“账号：...”而不新增伪造的抖音号。

新增：

- `GET/PATCH /api/users/me/profile-settings`

公开主页只展示允许公开的喜欢标签；收藏、观看历史和稍后再看始终只对本人开放。

`DELETE /api/sessions/current` 不经过强制鉴权；无 Token 或过期 Token 也返回 204。JWT 登出无服务端状态，响应不得修改资产 Token 或活跃标记 Cookie，否则旧慢登出响应可能清除更新登录刚写入的凭据。资产 Token 只在登录响应写入，普通鉴权响应不得刷新。Cookie 资产身份还要求 Web 维护的 SameSite=Strict、非 HttpOnly 活跃标记；Web 退出时先同步清除本地会话和该标记，再尽力调用无 Cookie 副作用的登出接口，因此离线退出立即关闭私有资产，而旧登出响应不能破坏更新登录。

### 10. Web 个人页按数据状态拆分组件和 hooks

新增或重构：

- `ProfileHero`
- `ProfilePrimaryTabs`
- `CreatorWorkTabs`
- `CreatorWorkToolbar`
- `ProfileVideoGrid`
- `ProfileCollectionGrid`
- `ProfileEmptyState`
- `ProfileEditor`
- `useProfileLibrary`
- `useCreatorContent`

一级标签为 `works | recommend | likes | favorites | history | watchLater`。作品二级标签为 `published | private | collections`。短剧和预约不渲染。

推荐标签复用现有 recommendation Feed 数据，不新增专用推荐接口。其他标签按各自游标独立保存 loading/error/empty/ready 状态，切换标签不覆盖另一个标签的缓存页。关系弹窗分页绑定请求代次、打开状态和发起时 Tab，切换或关闭后拒绝旧响应；清空观看历史先使在途历史请求失效，并在清空期间拒绝启动新分页。

公开主页通过 `GET /api/users/me/following/{targetUserId}` 直接读取单目标关系状态，不再扫描最多 2,000 条关注记录。关注/取关开始时使尚未完成的关系读取失效，因此成功写入不会被旧读响应覆盖。

会话动作使用稳定回调身份，避免资料写回触发依赖这些动作的 effect 重跑。公开主页以 `userId` 作为组件实例边界并用请求序号拒绝旧响应。合集创建为同一规范化载荷保留幂等键直到成功；稍后再看回滚只合并失败项；合集编辑器使用独立的公开/私密双游标搜索，不依赖主页已加载页。

宽屏资料页移除当前 24px 外层卡片留白和圆角 Hero，使用全宽头图、112px 头像、内联统计、18px 一级标签、14px 二级胶囊、右侧筛选工具和六列 3:4 网格。紧凑桌面保留 72px 图标栏并允许工具栏换行。900px 以下继续使用 GCFeed 现有移动布局，不尝试复制抖音移动 App。

### 11. 安全、隐私和数据限制

- 所有 `/users/me/**` 新接口必须鉴权并只使用鉴权上下文中的 user ID。
- 保护本地资产发布必须验证不可变上传 owner；媒体 URL 只能使用所属的 `video` 上传，封面 URL 只能使用所属的 `cover` 上传，`file`、`avatar`、类型互换和无 owner 路径全部拒绝；任意公开视频引用不能替代所有权证明。
- 批量接口最多接受 100 个去重后 ID，合集标题、描述和列表 limit 使用 Domain 常量限制。
- 搜索使用参数化 `ILIKE` 并转义通配符，不拼接 SQL。
- 公共列表在数据库查询和视频补齐两个阶段都执行可见性检查，防止竞态泄露。
- 删除或私密视频后，历史行为事实保留，但任何内容列表不再返回不可读媒体。
- 前端不在 localStorage 缓存私密作品、观看历史或稍后再看正文。

## Risks / Trade-offs

- [跨模块聚合导致查询复杂] → library Service 先读取有序 ID，再批量补齐视频卡；限制补取轮数并为用户行为索引增加覆盖排序字段。
- [互动异步落库使获赞统计短暂延迟] → `user_content_stat` 由同一 Worker 事务维护，API 明确以持久化统计为最终一致来源。
- [互动入队后可见性变化导致队列毒化或丢失已接收事实] → 新请求继续校验当前公开可读性，Worker 使用独立已接收事件入口和事件回执去重；私密/下架变化允许落库，缺失/删除/无效事件终止且不重入队。
- [历史事件表增长或重复启动恢复已删除投影] → 使用 `(user_id, video_id, created_at)` 索引和 PostgreSQL `DISTINCT ON` 回填，并在 `app_migration` 写入持久标记；首次成功后所有启动跳过原始事件回填。
- [批量操作部分成功造成页面与数据库不一致] → 所有权校验和状态变更放入单事务，使用操作记录保证重试稳定。
- [私密切换与缓存产生短暂泄露] → 公开 Feed/卡片缓存键加入可见性版本或在可见性变化后主动删除相关卡片和 Feed 索引。
- [个人页一次加载全部标签增加首屏成本] → 首屏只请求资料和当前标签，其他标签按需加载并保留分页状态。
- [完整对齐范围较大] → 实施按数据模型/API、Web 结构、视觉验证三个阶段推进，每阶段保持旧接口可用。

## Migration Plan

1. 增加账户 gender、视频 visibility、资料设置、内容统计、观看历史、稍后再看、合集、合集成员和批量操作表，并注册到统一迁移。
2. 以现有账户、视频、video_stat、interaction_action 和 video_view_events 数据回填资料设置、内容统计和最近观看历史；旧互动行为的最新版本回填为 `0`，兼容顺序使用 `updated_at` 和空事件 ID。观看历史原始事件回填成功后写入持久迁移标记，后续启动不得重复执行，以免恢复用户已删除的投影。现有视频 visibility 默认 public。内容统计 reconciliation 以同一 SQL 快照中的“事实值 - 基线聚合值”计算修复差量，再叠加到锁定后的当前统计行，避免覆盖在线并发增量。
3. 先部署兼容旧接口的新后端和新接口，验证公开 Feed 不返回私密视频。
4. 部署 Web 新个人页，切换到新增查询接口；旧作品接口继续保留。
5. 观察列表查询延迟、历史 upsert、批量事务和内容统计一致性指标。

回滚时可先回滚 Web 到旧个人页，再回滚 API 代码；新增列和表保持不删除，旧代码会忽略它们。若内容统计异常，可从 video/video_stat 重新构建 `user_content_stat`。

## Open Questions

- 是否需要在后续独立变更中为公开视频合集增加手动拖拽排序；本变更只保证显式 position 和稳定读取。
- 是否需要把公开“喜欢”能力默认开启；本设计采用隐私优先的默认 private。
