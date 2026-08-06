# 内部控制面、内容审核与稳定性治理实施路线

本文档是 Frux 审核后台、后台运营和系统治理能力的长期实施入口。切换会话或交接后，应先阅读本文并运行：

```bash
openspec list
```

各变更的准确范围、设计、规格和实现任务以 `openspec/changes/<change-name>/` 为准。本文只定义跨变更依赖、实施顺序、并行边界、上线门槛和回滚原则。

路线坚持以下原则：

- 后台运营是内部控制面的入口，不拥有审核、视频或治理领域数据。
- 权限和审计先于任何可改变生产状态的后台接口。
- 内容审核先建立视频状态机，再接机器审核、人审和 Web 工作台。
- 治理策略集中管理，但限流、降级和消息恢复在本地或既有基础设施中执行。
- 每个 OpenSpec change 单独实现、验证、合并和归档，不做九项能力的大爆炸发布。

## 1. 总体依赖关系

```text
共享控制面轨道

后台权限
    ↓
操作审计
    ├──────────────────────┐
    │                      │
    ▼                      ▼
人工审核与运营接口      稳定性治理接口


内容审核轨道

视频审核状态机
       ↓
机器审核接入
       ↓
人工审核工作流  ←── 后台权限 + 操作审计
       ↓
内容运营控制台


稳定性治理轨道

后台权限 + 操作审计
       ├───────────────┐
       ▼               ▼
运行时降级控制      RabbitMQ 死信恢复
       ↓
分层请求限流
```

推荐的严格单线程实施顺序：

1. `add-admin-authorization-foundation`
2. `add-admin-audit-trail`
3. `establish-video-review-lifecycle`
4. `add-automated-content-review`
5. `add-human-review-workflow`
6. `add-content-operations-console`
7. `add-runtime-degradation-controls`
8. `add-layered-request-rate-limits`
9. `add-rabbitmq-dead-letter-recovery`

该顺序优先形成完整内容治理闭环，再补稳定性治理。若允许多分支并行，应遵守第 6 节的并行边界。

## 2. 变更清单

| 顺序 | OpenSpec 变更 | 任务数 | 目标 | 前置变更 |
| --- | --- | ---: | --- | --- |
| 1 | `add-admin-authorization-foundation` | 14 | 建立当前账号驱动的细粒度后台权限 | 无 |
| 2 | `add-admin-audit-trail` | 14 | 建立不可变且与生产变更同事务提交的审计事实 | 后台权限 |
| 3 | `establish-video-review-lifecycle` | 18 | 建立待审、发布、拒绝、下架和恢复状态机 | 无 |
| 4 | `add-automated-content-review` | 17 | 建立审核案件、机器证据和策略路由 | 视频审核状态机 |
| 5 | `add-human-review-workflow` | 16 | 建立人审队列、租约、决定和原子处罚 | 权限、审计、机器审核 |
| 6 | `add-content-operations-console` | 18 | 提供审核与视频运营 Web 工作台 | 人审、权限、审计 |
| 7 | `add-runtime-degradation-controls` | 16 | 提供版本化降级开关和进程本地快照 | 权限、审计 |
| 8 | `add-layered-request-rate-limits` | 17 | 提供本地优先、Redis 协调的分层限流 | 降级控制 |
| 9 | `add-rabbitmq-dead-letter-recovery` | 18 | 提供有限重试、DLQ、检查和审计重放 | 权限、审计 |

## 3. 共享控制面轨道

### 3.1 后台权限

实施顺序：

```text
权限常量和角色映射
        ↓
读取当前账号状态和角色
        ↓
共享权限 Middleware
        ↓
建立 /api/admin 路由边界
```

关键约束：

- JWT 只证明身份，不能作为后台权限的最终事实源。
- 后台请求必须读取当前账号状态和角色，已停用或降权账号不能等待 Token 过期。
- 未知角色默认无权限。
- 第一阶段仅使用代码内封闭权限表，不增加通用策略语言或角色管理后台。
- 已有 `admin` 作为兼容 bootstrap 角色保留全部初始权限。

完成门槛：

- 普通用户、Reviewer、Operator 和兼容 Admin 的权限矩阵均有自动化覆盖。
- 每个 `/api/admin` 路由显式声明所需权限。
- 认证失败继续返回 401，权限不足稳定返回 403。

### 3.2 操作审计

实施顺序：

```text
不可变 Audit Fact
        ↓
PostgreSQL 追加写和稳定查询
        ↓
与领域变更共用事务
        ↓
审计查询 API
```

关键约束：

- 成功的生产状态变更与成功审计记录必须在同一事务提交。
- 审计模块只保存谁在何时对什么执行了什么操作，不接管业务状态。
- 禁止保存密码、Token、原始媒体、完整请求体和任意 Header。
- 审计记录无更新、删除或覆盖接口。
- 拒绝操作可做最佳努力审计，但审计失败不能伪装为原始业务成功。

完成门槛：

- 模拟审计插入失败时，受保护的业务变更整体回滚。
- 审计查询支持时间范围和稳定游标。
- 后续人审、下架、配置和重放能力可复用统一 Audit Fact。

## 4. 内容审核轨道

### 4.1 视频审核状态机

这是审核路线的第一个业务变更，必须先于审核任务表和审核后台。

```text
创建视频
   ↓
Pending Review ──批准──→ Published
      │                     │
      └────拒绝────→ Rejected
                            │
Published ──处罚──→ Offline ──合规恢复──→ Published
```

实施顺序：

1. 增加状态常量和领域转换，不改变现有状态数字含义。
2. 更新持久化约束和统计逻辑，但暂不改变新视频创建行为。
3. 审计 Feed、搜索、推荐、主页、合集、评论、预加载和媒体授权中的公开条件。
4. 将新视频创建切换为 `pending review`。
5. 更新 Web 状态展示和上传成功反馈。

关键约束：

- `visibility`、`media_status` 和审核生命周期继续相互独立。
- 公开视频必须同时满足已批准发布、公开可见和媒体基线就绪。
- 现有已发布视频视为历史已批准，不批量生成审核任务。
- `published_at` 在首次批准时设置，恢复下架内容不重写原发布时间。

完成门槛：

- Ready Media 不能绕过 Pending Review。
- Review Approved 不能绕过 Processing Media。
- 缓存中的 Pending、Rejected 或 Offline ID 无法泄露详情或媒体。
- 创作者可看到待审和拒绝作品，但它们不计入公开作品数。

### 4.2 机器审核接入

建议分四步启用，而不是直接允许自动下架：

```text
案件创建
   ↓
只记录机器证据
   ↓
全部路由到人审
   ↓
启用高置信自动通过
   ↓
最后启用高置信自动拒绝
```

实施顺序：

1. 建立 Review Case、Signal、Decision 和版本化策略模型。
2. 在媒体就绪后幂等创建审核案件，并增加缺失案件对账。
3. 接收内部鉴权的机器结果，只保存规范化标签、分数、证据引用和模型版本。
4. 先以影子方式计算路由，不改变视频状态。
5. 启用 `pending human` 路由。
6. 观测误判后再分别启用自动通过和自动拒绝。

关键约束：

- 每个 `(video_id, review_version)` 最多一个有效案件。
- 未知标签可保留为证据，但不能静默导致自动通过。
- 模型回调不能直接修改视频状态，必须经过版本化策略。
- Provider 失败时视频保持待审，不能以降级名义直接发布。

完成门槛：

- 重复媒体事件和机器结果不产生重复案件、证据或决定。
- 旧 Review Version 的结果不能影响新内容。
- 自动决定、案件状态和视频状态在同一事务提交。
- 审核延迟、结果分布、重复和失败均可观测。

### 4.3 人工审核工作流

实施顺序：

```text
稳定审核队列
      ↓
领取和租约
      ↓
批准/拒绝 API
      ↓
审计 + 视频状态 + 通知 Outbox 原子提交
```

关键约束：

- 人工查看时间不能占用数据库长事务，使用有过期时间的租约。
- 决定必须携带租约 Token、案件版本、理由码和幂等键。
- 两名审核员竞争同一案件时只有一人可以获得有效决定权。
- 通知异步发送，但通知 Outbox 必须与决定一起提交。
- 第一阶段不包含申诉、双人共识和审核员质量评分。

完成门槛：

- 并发领取、租约续期、过期、重复决定和旧版本决定均有测试。
- 审计写入失败时案件、视频和通知全部回滚。
- 案件详情能展示不可变的机器证据和历史决定。

### 4.4 内容运营控制台

Web 工作台最后实施，不能先于稳定的权限和审核 API。

实施顺序：

1. 扩展 typed Route、Session Permission 和 API 类型。
2. 增加懒加载的 Admin Shell，但暂不展示入口。
3. 实现审核队列和案件详情。
4. 实现视频搜索、下架和合规恢复。
5. 后端接口稳定后向相应权限用户开放导航。

关键约束：

- 继续使用现有 History API Router，不增加路由库。
- 客户端权限仅控制展示，后端始终重新鉴权。
- 审核和视频 API 分属各自领域模块，不能引入万能 Admin Repository。
- 下架和恢复需要理由、版本检查、确认和审计。

完成门槛：

- Loading、Empty、Error、Forbidden、Lease Expired 和 Version Conflict 状态均真实可见。
- Admin 页面通过懒加载避免无条件增大普通用户首屏 Bundle。
- 直接输入 Admin URL 不能读取未授权缓存数据。

## 5. 稳定性治理轨道

### 5.1 运行时降级控制

状态：已实现。当前以 `feed.preload.enabled` 接入兼容 preload API 和 Worker 非关键 cache
preheat；Admin API、不可变 revision、原子审计、本地 snapshot、陈旧 failure default、指标与
告警均已落地。后续新增 key 仍按本节约束逐项注册和验证。

实施顺序：

```text
代码注册的 Control Key
        ↓
不可变 Revision 和 Active Pointer
        ↓
只读本地 Snapshot
        ↓
后台写入和回滚 API
        ↓
接入一个非关键能力
```

推荐先接入一个容易验证的可选能力，例如非关键预加载或推荐增强，不应首先控制数据库写入、登录或内容正确性链路。

关键约束：

- 热路径只能读取原子本地快照，不同步请求 PostgreSQL、Redis 或治理 HTTP API。
- 未知 Key 不能通过后台动态创建。
- 每个 Key 明确定义正常默认值、故障默认值和最大陈旧时间。
- 控制面不可用时使用 Last Known Good；超过陈旧上限后回到注册的故障默认值。
- 更新和回滚必须使用 Expected Revision、理由和审计。

完成门槛：

- 新 Revision 能在预期轮询时间内传播到 API 和 Worker。
- 过期、读取失败、非法值和过度陈旧均有确定结果。
- Snapshot Age、Poll Failure 和 Applied Revision 可观测。

### 5.2 分层请求限流

实施顺序：

1. 抽象注册式 Rate Limit Policy 和本地 Token Bucket。
2. 先迁移已有 Playback Telemetry 限流，验证行为等价。
3. 增加可信代理 IP 和用户身份归一化。
4. 为一个非关键 Endpoint Group 接入 Redis 全局协调。
5. 再逐步覆盖上传、评论等高成本写接口。

请求路径：

```text
Request
   ↓
本地有界 Token Bucket
   ├─拒绝→ 429
   ↓
可选 Redis 原子配额
   ├─拒绝→ 429
   ├─失败→ 声明的本地降级或 Fail Closed
   ↓
Handler
```

关键约束：

- Redis 失败不能退化成无限流量。
- 本地 Entry Map 必须有容量上限和空闲回收。
- 浏览器不能提供任意配额 Descriptor。
- Prometheus 标签只使用固定 Endpoint Group、Layer 和 Result。
- 紧急配置只能选择预定义 Profile，不能动态注入任意速率。

完成门槛：

- Playback Telemetry 迁移前后有效配额一致。
- 多实例 Redis 配额、Redis 超时、本地降级和 Map 饱和均有测试。
- 429 返回稳定错误码和 Retry Metadata。

### 5.3 RabbitMQ 死信恢复

按消费者逐个迁移，不一次性修改所有 Queue。

实施顺序：

```text
消费者错误分类
      ↓
新版本 Quorum Queue + DLX/DLQ
      ↓
新旧 Queue 并行和幂等 Drain
      ↓
移除旧 Binding
      ↓
增加检查与单消息重放 API
```

建议迁移顺序：

1. 选择业务事实明确、幂等测试最完整的消费者作为试点。
2. 验证 Delivery Limit、Terminal Reject 和 Retryable Nack。
3. 再迁移互动、曝光、媒体和推荐投影等其余消费者。
4. 所有 Queue 稳定后再开放 Operator Replay。

关键约束：

- RabbitMQ Queue Type 不能原地切换，必须使用新 Queue Name。
- 重放保持原始 Event ID，新增 Replay ID 只用于审计和诊断。
- Replay 必须在新消息获得 Publisher Confirm 后才能 Ack DLQ 消息。
- PostgreSQL 只保存审计事实，不复制所有 DLQ Payload 成为第二套队列。
- 第一阶段只允许单消息重放，不提供 Payload 编辑和 Bulk Replay。

完成门槛：

- Poison Message 不再无限占用正常消费链路。
- DLQ 目标不可用时，关键 Quorum Queue 不静默丢消息。
- 新旧 Queue 切换期间重复投递不产生重复业务事实。
- DLQ Depth、Retry Exhaustion 和 Replay Failure 均有告警。

## 6. 并行实施建议

完成后台权限后，可以并行推进：

```text
轨道 A：add-admin-audit-trail

轨道 B：establish-video-review-lifecycle
```

视频审核状态机完成后，机器审核可以独立推进；审计完成后，稳定性轨道也可以开始：

```text
视频状态机 → 机器审核

权限 + 审计 ─┬→ 运行时降级控制 → 分层限流
             └→ RabbitMQ 死信恢复
```

人工审核必须等待：

- 后台权限完成；
- 操作审计完成；
- 机器审核能稳定生成 `pending human` 案件。

内容运营控制台必须等待人审 API 稳定，但可以与稳定性治理轨道并行。

并行开发注意事项：

- `router.go`、Migration 注册、`docs/product.md` 和监控配置是共享热点，多个分支不要同时进行大范围重排。
- 每个 change 使用独立分支和 PR；合并前基于最新主分支重新运行定向测试。
- 后置 change 不得提前复制前置 change 的类型或临时实现，应等待正式接口合入。

## 7. 集成检查点

### 检查点 A：控制面可承载生产操作

- 当前账号降权和停用立即影响后台权限。
- 成功生产变更不能缺少审计事实。
- 普通用户和内部服务 Token 不能访问 Admin API。

### 检查点 B：内容默认不可绕过审核

- 新视频默认待审。
- 所有公开读取和媒体授权统一检查审核状态。
- 历史已发布内容保持兼容。

### 检查点 C：审核后端形成闭环

- 机器证据可追溯到 Provider、Model 和 Policy Version。
- 灰区内容能稳定进入人审。
- 人工决定能原子修改案件、视频、审计和通知 Outbox。

### 检查点 D：运营工作台可用

- Reviewer 和 Operator 只看到各自权限范围。
- 并发和过期冲突不会被 UI 伪装为成功。
- 视频处罚和恢复均有理由与审计。

### 检查点 E：稳定性治理可控

- 降级开关不在请求热路径访问控制面。
- 限流失败模式不会产生无限流量。
- Poison Message 被隔离，重放需要显式权限和审计。

## 8. 发布和回滚原则

- 每个 change 独立迁移、发布和回滚，不等待整条路线完成。
- 数据结构先以兼容方式发布，再切换写入行为，最后开放操作入口。
- 高风险自动化按“记录结果 → 影子决策 → 人工路由 → 自动通过 → 自动拒绝”逐级启用。
- Web 入口最后开放；隐藏导航不能代替后端权限。
- Queue、状态或策略迁移都保留旧读取或旧 Binding，直到新路径指标稳定。
- 回滚优先停止新写入和新入口，不删除已产生的审核、审计、Revision 或 DLQ 事实。

## 9. 实施命令

开始单个变更前：

```bash
openspec status --change <change-name>
```

在 Copilot CLI 中使用正常语言提示，例如：

```text
Use the /openspec-apply-change skill to implement add-admin-authorization-foundation.
```

每个变更完成后：

```bash
openspec validate --all --strict
```

确认实现、任务和文档全部完成后，再使用 `openspec-archive-change` skill 归档对应 change。不要在同一次归档中合并多个尚未独立验收的变更。
