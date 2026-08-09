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
监控区分 `signaled`、`capacity_full`、`publish_failed`、`missing_job`、`stale` 与
`polling_recovery`。Kafka 只优化唤醒延迟，不是正确性或长期重试来源。

media shadow 只读 `(asset_id, profile_version)` durable job；缺失为 propagation pending，
已存在但 profile 冲突为 mismatch，不会 claim 或 signal job。
