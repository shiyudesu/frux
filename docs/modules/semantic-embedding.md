# 语义向量服务

## 1. 边界

`apps/semantic-embedding` 是独立 Python 3.12 CPU 服务，只提供固定文本向量推理。它不访问
PostgreSQL、Redis、RabbitMQ、Kafka 或浏览器，不保存请求历史，也不拥有推荐、回填或 ANN 逻辑。

## 2. 固定模型

- 模型：`sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2`
- revision：`e8f8c211226b894fcb81acc59f3b34ba3efd5f42`
- 输出：384 维、`float32`、finite、L2 normalized、CPU
- 最大序列：128 tokens；内部固定顺序 chunk 为 8

镜像构建时下载不可变 revision；运行时启用 Hugging Face/Transformers offline，启动必须完成
metadata 和中英 fixture 全向量自检。

## 3. HTTP 契约

服务只暴露 `GET /health/live`、`GET /health/ready`、受
`X-Internal-Token` 保护的 `GET /internal/v1/model` 与
`POST /internal/v1/embeddings`。默认 Compose 仅 `expose: 8081`，没有宿主机端口。

请求每批 1–32 项，body 最大 131072 bytes。`id` 为 1–128 字符受限 ASCII；title/description
分别 NFKC、折叠 Unicode 空白并限制为 1–200/0–2000 code points，总内容不超过 16384。
错误只返回稳定 `code/error`，不回显 token、文本、ID、向量、路径或异常。

## 4. 容量与部署

默认单进程、2 个真实推理 slot、8 个 waiter、2 秒排队、15 秒总 deadline、2 CPU、2 GiB。
容器以 UID/GID 10001 运行，只允许 64 MiB `/run/frux-tmp` tmpfs 写入，root/model filesystem
只读并 drop all capabilities。

Worker 仅以 `condition: service_started` 依赖服务。服务启动后失效时，Kafka embedding intake
仍先持久化 `hash-ngram-v1` 和 semantic job；语义 processor 暂停并在 metadata/resume 成功后恢复。
