# GCFeed 工程规范

本文定义 GCFeed 的目录职责、代码风格、接口设计、数据模型和测试约定。新增功能时优先遵循本文，再查看对应模块文档。

## 1. 技术栈

| 区域 | 技术 |
| --- | --- |
| API | Go、Hertz、GORM |
| 数据库 | PostgreSQL |
| 缓存 | Redis |
| 消息队列 | RabbitMQ |
| 鉴权 | JWT |
| Web | React、Vite |
| 规格驱动 | OpenSpec |

## 2. 后端分层

后端位于 `apps/api`，采用四层结构：

```text
apps/api/
  cmd/feed/main.go
  cmd/worker/main.go
  configs/
  internal/
    domain/{module}/
    application/{module}/
    infra/
    infra/httphertz/
    infra/persistence/{module}/
    interfaces/http/{module}/
    interfaces/http/router/router.go
  test/
```

| 层 | 职责 | 依赖方向 |
| --- | --- | --- |
| Domain | 实体、领域错误、业务不变量、仓储接口 | 只依赖标准库 |
| Application | 用例编排、分页游标、幂等、跨实体流程 | 依赖 Domain 接口 |
| Infrastructure | GORM、Redis、RabbitMQ、JWT、配置 | 实现 Domain/Application 所需接口 |
| Interfaces | HTTP Handler、DTO、路由、中间件 | 调用 Application Service |

模块接入顺序：

1. 定义 Domain 实体、错误和仓储接口。
2. 编写 Application Service。
3. 实现 GORM 模型和 Repository。
4. 编写 HTTP DTO 和 Handler。
5. 在 `router.Register` 装配 `Repository -> Service -> Handler -> Route`。
6. 补充 API 流程测试和模块文档。

跨模块聚合仍遵守领域所有权。以个人内容库为例，`application/library.Service` 只依赖 `ActionIndex`、`HistoryIndex`、`WatchLaterRepository`、`VideoCatalog`、`PrivacyReader` 等窄接口；`interfaces/http/router/library_adapters.go` 把 interaction、exposure、video、account 的接口适配为 library 需要的形状。不要让聚合 Handler 直接查询多个 GORM Repository，也不要让一个 Domain 包导入另一个模块的 Infrastructure。

## 3. 新增后端模块文件组

```text
apps/api/internal/domain/{module}/entity.go
apps/api/internal/domain/{module}/errors.go
apps/api/internal/domain/{module}/repository.go
apps/api/internal/application/{module}/service.go
apps/api/internal/infra/persistence/{module}/model.go
apps/api/internal/infra/persistence/{module}/gorm.go
apps/api/internal/interfaces/http/{module}/dto.go
apps/api/internal/interfaces/http/{module}/handler.go
apps/api/test/{module}_api_test.go
docs/modules/{module}.md
```

当前模块体量继续增长时，按职责拆出 `cursor.go`、`worker.go`、`event.go`、`errors.go` 等文件。

跨模块聚合可以在 composition root 同目录增加 `{module}_adapters.go`；仅放无状态的接口转换，不放业务规则或 SQL。

## 4. Go 包和命名

包名使用层级前缀 + 模块名：

```go
package domainvideo
package applicationvideo
package infravideo
package interfaceshttpvideo
```

导入时使用同名别名：

```go
import (
    applicationvideo "GCFeed/internal/application/video"
    domainvideo "GCFeed/internal/domain/video"
)
```

常用类型命名：

| 类型 | 命名 |
| --- | --- |
| 应用服务 | `Service`，构造函数 `New` |
| HTTP 入口 | `Handler`，构造函数 `New` |
| 仓储接口 | `Repository` |
| 仓储实现 | `Repository`，通过包名区分 |
| GORM 模型 | `{Entity}Model` |
| 响应转换 | `{xxx}ResponseFromDomain`、`{xxx}ResponseFromResult` |
| 参数解析 | `parse{Field}`、`parsePositiveInt64`、`parseLimit` |
| 错误写入 | `write{Module}Error` |

常量位置：

| 常量类型 | 位置 |
| --- | --- |
| 领域枚举、最大长度 | `domain/{module}/entity.go` |
| 应用默认值 | `application/{module}/service.go` |
| HTTP query 默认值 | `interfaces/http/{module}/handler.go` |

## 5. Domain 规则

Domain 层负责业务不变量和领域表达。

放在 Domain 的内容：

- 实体结构体。
- 状态常量和业务限制。
- 领域错误。
- 领域构造函数，例如 `NewPublished`、`NewComment`、`NewFollow`。
- 数据恢复函数，例如 `RestoreVideo`、`RestoreUserWithStats`。
- 实体方法，例如 `Authenticate`、`UpdateProfile`、`DeleteBy`、`Active`。
- 仓储接口。

领域构造函数负责清理字符串输入、校验 ID、校验必填字段、校验长度限制、设置默认状态和业务时间。读取路径使用 `Restore*` 保留数据库状态并做展示字段清洗。

## 6. Application 规则

Application 层负责用例编排。

放在 Application 的内容：

- `Service`。
- 用例入参结构，例如 `FeedRequest`。
- 用例返回结构，例如 `LoginResult`、`CreateResult`、`CommentListResult`。
- 跨实体流程，例如发布视频时处理幂等键。
- 游标解析和编码。
- 默认分页大小和最大分页裁剪。
- 基础设施能力的最小接口，例如 `TokenSigner`。

Service 依赖 Domain 的 `Repository` 接口，构造函数注入依赖。Redis、RabbitMQ、JWT 这类能力通过小接口注入，便于测试。

可选基础设施继续使用 functional option，例如账户资料设置仓储和视频缓存失效器。必需的聚合依赖应在构造函数中显式传入，避免运行时缺少核心数据源。

## 7. Infrastructure 规则

Infrastructure 层负责外部资源和技术实现。

主要目录：

```text
internal/infra/config/
internal/infra/database/
internal/infra/httphertz/
internal/infra/cache/
internal/infra/mq/
internal/infra/jwt/
internal/infra/persistence/{module}/
internal/infra/persistence/migration/
internal/infra/media/
```

GORM Repository 规则：

- 每个模块独立 `model.go` 和 `gorm.go`。
- `model.go` 只定义数据库模型和 `TableName`。
- `gorm.go` 实现 Domain Repository 接口。
- 写操作尽量保持事务边界清晰。
- 列表查询使用稳定排序字段和游标。
- 返回 Domain 实体，避免把 GORM 模型泄漏到 Application。
- PostgreSQL 唯一约束错误由启用 `TranslateError` 的 GORM 统一映射为 `gorm.ErrDuplicatedKey`。
- 显式索引名使用表名前缀，避免 PostgreSQL schema 级索引命名冲突。
- API 和 Worker 的完整 schema 初始化在同一个 PostgreSQL advisory transaction lock 内执行。
- `AutoMigrate` 后的模块回填保持显式且有顺序：补齐视频统计和可见性默认值、补齐资料隐私设置、用版本 `0` 与现有行为 `updated_at` 回填异步互动最新事件顺序、重建内容聚合、仅在 `app_migration` 无持久标记时从原始事件回填观看历史，再创建 Feed 专用索引。可删除投影的原始事实回填不得在每次启动重复执行。
- 跨表聚合计数写入和事实变化放在同一事务；提供基于事实表的 reconciliation 函数作为迁移和修复入口。
- 在线实例可能并发写聚合时，reconciliation 不得绝对覆盖统计行；应基于同一语句快照计算“事实值 - 快照聚合值”差量，再叠加到获得行锁后的当前值。

## 8. Interfaces 规则

Interfaces 层负责 HTTP 入口。

Handler 职责：

- 解析 path、query、body 和 header。
- 从鉴权上下文读取用户 ID。
- 调用 Application Service。
- 将结果转换为响应 DTO。
- 将业务错误映射成 HTTP 状态码。

Hertz Handler 使用 `func(context.Context, *app.RequestContext)` 签名。标准 `context.Context` 传入 Application Service，并启用客户端断开取消；鉴权身份等同步请求数据保存在 `RequestContext.Keys`，不得让池化的 `RequestContext` 逃逸请求生命周期。JSON 请求统一使用 HTTP binding 包的有界解码器，避免流式请求体被无限读入内存；隐私或协议敏感接口使用 `BindStrictJSON` 拒绝未知字段和尾随 JSON。

Handler 避免承载业务规则。业务判断放在 Domain 或 Application。

本地 `/uploads` 中的视频和封面必须记录不可变认证上传者。发布保护 URL 时验证上传者与作者一致；读取时同时验证不可变所有权、同所有者视频引用、生命周期、可见性与当前身份，再交给标准库文件服务并保留 Range/HEAD 语义。不得仅因“任意公开视频引用该 URL”就授权。浏览器媒体标签通过仅限 `/uploads` 的 HttpOnly 资产 Cookie 携带身份；Cookie 身份还必须同时具备 Web 会话维护的 SameSite=Strict、非 HttpOnly 活跃标记。退出时 Web 先同步删除活跃标记和本地登录态，再尽力请求无 Cookie 副作用的无状态登出接口，因此离线退出立即关闭私有资产访问，旧登出响应也不能清除更新登录的资产 Token；普通鉴权响应不得刷新资产 Cookie。不得把访问 Token 放入媒体 URL。头像和普通文件维持公开兼容。

生产媒体使用 `domain/media` 中的 `MediaObjectStore` 和 `MediaURLResolver` 窄接口。S3/MinIO、CDN、ffprobe/ffmpeg 和本地文件实现放在 `internal/infra/media`；Domain、Application 和 HTTP DTO 不导入 AWS SDK 类型。直传会话绑定 owner、kind、精确对象键、大小、SHA-256 和过期时间，完成前必须执行 HEAD 校验。公共输出使用不可变内容寻址键，原始/私密资源使用短期签名 URL，访问令牌不得进入对象 URL。

## 9. HTTP API 规范

路径使用资源名，方法表达动作：

| 方法 | 用途 |
| --- | --- |
| `GET` | 查询资源 |
| `POST` | 创建资源或提交复杂查询 |
| `PUT` | 设置确定状态 |
| `PATCH` | 部分更新 |
| `DELETE` | 删除或取消 |

路径约定：

```text
POST   /api/users
POST   /api/sessions
DELETE /api/sessions/current
GET    /api/users/me
PATCH  /api/users/me
POST   /api/videos
GET    /api/videos/{videoId}
GET    /api/feed-items
POST   /api/feed-queries
PUT    /api/videos/{videoId}/like
DELETE /api/videos/{videoId}/like
```

状态码约定：

| 状态码 | 场景 |
| --- | --- |
| `200` | 查询、更新、删除成功 |
| `201` | 创建成功 |
| `400` | 参数格式或业务输入错误 |
| `401` | 登录态缺失或 Token 异常 |
| `403` | 已登录但权限不足 |
| `404` | 资源缺失 |
| `409` | 幂等冲突或唯一性冲突 |
| `500` | 服务内部错误 |

写接口支持 `Idempotency-Key` 时，客户端可传最长 128 字符的幂等键。重复请求返回同一业务结果。

## 10. 分页和游标

列表接口优先使用游标分页。游标内容使用排序字段，编码为 URL-safe 字符串。

常见排序：

| 列表 | 排序 |
| --- | --- |
| Timeline Feed | `published_at DESC, id DESC` |
| 评论列表 | `created_at DESC, id DESC` |
| 关注列表 | `updated_at DESC, target_user_id DESC` |
| 粉丝列表 | `updated_at DESC, user_id DESC` |
| 创作者作品查询 | `created_at DESC, id DESC` |
| 创作者合集 | `updated_at DESC, id DESC` |
| 喜欢、收藏、稍后再看 | `updated_at DESC, video_id DESC` |
| 观看历史 | `last_watched_at DESC, video_id DESC` |

返回结构：

```json
{
  "items": [],
  "next_cursor": "",
  "has_more": false
}
```

## 11. 数据库规范

表名使用小写蛇形命名。领域主表使用单数名，例如 `user`、`video`。关系和行为表使用业务事实名，例如 `user_follow`、`interaction_action`。

通用字段：

| 字段 | 说明 |
| --- | --- |
| `id` | BIGINT 主键 |
| `created_at` | 创建时间 |
| `updated_at` | 更新时间 |
| `status` | 软状态字段 |
| `idempotency_key` | 写操作幂等键 |

高频计数独立成统计表，例如 `video_stat`、`user_relation_stat`。计数更新与事实写入放在同一事务中完成。缓存计数允许短暂偏差，持久化表保存最终事实。

生产媒体公开读条件额外要求 `media_status IN ('legacy_ready', 'ready')`。新对象存储视频在处理期间不计入公开作品数；基线就绪转换与统计增量在同一视频投影事务中更新。媒体任务按 `(asset_id, profile_version)` 唯一并使用数据库租约和指数退避，RabbitMQ 只负责唤醒，数据库任务是可恢复事实来源。对象删除不得放进用户请求事务；视频删除创建延迟 `media_cleanup_task`，Worker 幂等删除，Reconciler 修复过期租约、缺失对象和孤儿对象。

视频 `status` 表达生命周期，`visibility` 表达公开/私密，两者不得复用。所有匿名或跨用户内容读取必须同时验证 `status=published AND visibility=public`；缓存命中不能跳过数据库可读性校验。

最新状态投影与原始流水分表保存。例如 `video_view_events` 是不可变观看流水，`video_view_history` 是可删除的用户历史投影；清空投影不得级联删除原始事实。

端侧生命周期事件必须携带稳定 `event_id`，播放会话内使用 `playback_session_id + sequence`，跨请求最新状态使用有界 `occurred_at + event_id` 定序。相同用户重放相同事件不得重复更新投影；同 ID 不同规范化载荷必须返回冲突。历史聚合修复只更新仍存在的投影行，不得从原始事件重新创建用户已删除的历史。

播放技术遥测使用独立版本化批次，不进入观看历史或推荐行为投影。批次和事件载荷先规范化再计算哈希；同一 reporter 的写入用事务 advisory lock 串行，安全重放只计 duplicate，同 ID 异载荷回滚整批。原始遥测按 `created_at` 有界清理。

需要可靠投递到 RabbitMQ、但不能让外部队列决定 HTTP 事实是否提交的写路径使用 PostgreSQL Transactional Outbox。业务事实、投影与 Outbox 同事务提交；Worker 通过租约、重试和 publisher confirm 分发，下游继续按业务事件 ID 去重。

账号标识在 Domain 层统一去除首尾空白并转为小写；昵称、密码和非账号幂等键保持各自原有的大小写语义。

## 12. 错误处理

Domain 定义明确业务错误：

```go
var ErrInvalidVideoID = errors.New("invalid video id")
```

Application 可以包装跨资源错误，但保留可判断性：

```go
return nil, fmt.Errorf("%w: %d", domainvideo.ErrVideoNotFound, videoID)
```

HTTP 层使用 `errors.Is` 映射状态码。响应保持简洁：

```json
{"error":"invalid request"}
```

## 13. 前端规范

前端位于 `apps/web`，源码全部为 TypeScript（`strict` 全开），按模块分层：

```text
apps/web/src/types.ts        # 领域/API 类型 + localStorage type guard
apps/web/src/feedPreload.ts  # Feed 预加载契约、网络策略、候选顺序与分页边界
apps/web/src/feedPreloadController.ts # 有界原生媒体资源、代际取消、复用与调试状态
apps/web/src/api/            # apiRequest<T> 客户端与按域 fetch（feed/messages/social/account/creator/library）
apps/web/src/session.tsx     # SessionContext + useSession/useUnreadCount
apps/web/src/router.tsx      # Route union + normalizeRoute + useRoute/useNavigate
apps/web/src/pages/          # Login/Feed/Messages/Profile/PublicProfile/Upload
apps/web/src/components/     # AppShell/导航/Icon/VideoStage/FeedDetailsPanel 等共享组件
apps/web/src/hooks/          # useFeed/useFeedPreloading/useComments/useSwipe/useCreatorContent/useProfileLibrary
apps/web/src/styles/         # tokens/base/shell/feed/pages/responsive 模块化样式
apps/web/src/App.tsx         # Provider 组装 + 路由分发
apps/web/src/main.tsx
apps/web/src/styles.css      # 按固定顺序聚合 styles/ 下的样式
```

规则：

- 组件使用函数组件，props 全部显式 interface 类型化。
- API 调用集中使用 `api/src/client.ts` 的 `apiRequest<T>`，按域拆分 fetch 函数。
- 会话与导航通过 `useSession`/`useNavigate` 分发，不做多层 props 透传。
- localStorage 读出的 JSON 必须过 `types.ts` 的 type guard 窄化。
- 禁止 `@ts-nocheck`/`@ts-expect-error`/显式 `any`；构建门禁为 `tsc --noEmit && vite build`。
- 服务端错误显示为用户可理解文案。
- 页面状态保持清楚：loading、error、empty、success。
- 多 Tab 页面为每个 Tab 独立保存 items、cursor、hasMore、loading 和 error；切换 Tab 不得用另一列表覆盖已加载页。
- 个人内容正文不写入 localStorage；当前仅公开资料摘要可通过既有 type guard 缓存。
- 公开主页只渲染后端明确允许的能力。收藏、观看历史、稍后再看和私密作品不出现在公开页面；没有领域模型的“短剧”和“我的预约”不得添加占位 Tab。
- CSS class 使用语义命名。
- 图标使用 `components/Icon.tsx` 的本地 SVG 注册表与 `IconName` 联合类型，不引入图标字体或复制第三方品牌资产。
- 用户端 Shell 通过稳定 `data-ui` 标记支持浏览器几何和响应式验证。
- Feed 预加载候选必须来自活动场景已返回的有序 items；兼容 `/api/preload-videos` 不得作为 Web 场景排序来源。
- 保留媒体资源必须有严格数量上限，并在 scene、请求代际、登录态或源版本变化时清理监听器、定时器、src 和缓冲状态。
- 播放遥测必须是内存有界、失败隔离的附属能力；稳定 ID、单调 offset、首帧 fallback 和页面退出 flush 不得改变用户可见播放结果。

## 14. 测试规范

后端测试位于 `apps/api/test`。新增接口至少覆盖：

- 成功路径。
- 参数错误。
- 鉴权错误。
- 幂等重复请求。
- 游标分页稳定性。
- 关键状态变化和计数变化。

HTTP API 流程测试使用 Hertz `pkg/common/ut.PerformRequest` 在进程内调用路由。依赖真实网络 writer 的 `http.Handler` adaptor 集成（例如静态 Range 或 Prometheus）使用短生命周期本地 listener 验证。

常用命令：

```bash
cd apps/api
go test ./...
```

Web 单元测试和构建命令：

```bash
cd apps/web
pnpm run test
pnpm run build
```

OpenSpec 校验命令：

```bash
openspec validate --all --strict
```

## 15. 文档同步

改动以下内容时同步文档：

| 改动 | 需要同步 |
| --- | --- |
| 新增接口 | `docs/product.md`、`docs/modules/{module}.md` |
| 新增模块 | `docs/modules/README.md`、模块文档、OpenSpec |
| 改目录或分层 | 本文、`docs/quickread.md` |
| 改核心链路 | `docs/architecture.md` |
| 改前端页面 | `docs/uiux.md` |
| 改性能策略 | `docs/optimization.md` |

新增后端能力检查清单：

- Domain 实体、错误和仓储接口完整。
- Application Service 覆盖核心用例。
- Infrastructure Repository 实现接口。
- Handler 完成参数解析和错误映射。
- Router 完成依赖装配。
- API 测试覆盖成功和失败路径。
- 模块文档和产品状态更新。
