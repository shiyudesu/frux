# 推荐模块设计

## 1. 模块职责

推荐模块负责上下文化候选召回、策略排序、打散、反馈抑制和评估记录，为 Feed 提供可下发的视频列表。服务端从账户、关系、互动、曝光和当前可见性读取可信事实；客户端只提供有界会话与播放能力提示。

推荐系统的后续实施顺序、OpenSpec 依赖和阶段验收门槛见
[推荐系统演进路线](../recommendation-roadmap.md)。

## 2. 接口设计

| 方法 | 接口路径 | 作用 | 鉴权 | 幂等键 |
| --- | --- | --- | --- | --- |
| POST | `/internal/recommendation-candidates` | 一次完成召回、排序、打散 | 服务鉴权 | 支持 |
| POST | `/internal/exposure-decisions` | 判断候选是否近期曝光 | 服务鉴权 | 支持 |
| POST | `/internal/exposures` | 写入曝光记录 | 服务鉴权 | 支持 |
| POST | `/api/recommendation-feedback` | 提交推荐反馈 | 登录 | 必填 |

启用 `internal.enabled` 时，全部 `/internal` 推荐接口使用 `X-Internal-Token` 与配置的
`internal.token` 做恒定时间比较；启动会拒绝空值、占位值和弱 token。缺失或错误 Token
一律返回 401，不接受浏览器 JWT 代替服务间凭据。

## 3. 数据表设计

### 3.1 `reco_rule`

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | BIGINT | PK | 规则 ID |
| `scene` | VARCHAR(32) | NOT NULL | 场景，如 `feed` |
| `config_json` | JSON | NOT NULL | 召回、排序、打散参数 |
| `enabled` | TINYINT | NOT NULL, DEFAULT 1 | 是否启用 |
| `updated_at` | DATETIME | NOT NULL | 更新时间 |

索引建议：`uk_scene(scene)`。

### 3.2 `reco_exposure_log`

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | BIGINT | PK | 记录 ID |
| `user_id` | BIGINT | NOT NULL | 用户 ID |
| `video_id` | BIGINT | NOT NULL | 视频 ID |
| `scene` | VARCHAR(32) | NOT NULL | 场景 |
| `request_id` | VARCHAR(64) | NOT NULL | 请求 ID |
| `exposed_at` | DATETIME | NOT NULL | 曝光时间 |

索引建议：`idx_user_scene_time(user_id, scene, exposed_at)`、`idx_request_id(request_id)`。

## 4. 业务规则

| 规则 | 说明 |
| --- | --- |
| 候选只返回公开已发布视频 | 查询条件为 `status=published AND visibility=public`；私密、下架、删除和异常视频不进入候选 |
| 曝光写入校验可见性 | 推荐曝光持久化前再次校验视频仍为已发布公开状态 |
| 曝光去重按用户生效 | 同一用户近期曝光过的视频降低或移除优先级 |
| 打散避免同作者集中 | 同一作者的视频在单页中保持间隔 |
| 请求携带 scene | 不同 Feed 场景可使用不同策略 |
| 内部接口支持幂等 | 重复曝光写入不会产生重复事实 |
| 观看反馈可重试 | 推荐画像消费带稳定 `event_id` 的播放、进度、完播和跳过事件；重复投递只应用一次 |
| 观看 outcome 独立于画像 | durable view outbox 在投影前按服务端 `recorded_at` 验证并幂等保存 outcome；缺少 embedding 只让画像投影退避，不延迟已验证的曝光、进度、完播或跳过归因 |
| 进度参与兴趣权重 | 有效前台播放进度和完播提升内容兴趣，过短跳过不作为正反馈 |
| 发布与 HTTP 解耦 | 曝光模块通过事务 Outbox 向 Kafka behavior stream 可靠投递；只有 broker acknowledgement 后才 dispatched，失败保留 pending |
| 画像投影不影响写入结果 | 反馈和关注事实在各自事务内写入可租约重试的画像 Outbox；Worker 以稳定事件 ID 幂等投影，失败保留待重试 |

## 5. 测试建议

| 场景 | 期望 |
| --- | --- |
| 请求推荐候选 | 只返回已发布公开视频列表 |
| 候选视频转为私密 | 后续召回和曝光写入均不再接受该视频 |
| 同一作者候选过多 | 结果被打散 |
| 判断近期曝光视频 | 返回已曝光状态 |
| 写入曝光记录 | 记录 request_id 和曝光时间 |
| 重复写入曝光 | 结果稳定 |

## 6. 前端接入点

推荐模块主要服务后端 Feed。Web 将有界 context 与推荐请求一同发往 Feed；“更多”菜单的
推荐反馈只在已登录且有 request ID 时展示，提交中禁用重复点击。`not_interested` 和
`already_seen` 成功后从当前列表移除该视频，`reduce_author` 同时移除已加载的同作者视频；
失败显示可理解错误且不伪造成功。异步反馈会绑定发起时的用户和 Token，只有同一已认证推荐
会话仍然当前时才更新本地列表，因此登出、切换账号或重新登录不会修改替换账号的 Feed。

Web 为推荐场景独立保留内存快照。临时访问 Timeline、Following 或 Hot 后返回时，继续使用原
request/session、活动视频、签名 cursor、下一 refresh index 和已接受反馈的抑制集合，不重新请求
推荐首页。显式刷新才创建新的 request，并且 `recent_video_ids`、`current_video_id` 只从推荐
快照派生，不能读取刚离开的其他 Feed 场景。登录身份变化、FeedPage 卸载或快照无法同时保留
活动视频与分页尾部时，旧推荐快照失效并开始新的逻辑会话。
当推荐流为当前栏目时，左侧栏目旁显示刷新按钮；该按钮显式创建新的推荐 request，并以原推荐
快照的最近视频和活动视频构建下一 refresh context，其他 Feed 快照保持不变。

## 7. Context、反馈与评估

`POST /api/feed-queries` 在 `scene=recommend` 时接受 `context`。`request_id`、`session_id`
最长均为 64；`refresh_index` 为 0 到 1,000,000；`recent_video_ids` 最多 20 个正整数并按首次
出现去重；`current_video_id` 为非负整数。`network_class` 仅允许 `offline`、`slow_2g`、
`2g`、`3g`、`4g`、`5g`、`wifi`、`ethernet`、`unknown`；`viewport_class` 仅允许 `small`、
`medium`、`large`、`unknown`；播放能力最多 8 个，且仅允许 `mp4`、`dash`、
`media_source`、`media_capabilities`。枚举大小写、空白和连字符会归一化；超限、未知字段
或非法值返回 400，服务端不保存任意设备标识、Token 或 URL。

`POST /api/recommendation-feedback` 使用严格 JSON：

```json
{"video_id":1001,"request_id":"req-...","feedback_type":"not_interested"}
```

请求头 `Idempotency-Key` 必填且最长 128。`feedback_type` 为 `not_interested`、
`reduce_author` 或 `already_seen`；相同用户和键的同载荷重放返回首次结果，异载荷返回 409。
反馈不删除观看或互动事实：按视频或作者写入有过期时间的抑制，同时经 Outbox 投影为负向画像。

`recommendation_policy` 以 `(scene, version)` 唯一，保存 feature weights、各 Provider budget/
deadline、衰减、曝光窗口、打散、rollout、snapshot TTL、采样率、保留期和抑制时长。
曝光决策和排序都按同一用户、scene、request 选择的 active/default policy 使用其 `exposure_window_hours`，不会退回固定全局窗口。
`user_interest_profile` 与 `recommendation_applied_profile_event(user_id, source_event_id)` 保存并
幂等投影长期/近期向量、作者亲和和负向信号；`recommendation_behavior_event`、反馈、关注和 action 的
leased Outbox 提供可重试输入。`recommendation_request_log` 仅接受规范化的 `recommend` 场景，按 request ID 保存采样后的有界 context、
候选顺序、reasons、分量、策略和 degraded 标志；`recommendation_outcome` 用 request ID 关联
曝光、播放、进度、完播、跳过、点赞、收藏、关注和反馈，均不保存 Token、签名 URL 或高基数标签。
每个实际返回的推荐页（包括 Redis snapshot 后续页和降级 cursor 页）都会在 Feed 卡片 hydration 和当前可读性
过滤完成后，按最终响应的 video ID 追加服务端候选证据，按 `(user_id, request_id, video_id)` 唯一保存 policy version、排序位置、
served_at 和 expires_at；缺卡、不可读或其他未交付的 snapshot 成员绝不构成反馈或 outcome 归因依据。证据是交付与归因的安全边界：
只要最终页包含 Feed 卡片，证据持久化失败就计入监控并在 HTTP 成功响应前使该推荐页失败；请求日志和非安全指标仍是可选附属能力。它与采样请求日志、
客户端 `/api/video-view-events` 完全独立。重复交付同成员安全；每页只追加该次最终交付的新成员，
并沿用原请求的有界 expiry。超过投递宽限期的完整请求组在同一事务内替换，即使清理 Worker 尚未运行。证据保留至少策略 snapshot TTL（默认 5 分钟）和 2 分钟归因重试
窗口中的较大值；过期后额外保留 5 分钟有界投递宽限期，并由 Worker 按完整 `(user_id, request_id)`
请求组（而非候选行）分批清理。反馈可按其创建时间验证；观看 outcome 保留客户端发生时间 `occurred_at` 用于画像衰减和排序，
但必须按持久观看事实的服务端 `recorded_at` 验证严格区间
`served_at <= recorded_at < expires_at`。点赞、收藏和关注使用其服务端接收时间做同样验证；
宽限期只允许延迟投递查到证据而不会扩大可归因区间。伪造、时钟偏移的观看事件或 request ID
不能创造归因。
关注还必须验证推荐视频作者就是被关注者。耐久观看证据传播中的归因在事件发生后最多 2 分钟按退避重试；
窗口结束后无证据或作者不匹配的归因仅跳过 outcome，并计数监控，不回滚已经接受的事实或画像信号。
Worker 按每个已持久化 `recommend` policy（包括禁用和回滚版本）的精确版本和各自保留期分批
清理请求日志，并在全局批额中轮转 policy 起点，避免最新积压版本耗尽批额、较短 policy 提前删除其他版本的
评估记录，或让退役版本的日志永久滞留。
请求日志最大保存 500 个候选、每候选最多 8 个 reasons 和 8 个 score components；完整有效池的紧凑 JSON 负载上限为 1 MiB，
因此不会在正常最大池上静默截断排序前缀或解释。启用 Provider Quota Merge 的采样请求还保存最多
64 条封闭 `phase/provider/result/reason/count` 摘要；摘要不包含原始候选池、source score 或 Provider 错误正文。

## 8. Recall、排序与 Snapshot

Fresh、Hot、内容相似、已关注作者和会话延续 Provider 并发执行，各自受 policy budget 和
deadline 约束。服务实例还以 16 个全局 provider slots 限制忽略取消的下游调用；超时但未返回的调用持续占用 slot，
后续请求标记 capacity degraded 并使用有界冷启动回退，而不会无限累积 goroutine 或下游请求。相同视频合并并保留全部 reasons；单 Provider 超时或失败时保留健康结果并
标记 degraded，返回前重新验证 `published + public + media ready`。本地 hash n-gram embedding
是可替换模型接口的保底实现。

Policy 驱动的正常召回有两种兼容模式。未配置 Quota Merge 的策略以所选 Provider budget 总和作为统一
评分前的候选上限，预算总和必须不超过500；`recommend/v1` 和 `recommend/v2` 仍是五个 Provider 各100条，
JSON 中不出现新的配额字段，因此全部可读唯一候选都会按原路径进入统一特征加载和 Ranker。

可选 Quota Merge 策略必须同时提供 `pre_rank_pool_limit`、`recall_provider_order` 和
`recall_provider_reservations`。pool limit 为50–500，顺序必须精确覆盖所选 Provider，reservation 不得超过
各自 budget 且总和不得超过 pool；满足这些约束后 Provider budget 总和可以高于500。每个健康 Provider
先按自己的有限 source score、发布时间和 video ID 做稳定去重与 budget 截断；随后对完整有界唯一并集只做
一次可见性复检，再按显式 Provider 顺序完成 reservation rounds 和 deterministic round-robin fill。
同一视频始终只占一个全局槽位，但保留全部 reasons，并可同时满足多个 Provider 的 representation；可读候选
不足时记录 underfill，空出的容量回到公共 fill。最终至多500个唯一候选全部进入既有 Ranker，Provider 的
source score 不跨 Provider 比较，goroutine 完成顺序和 Go map 迭代不会改变候选池。

候选不会按响应页 `limit × 8` 派生上限，也不会在评分前按 `published_at` 全局排序截断。未带 Policy 的兼容
调用和 Repository Fallback 继续使用响应页派生的有界池。回滚只需停止选择带配额字段的策略，不需要数据库
Schema 或数据回滚。

视频发布 intake 当前只条件写入 `hash-ngram-v1`，成功后提交独立 embedding Kafka Group 的 Offset。
多模态向量、Hybrid Search、Similar Videos、Session Semantic Recall 和 ANN 均尚未实施。当前规划以
`add-multimodal-video-discovery` 为第一条多模态纵向 Change，只处理新公开视频与显式开发 Fixture，
使用 Exact Cosine，不要求历史 Backfill 或 HNSW。Provider 配额合并由
`add-recommendation-provider-quota-merge` 负责；两者完成后才接入 Session Semantic Recommendation。
长期画像、历史重建、HNSW、训练数据导出和权重学习均受
[推荐系统演进路线](../recommendation-roadmap.md)中的后置 Gate 控制。

观看行为 Worker 通过 `frux.recommendation.consume-view.v1` 在 raw fact 与 profile/outcome
handoff 同事务提交后才允许 Kafka offset commit；不等待 embedding 或后续画像投影。Worker
在 action Kafka active Group 之前初始化该 view Group，并等待其收到非空 Partition assignment
且 Supervisor 标记 healthy 后才继续 action startup；首次 assignment 有界超时或 fatal exit 会取消
Consumer 并使 Worker 启动失败。Source Group 通过 PostgreSQL durable marker 验证或初始化 committed
offsets。画像 Worker 用稳定事件 ID
消费 progress、complete、skip、like、favorite、follow 与反馈，
将画像物化在稳定时间点：先把累计分量衰减至下一个物化时间，再加入原始有界信号；乱序/延迟事件
确定性地衰减至当前物化时间。长期/近期向量、正负作者亲和和负向主题在排序读取时从最后物化时间
按可配置半衰期（默认长期 30 天、近期 24 小时）衰减，并以同一年龄因子保留陈旧画像置信度，避免
cosine 的缩放不变性保留陈旧偏好。没有实体画像时，首个投影先从有界耐久事实重建；仍待 profile
outbox 投递的 action、follow、feedback 不参加该重建，行为事实的源事件 ID 则在同一事务中标记，
避免稍后重放重复计数。
排序组合内容/会话相似、热度、
新鲜度、作者亲和、关注、负向和重复曝光分量，按 score、发布时间、video ID 稳定排序，再做
作者/内容打散；抑制过多时 `minimum_fallback_pool` 保留确定性冷启动回退。

首页将最多 500 个内部候选按 `(user, scene, request_id, policy_version)` 写入 Redis。未提供客户端 request ID 的
首页由服务端用密码学随机值生成新的有界会话 ID；首次重试可创建新会话。HMAC
cursor 绑定 snapshot ID、offset、过期时间、用户、scene 和 request ID；后续页只读取该排序，
并跳过新近不可见项。Redis 不可用时使用确定性 score cursor 并标记 degraded；该 cursor 也携带原始 request ID，
使后续页归属同一会话。过期、篡改或
绑定不匹配 cursor 返回 400。snapshot 同时保存首次操作的 degraded 标志和 provider 原因；首页重试、所有命中页和后续页均恢复该原始状态，
使响应与监控归因不把已降级会话伪装成健康会话。首页重试先按 `(user, scene, request_id)` 查找未过期 snapshot；
创建采用 Redis create-if-absent，竞争者读取并返回已经创建的同一排序，而不会覆盖或返回新的本地排序。

Redis 不可用或 snapshot 读写失败时，首页进入确定性 score cursor 降级路径。该首页在裁剪响应页前，
按同一 `(user, scene, request_id)` 记录完整的至多 500 条已排序候选及其 reasons/score components；
cursor 后续页绝不重复日志。同样的完整池记录规则适用于成功创建 snapshot。

## 9. 初始策略与回滚

API 和 Worker 的 `migration.AutoMigrate` 安全执行 `EnsureInitialPolicies`，只插入缺失版本，
绝不改写运营记录。`recommend/v1` 为 100%：五个 Provider 各 100 条，fresh/hot 150ms、
follow 200ms、similarity/session 250ms；半衰期 72h、曝光窗口 7d、snapshot TTL 300s、
稳定采样 10,000 PPM（1%）、保留 30d，`not_interested` 30d、`reduce_author` 14d、
`already_seen` 7d。v1 weights 为 content 0.70、session 0.25、hot 0.20、fresh 0.10、
author 0.15、follow 0.10、negative -0.75、exposure -0.40，作者上限为 10。
`recommend/v2` 仅稳定 hash cohort 的 5%，将 content/session/fresh 调整为
0.60/0.30/0.15、每作者上限降为 6，其余 v1 参数不变；不会覆盖已有同版本策略。

扩大 v2 前至少观察 24h：请求错误/降级率、snapshot hit、Provider timeout、profile lag、
曝光到播放/完播率和负反馈率不得劣于 v1 门槛。应用回滚调用
`PolicyService.Rollback(ctx, "recommend", 1)`；紧急 SQL 在事务中锁定 v1，关闭同 scene
其他 `enabled` 行，并将 v1 的 `enabled=true`、`config_json.rollout_percentage=100` 提交。
随后确认 `frux_recommendation_active_policy_version{scene="recommend"}` 为 1；保留日志、
Outbox 和事实以便调查。
