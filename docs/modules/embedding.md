# 视频向量模块设计

## 1. Live publication intake

Embedding 使用 `frux.embedding.video-published.v1` 独立 Group 消费 30 天保留的视频首次发布事实。
每条记录先按 NFKC 与 Unicode 空白规则规范化标题/简介，计算文本 hash，并条件写入
`hash-ngram-v1`；随后在同一 PostgreSQL 事务 upsert 固定
`semantic-minilm-l12-v2@e8f8c211226b894f` job。两者提交后才允许 Kafka Offset commit。

## 2. 固定语义契约

HTTP client 只接受
`sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2` revision
`e8f8c211226b894fcb81acc59f3b34ba3efd5f42`、384 维、finite、L2-normalized、float32、CPU
契约。它使用 `X-Internal-Token`、最多两个连接/并发、16 KiB metadata 和 1 MiB embedding response
上限，不自动重试，严格拒绝未知字段、顺序/ID/index/维度/模型不匹配和尾随 JSON。

## 3. Durable semantic jobs

`semantic_embedding_job` 按 `(video_id, model)` 唯一，保存 canonical text hash、pending /
processing / retry / suspended / completed / failed、attempts、available_at、lease 和 bounded error
class。重试为 5s、30s、2m、10m，之后封顶 30m；过期 lease 可回收，文本变化会重置 job 并阻止旧
worker 覆盖新结果。禁用或服务 metadata 不匹配只暂停语义 job，hash 继续推进。

该 live 路径不扫描历史视频，也不改变推荐 recall/ranking。部署到已有目录后，没有再次产生正常
`video.published` 事件的历史视频保持不变。本 change 不提供历史 scan、command/job、cursor、
checkpoint、dry-run、re-embedding mode 或 backfill 专用 retry。未来独立
`backfill-semantic-video-embeddings` change 单向依赖这里的固定模型 identity、canonicalization、
validated client、conditional persistence 和 coverage 接口，并自行负责历史选择、可恢复进度与
operator control；live integration 不反向依赖或调度它。

每个 processor 一次只 claim 一个 job，并使用每次 claim 唯一 token；complete、retry 和 heartbeat
都同时按 token、text hash、未过期 lease fencing。远程请求期间每 `lease_ttl/3` 续租，旧 attempt
不能完成或重试已回收 job。heartbeat 使用从处理 context 派生的短 deadline，shutdown 或 PostgreSQL
stall 会取消推理，不会挂住 processor。metadata validator 只有在 `ResumeSemanticJobs` 成功后才启动
processor。

共享 `video.published` 契约只校验 envelope、业务 identity、时间、key 和视频字段边界，不引入语义文本
规则。只有 embedding Group 在解码后执行 canonicalization；确定性 input 错误作为该 Group 的 terminal
结果安全提交，同一记录仍可被 Feed Group 正常消费。
