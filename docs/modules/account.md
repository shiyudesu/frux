# 账户模块设计

## 1. 模块职责

账户模块负责注册、登录、登出、当前用户资料、公开资料、个人资料更新和主页隐私设置，为其他模块提供统一身份与资料展示能力。作品、获赞和合集计数由视频模块维护，账户查询负责聚合读取。

## 2. 接口设计

| 方法 | 接口路径 | 作用 | 鉴权 | 幂等键 |
| --- | --- | --- | --- | --- |
| POST | `/api/users` | 用户注册 | 无 | 支持 |
| POST | `/api/sessions` | 密码登录并获取 Token | 无 | 无 |
| DELETE | `/api/sessions/current` | 无状态登出确认；Web 已同步清除本地会话和活跃标记，响应不修改 Cookie | 无 | 无 |
| GET | `/api/users/me` | 获取当前用户聚合资料 | 登录 | 无 |
| PATCH | `/api/users/me` | 原子更新头像、昵称、简介、性别和可选主页隐私 | 登录 | 无 |
| GET | `/api/users/me/profile-settings` | 获取当前用户主页隐私设置 | 登录 | 无 |
| PATCH | `/api/users/me/profile-settings` | 部分更新主页隐私设置 | 登录 | 无 |
| GET | `/api/users/{userId}` | 获取公开用户聚合资料 | 可匿名 | 无 |

`GET /api/users/me` 返回登录账号、角色、状态、性别、关系计数、公开/私密作品计数、获赞数、合集数和完整 `profile_settings`。`GET /api/users/{userId}` 返回公开展示字段、公开账号标识、性别、关系与内容统计，以及派生布尔值 `liked_videos_public`；不返回角色、账号状态、私密作品数或完整设置对象。

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
| `role` | VARCHAR(32) | NOT NULL | `user` / `admin` |
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

## 4. 业务规则

| 规则 | 说明 |
| --- | --- |
| 账号规范化 | 注册、恢复和登录查询前统一 trim 并转为小写 |
| 账号唯一 | 大小写变体共享同一规范化 `account` |
| 密码只保存哈希 | 接口和数据库都不保存明文密码 |
| 登录只允许正常账号 | 冻结和注销用户不能登录 |
| 登录失败不可枚举账号 | 未注册账号和密码错误均返回 `401`、`AUTH_INVALID_CREDENTIALS` 与相同兼容文本；Web 统一展示“账号或密码错误，请重新输入” |
| 当前用户资料走鉴权 | `/api/users/me` 只返回当前登录用户 |
| 登出不依赖有效 Token | `/api/sessions/current` 即使缺少或携带过期 access token 也返回 204，且不发送可能影响更新登录的 `Set-Cookie` |
| 离线退出立即生效 | Web `clearAuth` 先删除本地登录态和 SameSite=Strict 的 `/uploads` 活跃标记，再尽力调用登出接口；Cookie 资产身份必须同时携带有效 HttpOnly Token 与活跃标记 |
| Cookie 响应顺序安全 | 资产 Token 只在登录成功时写入；普通鉴权响应、`/uploads` 响应和登出响应都不刷新或清除它，旧慢登出响应不能破坏更新登录 |
| 公开资料裁剪 | 公开接口返回作为主页标识的 `account`，但不返回密码、角色、账号状态、私密作品数或完整隐私设置 |
| 资料字段校验 | 昵称、头像、简介和性别由 Domain 层校验 |
| 资料与隐私原子保存 | Web 编辑器通过一次 `PATCH /api/users/me` 在同一事务中保存资料和喜欢可见性；持久化只更新请求实际提供的列，并发部分更新不会覆盖无关资料或设置 |
| 性别枚举 | 只接受 `0`、`1`、`2`、`3` |
| 隐私设置部分更新 | 至少提交一个字段，只接受 `private` 或 `public` |
| 隐私默认值 | 喜欢和收藏设置均默认为 `private` |
| 喜欢公开能力 | 只有 `liked_visibility=public` 会派生公开主页的 `liked_videos_public=true` |
| 收藏始终本人可见 | 没有公开收藏接口或公开主页收藏 Tab；`favorite_visibility` 仅为兼容字段，Web 编辑器固定显示“仅自己可见”并写回 `private` |
| 内容统计来源 | `user_content_stat` 提供公开/私密作品、获赞和合集计数；所有值对外按非负数返回 |

## 5. 测试建议

| 场景 | 期望 |
| --- | --- |
| 注册新用户 | 返回用户资料，密码哈希写入数据库 |
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

## 6. 前端接入点

| 页面 | 接入能力 |
| --- | --- |
| 登录/注册页 | 注册、登录、错误提示 |
| 顶部用户区 | 展示当前用户资料和登出 |
| 个人主页 | 展示资料头图、关系/作品/获赞统计；一次保存头像、昵称、简介、性别和喜欢列表隐私；收藏明确仅自己可见 |
| 作者主页 | 展示公开用户资料，并根据 `liked_videos_public` 决定是否显示喜欢 Tab |

## 7. 错误码

| HTTP 状态 | code | 场景 |
| --- | --- | --- |
| 400 | `INVALID_REQUEST` | JSON 请求格式无效 |
| 400 | `ACCOUNT_VALIDATION_FAILED` | 账号、密码、昵称、资料或隐私设置校验失败 |
| 401 | `AUTH_INVALID_CREDENTIALS` | 登录账号不存在或密码错误，两种情况不可区分 |
| 401 | `AUTH_INVALID_ACCESS_TOKEN` | 当前登录态缺失、无效或已过期 |
| 404 | `ACCOUNT_NOT_FOUND` | 公开或当前账号不存在 |
| 409 | `ACCOUNT_ALREADY_EXISTS` | 规范化账号已注册 |
| 500 | `INTERNAL_ERROR` | 账号仓储、设置读取或 Token 签发等内部失败 |

响应同时保留原有 `error` 字段；Web 只根据 `code` 和安全 fallback 生成用户文案，不直接展示兼容文本。
