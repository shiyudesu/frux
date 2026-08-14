# 账户模块设计

## 1. 模块职责

账户模块负责注册、短期 Access JWT、耐久 Refresh Session、登录刷新、登出、修改密码、当前用户资料、公开资料、个人资料更新、主页隐私设置和后台主体读取，为其他模块提供统一身份与资料展示能力。作品和获赞计数由视频模块维护，账户查询负责聚合读取；后台权限注册表仍属于账户领域，但具体审核、运营和治理数据由各自模块拥有。

## 2. 接口设计

| 方法 | 接口路径 | 作用 | 鉴权 | 幂等键 |
| --- | --- | --- | --- | --- |
| POST | `/api/users` | 用户注册 | 无 | 支持 |
| POST | `/api/sessions` | 密码登录、创建 Refresh Session 并获取短期 Access Token | IP 分层限流 | 无 |
| POST | `/api/sessions/current/refresh` | 旋转 HttpOnly Refresh Token 并获取新 Access Token | IP 分层限流 + 同源校验 | 无 |
| POST | `/api/admin/auth/login` | 后台专用密码登录并获取 `admin_access` Token | IP 分层限流 | 无 |
| DELETE | `/api/sessions/current` | 撤销当前 Refresh Session；无 Cookie 时仍幂等成功，成功响应不清 Cookie 以避免慢响应破坏新登录 | 同源校验 | 无 |
| GET | `/api/users/me` | 获取当前用户聚合资料 | 登录 | 无 |
| PATCH | `/api/users/me` | 原子更新头像、昵称、简介、性别和可选主页隐私 | 登录 | 无 |
| PUT | `/api/users/me/password` | 校验当前密码、替换密码并重建当前设备会话 | 登录 + 用户分层限流 | 无 |
| GET | `/api/users/me/profile-settings` | 获取当前用户主页隐私设置 | 登录 | 无 |
| PATCH | `/api/users/me/profile-settings` | 部分更新主页隐私设置 | 登录 | 无 |
| GET | `/api/users/{userId}` | 获取公开用户聚合资料 | 可匿名 | 无 |

`GET /api/users/me` 返回登录账号、角色、状态、性别、关系计数、公开/私密作品计数、获赞数和完整 `profile_settings`。`GET /api/users/{userId}` 返回公开展示字段、公开账号标识、性别、关系与内容统计，以及派生布尔值 `liked_videos_public`；不返回角色、账号状态、私密作品数或完整设置对象。

`PATCH /api/users/me` 继续兼容原有资料字段，并可附带嵌套的 `profile_settings`：

```json
{
  "nickname": "new name",
  "bio": "new bio",
  "gender": 2,
  "profile_settings": {
    "liked_visibility": "public",
    "favorite_visibility": "private"
  }
}
```

资料与设置在同一个数据库事务中提交，任一写入失败都不会留下部分更新；成功响应为完整当前用户资料。独立的 `GET/PATCH /api/users/me/profile-settings` 继续保留兼容。

隐私设置响应：

```json
{
  "liked_visibility": "private",
  "favorite_visibility": "private"
}
```

修改密码请求只提交当前密码和新密码，确认密码属于 Web 本地校验：

```json
{
  "current_password": "old password",
  "new_password": "new password"
}
```

成功响应与登录 Token 响应相同，同时通过 Cookie 轮换 Refresh Token 和 `/uploads` 资产 Token。

## 3. 数据表设计

### 3.1 `account`

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | BIGINT | PK | 用户 ID |
| `account` | VARCHAR(64) | UNIQUE, NOT NULL | 规范化登录账号和公开主页标识 |
| `password` | VARCHAR(255) | NOT NULL | bcrypt 密码哈希 |
| `nickname` | VARCHAR(128) | NOT NULL | 昵称 |
| `avatar_url` | VARCHAR(512) | NULLABLE | 头像 |
| `bio` | VARCHAR(255) | NULLABLE | 简介 |
| `gender` | SMALLINT | NOT NULL, DEFAULT 0 | 0 未设置 / 1 男 / 2 女 / 3 其他 |
| `status` | SMALLINT | NOT NULL, DEFAULT 1 | 1 正常 / 2 冻结 / 3 注销 |
| `role` | VARCHAR(32) | NOT NULL | `user` / `reviewer` / `operator` / `admin` |
| `auth_version` | BIGINT | NOT NULL, DEFAULT 1 | 密码变化时递增的认证版本 |
| `created_at` | TIMESTAMPTZ | NOT NULL | 创建时间 |
| `updated_at` | TIMESTAMPTZ | NOT NULL | 更新时间 |

索引 `uk_account_account(account)` 保证规范化账号唯一。

### 3.2 `account_profile_setting`

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `user_id` | BIGINT | PK | 用户 ID |
| `liked_visibility` | VARCHAR(16) | NOT NULL, DEFAULT `private` | 喜欢列表可见性 |
| `favorite_visibility` | VARCHAR(16) | NOT NULL, DEFAULT `private` | 收藏列表可见性设置 |
| `created_at` | TIMESTAMPTZ | NOT NULL | 创建时间 |
| `updated_at` | TIMESTAMPTZ | NOT NULL | 更新时间 |

迁移会为缺失设置的账号补齐隐私优先的默认行。

### 3.3 `account_refresh_session`

| 字段 | 说明 |
| --- | --- |
| `id` / `family_id` | 随机会话标识和重放撤销族 |
| `user_id` / `auth_version` | 账号及创建时认证版本 |
| `secret_hash` | 当前 Refresh Secret 的 SHA-256；不保存原文 |
| `previous_secret_hash` / `previous_secret_valid_to` | 多标签页并发旋转的短期竞争证据 |
| `expires_at` / `last_used_at` | 固定 30 天绝对到期和最近轮换时间 |
| `revoked_at` / `revocation_reason` | 登出、改密、重放或到期撤销 |
| `replaced_by_session_id` | 改密后当前设备的新会话 |

Worker 以有界批次清理到期行和保留期外的已撤销行。

## 4. 业务规则

| 规则 | 说明 |
| --- | --- |
| 账号规范化 | 注册、恢复和登录查询前统一 trim 并转为小写 |
| 账号唯一 | 大小写变体共享同一规范化 `account` |
| 密码规则 | 新注册和新密码至少 8 个 Unicode 字符，UTF-8 不超过 bcrypt 的 72 字节边界；旧短密码仍可登录并迁移 |
| 密码只保存哈希 | `account.password` 仅保存 bcrypt；Refresh Session 仅保存 SHA-256 Secret Hash |
| 登录只允许正常账号 | 冻结和注销用户不能登录 |
| JWT 严格身份 | Token 必须带 `kid/iss/aud/sub/token_type/jti/iat/nbf/exp/auth_version`；消费端另带 Refresh Session ID，不携带授权角色 |
| Token purpose 与密钥隔离 | 用户端和后台使用不同 HS256 key ring；用户端只接受 `access` + `frux-consumer`，后台只接受 `admin_access` + `frux-admin` |
| Access Token 有界 | 用户 Access 默认 5 分钟且上限 15 分钟；登出或改密后旧 Access 最多存活到该到期时间，不能再次刷新 |
| Admin Token 有界 | `jwt.admin_access_ttl` 默认 30 分钟，配置范围 5 分钟到 8 小时；JWT 必须包含 `exp` |
| 密钥轮换 | 新 Token 使用 active `kid`；旧 key 只在有界重叠期验证。旧共享密钥兼容必须配置明确截止时间 |
| 后台角色封闭映射 | `reviewer`、`operator` 和兼容 `admin` 映射到代码注册的固定权限；普通用户、未知角色和未知权限默认拒绝 |
| 账号变更立即生效 | 后台每次读取当前状态、角色和 `auth_version`；停用、降权或改密后旧 Admin Token 下一次请求即失效 |
| 登录失败不可枚举账号 | 未注册账号和密码错误使用相同响应和 dummy bcrypt 主要计算路径；Web 统一展示“账号或密码错误，请重新输入” |
| 后台登录失败不可枚举 | 未知账号、错误密码、停用账号和无后台权限账号统一返回 `401 ADMIN_AUTH_INVALID_CREDENTIALS`；未知账号路径执行固定 dummy bcrypt 校验，避免明显时序差异 |
| 当前用户资料走鉴权 | `/api/users/me` 只返回当前登录用户 |
| Refresh 旋转 | 每次成功刷新替换 Secret；短竞争窗口内旧 Secret 返回 superseded 冲突。已知有效 Session ID 上任何非当前且不满足竞争窗口的 Secret 均 fail-closed 撤销整个 family，因此不保存无界历史 Hash |
| 改密原子性 | 旧密码哈希 CAS、密码更新、`auth_version + 1`、全部旧 Refresh Session 撤销和当前设备替换会话同事务提交 |
| 登出不依赖 Access Token | `/api/sessions/current` 通过 Refresh Cookie 撤销当前会话；缺少、过期或重复登出均返回 204 |
| Web Access 只存内存 | `localStorage` 不保存 Bearer Token；刷新页面通过 HttpOnly Refresh Cookie 恢复，缓存用户资料不是认证事实 |
| 离线退出立即生效 | Web 先清空内存态和 `/uploads` 活跃标记并广播到其他标签页，再尽力撤销服务端 Refresh Session |
| Cookie 响应顺序安全 | 登录、刷新和改密轮换 Refresh/资产 Cookie；普通鉴权和登出成功响应不清 Cookie，避免旧慢响应破坏新登录；当前刷新确认旧 Cookie 无效时才清理 |
| 公开资料裁剪 | 公开接口返回作为主页标识的 `account`，但不返回密码、角色、账号状态、私密作品数或完整隐私设置 |
| 跨模块公开身份 | Feed 作者、评论作者、直接回复目标和公开主页均以同一 `account.id/account/nickname/avatar_url` 为事实源；评论模块不维护另一套账号 |
| 无头像占位一致 | Web 对视频作者、评论作者、回复目标和公开资料缓存使用同一个用户头像 fallback，不因作者/评论者角色显示成不同身份 |
| 资料字段校验 | 昵称、头像、简介和性别由 Domain 层校验 |
| 资料与隐私原子保存 | Web 编辑器通过一次 `PATCH /api/users/me` 在同一事务中保存资料和喜欢可见性；持久化只更新请求实际提供的列，并发部分更新不会覆盖无关资料或设置 |
| 性别枚举 | 只接受 `0`、`1`、`2`、`3` |
| 隐私设置部分更新 | 至少提交一个字段，只接受 `private` 或 `public` |
| 隐私默认值 | 喜欢和收藏设置均默认为 `private` |
| 喜欢公开能力 | 只有 `liked_visibility=public` 会派生公开主页的 `liked_videos_public=true` |
| 收藏始终本人可见 | 没有公开收藏接口或公开主页收藏 Tab；`favorite_visibility` 仅为兼容字段，Web 编辑器固定显示“仅自己可见”并写回 `private` |
| 内容统计来源 | `user_content_stat` 提供公开/私密作品和获赞计数；所有值对外按非负数返回 |

## 5. 测试建议

| 场景 | 期望 |
| --- | --- |
| 注册新用户 | 返回用户资料，密码哈希写入数据库 |
| 密码过短或超过 72 字节 | 返回 `ACCOUNT_PASSWORD_INVALID`，不产生 bcrypt 内部错误 |
| 大小写变体注册和登录 | 解析到同一规范账号 |
| 未注册账号或密码错误 | 两种情况均返回 `401` 和 `AUTH_INVALID_CREDENTIALS`，且 Web 显示相同中文提示 |
| 重复注册 | 返回 `409` 和 `ACCOUNT_ALREADY_EXISTS`，Web 提示直接登录或更换账号 |
| 未登录访问当前用户 | 返回 401 |
| 更新个人资料 | 返回更新后的昵称、头像、简介和性别 |
| 原子保存失败 | 资料或设置任一写入失败时两者都保持原值 |
| 并发部分保存 | 分别更新昵称、简介或不同隐私字段时，所有已提交字段保留且互不覆盖 |
| 新用户读取隐私设置 | 喜欢和收藏均为 `private` |
| 更新喜欢可见性 | 当前用户设置更新，公开资料的 `liked_videos_public` 同步变化 |
| 公开资料裁剪 | 不出现角色、状态、私密作品数和完整设置 |
| 刷新旋转 | 新 Secret 可用，旧 Secret 在竞争窗口返回冲突，窗口外重放撤销 family |
| 改密成功 | 旧密码不能登录、当前设备获得新会话、其他 Refresh Session 被撤销 |
| 并发改密 | 使用同一旧哈希的请求最多一个成功，另一个返回 credential conflict |
| 后台改密失效 | 旧 Admin Token 在下一次权限读取时返回 401 |

## 6. 前端接入点

| 页面 | 接入能力 |
| --- | --- |
| 登录/注册页 | 注册、登录、8 字符/72 字节密码提示、错误提示 |
| 后台登录页 | `/admin/login` 只提供管理员账号和密码登录，不提供注册；Admin Session 独立保存在当前标签页 `sessionStorage` |
| 顶部用户区 | 展示当前用户资料和耐久登出 |
| 个人主页 | 展示资料与内容，并通过独立“账号安全”弹窗修改密码，不把凭证混入资料编辑请求 |
| 作者主页 | 展示公开用户资料，并根据 `liked_videos_public` 决定是否显示喜欢 Tab |

## 7. 错误码

| HTTP 状态 | code | 场景 |
| --- | --- | --- |
| 400 | `INVALID_REQUEST` | JSON 请求格式无效 |
| 400 | `ACCOUNT_VALIDATION_FAILED` | 账号、昵称、资料或隐私设置校验失败 |
| 400 | `ACCOUNT_PASSWORD_INVALID` | 新注册或新密码不满足统一规则 |
| 400 | `ACCOUNT_CURRENT_PASSWORD_INCORRECT` | 修改密码时当前密码错误 |
| 400 | `ACCOUNT_PASSWORD_UNCHANGED` | 新密码与当前密码相同 |
| 401 | `AUTH_INVALID_CREDENTIALS` | 登录账号不存在或密码错误，两种情况不可区分 |
| 401 | `AUTH_INVALID_ACCESS_TOKEN` | 当前登录态缺失、无效或已过期 |
| 401 | `AUTH_REFRESH_INVALID` | Refresh Session 缺失、过期、撤销或账号不可用 |
| 401 | `AUTH_REFRESH_REPLAYED` | 检测到旋转后 Secret 重放并撤销 family |
| 401 | `ADMIN_AUTH_INVALID_CREDENTIALS` | 后台账号、密码、状态或角色不满足登录条件，具体原因不可区分 |
| 401 | `ADMIN_AUTH_INVALID_ACCESS_TOKEN` | 后台 Token 缺失、过期、purpose 或 audience 不匹配 |
| 403 | `ADMIN_PERMISSION_DENIED` | 当前账号停用、角色未知或缺少后台接口声明的权限 |
| 404 | `ACCOUNT_NOT_FOUND` | 公开或当前账号不存在 |
| 409 | `ACCOUNT_ALREADY_EXISTS` | 规范化账号已注册 |
| 409 | `ACCOUNT_CREDENTIAL_CHANGED` | 并发改密时旧哈希或认证版本已变化 |
| 409 | `AUTH_REFRESH_SUPERSEDED` | 多标签页并发刷新中该请求使用了刚被替换的 Secret |
| 503 | `AUTHENTICATION_UNAVAILABLE` | Session 持久化、签发或安全决策暂时不可用 |
| 503 | `ADMIN_AUTHORIZATION_UNAVAILABLE` | 当前后台主体读取暂时不可用 |
| 503 | `ADMIN_AUTHENTICATION_UNAVAILABLE` | 后台凭证校验或 Token 签发暂时不可用 |
| 500 | `INTERNAL_ERROR` | 账号仓储、设置读取或 Token 签发等内部失败 |

响应同时保留原有 `error` 字段；Web 只根据 `code` 和安全 fallback 生成用户文案，不直接展示兼容文本。

## 8. 发布与回滚

应用内部游标和短期媒体 URL 使用独立 `security.hmac_secret`，不得复用任何 JWT key。先发布支持新
key ring、Refresh Session 和旧 Token 有界验证的后端，再发布内存会话 Web。迁移配置同时记录
`legacy_issued_until` 和覆盖旧最大 TTL/clock leeway 的
`legacy_accept_until`；截止时间可自然过期且不会阻止后续重启。确认窗口结束后移除 legacy 配置，
此后无 `kid` 或旧 audience Token 不再接受。截止前回滚可恢复旧 Web 和兼容后端；截止后回滚必须同时恢复旧密钥兼容配置。
`auth_version` 和 `account_refresh_session` 都是可保留的加法 schema，不需要在回滚时删除。
