# 视频向量模块设计

## 1. 模块边界

视频向量模块同时保留稳定的 `hash-ngram-v1` 基线，以及默认关闭的多模态合同、Durable Job、权威
向量事实、Exact Projection、查询向量和媒体帧准备能力。Application 合同不规定 Python、Go、ONNX、
本地/远程服务、模型家族、硬件或供应商；Infrastructure 已提供签名 HTTP Provider Adapter 和进程接线，
但当前仓库没有选择或内置正式模型服务，因此所有配置默认 `multimodal.enabled=false`，不会调用模型或
改变现有产品行为。

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

`video_jobs_enabled=true` 时，Worker 在领取任务前先完成 Provider readiness/contract handshake，再组装
FFmpeg Preparer 与 `MultimodalJobWorker`。握手失败会让 Worker 启动失败，不会提前领取 Job；启动后的
429/5xx、超时和网络故障仍进入既有 bounded retry，取消和 lease loss 仍由 fencing 隔离。

## 7. Configuration and rollback

配置必须一次提供完整合同及有界 provider/jobs/images/query/exact/hybrid 参数。Provider 配置包含
`endpoint`、`hmac_secret`、`protocol_version`、`allow_insecure_local`、`startup_timeout`、调用 deadline、
admission 和请求/响应字节上限。任何进程启用自身推理能力时，必须通过 readiness 得到与配置完全相同的
Provider 合同；API 的 Hybrid Search 还需要 query cache 和 Exact Repository。Similar-only 与全部关闭
模式不需要在线 Provider。

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

## 9. Provider HTTP protocol

协议版本固定为 `frux-multimodal-v1`，Provider base URL 下提供三个 POST 操作：

- `/v1/ready`：返回 readiness、`video/query` capability 和完整 immutable contract；
- `/v1/embed/video`：接收 canonical public text 与有界 base64 图片；
- `/v1/embed/query`：接收 canonical public query。

请求头使用 `X-Frux-Multimodal-Protocol`、`X-Frux-Operation-ID`、`X-Frux-Timestamp` 和
`X-Frux-Signature`。请求签名覆盖 protocol、method、path、timestamp、operation ID 与 body SHA-256；
响应通过 `X-Frux-Response-Signature` 覆盖 protocol、status、operation ID 与原始 body SHA-256。
非 loopback 地址必须使用 HTTPS，客户端拒绝 redirect，并对 JSON unknown field、trailing value、body
大小、source hash、vector digest、维度、finite 分量和 L2 norm 做严格校验。

HTTP 408/429/5xx 与 transport failure 映射为 retryable，其他非 2xx 映射为 terminal；错误响应也必须签名
且只使用 `invalid_request`、`unsupported_contract`、`capacity`、`unavailable`、`internal` 封闭 code。
客户端自身不重试，重试和降级仍由 Worker Job 与 Query Embedder 决定。

不需要真实模型即可运行 transport conformance：

```bash
cd apps/api
go test ./internal/infra/persistence/embedding -run '^TestHTTPMultimodalProvider'
```

该测试服务只存在于 `_test.go`，其单位向量只验证协议，不能作为运行时 Provider 或相关性证据。

## 10. Tongyi Embedding Vision Flash adapter

Adapter 支持两个显式档位：

| `FRUX_MULTIMODAL_PROFILE` | Revision | 请求方式 | Fusion |
| --- | --- | --- | --- |
| `tongyi-embedding-vision-flash-2026-03-06` | `2026-03-06-res1` | 同一 content 内的 `text + multi_images`，要求一个 `type=fused` 结果 | `provider-fusion-v1` |
| `tongyi-embedding-vision-flash` | `stable-independent-mean-v1` | 独立 `text` 与 `multi_images` content，要求两个独立结果 | `normalized-mean-fusion-v1` |

两个档位都使用 provider `alibaba-bailian`、model alias `tongyi-embedding-vision-flash` 和 768 维输出，
但合同不同，数据库和检索层不会把两者的向量混在一起。日期版支持原生融合，并固定 `res_level=1`；
无日期版不支持融合、`dimension` 或 `res_level` 参数，因此 Adapter 会分别归一化文本与多图序列向量，
取等权均值后再次归一化。更改融合算法或权重必须使用新的合同标识。

Adapter 使用百炼原生 Multimodal-Embedding HTTP API，不依赖 DashScope SDK。图片使用 Base64 Data URI，
原视频、对象存储地址和签名 URL 不离开 Frux 边界。无日期模型名称可能被服务商调整实现；如实际模型
行为发生变化，应升级 revision 并重新生成向量。需要可重复性时优先使用带日期的档位。

原生开发时，先创建仓库本地配置文件：

```bash
cp apps/.env.multimodal.example apps/.env.multimodal
# 编辑 apps/.env.multimodal，填写 Profile、共享 HMAC 和百炼 API Key

cd apps/api
go run ./cmd/multimodal-provider
```

从仓库内启动时，API、Worker 和 Adapter 会自动向上查找 `.env.multimodal`，也会识别推荐位置
`apps/.env.multimodal`，不需要执行 `source`。进程中已存在的环境变量优先于文件。API/Worker 只从文件加载
`FRUX_MULTIMODAL_PROFILE`、`FRUX_MULTIMODAL_ENDPOINT` 和 `FRUX_MULTIMODAL_HMAC_SECRET`；只有 Adapter
会加载 `DASHSCOPE_API_KEY` 与 Tongyi 边界参数。Adapter 固定调用阿里云共享的 Multimodal-Embedding
接口，不需要配置业务空间 Endpoint。文件缺失时继续使用普通系统环境变量，
文件存在但格式错误时启动失败。

`.env.multimodal` 已被 Git 忽略。Docker Compose 不会把宿主机文件挂进容器，使用 Compose 时仍应通过
`--env-file .env.multimodal` 注入所需变量。`/health` 只表示进程存活；Adapter 必须先用所选模型完成一次
真实 text embedding probe，之后才会在签名 `/v1/ready` 中报告所选合同 ready。API Key、上游 Endpoint、
请求内容、向量、source hash 和上游 request ID 不进入正常日志或 Prometheus label。

启用顺序：

1. 启动 Adapter，确认 startup probe 成功；
2. 为 API/Worker 设置相同的 `FRUX_MULTIMODAL_PROFILE`、`FRUX_MULTIMODAL_ENDPOINT` 和
   `FRUX_MULTIMODAL_HMAC_SECRET`；
3. 先只打开 `multimodal.enabled` 与 `video_jobs_enabled`，发布少量开发视频生成向量；
4. 确认 Job、Fact、Projection 和成本指标正常，再打开 `similar_videos_enabled`；
5. 使用真实 Golden Set 验收后，最后打开 `query_embedding_enabled` 与 `hybrid_search_enabled`。

北京地域公开原价为每千输入 Token 0.00015 元。Adapter 按 operation 累加 input/image/text/output Token，
便于用实际调用校准费用；该计数不包含请求内容。官方接口与模型限制以
[阿里云 Multimodal-Embedding 文档](https://help.aliyun.com/zh/model-studio/multimodal-embedding-api-reference)
为准。
