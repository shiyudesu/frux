# 全局搜索模块设计

## 1. 模块职责

`search` 模块聚合视频和账户的公开发现能力。它只搜索已发布、公开且媒体就绪的视频，以及状态正常的用户；不搜索评论、消息、私密内容或推荐内部数据。

Application 层依赖 `VideoSearchIndex` 和 `UserSearchIndex` 窄接口，PostgreSQL 查询由基础设施实现。该边界允许后续替换为 trigram、全文或外部搜索索引，而不改变 HTTP 和 Web 合约。

## 2. 接口设计

| 方法 | 接口路径 | 作用 | 鉴权 |
| --- | --- | --- | --- |
| GET | `/api/search/videos` | 搜索公开视频 | 可匿名 |
| GET | `/api/search/users` | 搜索正常用户 | 可匿名 |

共同 query：

- `q`：trim 后 1–64 个 Unicode code point。
- `cursor`：可选、不透明、带版本且绑定规范化查询和结果类型。
- `limit`：默认 20，范围 1–50。

响应均为：

```json
{
  "items": [],
  "next_cursor": "",
  "has_more": false
}
```

视频项复用公开 `Video` 字段；用户项只返回 `id`、`account`、`nickname`、`avatar_url` 和 `bio`。

## 3. 数据与排序

首版不新增数据表或外部依赖，直接使用 PostgreSQL 参数化查询。

视频相关性顺序：

1. 标题忽略大小写完全匹配。
2. 标题前缀匹配。
3. 标题包含匹配。
4. 简介包含匹配。
5. 同相关性按 `published_at DESC, id DESC`。

用户相关性顺序：

1. 账号完全匹配。
2. 账号前缀匹配。
3. 昵称前缀匹配。
4. 账号包含匹配。
5. 昵称包含匹配。
6. 同相关性按 `updated_at DESC, id DESC`。

## 4. 业务规则

| 规则 | 说明 |
| --- | --- |
| 视频可见性 | 仅返回 `published + public + media-ready` 视频 |
| 用户可见性 | 仅返回状态正常账号，不暴露角色、状态或私密设置 |
| 字面匹配 | `\`、`%`、`_` 在 `ILIKE` 中转义为普通字符 |
| 稳定游标 | 游标编码版本、结果类型、规范化 query、相关性和排序元组 |
| 游标隔离 | 跨 query 或跨视频/用户类型复用返回 400 |
| 匿名读取 | 搜索不要求登录，身份不会扩大可见范围 |
| 有界请求 | query、limit 和响应页均有明确上限 |

## 5. 测试建议

| 场景 | 期望 |
| --- | --- |
| 精确/前缀/包含匹配 | 按相关性和稳定元组排序 |
| 通配符字符 | 按字面值匹配，不扩大结果 |
| 私密/下架/处理中视频 | 不返回 |
| 冻结或注销用户 | 不返回 |
| 相同游标换 query/type | 返回非法游标 |
| 查询切换时旧响应返回 | Web 忽略旧响应 |

## 6. 前端接入点

- 顶部搜索框是受控 `role=search` 表单，Enter 和“搜索”按钮进入 typed `/search`。
- URL 的 `q` 与 `tab=videos|users` 是页面状态真相，浏览器前进/后退会同步输入框。
- 页面明确说明视频匹配标题/简介、用户匹配账号/昵称，避免把视频和用户 Tab 的范围混淆。
- 视频和用户 Tab 保留独立 items、cursor、has-more、loading 和 error 状态。
- 视频结果进入现有 `/videos/{videoId}`，用户结果进入 `/users/{userId}`。
- 空 query 不发起宽泛请求；页面展示输入提示。
- 参数错误返回可操作中文提示；数据库或搜索基础设施异常统一显示“搜索服务暂时不可用，请稍后重试”，网络失败提示检查连接，不向用户暴露内部错误文案。
- 搜索错误使用稳定 code：`SEARCH_QUERY_REQUIRED`、`SEARCH_QUERY_INVALID`、`SEARCH_QUERY_TOO_LONG`、`SEARCH_PARAMETERS_INVALID` 和 `SEARCH_SERVICE_UNAVAILABLE`；兼容 `error` 文本不得绕过 Web 统一消息解析器。
