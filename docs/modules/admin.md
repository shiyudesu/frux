# 后台运营模块设计

## 1. 模块职责

后台运营是内部控制面的 HTTP 入口，不拥有审核、视频、审计或运行时治理的领域数据。当前已实现共享权限边界；后续后台接口继续调用各领域 Application Service，并在路由上显式声明所需权限。

## 2. 接口设计

| 状态 | 方法 | 接口路径 | 作用 | 所需权限 |
| --- | --- | --- | --- | --- |
| 已实现 | GET | `/api/admin/me` | 返回当前持久化后台角色和权限集合 | `review.read` |
| 已实现 | GET | `/api/admin/audit-events` | 查询不可变后台操作事实 | `audit.read` |
| 规划中 | GET | `/api/admin/videos` | 按条件查询和运营视频 | `content.enforce` |
| 已实现 | GET | `/api/admin/review/cases` | 查询稳定人工审核队列 | `review.read` |
| 已实现 | GET | `/api/admin/review/cases/{caseId}` | 查询案件证据和历史 | `review.read` |
| 已实现 | POST/DELETE | `/api/admin/review/cases/{caseId}/claim`、`.../lease/*` | 领取、续租和释放 | `review.decide` |
| 已实现 | POST | `/api/admin/review/cases/{caseId}/decision` | 幂等批准或拒绝 | `review.decide` |
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
Access JWT
   ↓ 只验证身份并取得 user_id
读取 account.status + account.role
   ↓
封闭角色权限映射
   ↓
路由声明的单项权限检查
   ↓
Resolved Admin Principal → Handler
```

- `/api/admin` 路由先执行强制 JWT 鉴权，再执行参数化权限中间件。
- 后台权限始终读取当前账号；JWT 中的旧角色 claim 不保留权限，也不阻止数据库中的合法升权生效。
- 停用账号、普通用户、缺失账号和未知角色统一返回 `403 ADMIN_PERMISSION_DENIED`，不暴露满足条件所需的更高角色。
- 当前账号读取失败返回 `503 ADMIN_AUTHORIZATION_UNAVAILABLE`；缺少或无效 access token 继续返回既有 `401 AUTH_INVALID_ACCESS_TOKEN`。
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
| 两名 Reviewer 并发领取 | 只有一人获得 opaque lease token，另一人收到稳定 409 |
| 非持有人或过期租约决定 | 返回稳定冲突，不写案件、视频、审计或通知 |

## 7. 前端接入点

后续 Admin Shell 可使用 `/api/admin/me` 获取服务端确认的权限集合控制导航展示，但展示控制不能代替每个后台接口的服务端权限检查。
