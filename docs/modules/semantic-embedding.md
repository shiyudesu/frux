# 语义向量服务

## 1. 边界

`apps/semantic-embedding` 是独立 Python 3.12 CPU 服务，只提供固定文本向量推理。它不访问
PostgreSQL、Redis、RabbitMQ、Kafka 或浏览器，不保存请求历史，也不拥有推荐、回填或 ANN 逻辑。

一个 Uvicorn coordinator 负责认证、校验、排队和 deadline；原生 PyTorch 推理只在最多两个隔离子
进程执行。启动 fixture self-check 也在可终止进程内完成。请求超过 deadline、ASGI `http.disconnect` 或其他取消时，服务终止
对应推理进程、释放 admission，并预加载 replacement。挂起 native kernel 不会继续占用 slot；
replacement preload 失败按 100ms、500ms、1s、2s、最多 5s 的有界退避持续重试。pool 暴露 live
capacity，`/health/ready` 仅在配置要求的全部 worker 都存活时返回 200；全 worker 丢失时返回 503，
replacement 恢复后自动回到 ready。shutdown 会停止退避并回收 live/starting 子进程，子进程
stdout/stderr 也不会进入服务日志。

## 2. 固定模型

- 模型：`sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2`
- revision：`e8f8c211226b894fcb81acc59f3b34ba3efd5f42`
- 输出：384 维、`float32`、finite、L2 normalized、CPU
- 最大序列：128 tokens；内部固定顺序 chunk 为 8

镜像构建时下载不可变 revision；运行时启用 Hugging Face/Transformers offline，启动必须完成
metadata 和中英 fixture 全向量自检。一个 180 秒 monotonic 外层 deadline 同时覆盖 preload、
fixture validation 和完整 inference pool 初始化；每个 worker 只消费剩余预算，不独立重置 180 秒。
生产 model 与 fixture 路径固定在镜像契约内；`FRUX_EMBEDDING_MODEL_PATH` 和
`FRUX_EMBEDDING_FIXTURE_PATH` 会被视为未知配置。测试只能显式注入 `Settings` 或依赖。

## 3. HTTP 契约

服务只暴露 `GET /health/live`、`GET /health/ready`、受
`X-Internal-Token` 保护的 `GET /internal/v1/model` 与
`POST /internal/v1/embeddings`。默认 Compose 仅 `expose: 8081`，没有宿主机端口。
配置 token 必须是可打印 ASCII；请求头在进入 `compare_digest` 前执行相同检查，非 ASCII 值固定
返回有界 `401 AUTH_INVALID_INTERNAL_TOKEN`，不会触发 `TypeError` 或 500。

15 秒总 deadline 从 ASGI 请求进入开始，覆盖 body receive/parsing、鉴权、capacity、推理和
response send；慢上传会收到有界 504，阻塞 send 会被取消且不会无限占用请求 task。
请求每批 1–32 项，body 最大 131072 bytes。`id` 为 1–128 字符受限 ASCII；title/description
分别 NFKC、折叠 Unicode 空白并限制为 1–200/0–2000 code points，总内容不超过 16384。
错误只返回稳定 `code/error`，不回显 token、文本、ID、向量、路径或异常。

每个 HTTP 请求只记录封闭 `route`、数字 `status`、`duration_ms`、封闭 `result` 和 live
`capacity`。success、validation、auth、overload、timeout、canceled、unavailable、internal 都不记录
header/body/text/ID/vector/token/raw path/URL/raw error。Uvicorn access log 保持关闭；bind/start
产生的 `SystemExit` 只输出有界 startup category，不泄露原始 OS 错误、地址或 traceback。

## 4. 容量与部署

默认 1 个 HTTP coordinator、2 个隔离推理进程/slot、8 个 waiter、2 秒排队、15 秒总 deadline、
2 CPU、2 GiB。
容器以 UID/GID 10001 运行，只允许 64 MiB `/run/frux-tmp` tmpfs 写入，root/model filesystem
只读并 drop all capabilities。

Compose 默认启用 semantic generation，Worker 仅以 `condition: service_started` 依赖服务。服务启动
后失效时，Kafka embedding intake 仍先持久化 `hash-ngram-v1` 和 pending semantic job；不健康副本
停止 claim，metadata 恢复后打开本地 gate，不改写其他副本可见的共享状态。
