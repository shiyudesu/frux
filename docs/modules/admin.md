# 后台运营模块设计

## 1. 模块职责

后台运营是内部控制面的 HTTP 入口，不拥有审核、视频、审计或运行时治理的领域数据。当前已实现共享权限边界；后续后台接口继续调用各领域 Application Service，并在路由上显式声明所需权限。

## 2. 接口设计

| 状态 | 方法 | 接口路径 | 作用 | 所需权限 |
| --- | --- | --- | --- | --- |
| 已实现 | POST | `/api/admin/auth/login` | 校验现有账号的后台资格并签发独立 Admin Token | IP 分层限流 |
| 已实现 | GET | `/api/admin/me` | 返回当前持久化后台角色和权限集合 | `review.read` |
| 已实现 | GET | `/api/admin/audit-events` | 查询不可变后台操作事实 | `audit.read` |
| 已实现 | GET | `/api/admin/videos` | 按生命周期、作者、ID、关键词和有界创建时间查询视频 | `content.enforce` |
| 已实现 | POST | `/api/admin/videos/{videoId}/enforcement` | 按预期版本、注册原因和备注下架已发布视频 | `content.enforce` |
| 已实现 | POST | `/api/admin/videos/{videoId}/restoration` | 按预期版本恢复已批准的下架视频 | `content.enforce` |
| 已实现 | GET | `/api/admin/review/cases` | 查询待处理、本人进行中和最近完成的审核任务 | `review.read` |
| 已实现 | GET | `/api/admin/review/cases/{caseId}` | 查询案件证据和历史 | `review.read` |
| 已实现 | GET | `/api/admin/review/cases/{caseId}/preview-access` | 获取审核专用短期保护预览 | `review.read` |
| 已实现 | POST/DELETE | `/api/admin/review/cases/{caseId}/claim`、`.../lease/*` | 开始、恢复、延长和放回审核任务 | `review.decide` |
| 已实现 | POST | `/api/admin/review/cases/{caseId}/decision` | 幂等批准或拒绝 | `review.decide` |
| 已实现 | GET | `/api/admin/kafka-dead-letters` | Kafka DLQ retained offset/age 摘要 | `governance.execute` |
| 已实现 | GET | `/api/admin/kafka-dead-letters/{topic}/records` | 按 Partition/Offset 脱敏检查 | `governance.execute` |
| 已实现 | POST | `/api/admin/kafka-dead-letters/{topic}/records/{partition}/{offset}/replay` | 幂等、非破坏的单 Record 审计重放 | `governance.execute` |
| 规划中 | PATCH | `/api/admin/configs/{configKey}` | 发布配置修订 | `config.publish` |

## 3. 角色和权限

权限注册表是代码内封闭集合，第一阶段不增加角色管理表或通用策略语言。

| 角色 | 初始权限 |
| --- | --- |
| `user` | 无 |
| `reviewer` | `review.read`、`review.decide` |
| `operator` | `review.read`、`content.enforce`、`config.publish`、`governance.execute`、`audit.read` |
| `admin` | 全部已注册权限，作为兼容 bootstrap 角色 |

已注册权限为 `review.read`、`review.decide`、`content.enforce`、`config.publish`、`governance.execute` 和 `audit.read`。未知角色、未知权限和未配置映射均不授予权限。

## 4. 授权链路

```text
Admin Access JWT
token_type=admin_access
aud=frux-admin
   ↓ 只验证 purpose、audience、有效期并取得 user_id
读取 account.status + account.role
   ↓
封闭角色权限映射
   ↓
路由声明的单项权限检查
   ↓
Resolved Admin Principal → Handler
```

- `/api/admin/auth/login` 是唯一不要求 Admin Token 的后台接口，不提供注册；未知账号、错误密码、停用和无权限角色返回相同 401。
- 其余 `/api/admin` 路由先要求 `admin_access` + `frux-admin`，再执行参数化权限中间件；普通用户 `access` Token 稳定返回 `401 ADMIN_AUTH_INVALID_ACCESS_TOKEN`。
- 后台权限始终读取当前账号；JWT 中的旧角色 claim 不保留权限，也不阻止数据库中的合法升权生效。
- 停用账号、普通用户、缺失账号和未知角色统一返回 `403 ADMIN_PERMISSION_DENIED`，不暴露满足条件所需的更高角色。
- 当前账号读取失败返回 `503 ADMIN_AUTHORIZATION_UNAVAILABLE`；缺少、过期或 purpose/audience 错误的后台 Token 返回 `401 ADMIN_AUTH_INVALID_ACCESS_TOKEN`。
- 中间件把已解析主体写入请求上下文，Handler 使用共享 helper 做归因，不重复比较角色字符串。
- 配置了审计元数据的后台路由会最佳努力记录拒绝尝试；审计写入失败不能把原始 403 伪装为成功或其他错误。

## 5. 数据所有权

本权限基础不新增数据表，只读取 `account.id/status/role`。后续能力保持领域所有权：

- 审核案件和决定由审核模块拥有。
- 视频处罚和恢复由视频模块拥有。
- 不可变操作事实由 [admin-audit.md](admin-audit.md) 描述的审计模块拥有，并与生产变更同事务提交。
- 运行时配置由治理模块使用版本化 Revision 管理。

## 6. 测试要求

| 场景 | 期望 |
| --- | --- |
| Reviewer 读取后台主体 | 返回其精确权限，不获得运营和治理权限 |
| 普通用户或未知角色访问 | 返回 403，Handler 不执行 |
| Admin Token 对应账号已降权 | 立即返回 403，不等待 Token 过期 |
| Admin Token 对应账号已停用 | 立即返回 403 |
| 兼容 `admin` 账号访问 | 获得全部初始注册权限 |
| 缺少或无效 Token | 保持既有 401 响应 |
| 普通用户 Token 调用后台 | 返回 `401 ADMIN_AUTH_INVALID_ACCESS_TOKEN`，Handler 和权限读取不执行 |
| 后台 Token 调用用户 API | 返回 `401 AUTH_INVALID_ACCESS_TOKEN` |
| 两名 Reviewer 并发领取 | 只有一人获得 opaque lease token，另一人收到稳定 409 |
| 当前 Reviewer 刷新详情 | resume 轮换 token、旧 token 失效，任务继续出现在“我正在审核” |
| 非持有人或过期租约决定 | 返回稳定冲突，不写案件、视频、审计或通知 |
| Reviewer 请求待审视频预览 | 返回最长 5 分钟保护 URL，公共媒体资格保持不变 |

## 7. 前端接入点

Web 使用现有 History API typed router 懒加载 `/admin/login`、`/admin/reviews`、
`/admin/reviews/{reviewId}` 和 `/admin/videos`。`AdminSessionProvider` 使用版本化
`sessionStorage` 键保存 Admin Token/主体，和用户端 localStorage 会话完全独立；任一后台 API 的
匹配 Token 401 只清理 Admin Session，并返回后台登录页。审核任务页提供“待我处理 / 我正在审核 /
最近完成”，详情页使用短期保护预览、自动延长占用、刷新恢复和主动放回，不向审核员暴露 lease token。
Admin Shell 通过 `/api/admin/me` 获取服务端确认的封闭权限集合，只展示获准目的地；直接 URL 仍请求
拥有领域的后台 API，并稳定呈现登录、权限验证、403 和服务不可用状态。
