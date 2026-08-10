# Feed 模块设计

## 1. 模块职责
负责刷视频主链路，输出游标分页结果并驱动端侧播放生命周期。所有 Feed 场景和缓存卡片回源都只允许已发布公开视频；曝光、播放、进度、完播和跳过事实及观看历史投影由曝光模块维护。

## 2. 接口设计

| 方法 | 接口路径 | 作用 | 鉴权 | 幂等键 |
| --- | --- | --- | --- | --- |
| GET | `/api/feed-items` | 获取按 scene 排序的视频流 | 可匿名 | - |
| POST | `/api/video-view-events` | 上报曝光和观看事件 | 登录 | - |

### 2.1 Feed Items API

用于返回已发布且 `visibility=public` 的视频，支持时间线和热榜 Feed。

#### GET `/api/feed-items`

请求参数：

| 参数 | 位置 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `cursor` | query | string | 否 | - | 上一页返回的游标 |
| `limit` | query | int | 否 | 10 | 返回数量，最大 100 |
| `scene` | query | string | 否 | `timeline` | Feed 场景：`timeline`、`hot` |

`POST /api/feed-queries` 是推荐场景的复杂查询入口。`scene=recommend` 必须认证，并携带
受限的 `context`（request/session ID、refresh index、最近视频、当前视频、network、
save-data、viewport、播放能力）；完整字段上限和枚举见 [recommendation.md](recommendation.md)。
推荐 response 保持既有 `items/next_cursor/has_more` 形状，内部策略、分数、画像和 reasons
不会泄漏给客户端。

响应：

```json
{
  "scene": "timeline",
  "items": [
    {
      "video_id": 1001,
      "author_id": 12,
      "author_nickname": "tester",
      "author_avatar_url": "https://example.com/avatar.png",
      "title": "first video",
      "description": "hello timeline",
      "media_url": "https://example.com/video.mp4",
      "cover_url": "https://example.com/cover.jpg",
      "like_count": 0,
      "comment_count": 0,
      "favorite_count": 0,
      "published_at": "2026-05-03T12:00:00Z"
    }
  ],
  "next_cursor": "eyJwdWJsaXNoZWRfYXQiOiIyMDI2LTA1LTAzVDEyOjAwOjAwWiIsInZpZGVvX2lkIjoxMDAxfQ",
  "has_more": true
}
```

排序规则：

`scene=timeline`：

| 排序字段 | 方向 | 说明 |
| --- | --- | --- |
| `published_at` | DESC | 发布时间越新越靠前 |
| `id` | DESC | 同一发布时间下按视频ID倒序 |

`scene=hot`：

| 排序字段 | 方向 | 说明 |
| --- | --- | --- |
| `hot_score` | DESC | 最近 60 个分钟桶内的互动热度分 |
| `video_id` | DESC | 同分时按视频 ID 倒序 |

游标内容：

`scene=timeline`：

| 字段 | 说明 |
| --- | --- |
| `published_at` | 当前页最后一条视频的发布时间 |
| `video_id` | 当前页最后一条视频ID |

`scene=hot`：

| 字段 | 说明 |
| --- | --- |
| `window_end` | 当前热榜窗口结束分钟 |
| `offset` | 下一页起始排名位置 |

## 3. 数据表设计

Feed 依赖 `video` 和 `video_stat` 读取已发布公开视频与互动计数。曝光上报写入 `video_view_events` 行为流水；`event_type=exposed` 维护 `exposures`，`play/progress/complete/skip` 同事务维护 `video_view_history`。完整投影规则见 [exposure.md](exposure.md)。

`video_view_events`：

| 字段 | 说明 |
| --- | --- |
| `id` | 主键 |
| `user_id` | 上报用户 |
| `video_id` | 视频 ID |
| `scene` | Feed 场景 |
| `request_id` | 一次 Feed 请求标识 |
| `event_type` | `exposed`、`play`、`progress`、`complete`、`skip` |
| `watch_ms` | 有效前台播放时长 |
| `position_ms` | 媒体位置 |
| `event_id`、`playback_session_id`、`sequence` | 重试安全的播放会话顺序 |
| `occurred_at` | 有界事件发生时间 |
| `completed` | 是否完播 |
| `created_at` | 服务端持久接收时间；推荐归因窗口使用该可信时间，而非客户端 `occurred_at` |

`exposures`：

| 字段 | 说明 |
| --- | --- |
| `user_id` + `video_id` | 唯一曝光事实 |
| `first_exposed_at` | 首次曝光时间 |
| `last_exposed_at` | 最近曝光时间 |
| `exposure_count` | 重复曝光次数 |
| `last_scene` | 最近曝光场景 |

`video_view_history` 以 `(user_id, video_id)` 唯一，记录最近场景、事件、媒体位置、有效观看时长、完播状态和最近观看时间；纯曝光事件不会进入历史。Web 每个激活视频建立独立播放会话，暂停、切换、隐藏和退出会刷新状态，React Strict Mode 或请求重试不会制造重复事实。

推荐索引：

```sql
CREATE INDEX idx_video_timeline ON video (status, published_at DESC, id DESC);
```

## 4. Timeline 访问优化

`scene=timeline` 启用 Redis 读缓存：

| 缓存项 | TTL |
| --- | --- |
| 首页 | 5 秒 + 抖动 |
| 后续页 | 45 秒 + 抖动 |
| 视频卡片 | 15 分钟 |
| 视频计数 | 15 秒 |

缓存 key：

```text
feed:page:v1:{scene}:limit:{limit}:first
feed:page:v1:{scene}:limit:{limit}:cursor:{cursorHash}
video:card:v1:{video_id}
video:stat:v1:{video_id}
```

页缓存只保存 `video_id` 和排序字段。Feed Service 读取页后使用 Redis MGET 批量读取 `video:card` 和 `video:stat`，缓存缺失时批量回源 PostgreSQL。即使卡片来自 Redis，也会批量查询数据库重新确认 `status=published AND visibility=public`，因此旧页或旧卡片中的私密/下架 ID 会在组装阶段被丢弃。可见性、删除或生命周期变化会删除对应卡片和统计缓存；页缓存中的旧 ID 依靠上述校验安全失效。

关注流 Redis inbox 还必须绑定当前有效关注作者集合。读取时若发现取关作者的陈旧条目或缺少作者 ID 的旧格式条目，整页回退 PostgreSQL 关注关系真相源，不能把历史扇出内容继续返回给已取关用户。

## 5. Hot 访问优化

`scene=hot` 使用 Redis ZSET 维护一小时滑动热榜，粒度为 1 分钟。互动写入时按权重写入当前分钟桶：点赞 3 分，收藏 4 分，评论 5 分；取消点赞、取消收藏、删除评论写入对应负分。

缓存 key：

```text
feed:hot:minute:v1:{yyyyMMddHHmm}
feed:hot:window:v1:{windowEndUnix}
```

读取热榜时，Feed Service 合并窗口结束分钟前 60 个分钟桶，移除汇总分小于等于 0 的条目，再按分数倒序读取当前页。分钟桶 TTL 为 2 小时，窗口临时 key TTL 为 2 分钟。

## 6. Web Feed 顺序预加载

### 5.1 发布 fanout 的 Kafka 边界

首次公开视频由 `frux.video.published.v1` 提供 30 天保留事实。Feed 使用独立
`frux.feed.video-published.v1` Group，在卡片预热和 inbox/author-outbox 幂等 ZSET 写入成功后才提交
Offset。Embedding Group 的延迟和重放不会阻塞 Feed Group；重复记录继续使用原始
`published_at`，不会改变 Timeline 顺序。切换前使用独立 shadow Group 做信封、key、年龄和事实校验，
异常时只回滚 Feed responsibility 到 RabbitMQ。

关注场景在 Web 中额外展示 208px 关注目录。目录数据来自关系模块
`GET /api/users/me/following`，拥有独立的 query、cursor、loading 和 error 状态；目录滚轮与指针
不进入 Feed 切换处理器。点击用户进入公开主页，不产生作者过滤 Feed。目录可收起释放舞台宽度，
且不得展示关系 API 未提供的直播或未看作品事实。

Web 以当前场景已经返回的 `items` 数组作为唯一预加载顺序来源。`timeline`、`hot`、`following` 和 `recommend` 都从活动索引向后截取有效窗口，不再通过 `/api/preload-videos` 重新生成另一套候选顺序。

推荐首页正常情况下由 Redis snapshot 固定排序，签名 cursor 绑定用户、scene 和 request ID；
后续组装仍重新校验可见性，因此候选私密、下架或删除时会形成安全的页内 gap。Redis 不可用时
使用确定性 score cursor 并在服务端记录 degraded，客户端仍按普通 cursor 消费。

- 预加载代际绑定 scene、Feed request、登录态代际、video ID 和媒体源版本；切换场景、刷新列表或签名源变化会释放旧资源。
- 控制器最多保留上一条、当前条和网络策略允许的后续条目；窗口外资源清除监听器、定时器、媒体源和缓冲状态。
- 当活动索引加预加载窗口触及当前页末尾时，复用 Feed 原有分页路径提前加载下一页，追加项随后进入同一有序窗口。
- 预加载失败只影响对应候选，选择该视频时仍由可见播放器独立重试，不改变 Feed 的 loading/error 状态。

播放器层在该候选窗口上再收敛为 previous/current/next 三槽池：

- 槽 key 使用完整 generation、video ID 和 source revision；同 generation 相邻前进/后退只轮换角色，不重建媒体元素。
- pool 优先 current，其次 next、previous，绝对上限为 3；scene、请求代次、登录态或源 revision 变化会释放旧 adapter、监听器和媒体源。
- next slot 使用 `buffer_ms` 判断 ready；提交切换时即使未 ready 也不回退导航，而由新 current 显示 loading/buffering。
- 视频舞台直接挂载 pool 已 acquire 的预加载句柄；无句柄时使用同一 adaptive media resource 内部准备兼容 MP4。

## 7. Web Feed 场景连续性

Web 在同一次已挂载 Feed 会话中为 `timeline`、`recommend`、`following` 和 `hot` 分别保留内存快照。
快照包含有序卡片、活动视频、viewer action、后续 cursor、request ID，以及推荐场景专属的
session/context、下一 refresh index 和反馈抑制集合。用户直接切换 Feed 路由或通过浏览器 Back
返回时，如果快照仍有效，页面恢复原活动视频和分页尾部，不重新请求第一页。

- 快照绑定当前 Token 与用户 ID；登录、退出或账号替换会清除全部场景，避免跨身份恢复关注流、推荐流或互动状态。
- 显式刷新/重试只替换当前场景并从第一项开始，其他有效场景不受影响；FeedPage 卸载或浏览器完整刷新后不保留快照。
- 非活动场景最多保留连续的 120 张卡片。只有后缀同时包含活动视频和已加载分页尾部时才保留 cursor；
  无法安全压缩、缺少活动视频或 request/context 不一致时直接丢弃并重新加载，不能伪造分页连续性。
- 场景切换会提升 activation/request generation，并使旧 first-page、load-more 和预加载结果失去提交权限；
  迟到响应不能覆盖当前场景或污染已提交快照。
- 快照只保留数据。切换时关闭评论、取消 swipe，并释放 player/preload adapter、监听器、缓冲和媒体资源；
  返回后创建新的可见播放生命周期，不恢复播放时间、菜单、全屏或焦点。
- 点赞、收藏和评论计数按 video ID 更新所有包含该视频的有效快照；推荐负反馈的删除和抑制只修改推荐快照。
- 新推荐请求的 `recent_video_ids` 与 `current_video_id` 只读取推荐快照。Timeline、Following 和 Hot
  的活动视频不会进入推荐上下文；返回有效推荐快照时继续使用原 request/session、签名 cursor 和抑制状态。
- 左侧导航只在当前活动 Feed 的同一高亮行内显示低强调单向刷新按钮，不使用独立凸起方块。点击栏目主体仍恢复快照；
  点击刷新按钮会关闭临时 UI、保留其他场景快照，并让当前场景从第一页和第一条卡片重新开始。

Feed shadow parity 只读 PostgreSQL follower 真相与 Redis inbox/author-outbox，不执行预热或 fanout。
缺失事实按 propagation pending 最多内联重试三次，冲突才记录 mismatch；配置 shadow/active
cutover 时 parity reader 不能为空。
