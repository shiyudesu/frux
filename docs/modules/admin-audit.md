# 后台操作审计模块设计

## 1. 模块职责

后台操作审计模块保存不可变、隐私有界的特权操作事实。它只记录谁在何时、以何种权限、对什么目标执行了什么操作以及结果，不拥有审核案件、视频状态、治理配置或消息恢复状态。

成功的持久化特权变更必须与成功审计事实使用同一个 PostgreSQL 事务；授权拒绝等没有业务提交的尝试可最佳努力记录，记录失败只写低基数指标和安全日志，不覆盖原始错误。

## 2. 接口设计

| 方法 | 接口路径 | 作用 | 鉴权 |
| --- | --- | --- | --- |
| GET | `/api/admin/audit-events` | 按时间范围和稳定游标查询审计事实 | `audit.read` |

查询参数：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `from` | 是 | RFC3339 起始时间，包含边界 |
| `to` | 是 | RFC3339 结束时间，包含边界 |
| `actor_id` | 否 | 正整数操作者 ID |
| `action` | 否 | 已注册审计 action |
| `target_type` | 否 | 已注册目标类型 |
| `outcome` | 否 | `success` 或 `denied` |
| `cursor` | 否 | 与全部过滤条件绑定的编码游标 |
| `limit` | 否 | 默认 20，最大 100 |

时间范围最多 31 天。结果按 `created_at DESC, id DESC` 返回 `items`、`next_cursor` 和 `has_more`。

## 3. 数据表设计

### 3.1 `admin_audit_event`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | BIGINT | 自增审计事件 ID |
| `actor_id` | BIGINT | 操作者账号 ID |
| `permission` | VARCHAR(64) | 路由或操作使用的后台权限 |
| `action` | VARCHAR(64) | 封闭 action 标识 |
| `target_type` | VARCHAR(64) | 封闭目标类型 |
| `target_id` | VARCHAR(128) | 目标稳定标识 |
| `outcome` | VARCHAR(16) | `success` / `denied` |
| `request_id` | VARCHAR(128) | 服务端生成的请求关联 ID |
| `idempotency_key_hash` | VARCHAR(71) | 可选幂等键 SHA-256 摘要，不保存原值 |
| `detail_json` | JSONB | action 白名单内的有界详情 |
| `created_at` | TIMESTAMPTZ | 事实创建时间 |

索引覆盖全局时间、操作者、action、目标和 outcome 的时间倒序查询。Repository 只暴露追加和查询，不提供更新或删除方法。

## 4. 业务规则

| 规则 | 说明 |
| --- | --- |
| 成功事实同事务 | 受保护领域变更和审计插入任一失败时整体回滚 |
| 不可变 | 已提交事实没有更新、删除或覆盖 API |
| 详情白名单 | 每种 action 只接受预定义 key；理由、状态、决定、路由和版本同时使用封闭枚举或数字校验 |
| 隐私有界 | 不保存 Token、密码、任意 Header、原始媒体或完整请求/响应体 |
| 请求关联可信 | 审计 request ID 由服务端生成；幂等键只保存 SHA-256 摘要，不持久化调用方可控 Header 或原始重试凭据 |
| 拒绝记录最佳努力 | 使用进程总窗口限额、每操作者窗口限额、全局并发槽和独立短超时异步写入；失败产生失败指标和安全日志，饱和只计 dropped 指标，原始 403 不等待审计存储 |
| 稳定游标 | 游标绑定 actor/action/target/outcome/from/to，改变过滤条件后游标失效 |
| 组合校验 | 每个 action/outcome 同时绑定 permission、target type、HTTP method、route、reason、状态转换和必需 detail，不能拼接出语义矛盾的不可变事实 |

当前注册 action 包括审计查询、审核决定、内容处罚与恢复、配置发布、治理执行和死信重放。后续领域 change 使用 Application builder 创建事实，并由其 Infrastructure Repository 在已有 GORM 事务内调用共享追加 helper。

`governance.execute` 成功事实支持 update 和 rollback：分别绑定
`PATCH /api/admin/governance/controls/:key` 与
`POST /api/admin/governance/controls/:key/rollback`，保存 operation、previous/new revision
和封闭 `governance_changed` reason code；不保存运营输入的自由文本 reason。控制 revision、
active pointer 与成功审计同事务提交。

`review.decide` 成功事实绑定 `review.decide` 权限、review case、POST decision 路由、正
`review_version`、approved/rejected 结果和人工审核注册 reason code。审计保存幂等键摘要，
不保存租约 token 或 note；审计插入失败会回滚人工决定、案件、视频和通知 Outbox。

## 5. 错误码

| HTTP 状态 | code | 场景 |
| --- | --- | --- |
| 400 | `ADMIN_AUDIT_QUERY_INVALID` | 时间范围、过滤条件或 limit 无效 |
| 400 | `ADMIN_AUDIT_CURSOR_INVALID` | 游标格式无效或与过滤条件不匹配 |
| 403 | `ADMIN_PERMISSION_DENIED` | 当前主体缺少 `audit.read` |
| 503 | `ADMIN_AUDIT_UNAVAILABLE` | 审计查询暂时不可用 |

## 6. 测试要求

- Domain 覆盖枚举、字段长度、detail 白名单、敏感键和大小限制。
- PostgreSQL 覆盖追加、过滤、同时间 ID 排序、跨页稳定性和索引迁移。
- 事务测试必须证明审计插入失败会回滚代表性受保护变更。
- API 流程覆盖分页、过滤、游标绑定、无权限、非法范围和响应详情裁剪。

## 7. 前端接入点

后续操作日志页只消费查询 DTO，不展示服务端诊断错误；筛选条件和游标由页面状态保存，不允许客户端提交任意 SQL、详情 key 或审计修改请求。
