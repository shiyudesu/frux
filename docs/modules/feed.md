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
| `created_at` | 事件时间 |

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

## 5. Hot 访问优化

`scene=hot` 使用 Redis ZSET 维护一小时滑动热榜，粒度为 1 分钟。互动写入时按权重写入当前分钟桶：点赞 3 分，收藏 4 分，评论 5 分；取消点赞、取消收藏、删除评论写入对应负分。

缓存 key：

```text
feed:hot:minute:v1:{yyyyMMddHHmm}
feed:hot:window:v1:{windowEndUnix}
```

读取热榜时，Feed Service 合并窗口结束分钟前 60 个分钟桶，移除汇总分小于等于 0 的条目，再按分数倒序读取当前页。分钟桶 TTL 为 2 小时，窗口临时 key TTL 为 2 分钟。

## 6. Web Feed 顺序预加载

Web 以当前场景已经返回的 `items` 数组作为唯一预加载顺序来源。`timeline`、`hot`、`following` 和 `recommend` 都从活动索引向后截取有效窗口，不再通过 `/api/preload-videos` 重新生成另一套候选顺序。

- 预加载代际绑定 scene、Feed request、登录态代际、video ID 和媒体源版本；切换场景、刷新列表或签名源变化会释放旧资源。
- 控制器最多保留上一条、当前条和网络策略允许的后续条目；窗口外资源清除监听器、定时器、媒体源和缓冲状态。
- 当活动索引加预加载窗口触及当前页末尾时，复用 Feed 原有分页路径提前加载下一页，追加项随后进入同一有序窗口。
- 预加载失败只影响对应候选，选择该视频时仍由可见播放器独立重试，不改变 Feed 的 loading/error 状态。
