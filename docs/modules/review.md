# 自动内容审核模块

## 1. 模块职责

审核模块在视频公开前创建版本化案件，接收服务间认证的模型无关机器证据，并按不可变策略版本把案件确定性路由为自动通过、自动拒绝或待人工复审。模型推理、抽帧、OCR/ASR、供应商选择、人审分配和申诉不属于本模块。

## 2. 内部接口

| 方法 | 路径 | 作用 | 鉴权 |
| --- | --- | --- | --- |
| PUT | `/internal/review/cases/{caseId}/machine-results/{resultId}` | 幂等写入机器证据并应用策略决定 | `X-Internal-Token` |

请求严格拒绝未知字段、尾随 JSON 和超过 32 KiB 的请求体。正文包含 `video_id`、正整数 `review_version`、`provider`、`model_version`、`policy_version` 和最多 32 条 `signals`。每条 signal 包含规范化 label、`0..1` confidence，以及最多 8 个、总计不超过 2048 字节的证据引用。

## 3. 数据模型

- `review_case`：以 `(video_id, review_version)` 唯一，保存 `open`、`pending_human`、`approved`、`rejected` 和案件策略版本。
- `review_machine_result`：以 `(provider, result_id)` 唯一，保存规范化载荷 SHA-256；同身份异载荷冲突。
- `review_signal`：不可变保存 label、confidence、有界证据引用、provider、model 和 policy provenance。
- `review_decision`：每个机器结果最多一个自动决定，保存 outcome 和 policy version。
- `review_policy`：配置使用 JSONB，但读取必须恢复成经过 Domain 校验的 typed policy；版本唯一，数据库只允许一个 active 版本。

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
