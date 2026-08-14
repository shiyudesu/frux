# 分层请求限流设计

## 1. 策略注册

限流策略只在 `internal/application/ratelimit` 以 typed registry 注册，路由只能引用注册名。
未知策略会使 Router 启动失败，客户端不能提交策略、identity 或任意限额表达式。当前组为：

| endpoint group | identity | distributed | failure mode |
| --- | --- | --- | --- |
| `playback_telemetry` | server-derived user ID | local only | local |
| `public_search` | trusted-proxy normalized IP | Redis | stricter local fallback |
| `upload_session` | server-derived user ID | Redis | fail closed |
| `consumer_login` | trusted-proxy normalized IP | Redis | stricter local fallback |
| `session_refresh` | trusted-proxy normalized IP | Redis | stricter local fallback |
| `password_change` | server-derived user ID | Redis | fail closed |

策略固定声明 normal/emergency 的 local、distributed、fallback 配额算法，Redis deadline
和最小 retry metadata。除兼容旧行为的 `playback_telemetry` 固定 60 秒窗口外，其余策略使用
token bucket；`password_change` 使用 15 分钟固定窗口限制 bcrypt 尝试。配置只控制有界 entry capacity、idle TTL、Redis timeout 和 trusted proxy
CIDR；所有值在启动时校验。

## 2. 请求路径

每个受保护请求先经过有界进程内 limiter。entry 使用最小堆索引 idle expiry；map 达到容量时，
一次 miss 最多回收 admission 所需的过期堆顶，不在全局锁内扫描整个 map；仍满则保守拒绝新
identity，不增长内存。local allow 后，仅 distributed policy 执行一次 Redis Lua
token-bucket 操作；脚本原子读取、refill、消费并设置过期时间。

Redis 限流使用独立 go-redis client，开启 `ContextTimeoutEnabled` 并关闭 command retry。
因此 policy deadline 会终止等待，且单个请求的 mutating Lua 最多执行一次；错误进入声明的
fallback/fail-closed 路径。

Redis 错误或 deadline 到期时，`local` policy 使用独立且更严格的 fallback bucket；
`fail_closed` policy 返回稳定 503。任何路径都不会因 Redis 故障变成无限流量。

普通登录和 Refresh 在认证建立前按可信代理归一化 IP 限流；改密先经过 JWT middleware，再以服务端
user ID 限流，客户端不能通过 body、Cookie session ID 或账号字符串选择 quota identity。

## 3. 身份与响应

user policy 只读取 JWT middleware 写入的 `auth_user_id`。IP policy 先读取 socket peer；
只有 peer 命中 `rate_limit.trusted_proxies` 时才解析 `X-Forwarded-For`/`X-Real-IP`，并从代理链
右侧剥离可信代理。直接客户端提交的 forwarding header 被忽略。
Compose 为 Web nginx 分配固定内部地址 `172.31.250.10`，Docker 配置只信任该 `/32`；
直接访问 API 端口的调用方不能借此伪造 forwarding header。

超限返回 429、`RATE_LIMITED`、`Retry-After`、`retry_after_seconds` 及不含 identity 的
`RateLimit-Policy/Limit/Remaining`。Redis fail-closed 返回 503 `RATE_LIMIT_UNAVAILABLE`。

## 4. 紧急控制与观测

治理注册表只提供 `rate_limit.distributed.enabled` 和 `rate_limit.emergency.enabled`。
前者关闭 Redis 协调后仍执行声明的 fallback；后者只能选择代码预声明 emergency profile，
不能注入任意速率。

`frux_rate_limit_decisions_total{endpoint_group,layer,result}` 的标签均来自封闭集合。
Prometheus 告警和 `Frux Layered Rate Limits` Grafana 看板覆盖 rejection spike、Redis
fallback/backend error 和 local saturation。
