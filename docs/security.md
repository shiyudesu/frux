# 媒体安全

## 资产边界

- `media_asset.owner_id` 是认证上传者，创建后不可转移。
- 上传会话绑定 owner、kind、对象键、大小、Content-Type、SHA-256 和过期时间；完成接口重新 HEAD 校验。
- 对象键由服务端生成，客户端不能选择 bucket 或跨 owner 前缀。
- 同一 `Idempotency-Key` 只能重放相同规范化上传意图。

## 访问策略

| 资产 | 访问方式 | 缓存 |
| --- | --- | --- |
| 已就绪公开 MP4/segment/cover | v3 虚拟 URL 校验后签名 protected 对象 | 307 为 25 分钟，媒体响应为 30 分钟并 `must-revalidate` |
| DASH manifest | v3 虚拟 URL 校验后由 Frux 返回 | 30 分钟并 `must-revalidate` |
| 原始上传、处理中资产 | owner 获取短期签名 URL | `private, no-store` |
| 私密作品 | API 不返回公共播放源；owner 按当前引用状态获取签名访问 | `private, no-store` |
| 删除作品 | 立即移除 API 发现并拒绝 owner 访问，物理对象延迟清理 | 不新增缓存 |

生产对象存储 bucket 始终保持私有，公开播放也只使用短期签名 GET。签名 URL 不包含 JWT、Cookie 或
长期凭据。下架立即拒绝新签名，但已缓存 307 或已签发 URL 最多可继续使用 30 分钟。

## 处理与清理

- Worker 对输出计算 SHA-256，直接写确定性的 `processed/*` 最终键并 HEAD 校验；匹配对象复用，冲突
  不覆盖。公开状态由 PostgreSQL `public/exposure_generation` 控制，不复制或移动正文。
- 处理任务按资产和 profile 版本幂等，租约过期后可重试。
- 删除请求只写清理任务，不同步批量删除对象。
- Reconciler 检查缺失源、缺失变体、过期租约和孤儿对象。

## 播放遥测隐私

- 遥测请求使用严格 JSON schema，只允许版本、稳定事件 ID、技术事件和低基数环境维度。
- 不接受完整媒体 URL、签名参数、JWT、Cookie、标题、描述或任意 metadata map；CDN 只保存校验后的 hostname。
- PostgreSQL 可以保存认证用户 ID、视频 ID、request ID 和播放会话用于受控诊断，但 Prometheus/Grafana 标签禁止包含这些标识符。
- browser、OS、network、viewport、codec、quality、source 和 scene 都折叠到固定枚举；未知值归入 `unknown`/`other`。
- 原始遥测默认保留 168 小时并由有界清理任务删除；行为历史和推荐事实使用独立保留策略。
