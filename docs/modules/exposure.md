# 曝光与观看历史模块设计

## 1. 模块职责

曝光模块保存观看行为流水，维护推荐去重所需的曝光聚合，并从 `play`、`progress`、`complete`、`skip` 事件同步维护每个用户/视频的最新观看历史投影。Web 播放会话使用稳定事件 ID、会话 ID、序号和发生时间形成可重试的完整反馈闭环。

## 2. 接口设计

| 方法 | 接口路径 | 作用 | 鉴权 | 幂等键 |
| --- | --- | --- | --- | --- |
| POST | `/api/video-view-events` | 上报曝光、播放、完播或跳过事件 | 登录 | 无 |

成功创建返回 201；相同用户重放相同 `event_id` 返回已有结果和 200，同一 `event_id` 对应不同规范化载荷返回 409。所有事件返回 `event`；只有 `event_type=exposed` 额外返回 `exposure` 聚合对象。旧客户端可以继续省略新增字段。

新增可选请求字段：

| 字段 | 说明 |
| --- | --- |
| `event_id` | 客户端稳定事件 ID，与用户组成唯一幂等边界 |
| `playback_session_id` | 单次视频激活的播放会话 ID |
| `sequence` | 会话内单调事件序号 |
| `occurred_at` | 有界客户端发生时间；越界时按接口规则拒绝或回退 |
| `position_ms` | 当前媒体位置 |
| `duration_ms` | 已知媒体总时长 |

观看历史的列表和删除接口由 [library.md](library.md) 暴露。

## 3. 数据表设计

### 3.1 `video_view_events`

保存 `user_id`、`video_id`、`scene`、可选 `request_id`、`event_type`、`watch_ms`、`completed`、事件/播放会话标识、序号、发生时间、媒体位置、总时长和 `created_at` 原始流水。新事件按 `(user_id, event_id)` 唯一；历史行回填确定性的 `legacy-{id}` 事件 ID。带 recommendation request ID 的曝光、播放、进度、完播和跳过在行为 Worker 中幂等写入 `recommendation_outcome`，以 `view:{event_id}` 去重，供离线评估关联采样请求日志；该关联不改变曝光或历史写入的成功语义。

### 3.2 `exposures`

`(user_id, video_id)` 唯一，保存首次/最近曝光时间、曝光次数和最近场景。只有 `exposed` 事件更新该表。

### 3.3 `video_view_history`

| 字段 | 说明 |
| --- | --- |
| `user_id` + `video_id` | 复合主键，一位用户对一个视频一条最新状态 |
| `last_scene` | 最近观看场景 |
| `last_event_type` | 最近的 `play` / `progress` / `complete` / `skip` |
| `last_watch_ms` | 最近上报的观看时长 |
| `last_position_ms` | 最近上报的媒体位置 |
| `completed` | 最近事件的完播状态；`complete` 会强制为 true |
| `first_watched_at` | 首次进入历史的时间 |
| `last_watched_at` | 最近观看时间 |
| `last_occurred_at` + `last_event_id` | 最近事件的确定性顺序 |
| `created_at`、`updated_at` | 投影时间字段 |

索引 `idx_video_view_history_user_last` 按 `user_id, last_watched_at, video_id` 支持稳定倒序读取。

### 3.4 `view_event_outbox`

与观看事实同事务写入待发布事件，保存 `event_id`、载荷、尝试次数、租约、下次重试时间、已分发时间和错误摘要。Worker 获得租约后按迁移模式向 single 或 dual 传输发布并等待确认；Kafka 路径使用
`frux.exposure.view-event-recorded.v1`、`user:{user_id}` key 和幂等 acknowledged production，
dual/mirror 模式同时要求 RabbitMQ acknowledgement。只有所有所需传输确认后才标记 dispatched；
部分成功仍保留 pending，重复投递由下游按 `event_id` 去重。

### 3.5 `video_view_history_deletion`

以 `(user_id, video_id)` 保存单项删除水位；`video_id=0` 表示清空全部历史的水位。历史写入和删除按用户 advisory lock 串行。为覆盖允许的客户端未来时钟偏差，只有 `occurred_at` 晚于“删除时间 + 未来偏差窗口”的事件才能重新创建投影，延迟重试不会恢复删除前的历史。

## 4. 业务规则

| 规则 | 说明 |
| --- | --- |
| 可上报视频 | 必须为已发布，并且公开或属于当前用户；他人的私密视频和下架/删除视频返回 404 |
| 历史事件 | `play`、`progress`、`complete`、`skip` 在写事件的同一事务内 upsert 历史 |
| 曝光隔离 | `exposed` 更新 `exposures`，但不创建观看历史 |
| 端侧上报频率 | Web 在有效前台播放或媒体位置跨越 10 秒时上报 `progress`，并在暂停、seek、切换、隐藏和退出时刷新最终状态；不按每次 `timeupdate` 写事件 |
| 有效观看时间 | `watch_ms` 只累计可见页面中实际播放的时间，不包含后台、暂停和等待 |
| 完播判定 | 媒体结束，或同时达到 95% 且距末尾不超过 2 秒时写一次 `complete`；循环播放不重复写完播 |
| 最近状态覆盖 | 最近场景、事件和时间按同会话非递减时间/更高序号或跨会话 `(occurred_at, event_id)` 更新；位置和有效观看时长始终取最大值，完播状态做 OR，因此迟到事件不能回退投影也不会丢失已发生的更高进度/完播 |
| 幂等冲突 | 相同事件 ID 和相同载荷返回已有事实；相同 ID 不同载荷返回 409 |
| 曝光重放快照 | exposed 事件保存首次曝光时间和当次曝光计数快照；后续曝光不会改变旧事件重放的响应 |
| 可靠发布 | 事实、历史/曝光投影与 Outbox 同事务提交；single 传输或 dual 中任一所需传输暂时不可用时不丢失已接受反馈 |
| 历史迁移 | 统一迁移从现有三类观看事件按最新 `created_at, id` 补齐投影，并在同一事务写入 `app_migration` 持久标记；后续启动跳过回填，避免恢复用户已删除的历史 |
| 删除语义 | 删除观看历史只删除投影，不删除原始事件或 `exposures`；单项/清空删除水位阻止迟到旧事件恢复投影，删除后真实发生的新播放仍可重建历史 |

## 5. 测试建议

| 场景 | 期望 |
| --- | --- |
| 上报 exposed | 写原始事件和曝光聚合，不写历史 |
| 上报 play/progress 后 complete | 同一历史行更新位置、有效时长、事件和完播状态 |
| 重放相同 event_id | 返回已有结果，不重复投影或入队 |
| 同 event_id 不同载荷 | 返回 409 |
| 旧事件晚于新事件提交 | 历史仍保留确定性较新的会话序号或 `(occurred_at, event_id)` 状态 |
| 任一所需传输暂时不可用 | HTTP 仍接受事实，Outbox 保留事件、释放租约并在恢复后发布 |

推荐 active Group 为 `frux.recommendation.consume-view.v1`；shadow Group 为
`frux.recommendation.consume-view.v1.shadow.<deployment>`，只做契约、age 和耐久 fact parity。
View stream 必须先于 action stream cutover；action boundary 必须严格更晚，且 Worker 先启动
view active/shadow Group。Boundary 使用 Broker `LogAppendTime`，不使用 producer clock。
| 上报自己的私密已发布视频 | 允许写入 |
| 上报他人的私密视频 | 返回 404 |
| 清理观看历史 | 原始事件和曝光聚合保持不变 |
| 清理后重启 API/Worker | 一次性回填标记阻止原始事件恢复已删除投影 |

## 6. 前端接入点

| 页面 | 接入能力 |
| --- | --- |
| Feed/播放器 | 上报曝光、播放、进度、完播和跳过事件；页面退出使用 keepalive，并用按用户隔离的有界本地待发送队列保留稳定事件 ID 重试 |
| 个人主页观看历史 Tab | 通过个人内容库读取进度，删除单项或清空投影 |
