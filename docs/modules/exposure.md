# 曝光与观看历史模块设计

## 1. 模块职责

曝光模块保存观看行为流水，维护推荐去重所需的曝光聚合，并从 `play`、`complete`、`skip` 事件同步维护每个用户/视频的最新观看历史投影。

## 2. 接口设计

| 方法 | 接口路径 | 作用 | 鉴权 | 幂等键 |
| --- | --- | --- | --- | --- |
| POST | `/api/video-view-events` | 上报曝光、播放、完播或跳过事件 | 登录 | 无 |

成功返回 201。所有事件返回 `event`；只有 `event_type=exposed` 额外返回 `exposure` 聚合对象。观看历史的列表和删除接口由 [library.md](library.md) 暴露。

## 3. 数据表设计

### 3.1 `video_view_events`

保存 `user_id`、`video_id`、`scene`、可选 `request_id`、`event_type`、`watch_ms`、`completed` 和 `created_at` 原始流水。

### 3.2 `exposures`

`(user_id, video_id)` 唯一，保存首次/最近曝光时间、曝光次数和最近场景。只有 `exposed` 事件更新该表。

### 3.3 `video_view_history`

| 字段 | 说明 |
| --- | --- |
| `user_id` + `video_id` | 复合主键，一位用户对一个视频一条最新状态 |
| `last_scene` | 最近观看场景 |
| `last_event_type` | 最近的 `play` / `complete` / `skip` |
| `last_watch_ms` | 最近上报的观看时长 |
| `completed` | 最近事件的完播状态；`complete` 会强制为 true |
| `first_watched_at` | 首次进入历史的时间 |
| `last_watched_at` | 最近观看时间 |
| `last_event_id` | 最近事件的确定性并列顺序 |
| `created_at`、`updated_at` | 投影时间字段 |

索引 `idx_video_view_history_user_last` 按 `user_id, last_watched_at, video_id` 支持稳定倒序读取。

## 4. 业务规则

| 规则 | 说明 |
| --- | --- |
| 可上报视频 | 必须为已发布，并且公开或属于当前用户；他人的私密视频和下架/删除视频返回 404 |
| 历史事件 | `play`、`complete`、`skip` 在写事件的同一事务内 upsert 历史 |
| 曝光隔离 | `exposed` 更新 `exposures`，但不创建观看历史 |
| 最近状态覆盖 | 仅当 `(created_at, event_id)` 更新时覆盖最近场景、事件、时长、完播状态和时间；迟到或并发后提交的旧事件不能回退投影，同时保留最早首次观看时间 |
| 历史迁移 | 统一迁移从现有三类观看事件按最新 `created_at, id` 补齐投影，并在同一事务写入 `app_migration` 持久标记；后续启动跳过回填，避免恢复用户已删除的历史 |
| 删除语义 | 删除观看历史只删除投影，不删除原始事件或 `exposures` |

## 5. 测试建议

| 场景 | 期望 |
| --- | --- |
| 上报 exposed | 写原始事件和曝光聚合，不写历史 |
| 上报 play 后 complete | 同一历史行更新时长、事件和完播状态 |
| 旧事件晚于新事件提交 | 历史仍保留确定性较新的 `(created_at, event_id)` 状态 |
| 上报自己的私密已发布视频 | 允许写入 |
| 上报他人的私密视频 | 返回 404 |
| 清理观看历史 | 原始事件和曝光聚合保持不变 |
| 清理后重启 API/Worker | 一次性回填标记阻止原始事件恢复已删除投影 |

## 6. 前端接入点

| 页面 | 接入能力 |
| --- | --- |
| Feed/播放器 | 上报曝光、播放、完播和跳过事件 |
| 个人主页观看历史 Tab | 通过个人内容库读取进度，删除单项或清空投影 |
