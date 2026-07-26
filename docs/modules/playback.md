# 播放优化模块设计

## 1. 模块职责

播放优化模块负责端侧播放策略、预加载建议和播放质量上报，目标是降低首帧耗时和切换卡顿。

## 2. 接口设计

| 方法 | 接口路径 | 作用 | 鉴权 | 幂等键 |
| --- | --- | --- | --- | --- |
| GET | `/api/playback-config` | 获取端侧播放参数 | Bearer JWT | 无 |
| GET | `/api/preload-videos` | 兼容客户端按发布时间获取补充资源 | Bearer JWT | 无 |
| POST | `/api/playback-qos-reports` | Web 客户端上报首帧和卡顿质量数据 | Bearer JWT | 支持 |
| POST | `/api/playback-telemetry-batches` | 上报版本化播放技术遥测批次 | 登录鉴权 | batch/event ID |
| POST | `/internal/playback-qos-reports` | 上报首帧和卡顿质量数据 | 服务鉴权 | 支持 |

## 3. 数据表设计

### 3.1 `playback_config`

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | BIGINT | PK | 配置 ID |
| `platform` | VARCHAR(16) | NOT NULL | `iOS` / `Android` / `Web` |
| `network_type` | VARCHAR(16) | NOT NULL | `WiFi` / `4G` / `5G` |
| `preload_count` | INT | NOT NULL | 预加载条数 |
| `buffer_ms` | INT | NOT NULL | 缓冲阈值 |
| `updated_at` | DATETIME | NOT NULL | 更新时间 |

索引建议：`uk_platform_network(platform, network_type)`。

### 3.2 `playback_qos_log`

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | BIGINT | PK | 记录 ID |
| `user_id` | BIGINT | NOT NULL, DEFAULT 0 | 用户 ID，0 表示匿名或系统来源 |
| `video_id` | BIGINT | NOT NULL | 视频 ID |
| `first_frame_ms` | INT | NULLABLE | 首帧耗时 |
| `stutter_count` | INT | NOT NULL, DEFAULT 0 | 卡顿次数 |
| `watch_ms` | INT | NOT NULL, DEFAULT 0 | 观看时长 |
| `idempotency_key` | VARCHAR(128) | NULLABLE | 幂等键 |
| `created_at` | DATETIME | NOT NULL | 上报时间 |

索引建议：`idx_video_time(video_id, created_at)`，`uk_user_idempotency(user_id, idempotency_key)`。

### 3.3 `playback_telemetry_batch`

保存 schema version、batch ID、播放会话、载荷哈希、事件总数、接受/重复计数和客户端发送时间。认证用户按 `(user_id, batch_id)` 唯一；模型同时保留匿名会话约束，但当前 v1 HTTP 策略只接受认证用户。

### 3.4 `playback_telemetry_event`

保存规范化事件、单调 offset、媒体位置/时长、首帧、卡顿/seek 区间、帧质量、错误、选源和低基数客户端维度。认证用户按 `(user_id, event_id)` 唯一，`created_at` 用于保留期清理，`playback_session_id + offset_ms` 用于会话诊断。

## 4. 业务规则

| 规则 | 说明 |
| --- | --- |
| 播放配置按端和网络匹配 | Web 和移动端可使用不同预加载策略 |
| Web 预加载服从活动 Feed 顺序 | 当前 Web 从活动场景已经返回的有序 items 派生上一条、当前条和后续窗口 |
| 兼容补充接口不代表 Feed 顺序 | `/api/preload-videos` 仅按发布时间为旧客户端提供 current-video/refill 资源 |
| 端侧策略有界 | `preload_count` 绝对上限为 4；低内存、慢网、省流和离线状态会进一步收缩或只加载封面/元数据 |
| 缓冲阈值真实生效 | 立即下一条达到有效 `buffer_ms` 时标记可切换；无可靠 buffered range 时使用 `canplay` 兜底 |
| 预加载代际隔离 | scene、请求代际、登录态、video ID 或源版本变化会取消旧任务并释放资源 |
| 预加载只读公开内容 | 当前视频定位和后续候选要求 `status=published AND visibility=public`，且媒体为 `legacy_ready` 或 `ready` |
| 播放源保持兼容 | 响应保留 `media_url`、`cover_url`，生产媒体附加有序 `playback_sources` 和 `media_status` |
| 基线优先 | `media_url` 始终投影浏览器兼容 H.264/AAC faststart MP4；DASH 和其他 MP4 清晰度作为增量来源 |
| QoS 上报写流水 | 首帧、卡顿、观看时长写入日志 |
| 遥测批次有界 | schema v1 每批最多 50 个事件、64 KiB；事件 offset 单调，批次和事件 ID 稳定重试 |
| 遥测与行为分离 | 技术质量事件不替代曝光、进度、完播和跳过等行为事实 |
| 首帧准确测量 | 优先 `requestVideoFrameCallback`，否则依次使用 advancing-time 和 `playing` fallback |
| 卡顿分类 | 只在期望播放期间统计 rebuffer；暂停和 seek 不计普通卡顿 |
| 隐私安全 | 只接受有界技术维度；未知字段、完整 URL、token、cookie 和自由 metadata 被拒绝 |
| 失败隔离 | 遥测发送和重试不改变播放器成功状态，不阻塞页面退出 |
| 保留期清理 | `playback.telemetry.retention`、`cleanup_interval` 和 `cleanup_batch_size` 控制有界清理 |
| 指标供监控聚合 | 接受事件即时更新低基数 Prometheus 指标，标识符不进入标签 |
| 缺省配置可兜底 | 匹配不到配置时返回 Web 默认配置 |

## 5. 测试建议

| 场景 | 期望 |
| --- | --- |
| 获取 Web 播放配置 | 返回 preload_count 和 buffer_ms |
| 获取兼容补充视频 | 按发布时间返回 current-video 之后的资源；无锚点时从最新资源 refill |
| Feed 场景预加载 | 推荐、热门、关注、时间线均严格保持各自响应顺序 |
| 网络策略 | WiFi/5G、4G/default、慢网、离线和 save-data 使用有界差异化策略 |
| 当前视频变私密 | 不作为预加载定位点或返回项 |
| 视频仍在处理 | 不进入预加载列表 |
| 多变体视频 | 返回稳定排序的 MP4 与 DASH 播放源，同时旧客户端仍可使用 `media_url` |
| 上报 QoS | 写入 `playback_qos_log` |
| 上报遥测批次 | 返回 event_count、accepted_count 和 duplicate_count |
| 重放相同 batch/event | 不重复写入或聚合；同 ID 异载荷返回 409 |
| 版本、数量和体积超限 | 整批返回 400，不进行无界或部分处理 |
| 敏感/未知字段 | 严格 JSON 解码拒绝 URL、token、cookie、metadata 等未声明字段 |
| 首帧和卡顿 | 精确/fallback 首帧、seek 排除、暂停关闭和页面退出均产生正确事件 |
| 配置缺失 | 返回默认配置 |

## 6. 前端接入点

| 页面 | 接入能力 |
| --- | --- |
| Feed 页 | 获取播放配置；从当前 Feed items 派生有序候选并提前触发原分页 |
| Feed 预加载控制器 | 保留有界原生视频资源、复用已准备媒体、记录 attempt/ready/reuse/cancel/failure 调试计数 |
| 视频播放器 | 同时保留旧 QoS，并上报首帧、播放结果、卡顿区间、seek、选源、帧质量和终止错误 |
| 监控看板 | 展示播放质量指标 |

## 7. 发布与兼容

- 新 Web 客户端同时发送 telemetry 和旧 QoS，用相同发布窗口比较首帧、卡顿和失败趋势。
- v1 遥测接口初始只接受认证用户；匿名策略待服务端可签发稳定匿名会话后再开放。
- 旧 QoS 端点至少保留两个稳定 Web 发布版本；只有新遥测覆盖率、首帧趋势和卡顿趋势连续两周一致后，才停止 Web 主动调用，服务端兼容端点继续保留。
