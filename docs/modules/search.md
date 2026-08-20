# 全局搜索模块设计

## 1. 模块职责

`search` 模块聚合视频和账户的公开发现能力。它只搜索已发布、公开且媒体就绪的视频，以及状态正常的用户；不搜索评论、消息、私密内容或推荐内部数据。

Application 层依赖 `VideoSearchIndex` 和 `UserSearchIndex` 窄接口，PostgreSQL 查询由基础设施实现。
视频搜索还提供默认关闭的 Multimodal Query + Exact Retrieval 组合；用户搜索始终保持 lexical-only。
当 Query/Hybrid 开启时，API 在注册路由前对签名 HTTP Provider 执行 query capability 与完整合同握手，
然后组装 bounded query cache、query embedder、Exact index 和 Hybrid option；Similar-only 不依赖在线模型。

## 2. 接口设计

| 方法 | 接口路径 | 作用 | 鉴权 |
| --- | --- | --- | --- |
| GET | `/api/search/videos` | 搜索公开视频 | 可匿名 |
| GET | `/api/search/users` | 搜索正常用户 | 可匿名 |
| GET | `/api/videos/{videoId}/similar` | Exact 相似视频；无向量时返回健康 unavailable | 可匿名 |

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

视频项复用公开 `Video` 字段；用户项只返回 `id`、`nickname`、`avatar_url` 和 `bio`，不返回私有登录账号。

## 3. 数据与排序

Lexical 路径直接使用 PostgreSQL 参数化查询。启用多模态后，第一页先得到 bounded lexical candidates，
再从 normalized-query + contract cache 读取或进行一次无 HTTP 重试的有界 query embedding，并对 active
contract Projection 做数据库侧 Exact Cosine。失败、饱和、超时或无覆盖时第一页返回 lexical-only。

视频相关性顺序：

1. 标题忽略大小写完全匹配。
2. 标题前缀匹配。
3. 标题包含匹配。
4. 简介包含匹配。
5. 同相关性按 `published_at DESC, id DESC`。

Hybrid v1 用显式 lexical/semantic reservation、video ID 去重、固定 round-robin fill 和版本化 rank
combination；同一视频保留两种内部 reason。结果在响应前再次验证 published/public/media-ready，按
hybrid score、`published_at DESC, id DESC` 稳定分页。

Hybrid cursor 绑定 normalized query、mode、merge version、contract key、完整排序元组和 expiry；Hybrid
后续页无法重现兼容 query vector 时返回可重试 503，不能静默切换成 lexical 顺序。Lexical cursor 独立。
Provider 在启动后超时、饱和或短暂不可用时，Hybrid 首页面仍降级到 Lexical；带 Hybrid cursor 的请求则
继续返回可重试错误，以免在同一游标链中改变排序语义。

相似视频使用 source 的 active-contract 权威向量，Exact 查询排除 source，复检候选可见性和发布时间，
cursor 绑定 source、contract、exact ranking version 与排序元组。source 可读但无向量时返回
`semantic_available=false` 与空页；source 不可读返回404。

用户相关性顺序：

1. 昵称忽略大小写完全匹配。
2. 昵称前缀匹配。
3. 昵称包含匹配。
4. 同相关性按 `updated_at DESC, id DESC`。

## 4. 业务规则

| 规则 | 说明 |
| --- | --- |
| 视频可见性 | 仅返回 `published + public + media-ready` 视频 |
| 用户可见性 | 仅返回状态正常用户，不选择或返回登录账号、角色、状态或私密设置 |
| 用户发现边界 | 用户查询只匹配昵称；仅匹配私有登录账号的查询不返回该用户 |
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
| 仅登录账号匹配 | 不返回用户，也不通过结果确认账号存在 |
| 相同游标换 query/type | 返回非法游标 |
| 旧用户搜索游标 | 拒绝账号/昵称混合相关性版本，不影响视频游标 |
| 查询切换时旧响应返回 | Web 忽略旧响应 |
| Semantic 首页面不可用 | 返回原 Lexical 结果并记录 fallback |
| Hybrid 后续页无法重现 | 返回可重试错误，不混入 Lexical 页 |
| Similar source 无向量 | 200 空页且 `semantic_available=false` |

## 6. 前端接入点

- 顶部搜索框是受控 `role=search` 表单，Enter 和“搜索”按钮进入 typed `/search`。
- URL 的 `q` 与 `tab=videos|users` 是页面状态真相，浏览器前进/后退会同步输入框。
- 页面明确说明视频匹配标题/简介、用户只匹配昵称，避免暗示私有登录账号可被公开发现。
- 视频和用户 Tab 保留独立 items、cursor、has-more、loading 和 error 状态。
- 搜索页在固定应用壳层内拥有独立纵向滚动；结果超过视口时，全部首屏结果、错误提示和显式“加载更多”入口都可滚动到达。
- “加载更多”使用当前分类的 `next_cursor` 请求下一页，去重追加结果，并以新的 `has_more` 和 `next_cursor` 决定后续入口。
- 视频结果进入现有 `/videos/{videoId}`，用户结果进入 `/users/{userId}`。
- 空 query 不发起宽泛请求；页面展示输入提示。
- 参数错误返回可操作中文提示；数据库或搜索基础设施异常统一显示“搜索服务暂时不可用，请稍后重试”，网络失败提示检查连接，不向用户暴露内部错误文案。
- 视频详情页用明确 loading/unavailable/empty/error 状态展示相似视频；功能关闭或无向量不渲染伪造结果。
- 搜索错误使用稳定 code：`SEARCH_QUERY_REQUIRED`、`SEARCH_QUERY_INVALID`、`SEARCH_QUERY_TOO_LONG`、`SEARCH_PARAMETERS_INVALID` 和 `SEARCH_SERVICE_UNAVAILABLE`；兼容 `error` 文本不得绕过 Web 统一消息解析器。
