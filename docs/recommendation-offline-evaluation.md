# 推荐离线评估

`cmd/recommendation-offline-evaluate` 在不启动 API、Worker、PostgreSQL、Redis、Kafka、S3 或模型
Provider 的前提下，提供三条相互独立的证据链：

| Track | 解决的问题 | 不能证明什么 |
| --- | --- | --- |
| `public-dataset` | 简单 Baseline 在公开短视频交互/内容特征上的 Top-K 表现 | Frux 线上提升、因果效果 |
| `replay` | 冻结候选上是否精确复现生产打分、排序和多样性 | Recall 缺失候选、反事实结果 |
| `golden` | 人工语义相关性、Session 方向和负反馈抑制是否符合预期 | 大样本统计显著性 |

三种报告都固定写入 `external_model_calls: 0`，不会训练权重、生成向量、修改策略或进入 Shadow。

## 公共数据准备

真实数据放在忽略目录 `apps/api/.offline-data/`，报告放在 `apps/api/.offline-reports/`。仓库只保存
人工编写的合成 schema Fixture，不包含公开数据集原始用户/视频记录。

### KuaiRec

来源：[KuaiRec 官方仓库](https://github.com/chongminggao/KuaiRec)。仓库当前声明 CC BY-SA 4.0；
使用前仍应自行核对下载页面、版本和许可。`kuairec-v2` Adapter 读取：

- `small_matrix.csv` 或 `big_matrix.csv`：`user_id`、`video_id`、`play_duration`、
  `video_duration`、`time`、`date`、`timestamp`、`watch_ratio`；
- `item_categories.csv`：`video_id`、`feat`；
- 可选规范化作者以及 Text/Image/Multimodal 预计算特征 CSV。

Adapter 会验证 `play_duration / video_duration` 与 `watch_ratio` 的误差不超过 0.01。KuaiRec
没有原生 like 字段，评估器不会从字段缺失合成 like 指标。

### MicroLens

来源：[MicroLens 官方仓库](https://github.com/westlake-repl/MicroLens)。不同规模/发布时间的布局和
获取条款可能不同，因此 Frux 不猜测原始文件格式，也不自动下载。操作者先通过官方处理流程生成
`microlens-canonical-v1`：

- interactions：`user_key,video_key,occurred_at,watch_ratio`；
- items：`video_key,author_key,categories`；
- 可选 Text/Image/Multimodal 预计算特征；
- manifest 中记录官方 release、来源、引用、许可审阅状态和 normalization recipe。

## Manifest

每个数据根目录必须包含严格 JSON manifest：

- `version=recommendation-public-dataset-manifest-v1`；
- dataset、release、source URL、citation、license ID、`license_status=operator_reviewed`；
- schema 与 MicroLens normalization recipe；
- 每个文件的相对路径、role、schema、SHA-256 和数据行数。

绝对路径、目录逃逸、符号链接、未知字段、hash/行数不符或未声明的 schema 都会在计算指标前失败。
报告只保留相对角色和校验信息，不记录本机绝对路径。

## Public Dataset 规则

`short-video-session-v1` 固定为：

- `watch_ratio >= 0.8`：positive；
- `watch_ratio <= 0.2`：quick skip；
- 中间值：neutral；
- 缺失值单独统计，不转换为负样本；
- 每个用户至少 3 条更早历史；
- 最近 20 条更早交互组成 Session；
- 最后一个 positive 作为 held-out target；
- 已交互视频从候选中排除，target 必须仍在候选全集。

Baseline：Popularity、Recent Interaction、Category、Text-only、Image-only、Multimodal、
Multimodal + Session Interest。后三类只消费 manifest 声明的预计算向量；缺失特征会使对应 Case
显示 unavailable，不会补零或调用模型。

指标包括 Recall@K、NDCG@K、HitRate@K、MRR、Catalog Coverage、作者/分类覆盖和集中度、
重复 run、Case/特征覆盖以及确定性排名工作量。真实纳秒延迟和 Embedding 吞吐只能通过 manifest
中带校验和的独立性能证据提供，避免破坏规范报告的字节级复现。

## 命令

默认只验证输入，不写报告：

```bash
cd apps/api
go run ./cmd/recommendation-offline-evaluate public-dataset \
  --root ./.offline-data/kuairec
```

执行公共数据评估：

```bash
go run ./cmd/recommendation-offline-evaluate public-dataset \
  --root ./.offline-data/kuairec \
  --evaluate --k 1,5,10,20 \
  --output-json ./.offline-reports/kuairec.json \
  --output-markdown ./.offline-reports/kuairec.md
```

生产 Replay：

```bash
go run ./cmd/recommendation-offline-evaluate replay \
  --input ./testdata/recommendation-offline/replay-v1/bundle.json \
  --baseline ./testdata/recommendation-offline/replay-v1/baseline.json \
  --candidate ./testdata/recommendation-offline/replay-v1/candidate.json \
  --evaluate --k 1,2 \
  --output-json /tmp/replay.json \
  --output-markdown /tmp/replay.md
```

只有 Feature Weights 和 Diversity 差异可 Replay。Recall、Deadline、特征生成、抑制、Fallback、
Rollout、采样、保留期、模型合同或 Snapshot 行为变化默认拒绝；`--diagnostic-only` 只能列出差异，
不会输出比较指标或策略建议。

人工 Golden Set：

```bash
go run ./cmd/recommendation-offline-evaluate golden \
  --input ./testdata/recommendation-offline/golden-v1.json \
  --evaluate --k 1,3 \
  --output-json /tmp/golden.json \
  --output-markdown /tmp/golden.md
```

候选展示时隐藏策略名和原始排名，相关性使用 0-3 分，至少两名独立标注者；最大/最小判断相差
2 分及以上时必须仲裁。Public Dataset 的 watch ratio 不能作为 Frux Golden truth。

## 可复现 Fixture

以下快照由合成数据生成，适合 CI 和面试演示：

- `apps/api/testdata/recommendation-offline/expected/kuairec.md`
- `apps/api/testdata/recommendation-offline/expected/microlens.md`
- `apps/api/testdata/recommendation-offline/expected/replay.md`
- `apps/api/testdata/recommendation-offline/expected/golden.md`

Fixture 中 Content Baseline 优于 Popularity 只是为了验证指标方向，不能引用为真实公开数据集结论。
真实 MicroLens 与 KuaiRec 必须分别生成报告，不允许合并用户/视频 ID 或平均成一个总分。
