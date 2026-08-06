# 系统治理模块设计

## 1. 模块职责

系统治理模块提供代码注册、版本化、可审计的运行时降级控制。控制面保存在 PostgreSQL；
API 与 Worker 只在后台轮询并原子替换本地快照，业务热路径不访问 PostgreSQL、Redis 或治理
HTTP API。

限流和死信恢复仍属于后续能力，本模块当前只实现降级控制。

## 2. 注册控制

控制键只能在 `domain/governance` 注册。每个定义固定声明：

- owner、说明和 value type；
- normal default 与 control-plane failure default；
- 允许读取的 `api` / `worker` 进程；
- last-known-good 最大陈旧时间。

当前注册 `feed.preload.enabled`，类型为 boolean，API 与 Worker 都可用；normal default 为
`true`，failure default 为 `false`，最大陈旧时间为 2 分钟。它只关闭兼容
`/api/preload-videos` 返回和 Worker Feed cache preheat，不改变发布 fanout、Feed 正确性或耐久
业务事实。

未知键不能经 API 创建；进程读取不支持的键时使用该键的 failure default 并记录有界指标。

## 3. 数据与并发

`governance_control_revision` 保存不可变 revision：`control_key + revision`、typed value、
reason、可选 expiry、actor、创建时间和可选 rollback source。

`governance_control_active` 每个键只保存一个 active revision pointer。更新和回滚都要求
`expected_revision`；事务先取得按 key 的 PostgreSQL advisory lock，再校验 active pointer，
创建 `expected + 1` revision、切换 pointer，并追加 `governance.execute` 成功审计。任一步失败
都整体回滚。回滚复制较早且尚未过期 revision 的 typed value，生成新 revision，不修改历史。

## 4. 管理接口

全部接口要求当前账号具有 `governance.execute`：

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| GET | `/api/admin/governance/controls` | 查询注册定义和 active revision |
| GET | `/api/admin/governance/controls/{key}/revisions` | 查询不可变 revision 历史，默认 20、最大 100 |
| PATCH | `/api/admin/governance/controls/{key}` | 使用 expected revision 更新 typed value、reason 和可选 expiry |
| POST | `/api/admin/governance/controls/{key}/rollback` | 使用 expected revision 选择较早有效 revision |

并发冲突返回 409；未知 key、非法 reason/expiry/revision 返回 400；目标 revision 缺失返回
404；控制面或原子审计提交失败返回 503。

## 5. 本地快照与失败语义

API 和 Worker 默认每 5 秒轮询，单次最多 2 秒。有效 snapshot 完整验证后通过
`atomic.Pointer` 一次替换；轮询失败或非法 snapshot 不覆盖 last-known-good。

求值顺序：

1. 未成功加载、进程不支持或 snapshot 超过 key 的最大陈旧时间：failure default；
2. key 没有 active revision：normal default；
3. active revision 已过期：normal default；
4. 其余情况：active typed value。

进程关闭会取消 poller；热路径只进行注册表查询和原子内存读取。

## 6. 观测与处置

暴露：

- `frux_governance_active_revision{process,key}`
- `frux_governance_poll_total{process,result}`
- `frux_governance_snapshot_age_seconds{process,key}`
- `frux_governance_invalid_controls_total{process,reason}`
- `frux_governance_evaluation_fallback_total{process,key,reason}`

标签仅来自封闭 process、key、result 和 reason 集合。告警位于
`apps/monitoring/alerts/governance.yml`，覆盖 snapshot 超过 2 分钟和 5 分钟内重复 poll
failure。

处置时先确认 API/Worker applied revision 与 active pointer 一致，再检查 PostgreSQL 连接和
poll failure。紧急回滚仍通过 Admin API 生成新 revision；不要更新或删除历史行。
