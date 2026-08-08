# Frux 工程规范

本文定义 Frux 的目录职责、代码风格、接口设计、数据模型和测试约定。新增功能时优先遵循本文，再查看对应模块文档。

## 1. 技术栈

| 区域 | 技术 |
| --- | --- |
| API | Go、Hertz、GORM |
| 数据库 | PostgreSQL |
| 缓存 | Redis |
| 消息队列 / 事件流 | RabbitMQ、Apache Kafka（KRaft） |
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

全局搜索使用相同模式：`application/search.Service` 只依赖视频与账户搜索索引，游标、query 绑定和输入限制由 search Domain/Application 拥有；PostgreSQL `ILIKE`、公开视频/正常账户过滤和 DTO 映射留在 Infrastructure/Interfaces。搜索 Handler 不得直接拼 SQL 或跨仓储聚合。

评论通知同样遵守所有权：`interaction` 拥有根/回复/评论点赞事实、计数和 `interaction_comment_notification_outbox`；Worker 通过 Application 层的窄 `CommentNotificationMessageWriter` 调用 `message.Service`。message 只持久化消息和结构化目标，不反查互动表；interaction Domain/Application 不导入 message Infrastructure。

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
    applicationvideo "github.com/shiyudesu/frux/internal/application/video"
    domainvideo "github.com/shiyudesu/frux/internal/domain/video"
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
- 同一资源有多种排序时，游标必须包含版本和 sort discriminator；跨排序复用必须拒绝。两级评论根游标绑定 `latest/hot`，回复游标独立按正序元组编码。
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
internal/infra/kafka/
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
- 聚合列表水合必须保持查询次数有界。根评论页最多 100 根时，回复预览使用 window function 每根上限 3 条，作者、直接目标和 viewer 状态批量读取；禁止在 Application/Handler 中逐根调用 Repository。
- 返回 Domain 实体，避免把 GORM 模型泄漏到 Application。
- PostgreSQL 唯一约束错误由启用 `TranslateError` 的 GORM 统一映射为 `gorm.ErrDuplicatedKey`。
- 显式索引名使用表名前缀，避免 PostgreSQL schema 级索引命名冲突。
- API 和 Worker 的完整 schema 初始化在同一个 PostgreSQL advisory transaction lock 内执行。
- `AutoMigrate` 后的模块回填保持显式且有顺序：补齐视频统计和可见性默认值、补齐资料隐私设置、用版本 `0` 与现有行为 `updated_at` 回填异步互动最新事件顺序、重建内容聚合、仅在 `app_migration` 无持久标记时从原始事件回填观看历史，再创建 Feed 专用索引。可删除投影的原始事实回填不得在每次启动重复执行。
- 跨表聚合计数写入和事实变化放在同一事务；提供基于事实表的 reconciliation 函数作为迁移和修复入口。
- 在线实例可能并发写聚合时，reconciliation 不得绝对覆盖统计行；应基于同一语句快照计算“事实值 - 快照聚合值”差量，再叠加到获得行锁后的当前值。
- 由业务模块拥有的 Transactional Outbox 与业务事实同事务提交。通用 Worker 模式为：有界批次、`FOR UPDATE SKIP LOCKED`、稳定 lease owner、租约超时、指数退避、terminal 分类、稳定 event ID 下游去重和受监督 shutdown；不得让 message 或 RabbitMQ 成为互动 HTTP 事务的提交前依赖。
- RabbitMQ Consumer 必须在 Ack/Nack 前分类：格式错误、无效必填字段和 terminal domain error 使用 reject/no-requeue；基础设施错误才 requeue。受保护 Consumer 使用新名称 Quorum Queue、`x-delivery-limit`、`overflow=reject-publish` 和有界 DLQ；Queue Type 不得原地修改。所有新 Quorum Consumer（包括 `dual` 次 Consumer）必须使用独立受监督 Channel 和最大 30 秒的有界退避。关键流程使用 at-least-once dead-lettering，允许由数据库任务恢复的唤醒队列可使用 at-most-once DLX。
- Kafka Topic、Producer、Consumer Group、Key Kind、Retention、Cleanup Policy 和迁移模式必须来自
  `internal/infra/kafka` 封闭注册表。业务代码不得接受任意 Topic/Group 字符串；Domain 不导入
  franz-go 类型。JSON Envelope 和 Payload 使用显式版本、严格未知字段/尾随数据校验及有界大小。
- Kafka Producer 固定使用 idempotence、`acks=all`、有界 delivery deadline 和逐 Record 结果；
  Application 不在不确定结果后自行无界重发。Consumer 禁用 auto commit，按 Partition 顺序和有界
  并发处理，只在 durable-success 或注册 terminal 结果后显式提交 Offset；Commit 不确定必须结束
  当前 Session，由稳定 Event ID 和耐久幂等边界承受重投。
- Kafka 迁移只允许 registered primary/mirror Producer 与 active/shadow Consumer。Shadow Group
  必须使用独立 Group ID，只做 Envelope、Key、Age 和可选 Parity 校验，不调用变更业务状态的
  Handler。基础设施阶段所有业务流继续保持 RabbitMQ Producer/Consumer active。
- Queue 迁移只允许 `legacy -> dual -> new`。`dual` 期间新旧 Consumer 同时运行，业务层必须按原 Event ID 幂等；旧 Queue ready/unacked 持续归零后才能移除旧 Binding。回滚先恢复 `dual`，不得先删除新 DLQ。
- DLQ Preview 只通过服务端 RabbitMQ Management Adapter 返回 Payload 大小、SHA-256、JSON 顶层字段等脱敏诊断，不复制 Payload 到 PostgreSQL。Operator Replay 仅允许 allowlist Queue 的队头单消息，必须从 `x-death` 验证原 Source Queue、Exchange 和 Routing Key，拒绝直接 DLQ 投递；保持原 Payload/Event ID，增加 Replay ID。成功 Audit Fact 必须在发布前可构造，不能直接进入有界审计字段的合法 Event ID 使用稳定 SHA-256 引用；Publisher Confirm 后写成功审计，完成后才 Ack DLQ。
- 持久化特权操作必须接收已验证的 `domain/adminaudit.Fact`，并在拥有业务变更的 GORM 事务中通过 `infra/persistence/adminaudit.AppendInTransaction` 追加成功事实。审计 Repository 不提供更新或删除；审计插入失败必须使受保护变更回滚。外层事务成功返回后，拥有者才调用 `RecordCommittedWrite` 记录提交指标，不得在事务提交前报告成功。审计 Domain 按 action/outcome 封闭校验 permission、target、method、route、reason 和状态转换；request ID 必须由服务端生成，幂等键只保存 SHA-256 摘要。授权拒绝等无业务提交的尝试由 Application 审计服务使用进程总窗口限额、每操作者窗口限额、全局并发槽和独立短超时异步记录；数据库失败进入低基数指标和安全日志，限额或并发饱和只计 dropped 指标，不能延迟或替换原始 403。
- 运行时降级控制使用 `domain/governance` 封闭注册表；定义必须包含 typed normal/failure
  default、process scope 和 max staleness。持久化使用 immutable revision +
  active pointer，更新/回滚在按 key PostgreSQL advisory lock 内校验 expected revision，并与
  `governance.execute` audit 同事务提交。API/Worker 只能由后台 poller 读取 Repository，验证
  完整 snapshot 后用原子指针替换；Application 热路径只能依赖 `Bool(key)` 等窄 reader，不得
  查询数据库、Redis 或治理 HTTP。新 control 必须同时增加 registry、低基数 metric label、
  normal/missing/expired/stale/process-scope 测试和模块文档。
- 请求限流使用 `application/ratelimit` typed registry；路由只能引用注册 policy，未知名必须
  启动失败。每次请求先执行 bounded local token bucket，entry map 必须有 capacity 和 idle
  expiry，满载时保守拒绝。distributed policy 只允许一次带短 deadline 的 Redis Lua 原子操作，
  并显式声明 stricter local fallback 或 fail closed；Redis 故障不得变为无限流量。
- user quota identity 只能来自 JWT middleware 的 server context；IP quota 只有在 socket peer
  命中配置的 trusted proxy CIDR 时才消费 forwarded header。governance 只能选择代码注册的
  distributed 开关或 emergency profile，不得写入任意 rate。指标标签只使用 registered
  endpoint group、local/distributed/fallback layer 和封闭 result。

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

公开读取需要 viewer 状态时使用 optional-auth middleware：无 Token 或无效 Token 继续匿名读取，有效 access JWT 只把 user/role 写入 `RequestContext.Keys`。根评论、回复和 thread context 使用该模式返回匿名公共数据，并仅在有效 viewer 下补充 `liked`、`can_delete`；创建、点赞和删除仍使用强制鉴权。optional-auth 不得放宽父视频 `published + public + media-ready` 校验。

所有 `/api/admin` 路由必须先使用强制 JWT 鉴权建立用户 ID，再通过参数化 Admin Permission Middleware 读取当前 `account.status/role` 并检查路由声明的单项权限。JWT role claim 不能作为后台授权事实；停用、降权、普通和未知角色默认拒绝。权限中间件把 `AdminPrincipal` 写入 `RequestContext.Keys`，后台 Handler 只能通过共享 helper 读取主体用于归因，不得自行比较角色字符串。当前封闭权限集合和角色映射位于 `domain/account`，后续审核、审计、视频运营和治理模块复用该边界，但继续拥有各自数据与事务。

死信摘要、Preview 和单消息 Replay 均要求 `governance.execute`。Replay 的成功/失败 Audit Fact
必须包含 Queue、原 Event ID、Replay ID、reason code 和封闭 failure code；不得保存原 Payload、
任意 Header 或 RabbitMQ 凭据。

后台审计查询必须要求 `audit.read`，强制提交不超过 31 天的时间范围，并使用绑定全部过滤条件的 `(created_at, id)` 编码游标。HTTP 响应只返回 Domain 已验证的 action-specific detail；Handler 不接受任意详情结构，也不提供审计更新或删除入口。

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
| 评论根列表（最新） | `created_at DESC, id DESC` |
| 评论根列表（热门） | `hot_score DESC, created_at DESC, id DESC` |
| 评论回复 | `created_at ASC, id ASC` |
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

创作者和审核员预览属于受保护读取，不属于公共媒体投影。Owner asset access 在 ready 时优先选择
protected baseline/cover variant，无匹配 variant 时才回退 original；授权仍绑定不可变 owner 和当前
视频引用。签名 URL JSON 必须返回 `Cache-Control: private, no-store`，Web 请求使用 no-store，
凭据只保存在组件内存。审核 cover 解析失败不得使有效 video preview 一并失败。

视频 `status` 表达审核生命周期：1 草稿、2 已发布、3 下架、4 删除、5 待审核、6 已拒绝；`visibility` 表达公开/私密，`media_status` 表达媒体处理，三者不得复用。新建视频固定为待审核且 `published_at` 为空；批准首次设置微秒精度发布时间，恢复下架内容不重写。批准、拒绝、下架和恢复必须在数据库行锁内执行操作型转换，不能用通用目标状态覆盖并发决定。所有匿名或跨用户内容读取必须同时验证 `status=published AND visibility=public AND media_status IN (legacy_ready, ready)`；媒体提升在投影事务内重新确认当前资格，失效变体降回保护前缀，任意状态 hydration、`/media` 直读和缓存命中都不能跳过审核门。公共提升使用 CAS 和新 exposure generation，保护副本不删除；公共媒体缓存最长 60 秒并要求重验证。撤销失败返回错误且幂等重试继续执行，过期并发副作用必须按当前数据库资格补偿。发布事件 ID 绑定 `video_id + published_at`，允许失败后安全重试。

最新状态投影与原始流水分表保存。例如 `video_view_events` 是不可变观看流水，`video_view_history` 是可删除的用户历史投影；清空投影不得级联删除原始事实。

端侧生命周期事件必须携带稳定 `event_id`，播放会话内使用 `playback_session_id + sequence`，跨请求最新状态使用有界 `occurred_at + event_id` 定序。相同用户重放相同事件不得重复更新投影；同 ID 不同规范化载荷必须返回冲突。历史聚合修复只更新仍存在的投影行，不得从原始事件重新创建用户已删除的历史。

播放技术遥测使用独立版本化批次，不进入观看历史或推荐行为投影。批次和事件载荷先规范化再计算哈希；同一 reporter 的写入用事务 advisory lock 串行，安全重放只计 duplicate，同 ID 异载荷回滚整批。原始遥测按 `created_at` 有界清理。

需要可靠投递到 RabbitMQ、但不能让外部队列决定 HTTP 事实是否提交的写路径使用 PostgreSQL Transactional Outbox。业务事实、投影与 Outbox 同事务提交；Worker 通过租约、重试和 publisher confirm 分发，下游继续按业务事件 ID 去重。

Outbox 不要求最终目标一定是 RabbitMQ。评论通知 Outbox 由 Worker 直接调用 message Application 窄接口：互动事务只提交 durable event，消息写入失败后按租约重试，`recipient + event_id` 去重；历史迁移不得合成旧通知。

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

HTTP 层使用 `errors.Is` 映射状态码。所有一方 JSON API 错误响应使用统一信封：

```json
{"code":"INVALID_REQUEST","error":"invalid request"}
```

- `code` 是稳定、与展示语言无关的机器契约，使用大写 snake case；公共协议错误由 Interfaces 层共享定义，模块业务错误由对应 Handler 映射。
- `error` 保留原有简洁文本用于兼容旧客户端和诊断，不是用户界面文案。
- 未预期的仓储、缓存、队列、对象存储或内部服务错误统一映射为安全的 unavailable/internal code，不得返回堆栈、SQL、凭据、对象键或包装后的基础设施错误。
- HTTP 状态码继续表达错误类别；Domain 和 Application 不依赖 HTTP error code。

## 13. 前端规范

前端位于 `apps/web`，源码全部为 TypeScript（`strict` 全开），按模块分层：

```text
apps/web/src/types.ts        # 领域/API 类型 + localStorage type guard
apps/web/src/feedPreload.ts  # Feed 预加载契约、网络策略、候选顺序与分页边界
apps/web/src/feedPreloadController.ts # 有界原生媒体资源、代际取消、复用与调试状态
apps/web/src/player/          # 播放状态机、能力选源、MP4/DASH adapters、fallback 与三槽池
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
- 手写路由的 search 参数也必须类型化和验证。视频讨论使用 `NavigationTarget` 构造 `/videos/${number}`，只接受正整数 `comment`/`highlight`，且 `highlight` 不能脱离根 `comment`；不得让页面直接拼接未校验 query。
- localStorage 读出的 JSON 必须过 `types.ts` 的 type guard 窄化。
- 禁止 `@ts-nocheck`/`@ts-expect-error`/显式 `any`；构建门禁为 `tsc --noEmit && vite build`。
- `ApiError` 仅保存 HTTP status、稳定 code 和诊断文本；组件不得直接展示服务端 `error`/`message`、`ApiError.message`、浏览器错误或任意 `Error.message`。
- 用户可见错误统一经过 `apiErrorMessage`：显式 `UserFacingError` 可展示，网络失败使用固定连接提示，已知 code 查中文目录，未知 4xx 使用调用方 fallback，未知 5xx 使用带“请稍后重试”的安全 fallback。
- 登录凭据错误与登录态失效使用不同 code；前者展示统一的账号或密码错误，后者继续触发清理会话并跳转登录。
- 页面状态保持清楚：loading、error、empty、success。
- 多 Tab 页面为每个 Tab 独立保存 items、cursor、hasMore、loading 和 error；切换 Tab 不得用另一列表覆盖已加载页。
- 多排序/嵌套列表按资源和排序分区保存状态。评论 controller 按 video+sort 保存根页、按 root 保存回复页，并对 preview/context/page 实体按 ID 去重；草稿、展开、focused target 和各操作 busy/error 不能互相覆盖。
- 个人内容正文不写入 localStorage；当前仅公开资料摘要可通过既有 type guard 缓存。
- 公开主页只渲染后端明确允许的能力。收藏、观看历史、稍后再看和私密作品不出现在公开页面；没有领域模型的“短剧”和“我的预约”不得添加占位 Tab。
- CSS class 使用语义命名。
- 图标使用 `components/Icon.tsx` 的本地 SVG 注册表与 `IconName` 联合类型，不引入图标字体或复制第三方品牌资产。
- 用户端 Shell 通过稳定 `data-ui` 标记支持浏览器几何和响应式验证。
- Feed 预加载候选必须来自活动场景已返回的有序 items；兼容 `/api/preload-videos` 不得作为 Web 场景排序来源。
- 保留媒体资源必须有严格数量上限，并在 scene、请求代际、登录态或源版本变化时清理监听器、定时器、src 和缓冲状态。
- 播放遥测必须是内存有界、失败隔离的附属能力；稳定 ID、单调 offset、首帧 fallback 和页面退出 flush 不得改变用户可见播放结果。
- DASH 依赖必须固定版本并通过动态 import 隔离；本地控制 UI 只消费 `NormalizedPlayerState`，不得直接依赖 dash.js 类型或事件。
- Feed 播放资源由三槽 pool 持有，Stage 只能挂载外部资源，不能重新 configure、load 或 destroy pool 句柄。

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

## 16. 推荐与自动审核工程约束

- 推荐 context 只使用 `domain/recommendation.RecommendationContext`；HTTP 必须严格绑定并拒绝
  超限/未知字段，客户端不提供身份、关系、曝光或任意 metadata。
- `recommendation_policy` 必须经 Domain 校验后才可启用。`EnsureInitialPolicies` 仅以
  `(scene, version)` conflict-do-nothing 插入 bootstrap 版本，API 和 Worker migration 均可安全调用，
  不得在启动时覆盖运营策略。
- 请求日志、反馈、画像事件和 outcome 是耐久事实；行为、反馈、关注和 action 的画像/归因投影使用
  带租约、指数退避的 Outbox，所有
  Worker 消费以稳定事件 ID 去重。日志载荷、候选数和保留清理必须有界。
- Redis snapshot 是优化而非真相：cursor 必须签名并绑定用户/scene/request，页组装必须再次
  校验可见性，Redis 失败必须走确定性 degraded cursor。
- 推荐 API-flow 测试覆盖 context、认证、Provider 降级、策略、snapshot、反馈和 outcome；
  定向并发基准命令为
  `cd apps/api && go test ./internal/application/recommendation -run '^$' -bench '^BenchmarkRecommendBoundedPool$' -benchtime=5s`。
- 自动审核以 `(video_id, review_version)` 唯一建案，视频 review version 必须为正数。机器结果
  仅通过 internal-token PUT 接口写入，使用严格有界 JSON；`(provider, result_id)` 身份重放
  必须比较规范化载荷哈希，同身份异载荷冲突。
- review signal、decision 和 policy provenance 是不可变事实。策略配置必须恢复为 Domain typed
  policy 后才能路由，优先级固定为 reject > human > approve；未知 label 保留证据但至少进入人审。
  自动通过/拒绝的 result、signal、decision、case 和 video 转换必须在同一 PostgreSQL 事务及
  行锁内提交，外部媒体提升或保护在提交后幂等执行。
- 初始 review policy 只按版本 conflict-do-nothing 插入，不覆盖运营启停；有媒体主体的 pending 视频
  丢失 intake 由有界 reconciliation 修复。Prometheus 标签不得包含 provider、model、policy、
  video、case 或 result identity。
- 生产审核通过 `application/review.ModerationProvider` 接入可替换 HTTP inference gateway，
  Domain/Application 不导入供应商 SDK。`review_moderation_job` 按
  `(case_id, review_version, provider_config_version)` 唯一，intake 同事务创建；数据库时间租约、
  `SKIP LOCKED`、稳定 request/result ID、重试、stale cancellation 和 reconciliation 是恢复边界。
- moderation input profile 只允许受保护视频的确定性 JPEG 帧和既有有界标题/简介。帧数最多 12、
  最长边 512、总计最多 8 MiB；manifest 保存 timestamp/hash/对象键，不保存签名 URL。样本进入
  私有 `moderation/` 前缀并由通用 cleanup task 在接受结果或 retention 到期后清理。
- machine result 必须保存 `production_provider/test_seed/recovery/legacy_unknown` source、
  generated time 和 `disabled/observe/approve_only/enforce` rollout mode。mode 只能收紧 policy；
  provider 或 extraction 失败耗尽后以 recovery provenance 和未知 `moderation_unavailable`
  label 转人工，不得伪造 safe/unsafe 判断或绕过 review gate。
- 人工队列固定使用 `priority DESC, created_at ASC, id ASC`，签名 cursor 必须绑定 priority
  过滤和完整排序元组。pending-human priority 必须由触发人审的 signal confidence 确定性映射到
  `1..100` 并与状态原子落库；队列查询必须直接按数据库时间纳入已过期租约，不能依赖固定上限
  的回收批次，并通过关联 video status 和 review version 排除终态或已被新版本替代的主体。
  claim 必须依次锁定 case 和 video；发现无效主体时原子写入 cancelled/superseded 终态及单条
  不可变历史后返回冲突，不能创建会被反复消费的租约。领取只返回一次 256-bit opaque token，
  数据库只存 SHA-256；claim、
  renew、release、decision 都校验 case version，decision 还校验 reviewer、数据库时间租约和
  video review version。相关行锁必须先于数据库时间采样；decision 必须先锁 case 和 video。
- 人工 reason code 是按 outcome 封闭注册表；note 规范化且有 Unicode 上限，特殊 other reason
  必须有 note。决定幂等键按 reviewer+case 隔离并绑定 outcome/reason/note/review/case version。
- 人工决定、case、video、内容统计、成功 audit、notification outbox 和幂等回执必须同事务；
  audit 失败整体回滚。决定幂等重放只有在当前 video review version 仍匹配案件时才允许重试
  媒体提升、保护和发布副作用。作者通知由 Review Worker 通过 message Application 窄接口耐久
  投递，外部投递失败不得回滚决定。人工 Prometheus 标签不得含 reviewer、case、video、token 或 reason。
- 内容运营查询和写入保留在 video domain/application/infrastructure/HTTP 四层，不创建通用后台数据仓储。
  查询 cursor 必须签名并绑定 lifecycle、author、video ID、keyword 和完整时间窗口；排序固定为
  `(created_at DESC, id DESC)`。下架/恢复使用注册 reason、bounded Unicode note 和正
  `expected_version`，事务内同时提交 video/version、内容统计、不可变处罚记录、成功 audit 和
  admin transition intent；audit 失败必须整体回滚。Worker 以有界批次、`SKIP LOCKED` 租约和
  指数退避重试缓存失效及按当前视频状态执行的媒体保护/发布，只有全部成功才标记 delivered，
  不得在请求路径吞掉副作用失败。
- Web 后台继续使用 Route union、`normalizeRoute`、`useNavigate` 和 SessionProvider，不引入路由库。
  Admin Shell 必须 route-level lazy load；权限集合只控制展示，API 403、租约过期和版本冲突必须
  作为独立可恢复状态，不能显示乐观成功。审核决定在同一 case 与规范化 payload 未变化且尚未
  成功时必须复用同一幂等键；队列收到 403 必须清除缓存行；视频查询默认结束时间必须包含当前分钟。
- 治理 control mutation 要求 `governance.execute`、非空 reason 和 expected revision；rollback
  选择较早且未过期 revision，但必须创建新的 immutable revision。控制面失败不得阻塞请求：
  last-known-good 在 max staleness 内继续使用，之后使用代码注册 failure default。
