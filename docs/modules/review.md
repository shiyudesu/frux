# 自动内容审核模块

## 1. 模块职责

审核模块在视频公开前创建版本化案件，接收服务间认证的模型无关机器证据，并按不可变策略版本把案件确定性路由为自动通过、自动拒绝或待人工复审。人工复审提供稳定优先级队列、租约领取、决定和不可变历史；模型推理、抽帧、OCR/ASR、供应商选择、申诉和多人共识不属于本模块。

## 2. 内部接口

| 方法 | 路径 | 作用 | 鉴权 |
| --- | --- | --- | --- |
| PUT | `/internal/review/cases/{caseId}/machine-results/{resultId}` | 幂等写入机器证据并应用策略决定 | `X-Internal-Token` |
| GET | `/api/admin/review/cases` | 按优先级和年龄读取可领取案件 | `review.read` |
| GET | `/api/admin/review/cases/{caseId}` | 读取主体、机器证据和完整决定历史 | `review.read` |
| POST | `/api/admin/review/cases/{caseId}/claim` | 按预期 case version 领取案件 | `review.decide` |
| POST | `/api/admin/review/cases/{caseId}/lease/renew` | 当前持有人续租 | `review.decide` |
| DELETE | `/api/admin/review/cases/{caseId}/lease` | 当前持有人释放租约 | `review.decide` |
| POST | `/api/admin/review/cases/{caseId}/decision` | 幂等批准或拒绝 | `review.decide` |

请求严格拒绝未知字段、尾随 JSON 和超过 32 KiB 的请求体。正文包含 `video_id`、正整数 `review_version`、`provider`、`model_version`、`policy_version` 和最多 32 条 `signals`。每条 signal 包含规范化 label、`0..1` confidence，以及最多 8 个、总计不超过 2048 字节的证据引用。

## 3. 数据模型

- `review_case`：以 `(video_id, review_version)` 唯一，保存 `open`、`pending_human`、`approved`、`rejected`、`cancelled`、`superseded` 和案件策略版本。
- `review_machine_result`：以 `(provider, result_id)` 唯一，保存规范化载荷 SHA-256；同身份异载荷冲突。
- `review_signal`：不可变保存 label、confidence、有界证据引用、provider、model 和 policy provenance。
- `review_decision`：每个机器结果最多一个自动决定，保存 outcome 和 policy version。
- `review_policy`：配置使用 JSONB，但读取必须恢复成经过 Domain 校验的 typed policy；版本唯一，数据库只允许一个 active 版本。
- `review_assignment_history`：不可变保存 claimed、renewed、released、expired、decided、cancelled、superseded 事件。
- `review_human_decision`：每案最多一个人工决定，保存 reviewer、outcome、注册 reason、bounded note 和版本。
- `review_human_decision_idempotency`：按 case、reviewer 和幂等键摘要绑定规范化 payload。
- `review_notification_outbox`：与决定同事务写入的作者通知事实，由 Worker 租约投递到 message。

视频持有正整数 `review_version`，初始值为 1。历史证据不随后续版本覆盖。

## 4. 分类与路由

注册分类为 `sexual_content`、`graphic_violence`、`hate`、`harassment`、`self_harm`、`illegal_activity`、`spam` 和 `safe`。供应商新 label 可作为证据保留，但不能静默批准。

路由优先级固定为：

1. 任一注册 label 达到 reject threshold -> `reject`。
2. 否则任一 label 达到 human threshold，或出现策略未注册/未知 label -> `human`。
3. 否则使用策略的 typed default outcome。

初始 v1 策略是保守的 active human-routing 策略；启动只按版本 conflict-do-nothing 插入，绝不覆盖运营状态。

## 5. 事务与恢复

- 媒体基线 ready 且视频仍为 pending-review 时创建案件；重复 ready 事件返回同一案件。
- Worker 定期扫描 ready pending 视频与缺失案件，补建遗漏 intake。
- 结果事务按稳定结果身份串行，锁定案件和视频行，再写 result、signals、decision、case 和视频生命周期。
- 自动通过把视频转换为 published，自动拒绝转换为 rejected；决定、案件和视频必须同事务提交。
- human 只把案件置为 `pending_human`，视频继续 pending-review。
- 提交后再执行媒体提升/保护和发布事件；失败可通过同一结果重放重试，不重复证据或决定。

## 6. 可观测性

`frux_review_events_total{stage,result}` 只使用固定低基数 stage/result。stage 为 `intake`、`provider_result`、`routing`、`reconciliation`；result 折叠为 created/existing/accepted/approve/reject/human/duplicate/invalid/conflict/retry/success/unknown，不包含 provider、model、video、case 或 result ID。

## 7. 人工复审规则

- 可领取队列固定按 `priority DESC, created_at ASC, id ASC` 排序；HMAC 游标绑定 priority 过滤和完整排序元组。待人审优先级按触发人审的最高 signal confidence 确定为 `1..100`，默认 human 路由为 `1`，并与路由状态原子持久化。
- 迁移会把历史 `pending_human` 案件的零优先级回填为最低有效优先级 `1`，避免被后续新案件长期饿死。
- 队列查询直接把 `lease_expires_at <= clock_timestamp()` 视为可领取，不依赖固定批量的过期租约回收；重新领取时仍原子记录 expired 和 claimed 历史。
- 队列及其统计只包含视频仍为 pending-review 且 `video.review_version = case.review_version` 的案件。claim 同时锁定案件和视频；视频已终态时把案件置为 `cancelled`，版本已推进时置为 `superseded`，只写一次对应历史并返回既有冲突，不创建租约。
- claim、renew、release 和 decision 都校验 case version。租约只保存 256-bit opaque token 的 SHA-256，所有有效期判断使用 PostgreSQL `clock_timestamp()`；过期领取会保留 expired 历史。
- 决定仅允许 approve/reject。批准 reason 为 `content_compliant`、`false_positive`；拒绝 reason 为注册审核分类或 `other_policy_violation`，后者必须填写最多 1000 Unicode 字符的 note。
- decision 要求当前 reviewer、未过期 token、匹配 case/review version 和必填 `Idempotency-Key`。同键同规范化 payload 返回原结果；异 payload 返回稳定 409。
- 人工决定、案件关闭、视频生命周期、内容统计、成功审计事实、通知 Outbox 和幂等回执在同一事务提交；审计插入失败全部回滚。
- 决定提交后媒体提升/保护继续使用既有幂等 publication adapter；幂等重放仅在当前 video review version 仍匹配案件时重试这些副作用。通知失败只重试 Outbox，不回滚审核事实。

人工指标为 `frux_human_review_queue_available`、`frux_human_review_queue_oldest_age_seconds`、
`frux_human_review_operations_total{operation,result}` 和
`frux_human_review_notifications_total{result}`；标签不包含 reviewer、case、video、reason 或 token。

## 8. Web 审核工作台

- `/admin/reviews` 独立维护首屏加载、刷新、分页、空、错误和服务端 403 状态。
- 队列收到服务端 403 时清空缓存案件、cursor 和 `has_more`，禁止在 forbidden 状态继续渲染旧表格。
- `/admin/reviews/{reviewId}` 展示审核主体、机器 signal、自动决定、租约历史和人工决定不可变历史。
- Reviewer 领取后只在内存保存 opaque lease token；领取、续租和决定始终携带最新 case version。
- 同一 case/version 与决定 payload 在成功前复用同一 Web 幂等键，响应丢失后的重试不会创建第二个决定；payload 或 case 变化后生成新键。
- 租约过期时保留已检查证据，禁用旧决定并提供返回队列；case/video 版本冲突提供显式刷新。
- 只有 `review.decide` 才渲染领取和决定控件，但每个写接口仍由服务端权限中间件强制授权。
