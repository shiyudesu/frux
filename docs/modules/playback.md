# 播放优化模块设计

## 1. 模块职责

播放优化模块负责端侧播放策略、预加载建议和播放质量上报，目标是降低首帧耗时和切换卡顿。

## 2. 接口设计

| 方法 | 接口路径 | 作用 | 鉴权 | 幂等键 |
| --- | --- | --- | --- | --- |
| GET | `/api/playback-config` | 获取端侧播放参数 | Bearer JWT | 无 |
| GET | `/api/preload-videos` | 获取预加载视频列表 | Bearer JWT | 无 |
| POST | `/api/playback-qos-reports` | Web 客户端上报首帧和卡顿质量数据 | Bearer JWT | 支持 |
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

## 4. 业务规则

| 规则 | 说明 |
| --- | --- |
| 播放配置按端和网络匹配 | Web 和移动端可使用不同预加载策略 |
| 预加载围绕当前 Feed 页 | 返回当前视频后续若干条视频资源 |
| 预加载只读公开内容 | 当前视频定位和后续候选要求 `status=published AND visibility=public`，且媒体为 `legacy_ready` 或 `ready` |
| 播放源保持兼容 | 响应保留 `media_url`、`cover_url`，生产媒体附加有序 `playback_sources` 和 `media_status` |
| 基线优先 | `media_url` 始终投影浏览器兼容 H.264/AAC faststart MP4；DASH 和其他 MP4 清晰度作为增量来源 |
| QoS 上报写流水 | 首帧、卡顿、观看时长写入日志 |
| 指标供监控聚合 | QoS 日志可被监控模块汇总 |
| 缺省配置可兜底 | 匹配不到配置时返回 Web 默认配置 |

## 5. 测试建议

| 场景 | 期望 |
| --- | --- |
| 获取 Web 播放配置 | 返回 preload_count 和 buffer_ms |
| 获取预加载视频 | 返回后续视频列表 |
| 当前视频变私密 | 不作为预加载定位点或返回项 |
| 视频仍在处理 | 不进入预加载列表 |
| 多变体视频 | 返回稳定排序的 MP4 与 DASH 播放源，同时旧客户端仍可使用 `media_url` |
| 上报 QoS | 写入 `playback_qos_log` |
| 配置缺失 | 返回默认配置 |

## 6. 前端接入点

| 页面 | 接入能力 |
| --- | --- |
| Feed 页 | 获取播放配置和预加载视频 |
| 视频播放器 | 上报首帧耗时、卡顿次数和观看时长 |
| 监控看板 | 展示播放质量指标 |
