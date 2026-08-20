# 视频向量模块设计

## 1. 模块边界

视频向量模块同时保留稳定的 `hash-ngram-v1` 基线，以及默认关闭的多模态合同、Durable Job、权威
向量事实、Exact Projection、查询向量和媒体帧准备能力。Application 合同不规定 Python、Go、ONNX、
本地/远程服务、模型家族、硬件或供应商；当前仓库没有选择或内置正式 Provider，因此所有配置默认
`multimodal.enabled=false`，不会调用模型或改变现有产品行为。

多模态路径不训练/微调模型，不扫描历史视频，不创建 HNSW/IVFFlat，也不接入推荐 Policy 或新的
Recall Provider。开发 Fixture 和功能启用后首次公开的新视频是第一阶段唯一向量来源。

## 2. Live publication intake

Embedding 使用 `frux.embedding.video-published.v1` 独立 Group 消费保留 30 天的首次发布事实。
处理顺序固定为：

1. 校验 Kafka envelope、视频 ID key、事件身份、时间和有界视频字段；
2. 仅拒绝无效 UTF-8 和非空白控制字符，再使用既有 `BuildVideoText` 规则拼接标题和简介；
3. 生成确定性的 `hash-ngram-v1` 向量与文本 hash；
4. 条件持久化 `(video_id, hash-ngram-v1)`；
5. 仅在持久化成功后提交 Kafka Offset。

启用 `video_jobs_enabled` 时，第4步完成后还会按完整合同与 source hash 幂等创建/刷新
`multimodal_embedding_job`。Kafka 只等待这次 PostgreSQL handoff，不等待媒体准备或 Provider 推理；
确定性无效输入注册 terminal/no-op，数据库 handoff 失败保持 retryable。

重复或回放事件使用相同文本 hash，不创建重复事实，也不刷新未变化记录的 `updated_at`。文本变化时
更新同一模型行。Feed 使用独立 Group，因此 embedding 延迟或失败不会阻塞 Feed fanout。

## 3. Persistence and parity

`video_embedding` 以 `(video_id, model)` 唯一保存维度、JSON 向量、文本 hash 和时间戳。当前唯一由
发布消费链路写入的模型是 `hash-ngram-v1`。

Kafka shadow 模式只读取该 hash 事实并比较预期文本 hash：

- 尚未出现持久化事实时返回 pending 并进行有界内联重试；
- 已存在且 hash 一致时返回 match；
- 已存在但 hash 冲突时返回 mismatch；
- shadow 路径不得生成或更新向量。

## 4. Failure and observability

数据库失败属于 retryable consumer failure，当前 Kafka Session 结束并依靠稳定事件 ID 和条件写入
承受重投。确定性 hash 生成不依赖外部网络或模型容量。

指标使用 `frux_video_embedding_vectors_total{model="hash",source="event",outcome}`，outcome 仅为
`generated`、`skipped` 或 `failed`。Kafka Group lag、commit 和 delivery delay 由通用 Kafka 指标
提供；标签不得包含视频 ID、文本、向量或错误正文。

## 5. Multimodal contract and persistence

完整合同由 provider/model/revision alias、dimension、text canonicalizer、frame sampling、image
preprocessing 和 fusion policy 组成。每个结果还绑定 source hash 与 vector digest；维度、finite 分量、
L2 norm、合同、source hash 或 digest 任一不匹配都以封闭失败码拒绝。

PostgreSQL 使用：

- `multimodal_embedding_job`：pending/leased/retry/succeeded/terminal、数据库时间租约、claim token、
  heartbeat、reclaim、backoff 与 fenced transition；
- `multimodal_job_operation`：人工 requeue 的幂等 receipt；
- `multimodal_vector_fact`：按 `(video_id, contract_key)` 隔离的权威向量事实；
- `multimodal_projection`：按当前合同重建的 Exact 检索数据。

迁移只创建空表、约束和普通 B-tree 索引，不读取 `video` 创建历史任务。旧视频没有行是健康的
“semantic uncovered”状态，仍参加 Lexical、Hash、Fresh、Hot、Following 和现有 Feed。

## 6. Media preparation and job execution

Worker 每次尝试都会重新读取视频、媒体资产和当前 source facts；必须满足 published/public/media-ready/
source-current 才能准备最多16张、受 MIME/像素/单张字节/总字节约束的 transient JPEG。临时目录在每次
调用后清理，图片、向量、原始 query、凭证、签名 URL 和 Provider error 正文不会进入普通日志。

Provider admission 是非阻塞的；超时、取消、忽略 cancellation 的调用、heartbeat loss 与 stale token
不会形成无界本地队列。推理前后都复检 source hash；期间发生标题、媒体、可见性或版本变化时丢弃结果，
刷新 exact-contract Job，不覆盖新事实。

## 7. Configuration and rollback

配置必须一次提供完整合同及有界 provider/jobs/images/query/exact/hybrid 参数。任何进程启用自身能力时，
必须注入与配置完全相同的 Provider 合同；API 的 Hybrid Search 还需要 query cache 和 Exact Repository。
关闭模式不需要 Provider。

回滚顺序是关闭 `hybrid_search_enabled`、`similar_videos_enabled`、`query_embedding_enabled` 和
`video_jobs_enabled`，最后关闭 `multimodal.enabled`。Hash、Lexical Search、发布、Feed 和推荐不受影响；
已写入的 Job/Fact/Projection 可保留排查，不需要历史重建。

运维接口 `/api/admin/multimodal-jobs` 和 `/api/admin/multimodal-jobs/{jobId}/requeue` 要求
`governance.execute`，只返回封闭状态、尝试次数和合同 alias，不返回 source hash、claim token、图片、
向量、query、URL、凭证或 raw error；requeue 与审计在同一事务提交。

## 8. Evaluation

`go run ./cmd/multimodal-eval -input testdata/multimodal/golden-v1.json` 读取版本化人工 Golden Set，
比较 lexical、text、image、multimodal 和 hybrid 的 Recall@K、NDCG@K、MRR、词法重叠与 P50/P95。
Fixture 结果只验证命令和报告格式；选择具体预训练模型前必须用真实开发样本替换评分，不得把报告解释为
CTR、完播率或因果提升。
