# 推荐系统演进路线

本文档是 Frux 推荐系统的唯一长期实施入口。当前项目处于开发和低数据量阶段，目标是先完成可解释、可回放、可降级的多模态内容发现与会话推荐，而不是建立依赖大量真实用户、自训练数据或近似索引的模型系统。

## 1. 当前实现与规划边界

当前已经实现：

- Fresh、Hot、内容相似、关注作者和 Session Continuation 等有界 Recall Provider；
- `hash-ngram-v1` 文本向量与现有内容/会话相似度；
- 版本化推荐策略、曝光抑制、负反馈、作者打散、Snapshot 与签名游标；
- 完整有界候选池评分，以及可选的 Provider Order、Reservation、可见性优先和 Round-Robin Quota Merge；
- 推荐请求证据、观看 Outcome、Outbox、Kafka 消费、幂等和降级；
- 词法公开视频/用户搜索。

当前尚未启用或完成验证：

- 真实凭证下的预训练多模态视频向量生成与 Golden Set 验收；
- 自然语言语义搜索和多模态相似视频的开发环境启用；
- Semantic Recall Provider 和 `semantic_similarity` 排序分量；
- 多模态 Session Interest；
- pgvector Exact Projection、HNSW 或其他 ANN；
- 多模态公开数据集评估、Shadow 和 Rollout。

所有 active OpenSpec change 都是规划产物。任务未完成的能力不得在文档、简历、演示或接口中描述为已经上线。

## 2. 总原则

当前路线固定为：

```text
pretrained multimodal representation
        +
deterministic rules
        +
bounded session context
```

- 使用预训练模型理解公开视频标题、简介和有界封面/关键帧，不训练或微调 Frux 模型。
- 模型的编程语言、运行位置、硬件、进程边界和供应商属于可替换实现，不进入产品规格。
- 排序、Provider 配额、去重、多样性、时效性、置信度和降级使用版本化确定性规则。
- 第一版使用当前/近期会话事实，不依赖长期用户画像或跨用户协同数据。
- PostgreSQL 保存业务事实、任务状态和权威向量；Projection、pgvector、HNSW 和 Redis 均可重建。
- 多模态失败不得阻塞发布、词法搜索、Feed、`hash-ngram-v1` 或现有推荐策略。
- 低流量阶段只声明离线相关性、确定性回归和工程性能，不声明统计显著的线上 CTR、完播率或留存提升。

## 3. 两条并行基础路线

多模态推荐接入前先完成两条可以独立验证的路线。

### 3.1 推荐候选正确性

现有候选不得在 Provider 配额分配、跨 Provider 去重/合并和统一评分前，仅因 `published_at` 排序被固定 Top-N 过早截断。

当前已经分别建立：

1. `fix-recommendation-candidate-pool-truncation`：移除全局发布时间预排序截断；
2. `add-recommendation-provider-quota-merge`：增加稳定的 Budget、Reservation、去重、缺额补位、超时和降级规则。

这两项不依赖多模态模型，也能改善现有 Fresh、Hot、Following、Similarity 和 Session Provider，因此不应继续隐藏在 pgvector/ANN Change 中。

### 3.2 多模态内容发现

`add-multimodal-video-discovery` 是新的第一条多模态纵向 Change，负责：

```text
环境无关 MultimodalEmbeddingProvider
        ↓
新公开视频 Durable Embedding Job
        ↓
版本化 Multimodal Video Embedding
        ↓
Exact Cosine
        ├──→ Lexical + Semantic Hybrid Search
        └──→ Similar Videos
```

它明确不包含用户画像、推荐 Provider、历史 Backfill、HNSW、训练或正式 Rollout。

两条路线在 Session Semantic Recommendation 阶段汇合：

```text
候选池正确性 ───────────────┐
                             ├──→ Session Semantic Recall + Ranking
多模态视频向量与 Exact Search ┘
```

## 4. 第一阶段不处理开发历史

当前没有必须迁移的生产历史数据，因此多模态第一版只处理：

- 功能启用后首次公开的新视频；
- 显式创建的开发 Fixture、Golden Set 或 Demo 视频。

已有视频缺少多模态向量是正常状态：

```text
有 active-contract vector
    → 可参加 Semantic Search / Similar / Future Semantic Recall

无 active-contract vector
    → 不参加语义路径
    → 继续参加 Lexical / Fresh / Hot / Following / Hash
```

第一版不需要全库扫描、历史视频 Checkpoint Backfill、历史用户画像重建，也不为达到100%覆盖而阻塞功能启用。

如果未来存在必须保留的真实数据，再根据实际规模、成本和恢复要求重新提出独立 Backfill Change。

## 5. 多模态视频合同

Frux 通过应用层窄接口访问 Provider：

```text
MultimodalEmbeddingProvider
├── EmbedVideoContent(public text + prepared images)
└── EmbedSearchQuery(normalized public query text)
```

Provider 实现可以是本地模型、远程服务、外部 API、ONNX 或离线适配，但必须遵守同一合同。

每条向量绑定 provider、model、immutable revision、dimension、text canonicalizer、frame-sampling policy、image-preprocessing policy、fusion policy、source input hash 和 vector digest。任意兼容字段不同的向量不得静默比较、融合或共用索引。

视频输入只包含当前 published、public、media-ready 视频的规范化标题/简介和有界准备图像。搜索输入只包含规范化公开查询。不得发送用户身份、行为、Session/Request ID、凭据、Token、签名 URL、评论、消息、私密或待审内容。

## 6. 新视频 Durable Embedding Job

新视频链路：

```text
首次公开事实
    ↓
现有 hash-ngram-v1 安全
    ↓
PostgreSQL Multimodal Job durable handoff
    ↓
Kafka source 可提交
    ↓
独立 Worker 领取 Job
    ↓
准备文本/关键帧并调用 Provider
    ↓
校验合同、向量、Lease 与当前视频状态
    ↓
条件保存权威向量事实
```

Job 必须具备：

- `pending`、`leased`、`retry`、`succeeded`、`terminal`；
- 数据库时间 Lease、Heartbeat、Reclaim 和 Fencing；
- 有界并发、Deadline、Retry-After、Attempts 和 Backoff；
- Manual Requeue、Cleanup 和固定失败码；
- Source Hash、Contract 和 Video Lifecycle 条件写入。

Provider 调用不在 Kafka Handler、视频发布或 Feed 同步路径中。

## 7. Exact 与 HNSW

### 7.1 第一版只使用 Exact

Exact 对每个符合条件的 active-contract vector 计算真实余弦相似度并返回准确 Top-K：

```text
Query Vector
    ↓
过滤 current + published + public + media-ready
    ↓
比较全部合格向量
    ↓
Exact Top-K
```

Exact 结果可复现、没有近似召回损失、不需要索引构建和重建，并且是未来 HNSW Recall 的真实基线。当前开发数据量没有证明 ANN 的必要性。

Exact 可以使用 pgvector 的无 ANN 索引查询，但 pgvector Projection 仍是可重建结构，权威向量事实不得只存在于索引列中。

### 7.2 HNSW 严格后置

HNSW 是近似最近邻图索引。只有 Exact 的实测数据达到预先定义的门槛，才创建独立 HNSW Change。

门槛必须来自合格向量行数、Exact P50/P95/P99、数据库 CPU/查询计划/并发、HNSW 相对 Exact 的 Recall@K、索引大小/构建/WAL/重建成本以及过滤后的候选补位质量。不得为了展示 ANN 技术而提前引入 HNSW。

## 8. 多模态视频发现

### 8.1 Hybrid Public Video Search

现有 `/api/search/videos` 保留 Lexical Path，并在兼容 Query Vector 可用时执行：

```text
Lexical Candidates
        +
Exact Semantic Candidates
        ↓
Versioned Deterministic Hybrid Merge
        ↓
Visibility Revalidation
        ↓
Stable Cursor Page
```

第一页 Query Embedding 必须有 Cache、Deadline、Admission Limit 且不在 HTTP 内重试。失败时返回 Lexical-only 结果。Hybrid Cursor 后续页不能静默切换为 Lexical 模式；无法复现兼容 Query Vector 时返回可重试错误。

用户搜索保持 Lexical-only。

### 8.2 Similar Videos

相似视频读取当前可见源视频的 active-contract vector，执行 Exact Cosine，排除源视频自身，复检邻居可见性，并使用绑定 Source/Model/Order 的不透明 Cursor。

这项能力不依赖用户画像，可作为第一批可见结果验证多模态向量是否有价值。

## 9. Session Semantic Recommendation

在多模态发现和候选池正确性完成后，创建独立 `add-session-semantic-recommendation` Change。

第一版 Session Query Vector 使用已验证的当前/近期会话事实：

```text
Current Video
+ Completed / Sustained Recent Videos
+ Like / Favorite
- Early Skip
- not_interested / already_seen
```

所有分量使用同一模型合同、固定行为权重、时间衰减和归一化规则。缺失向量时跳过或降级，不调用 Provider 重新解释用户行为。

Session Profile 必须有 Confidence，至少考虑有效信号数量、信号质量、时间新鲜度和兴趣方向一致性。Confidence 控制 Semantic Reservation 和 `semantic_similarity` 有效权重，防止一次误点支配推荐。

长期 Recent/Long-term/Negative 用户画像不属于第一版前置条件。

## 10. Semantic Recall 与排序

未来 Semantic Provider 输入 Session Query Vector、Policy Budget/Reservation/Deadline、有界近期视频排除列表和 Active Model Contract；输出 Video ID、finite positive cosine similarity、`semantic_session` Recall Reason 与 `semantic_similarity` Score Component。

任何启用 Semantic 的策略必须：

- 配置完整 Budget、Reservation 和 Deadline；
- 给 `semantic_similarity` 正有限权重；
- 至少保留一个 Fresh、Hot、Following、Similarity 或 Session Continuation 基础 Provider；
- 使用独立无队列容量，不能吞占基础 Provider Slots；
- 在失败、缺失画像或缺失向量时保持 Hash/Non-vector Fallback。

`recommend/v1` 和 `recommend/v2` 在独立 Rollout Change 前保持不变。

## 11. 诊断事实与评估

### 11.1 Recommendation Impressions

`persist-recommendation-training-impressions` 保留，但定位为诊断事实，不是自动训练入口。它记录实际交付顺序、Provider、Policy、Score Components、模型合同和必要的降级摘要，用于重现线上决策、调查候选丢失和配额失衡、Deterministic Replay、Active/Shadow 对比及策略回滚验证。数据必须最小化、可删除并设置保留期。

### 11.2 Public Dataset Evaluation

使用独立 Adapter 和隔离数据目录：

- MicroLens：多模态视频内容、相似视频、语义搜索和 Session Top-K；
- KuaiRec：Watch Ratio、完播/跳过规则、Session 行为、Popularity Baseline 和排序评估。

两个数据集独立实验，不混合用户或视频 ID，不进入 Frux 真实业务指标。

至少比较 Popularity、Recent、Category、Text-only、Image-only、Multimodal 和 Multimodal + Session Interest。指标包括 Recall@K、NDCG@K、HitRate@K、MRR、Catalog Coverage、多样性、重复率、Exact 延迟和 Embedding 吞吐。

### 11.3 Human Golden Set

维护版本化人工集合：Query → Relevant Videos、Source Video → Similar Videos、Session Facts → Expected Interest Direction、Negative Feedback → Expected Suppression。Golden Set 用于回归和启用门槛，不用于声称线上因果提升。

## 12. Dormant、Shadow 与 Rollout

推荐语义路径按以下状态推进：

```text
Dormant
   ↓
Shadow
   ↓
独立 Rollout Change
```

- Dormant：代码和配置存在，但生产策略不引用；
- Shadow：旁路运行，不改变候选、排序、Snapshot、响应、证据或归因；
- Rollout：Shadow 通过覆盖、延迟、错误率、Fallback 和人工相关性门槛后，以稳定 Cohort、小 Reservation 和 Kill Switch 启用。

低数据量不足以支持显著性结论时，只依据安全护栏、确定性回归、公开数据集和人工质量评审推进，不宣称统计显著提升。

## 13. 长期画像和训练严格后置

只有 Session Interest 已证明有价值且真实用户行为达到明确门槛，才重新提出长期用户画像，包括 Recent/Long-term/Negative Vector、Event Ledger、模型版本隔离、Confidence、时间衰减、幂等投影和按需 Rebuild。

训练数据导出、Learning to Rank、Bandit、协同过滤和权重学习必须同时满足数据、隐私、统计 Power、资源和维护成本 Gate。任一 Gate 未满足时继续使用预训练表示和确定性规则。

## 14. Active Change 处置表

下表是当前实施权威。被标记为“替代”或“后置”的 Change 即使仍位于 `openspec/changes/`，也不得直接 `/opsx:apply`。

| Change | 处置 | 说明 |
| --- | --- | --- |
| `add-multimodal-video-discovery` | 已完成并归档 | 默认关闭的环境无关合同、新视频 Job、Exact、Hybrid Search、Similar Videos、Golden Set；正式 Provider/模型合同留给后续独立 Change |
| `integrate-multimodal-provider-runtime` | 已完成并归档 | 双向签名 HTTP 协议、readiness/contract handshake、API/Worker 接线与 conformance；不选择或内置具体模型 |
| `integrate-tongyi-embedding-vision-flash` | 已完成并归档 | 可选 Tongyi Flash 日期版原生融合或无日期版确定性本地融合合同、百炼 HTTP Adapter、startup/video/query、Exact/Similar/Hybrid 实机验收与成本指标；默认关闭 |
| `add-multimodal-acceptance-runner` | 已完成、通过真实 Runner 验收并归档 | 默认无调用的验收计划、双重计费门禁、S3/审核/Job/Fact/Projection/Similar/Hybrid 编排与脱敏报告；真实链路已验证 2 次视频向量与 1 次查询向量调用，下一步进入真实 Golden Set 采集 |
| `add-session-semantic-recommendation` | 已完成、通过零模型调用技术验收并归档 | 使用可信当前/近期会话事实组合已有 active-contract 视频向量，新增默认关闭的 `semantic_session` Exact Recall、`semantic_similarity`、Confidence、Quota Underfill 与脱敏证据；版本化 Golden Set 通过，未 Shadow、未 Rollout |
| `add-session-semantic-acceptance-runner` | 已完成、通过真实 Runner 验收并归档 | 使用已有真实 active-contract 视频向量、临时低 Cohort 策略和正常行为/Feed API，已验证真实 `semantic_session`、Request Log、Quota、Snapshot 二页复用与推荐阶段零模型调用；临时策略与收藏均完成窄清理 |
| `fix-recommendation-candidate-pool-truncation` | 已完成并归档 | 当前策略预算内的完整有界候选集进入统一评分，已移除 response-limit 派生的 recency 截断 |
| `add-recommendation-provider-quota-merge` | 已完成并归档 | 预算总和可高于 pool；显式 Provider Order、Reservation、可见性优先和 Round-Robin Fill 保证至多500条确定输入 |
| `add-semantic-embedding-service` | 被替代 | 外部文本-only 合同被环境无关多模态合同替代 |
| `integrate-semantic-video-embeddings` | 被替代 | Durable Job 原则已吸收到新多模态 Change |
| `backfill-semantic-video-embeddings` | 后置 | 开发阶段不处理历史；存在真实迁移需求时重新提案 |
| `enable-pgvector-recommendation-index` | 被替代/后置 | 第一版 Exact 归入多模态发现；HNSW 需独立容量 Change |
| `add-pgvector-recommendation-recall` | 部分已拆分，剩余后置 | Candidate Truncation 与 Quota Merge 已独立提案；Session Semantic Recall 后续另建 |
| `project-semantic-user-interest` | 后置 | 第一版使用 Session Context；长期行为达到门槛后重提 |
| `rebuild-semantic-user-interest` | 后置 | 长期画像存在后才有意义 |
| `persist-recommendation-training-impressions` | 保留但重定位 | 诊断事实，不自动生成训练数据 |
| `evaluate-recommendation-policies-offline` | 已完成并归档 | 独立 Production Replay、盲评 Golden Set、KuaiRec v2 与 MicroLens canonical Adapter、7 类 Baseline、确定性 JSON/Markdown；零模型调用，不训练、不自动推荐策略 |
| `shadow-semantic-ann-recall` | 后置并需重命名 | Exact/Semantic Shadow，不应强绑定 ANN |
| `export-recommendation-training-dataset` | 退出 Active Path | 只有所有训练 Gate 满足后重提 |
| `learn-recommendation-policy-weights` | 退出 Active Path | 当前不训练、不学习线上权重 |

OpenSpec 暂无“abandoned/superseded”一等状态，因此这些旧目录暂时保留设计历史，不使用 `archive` 冒充已经完成。后续可以在用户明确批准后，使用不向长期 specs 同步的历史整理方式处理。

## 15. 实施顺序

```text
Track A：推荐候选正确性
  candidate truncation fix
      ↓
  provider quota merge

Track B：多模态内容发现
  add-multimodal-video-discovery
      ├──→ Hybrid Search
      └──→ Similar Videos
      ↓
  integrate-multimodal-provider-runtime
      ↓
  choose and validate concrete pretrained model

Track A + Track B
      ↓
  add-session-semantic-recommendation
      ↓
  diagnostic impressions + public dataset evaluation
      ↓
  semantic shadow
      ↓
  independent rollout

Later Gates
  ├── Exact capacity exceeded → HNSW proposal
  ├── Real historical migration → Backfill proposal
  ├── Sufficient long-term behavior → User profile proposal
  └── Data/privacy/power/resources pass → Training proposals
```

## 16. 第一版完成边界

第一版完成于：

- 新公开视频可异步生成版本化多模态向量；
- 无历史 Backfill 仍能正常启用；
- Exact Cosine 可稳定查询；
- Hybrid Public Video Search 与 Similar Videos 可用；
- Lexical、Hash 和现有 Feed Fallback 始终可用；
- Candidate Pool 与 Provider Quota Merge 正确；
- Session Semantic Recall、Ranking 和 Trace 可解释、可回放；
- MicroLens/KuaiRec Adapter、Golden Set 和自动报告可复现；
- Semantic Shadow 与 Active 结果隔离。

第一版不要求 HNSW、长期画像、训练、全量历史覆盖或统计显著的线上提升。
