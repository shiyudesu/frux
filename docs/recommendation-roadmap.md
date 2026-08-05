# 推荐系统演进路线

本文档是 Frux 推荐系统后续实施顺序的长期入口。切换会话或交接后，应先阅读本文并运行：

```bash
openspec list
```

各阶段的详细范围、设计和任务以 `openspec/changes/<change-name>/` 为准。当前路线坚持“先建立可信测量，再引入模型能力”，不得因为后续计划已存在而跳过前置依赖。

## 1. 总体顺序

```text
训练数据轨道

可信 Impression
      ↓
隐私数据集导出
      ↓
离线策略评估
      ↓
学习现有线性权重


语义召回轨道

Embedding 服务
      ↓
新视频实时接入 ──→ 历史视频回填 ──→ pgvector 索引
      ↓                                  ↓
实时语义画像 ──→ 历史画像重建 ──→ ANN Recall Provider
                                         ↓
                                   Shadow 评估
```

推荐的严格单线程实施顺序：

1. `persist-recommendation-training-impressions`
2. `export-recommendation-training-dataset`
3. `evaluate-recommendation-policies-offline`
4. `learn-recommendation-policy-weights`
5. `add-semantic-embedding-service`
6. `integrate-semantic-video-embeddings`
7. `backfill-semantic-video-embeddings`
8. `project-semantic-user-interest`
9. `rebuild-semantic-user-interest`
10. `enable-pgvector-recommendation-index`
11. `add-pgvector-recommendation-recall`
12. `shadow-semantic-ann-recall`

## 2. 数据与学习轨道

| 顺序 | OpenSpec 变更 | 任务数 | 目标 |
| --- | --- | ---: | --- |
| 1 | `persist-recommendation-training-impressions` | 24 | 持久化实际交付卡片的紧凑可信训练事实 |
| 2 | `export-recommendation-training-dataset` | 24 | 生成可重复、匿名化、版本化的离线数据集 |
| 3 | `evaluate-recommendation-policies-offline` | 27 | 回放线性策略并生成观测型离线指标 |
| 4 | `learn-recommendation-policy-weights` | 26 | 学习现有八个特征权重并输出禁用的候选策略 |

依赖关系：

```text
persist impressions
        ↓
export dataset
        ↓
offline evaluation
        ↓
learn policy weights
```

实施约束：

- 未完成可信 Impression 前，不得从完整候选池推导训练负样本。
- 未曝光的已交付卡片不得标记为负样本。
- 离线评估在没有有效 propensity 时只能报告观测结果，不得宣称因果提升。
- 权重学习只输出候选配置，不自动写库、激活或扩大流量。

完成前三项后，项目才具备评估后续语义召回是否真正提升观看质量的基础。

## 3. 语义 Embedding 与画像轨道

| 顺序 | OpenSpec 变更 | 任务数 | 目标 |
| --- | --- | ---: | --- |
| 5 | `add-semantic-embedding-service` | 26 | 提供固定模型、固定维度的内部语义向量服务 |
| 6 | `integrate-semantic-video-embeddings` | 27 | 为新发布视频实时生成并保存语义向量 |
| 7 | `backfill-semantic-video-embeddings` | 22 | 为历史公开视频补齐语义向量 |
| 8 | `project-semantic-user-interest` | 26 | 从新行为实时投影模型版本化的语义用户画像 |
| 9 | `rebuild-semantic-user-interest` | 22 | 从耐久事实重建历史用户的语义画像 |

依赖关系：

```text
add-semantic-embedding-service
              ↓
integrate-semantic-video-embeddings
        ┌─────┴─────┐
        ↓           ↓
video backfill   live semantic profile
        └─────┬─────┘
              ↓
semantic profile rebuild
```

实施约束：

- `hash-ngram-v1` 始终保留为降级路径。
- 实时接入不负责历史回填，历史回填不改变实时消费语义。
- 实时画像不负责历史重建；缺失语义视频向量的事件必须延迟重试，不能伪造已应用状态。
- 所有向量按模型版本隔离，禁止把 128 维 hash 画像解释成 384 维语义画像。

## 4. pgvector 与 ANN 轨道

| 顺序 | OpenSpec 变更 | 任务数 | 目标 |
| --- | --- | ---: | --- |
| 10 | `enable-pgvector-recommendation-index` | 28 | 建立可重建的 HNSW 语义视频索引和有界查询接口 |
| 11 | `add-pgvector-recommendation-recall` | 23 | 增加可由新策略显式选择的 `semantic_ann` Provider |
| 12 | `shadow-semantic-ann-recall` | 20 | 不影响线上结果地评估 ANN 延迟、覆盖率和候选重合度 |

依赖关系：

```text
semantic video backfill ──→ pgvector index ──┐
                                             ├─→ semantic_ann provider ─→ shadow
live/rebuilt semantic profile ───────────────┘
```

实施约束：

- pgvector 是可重建投影，版本化 JSON Embedding 仍是模型输出事实源。
- `semantic_ann` Provider 注册不等于启用；现有 `recommend/v1`、`recommend/v2` 不得被修改。
- Provider 失败只能降级到现有召回源，不能让 Feed、归因或 hash fallback 失效。
- Shadow 候选不得进入排序、Snapshot、响应、请求日志或归因证据。
- 历史画像重建不是 Provider 编译依赖，但属于扩大 Shadow 流量前的运营覆盖门槛。

## 5. 并行实施建议

完成数据轨道的前三项后，可以并行推进：

```text
轨道 A：learn-recommendation-policy-weights

轨道 B：add-semantic-embedding-service
          ↓
        integrate-semantic-video-embeddings
```

实时视频接入完成后：

- `backfill-semantic-video-embeddings`
- `project-semantic-user-interest`

可以并行实施。

随后：

- `rebuild-semantic-user-interest`
- `enable-pgvector-recommendation-index`

可以并行实施，但 ANN Provider 扩大验证流量前应确保二者均达到可接受覆盖率。

## 6. 阶段验收门槛

### 数据轨道完成

- 训练 Impression 与最终交付卡片一致。
- 数据集导出可重复、可校验且不泄露账号、Token、URL 或原始画像。
- 离线评估能报告样本覆盖、排除原因和位置分层结果。
- 学习权重不会自动激活策略。

### 语义数据完成

- 新视频语义向量持续生成，hash 覆盖不下降。
- 历史视频回填可恢复、可取消、可重复。
- 实时与重建画像在相同事实和时间点上结果一致。
- 模型、维度和画像版本不发生混用。

### ANN 基础设施完成

- 索引覆盖率、陈旧率和查询延迟可观测。
- 查询只返回已发布、公开、媒体就绪的视频。
- Provider 超时或数据库异常不会改变现有 Feed 可用性。
- Shadow 不改变任何用户可见结果。

## 7. Shadow 之后

现有计划不会自动启用语义召回。Shadow 达到覆盖率、延迟、错误率和候选质量门槛后，再单独创建小型灰度变更，例如：

```text
rollout-semantic-ann-policy
```

该变更只负责：

- 创建包含 `semantic_ann` budget/deadline 的新 Policy；
- 以 1%–5% 稳定 cohort 灰度；
- 对比观看、完播、快速跳过、负反馈和作者覆盖；
- 达不到门槛时回滚到不包含 `semantic_ann` 的策略。

双塔召回、学习型精排、序列 Transformer 和 Bandit 不属于当前路线。在可信数据、语义 ANN 和灰度评估完成前，不应创建这些实施变更。
