# 视频向量模块设计

## 1. 模块边界

当前视频向量模块只提供确定性的 `hash-ngram-v1` 文本向量。它不调用远端模型服务，不创建语义
任务，不扫描历史视频，也不参与推荐策略、pgvector 或 ANN 召回。

语义服务与语义视频向量属于 `docs/recommendation-roadmap.md` 的未来步骤，必须在可信训练数据和
离线评估前置阶段完成后单独实施。

## 2. Live publication intake

Embedding 使用 `frux.embedding.video-published.v1` 独立 Group 消费保留 30 天的首次发布事实。
处理顺序固定为：

1. 校验 Kafka envelope、视频 ID key、事件身份、时间和有界视频字段；
2. 仅拒绝无效 UTF-8 和非空白控制字符，再使用既有 `BuildVideoText` 规则拼接标题和简介；
3. 生成确定性的 `hash-ngram-v1` 向量与文本 hash；
4. 条件持久化 `(video_id, hash-ngram-v1)`；
5. 仅在持久化成功后提交 Kafka Offset。

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
