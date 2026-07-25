# 个人内容库模块设计

## 1. 模块职责

`library` 模块聚合互动模块的喜欢/收藏事实、曝光模块的观看历史、自己拥有的稍后再看事实和视频模块的可读卡片。它不复制上游事实，只在 Application 层通过窄接口保持行为顺序、执行隐私检查并过滤不可读视频。

## 2. 接口设计

| 方法 | 接口路径 | 作用 | 鉴权 |
| --- | --- | --- | --- |
| GET | `/api/users/me/liked-videos` | 当前用户喜欢列表 | 登录 |
| GET | `/api/users/me/favorite-videos` | 当前用户收藏列表 | 登录 |
| GET | `/api/users/{userId}/liked-videos` | 隐私允许时读取公开喜欢列表 | 可匿名 |
| GET | `/api/users/me/watch-history` | 当前用户观看历史 | 登录 |
| DELETE | `/api/users/me/watch-history/{videoId}` | 删除一条历史投影，返回 204 | 登录 |
| DELETE | `/api/users/me/watch-history` | 清空历史投影，返回 204 | 登录 |
| GET | `/api/users/me/watch-later` | 当前用户稍后再看 | 登录 |
| PUT | `/api/videos/{videoId}/watch-later` | 设为稍后再看 | 登录 |
| DELETE | `/api/videos/{videoId}/watch-later` | 移除稍后再看 | 登录 |

列表 query 为可选 `cursor`、`limit`，默认 `limit=20`，范围 1–100。响应：

```json
{
  "items": [
    {
      "video": {"id": 1001, "visibility": "public"},
      "updated_at": "2026-07-24T08:00:00Z",
      "history": {
        "last_scene": "recommend",
        "last_event_type": "progress",
        "last_watch_ms": 18000,
        "effective_watch_ms": 18000,
        "last_position_ms": 22000,
        "completed": false,
        "last_watched_at": "2026-07-24T08:00:00Z"
      }
    }
  ],
  "next_cursor": "...",
  "has_more": true
}
```

`history` 只出现在观看历史项。PUT/DELETE 稍后再看返回 `video_id`、`active`、`updated_at`。

## 3. 数据表设计

### 3.1 `user_watch_later`

| 字段 | 约束 | 说明 |
| --- | --- | --- |
| `user_id` + `video_id` | 复合主键 | 每个用户/视频一条事实 |
| `status` | SMALLINT, DEFAULT 1 | 1 active / 2 removed |
| `created_at` | 时间 | 首次创建时间 |
| `updated_at` | 时间 | 最近设置状态时间 |

索引 `idx_user_watch_later_user_status_updated` 支持按 `user_id, status, updated_at, video_id` 读取。喜欢/收藏继续存于 `interaction_action`；观看历史存于 `video_view_history`。

## 4. 业务规则

| 规则 | 说明 |
| --- | --- |
| 身份来源 | 所有 `/users/me/**` 接口只使用 JWT 上下文用户 ID |
| 行为排序 | 喜欢、收藏、稍后再看按 `updated_at DESC, video_id DESC` |
| 历史排序 | 观看历史按 `last_watched_at DESC, video_id DESC` |
| 历史进度 | `last_position_ms` 表示媒体位置，`last_watch_ms` 表示有效前台播放时长；Web 优先展示媒体位置并兼容旧响应 |
| 历史单调性 | 只有确定性更新的 `(occurred_at, event_id)` 可覆盖最近状态，旧进度或 skip 不能回退已完成记录 |
| 稳定游标 | URL-safe Base64 游标编码时间和 `video_id`；非法游标返回 400 |
| 活跃事实 | 喜欢/收藏只读取 `interaction_action.status=1`，稍后再看只读取 `status=1` |
| 可读过滤 | 列表只补齐 `status=published` 且公开或属于当前用户的私密视频；删除、下架和他人的私密视频不返回 |
| 喜欢/收藏补取 | 最多三轮补取候选，尽量填满因不可读视频产生的空洞 |
| 历史/稍后再看补取 | 最多三轮跨候选页过滤并补取；达到轮次上限时用下一游标和 `has_more=true` 延续读取，正常边界内不会阻断更旧的可读项目 |
| 公开喜欢隐私 | `liked_visibility=public` 才可读取；否则返回 403 `liked videos are private` |
| 收藏范围 | 收藏列表只有本人接口；`favorite_visibility` 当前不产生公开接口 |
| 稍后再看幂等 | PUT 添加前验证视频当前可读；DELETE 可重复执行，即使视频已不可读也可写 removed 状态 |
| 历史删除 | 204 且天然幂等，只删除投影并写单项/全局删除水位；Web 清空前先使所有在途历史分页请求失效，并在清空期间拒绝新分页。删除水位覆盖允许的未来时钟偏差窗口，旧响应和删除前迟到事件不能恢复项目；窗口后真实新播放可重建历史 |

## 5. 测试建议

| 场景 | 期望 |
| --- | --- |
| 读取喜欢/收藏 | 按行为更新时间稳定倒序 |
| 已取消行为 | 不出现在列表 |
| 视频变私密或下架 | 他人列表不泄露卡片 |
| 私密公开喜欢列表 | 返回 403 且无项目 |
| 重复 PUT/DELETE 稍后再看 | 只保留一条事实并返回当前状态 |
| 删除/清空历史 | 投影消失，原始观看事件保留 |
| 清空期间旧分页响应返回 | 请求代次已失效，页面仍保持空历史 |
| 历史/稍后再看前部候选不可读 | 继续补取并返回更旧的可读项目 |
| 不可读候选超过补取边界 | 单次请求最多补取三轮，返回可继续读取的游标和 `has_more=true` |

## 6. 前端接入点

| 页面 | 接入能力 |
| --- | --- |
| 本人个人主页 | 喜欢、收藏、观看历史、稍后再看独立分页状态 |
| 推荐 Tab | 复用现有推荐 Feed API，不经过 library 后端 |
| 观看历史 | 按媒体位置显示已看秒数/已看完，兼容旧 `last_watch_ms`，支持单项删除和清空 |
| 稍后再看 | 支持乐观移除，失败时恢复 |
| 公开主页 | 仅在 `liked_videos_public=true` 时显示喜欢 Tab |
