# 媒体任务模块设计

## 1. 职责

生产媒体处理以 PostgreSQL `media_processing_job` 为权威状态机。任务按
`(asset_id, profile_version)` 唯一，数据库保存 attempts、next attempt、lease、heartbeat、终态和
reconciliation 状态；ffmpeg 不由 Kafka Offset 表示。

## 2. Kafka 唤醒

上传完成先耐久创建任务，再尽力发布短保留的
`frux.media.processing-requested.v1`，key 为 `asset:{asset_id}`。发布失败不撤销上传或任务。
Consumer 只严格验证命令与数据库任务、向有界本地调度器发送 signal，然后提交 Offset；本地无容量时
也可提交，因为 5 秒轮询和 reconciliation 会重新发现任务。重复、丢失、延迟、重启前到达的命令均不
改变数据库租约带来的单任务执行保证。

Kafka signal 与 5 秒 polling 进入同一个有界 worker pool。worker 先占用 slot，再 claim 最多一个
任务，因此两条入口合计不会超额租赁。每次 claim 使用随机唯一 token，不使用 hostname；heartbeat、
complete、retry 和 failed transition 都要求 token 匹配且 lease 尚未过期。heartbeat DB 调用使用从
处理 context 派生的短 deadline，stall 或 shutdown 会取消 ffmpeg，旧 token 不能更新已回收任务。

## 3. 恢复与监控

Worker 保留处理租约续期、过期 lease 回收、重试时间、终态通知、缺失输出修复和孤儿对象清理。
过期处理租约回到 `retryable` 时记录 `lease_expired`，便于区分进程中断和媒体内容错误。ffmpeg
失败只持久化有界诊断，但保留 stderr 末尾，因为最终错误通常出现在输出尾部；命令超时使用
`probe_timeout`、`transcode_timeout` 或 `dash_timeout`，不再只显示 `signal: killed`。
监控区分 `signaled`、`capacity_full`、`publish_failed`、`missing_job`、`stale` 与
`polling_recovery`。Kafka 只优化唤醒延迟，不是正确性或长期重试来源。

media shadow 只读 `(asset_id, profile_version)` durable job；缺失为 propagation pending，
已存在但 profile 冲突为 mismatch，不会 claim 或 signal job。

Worker 将当前步骤和可计算的步骤内进度写回同一任务。下载和上传按字节计算，整理/转换按已处理媒体
时间计算，检查和最终提交不伪造百分比。进度更新与 heartbeat 使用相同 claim token 和未过期 lease，
最多每 5 秒持久化一次；旧 Worker 尝试不能覆盖已被回收任务的进度。

后台重新处理只接受失败任务，要求 `content.enforce`、注册原因和 `Idempotency-Key`。任务重置、成功
审计、幂等回执和视频状态修复 Outbox 同事务提交；状态修复失败由 Worker 重试，不重复重置处理任务。

## 4. 处理策略

`media.processing` 显式配置：

- `max_duration`：允许处理的最大源视频时长。
- `command_timeout`：单次 ffprobe/ffmpeg 命令预算，必须不小于 `max_duration`。
- `ffmpeg_preset`：封闭允许列表中的 x264 preset。

当前本地、Docker 和 Prod 默认分别为 180 分钟、360 分钟和 `veryfast`。活动
`profile_version=v2` 只生成一个源分辨率 H.264/AAC faststart MP4，不再生成多清晰度或 DASH。
H.264/AAC 源走 stream copy，只有音频不兼容时只转 AAC，其他已接受视频 codec 只执行一次原分辨率
H.264 转码。未完成的 `v1` retryable 任务使用相同单输出恢复路径，已完成的历史多源对象不改动。
Prod 保持单个媒体执行 slot，避免同一 VPS 上多个 x264 进程竞争 CPU 和内存。

## 5. 对象存储流量与公开交付

新处理结果计算校验和后直接写入确定性的 `processed/{asset}/{profile}/{checksum}/...` 最终键：
已存在且大小、校验和一致时直接复用；冲突时明确失败，不覆盖已有对象。封面完成后直接把已校验的
上传键登记为 ready variant，不再下载并重新上传相同文件。

公开状态只保存在 PostgreSQL。发布为同一受保护对象生成
`/media/media/v3/{generation}/{variant_id}/{filename}` 虚拟地址；公开 resolver 每次签名之前校验
generation、variant 公开状态以及视频当前仍为已发布、公开且媒体就绪。下架清空 generation，恢复时
生成新 generation，均不复制或移动媒体正文。

历史 `media/v2/*` 变体先验证对应 `processed/*` 对象；缺失时仅做一次兼容修复读取，再切换为 v3。
旧对象至少保留 30 分钟并进入延迟清理，迁移窗口内旧 v2 地址仍可读。

```text
旧流程：源文件 GET -> 临时输出 PUT -> 临时输出 GET -> 最终输出 PUT
        -> 发布时最终输出 GET -> public copy PUT

新流程：源文件 GET -> 确定性最终输出 PUT
        -> 发布/下架/恢复只更新 PostgreSQL exposure
```

公开 307 缓存 25 分钟，雨云签名 GET 和媒体响应缓存 30 分钟；Range、HEAD、ETag 和历史 DASH 相对
分片继续兼容。下架立即拒绝新签名，但已经缓存的 307 或签名地址最多可继续使用 30 分钟。私密、owner、
reviewer 和 moderation 访问保持 `private, no-store`。

`frux_media_object_outbound_bytes_total{source}` 记录处理源读取、历史修复读取及公开 full/range 请求
估算；标签仅使用封闭低基数来源，不包含用户、视频、资产、URL 或对象键。

## 6. 受保护浏览器访问

本地 `/uploads` 的私密视频、封面和处理中预览继续由不可变 owner、视频引用和生命周期共同授权。
浏览器 `<video>/<img>` 使用仅限 `/uploads` 的 HttpOnly 资产 JWT，并同时要求 Web 维护的
SameSite=Strict 活跃标记。资产 JWT 与五分钟消费端 Access Token 同寿命，在登录、Refresh 和改密时
轮换，普通媒体或 API 响应不延长它；离线退出先删除活跃标记，因此旧 Cookie 不能继续作为浏览器资产
身份。生产对象存储的短期签名 URL 仍不得包含 Access 或 Refresh Token。
