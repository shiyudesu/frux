# 模块文档索引

本文是业务模块设计入口。产品范围和功能优先级见 [../product.md](../product.md)，工程实现规则见 [../engineering.md](../engineering.md)。

## 模块列表

| 领域 | 模块 | 文档 | 状态 |
| --- | --- | --- | --- |
| 用户 | 账户 | [account.md](account.md) | 已实现 |
| 用户 | 关系 | [relation.md](relation.md) | 已实现 |
| 内容 | 视频 | [video.md](video.md) | 已实现 |
| 内容 | 媒体任务 | [media.md](media.md) | 已实现 |
| 内容 | 视频向量 | [embedding.md](embedding.md) | 已实现 |
| 内容 | 互动 | [interaction.md](interaction.md) | 已实现 |
| 内容 | 个人内容库 | [library.md](library.md) | 已实现 |
| 发现 | 全局搜索 | [search.md](search.md) | 已实现 |
| 分发 | Feed | [feed.md](feed.md) | 已实现 |
| 分发 | 曝光与观看历史 | [exposure.md](exposure.md) | 已实现 |
| 分发 | 推荐 | [recommendation.md](recommendation.md) | 已实现 |
| 治理 | 审核 | [review.md](review.md) | 规划中 |
| 治理 | 后台权限与运营入口 | [admin.md](admin.md) | 部分实现 |
| 治理 | 后台操作审计 | [admin-audit.md](admin-audit.md) | 已实现 |
| 体验 | 消息通知 | [message.md](message.md) | 已实现 |
| 体验 | 私信聊天 | [chat.md](chat.md) | 已实现 |
| 体验 | 播放优化 | [playback.md](playback.md) | 已实现 |
| 稳定性 | 系统治理 | [governance.md](governance.md) | 部分实现（运行时降级控制和分层限流已实现） |
| 稳定性 | 分层请求限流 | [rate-limiting.md](rate-limiting.md) | 已实现 |
| 稳定性 | Kafka 故障恢复 | [kafka-failure-recovery.md](kafka-failure-recovery.md) | 已实现 |
| 稳定性 | 监控告警 | [monitoring.md](monitoring.md) | 部分实现（Prometheus、告警和 Grafana 已实现） |

## 模块文档模板

每个模块保持相同结构：

1. 模块职责。
2. 接口设计。
3. 数据表设计。
4. 业务规则。
5. 测试建议。
6. 前端接入点。

已实现模块应补齐业务规则和测试建议。规划中模块先定义最小闭环能力，后续通过 OpenSpec change 细化实现方案。
