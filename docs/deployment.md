# 部署与媒体交付

Frux 有两套现成的 Docker Compose：

| 环境 | Compose | 对象存储 | 用途 |
| --- | --- | --- | --- |
| 本地开发 | `apps/docker-compose.yml` | MinIO | 开发、测试、调试 |
| NAT 主机 Prod | `apps/docker-compose.prod.yml` | 私有自托管 MinIO | 个人演示、低流量试运行 |

Prod 是一台 NAT 主机上的新部署。PostgreSQL、Redis、Kafka 和 MinIO 都只有一个实例，不具备高可用，
也没有生产级 Kafka TLS、认证和复制。需要严格生产架构时，应迁移到多故障域数据库、消息系统和对象存储。

实际部署命令见：

- [Prod 部署](operations/prod.md)
- [自托管 MinIO](operations/self-hosted-minio.md)
- [雨云对象存储（旧部署）](operations/rainyun-object-storage.md)

## 本地环境

本地 Compose 会启动 PostgreSQL、Redis、单节点 Kafka、MinIO、API、Worker、Web、Prometheus 和
Grafana。

```bash
export FRUX_INTERNAL_TOKEN="$(openssl rand -base64 48 | tr -d '\n')"
cd apps
docker compose up -d --build
```

常用地址：

| 服务 | 地址 |
| --- | --- |
| Web | `http://127.0.0.1:5173` |
| API健康检查 | `http://127.0.0.1:8080/health` |
| MinIO | `http://127.0.0.1:9000` |
| MinIO控制台 | `http://127.0.0.1:9001` |
| Grafana | `http://127.0.0.1:3000` |

本地数据保存在Docker Volume中。下面的命令会把它们全部删除：

```bash
docker compose down -v
```

## NAT 主机 Prod 结构

Prod 支持两种公开入口。默认且推荐的是域名 + Caddy HTTPS；`direct-http` 仅用于个人、预生产或
连通性验证，使用一个公网 IPv4 和两个不同端口，不提供传输加密。

### 默认：域名与 Caddy HTTPS

```text
公网分配的 HTTPS 高端口/tcp ──NAT──> 主机 443/tcp
公网分配的 SSH 高端口/tcp   ──NAT──> 主机 22/tcp

https://FRUX_DOMAIN:<public-port>
    └─ 主机 systemd Caddy :443
       ├─ /api/*、/uploads/*、/media/*、/health → 127.0.0.1:18081
       └─ 其他路径                              → 127.0.0.1:18080

https://FRUX_S3_DOMAIN:<public-port>
    └─ 主机 systemd Caddy :443 → 127.0.0.1:19000
                                  └─ MinIO S3 API

SSH Tunnel → 127.0.0.1:19001 → MinIO Console

Docker Compose
    ├─ Web
    ├─ API
    ├─ Worker
    ├─ PostgreSQL
    ├─ Redis
    ├─ 单节点 Kafka
    ├─ MinIO + minio-init
    └─ PostgreSQL backup

API / Worker → http://minio:9000 → 私有 FRUX_S3_BUCKET
```

`FRUX_DOMAIN` 和 `FRUX_S3_DOMAIN` 是两个不同的裸主机名；两条 DNS 记录都指向 NAT 公网地址。
浏览器 Origin、媒体 URL 和预签名 S3 URL 都包含 `FRUX_PUBLIC_HTTPS_PORT`。主机 Caddy 仍只监听
本地 443，并加载一张通过 DNS-01 手动签发或续期、同时覆盖两个主机名的证书。

Web、API、MinIO API 和 MinIO Console 只绑定宿主机回环地址。PostgreSQL、Redis、Kafka 和 Worker
不发布宿主机端口。MinIO Console 没有公开 Caddy 路由，只能通过 SSH 高端口建立隧道访问。

### 可选：直接 IPv4 与双 HTTP 端口

```text
http://PUBLIC_IPV4:18082
    └─ Web nginx
       ├─ /api/*、/uploads/*、/media/*、/health → Compose 私网 API:8080
       └─ 其他路径                              → SPA

http://PUBLIC_IPV4:19000
    └─ MinIO S3 API

SSH Tunnel → 127.0.0.1:19001 → MinIO Console
```

此模式只把 Web 和 MinIO S3 API 绑定到 `0.0.0.0`。API 的诊断端口和 MinIO Console 仍绑定
`127.0.0.1`，PostgreSQL、Redis、Kafka 和 Worker 仍不发布端口。应用 Origin 与 S3 Origin 必须使用
同一 IPv4、不同端口，MinIO CORS 仍只允许精确应用 Origin。

直接 IP 只改变寻址方式，不能作为规避备案、接入商策略或防火墙规则的合规依据；HTTP 还会让登录和
预签名 URL 暴露给链路观察者。中国大陆服务器对公网提供互联网信息服务时，应按实际业务向接入商和
主管部门确认备案要求。

Prod运行GHCR中的固定Digest镜像。服务器不Clone仓库，也不安装Go或Node。CI通过且你批准
`production` Environment后，GitHub发布新的部署包；服务器通过systemd每小时检查一次。
主机上的 `/usr/local/sbin/frux-deploy` 属于服务器自有引导程序，不在签名 bundle 内自动替换；部署器
自身发生功能变更时，需要按 [Prod 操作手册](operations/prod.md#启用生产多模态链路) 从已审核 Commit
原子更新一次，再应用新的环境变量。

只有这些路径变化才会构建镜像并请求Prod审批：

```text
apps/api/**
apps/web/**
apps/docker-compose.prod.yml
apps/.env.prod.example
apps/.env.release.example
scripts/postgres-backup.sh
scripts/prod-deploy.sh
.github/workflows/deploy.yml
```

README、`docs/**`、Issue模板和普通OpenSpec修改仍运行CI，但不会触发CD。

## 私有 MinIO 媒体流程

Prod Compose 创建私有 `FRUX_S3_BUCKET` 和持久化 `minio_data` Volume。MinIO 根凭据
`FRUX_MINIO_ROOT_USER`/`FRUX_MINIO_ROOT_PASSWORD` 只用于服务管理和初始化；API/Worker 使用独立的
`FRUX_S3_ACCESS_KEY`/`FRUX_S3_SECRET_KEY`，权限限制在该 Bucket。

上传流程：

```text
浏览器请求上传会话
    ↓
API按配置的公开 S3 Origin 返回短期签名PUT
    ↓
浏览器经Caddy HTTPS或直接S3端口直传MinIO
    ↓
API用HeadObject校验大小、类型和SHA-256
    ↓
Worker通过 http://minio:9000 下载源文件并写确定性最终键
```

Worker 是 Prod 必需服务，部署与回滚都和 API、Web 一起启动并通过健康检查。当前单机策略保持一个
媒体执行 slot，最大源时长 180 分钟，单次 FFmpeg 命令预算 360 分钟，并使用 `veryfast` preset；
新任务只生成一个源分辨率 MP4，兼容 H.264/AAC 源可以 stream copy 快速封装。FFmpeg 仍是媒体
状态机的必需依赖，不能因为上传量少或部分输入可 stream copy 而禁用。PostgreSQL 中的
durable job 继续负责排队、租约、重试和失败原因。

后台视频运营从 API 读取同一 PostgreSQL 任务状态，不需要服务器 shell 或数据库密码。进度更新被
节流到最多每 5 秒一次；管理页面根据活动状态以 5/10/30 秒自适应轮询，浏览器标签隐藏时停止请求。

公开视频播放：

```text
浏览器请求 Frux /media/*
    ↓
API校验v3 generation、variant和视频当前公开资格
    ├─ 新视频MP4：可缓存25分钟的307，目标为30分钟MinIO签名GET
    └─ 历史v2/MPD/分片：兼容读取并逐步迁移
```

视频字节经配置的公开 S3 Origin 从 MinIO 提供；API 只处理授权、小型 MPD 清单和重定向。Caddy
模式不能改写签名请求的 Host、path、query、method 或 Range，直接 IP 模式则让请求原样到达 MinIO。
原视频、私密视频、审核样本和未知对象
不会获得公开签名地址。发布、下架和恢复只修改 PostgreSQL exposure，不复制 MinIO 对象；旧v2对象迁移后
至少保留30分钟再清理。

MinIO CORS 只允许配置的完整应用 Origin，默认是 `https://FRUX_DOMAIN:<public-port>`，直接 IP
模式是 `http://PUBLIC_IPV4:<app-port>`；它仅开放上传和播放所需的
方法、请求头和响应头。详细配置与验证见 [自托管 MinIO](operations/self-hosted-minio.md)。

## 数据和新部署

本方案是新建演示环境，不迁移旧 PostgreSQL、Redis、Kafka 或雨云对象。新主机使用新密钥、空数据库、
空消息状态和空 MinIO Bucket。PostgreSQL 是业务数据和媒体元数据的权威来源；一套数据库只能对应
一个活动 Bucket，绝不能让同一数据库同时向雨云和自托管 MinIO 写入。

首次部署：

1. 在新 NAT 主机创建空持久卷和全新 Secret。
2. 选择默认的两个 DNS 主机名、固定 NAT 映射和 DNS-01 证书，或显式配置一个 IPv4 与两个 HTTP 端口。
3. 启动包含 API、Worker、MinIO 和初始化器的完整 Compose。
4. 完成注册、上传、处理、审核、发布、Range 播放、重启和备份验收。
5. 切换公开链接，但保留旧主机和雨云 Bucket 至少 72 小时。

回滚只恢复旧公开入口，不把新主机写入合并回旧系统。不要把旧数据库接到新 Bucket，也不要把新数据库
接到旧雨云 Bucket。

## 发布和回滚

Prod部署包固定API和Web镜像Digest。部署代理会：

1. 验证部署包文件和SHA-256。
2. 拉取镜像。
3. 更新Compose并启动API、Web、Worker、MinIO和初始化器。
4. 检查API、Web、所选公开入口、MinIO、数据库备份和Worker Kafka状态。
5. 失败时恢复上一版本。

镜像回滚保留 PostgreSQL、Redis、Kafka、上传、备份和 `minio_data` Volume；不要使用
`docker compose down -v`。数据库迁移不会自动回滚，新迁移必须兼容上一版应用。

现有 PostgreSQL 定时备份仍然必需，但不会备份 MinIO 对象。单盘 MinIO 必须另配云厂商磁盘快照或
外部 MinIO/S3 镜像，才能覆盖主机或数据盘丢失。没有可用快照或镜像时，文档不承诺媒体恢复。

## 升级为严格生产环境

当前单机方案不满足以下要求：

- 多故障域PostgreSQL和Redis。
- 至少3个Kafka Broker，replication factor至少3，`min.insync.replicas`至少2。
- Kafka TLS、认证、ACL和预建Topic。
- 多节点 MinIO、独立媒体故障域和对象存储高可用。
- 独立监控、告警、MinIO 异地镜像和经过演练的恢复。
- 多实例API/Worker和滚动发布。

Kafka的完整要求见 [Kafka event backbone](kafka.md)，故障处理见
[Kafka故障恢复](modules/kafka-failure-recovery.md)。

外部审核网关的配置和上线阶段见 [审核模块](modules/review.md)。默认
`moderation.mode=disabled`，未配置真实网关时不要开启自动审核。

## 可选 Tongyi 多模态 Adapter

API 镜像同时包含 `frux-multimodal-provider`，但 Compose 和 Kubernetes 默认都不启动它。生产 Compose
可通过显式 `multimodal` profile 启动 Adapter。Adapter 只需要
公网访问百炼，不访问 PostgreSQL、Redis、Kafka 或对象存储；视频图片由 Worker 通过签名的 Frux 协议
以内联 Base64 发送。

同机开发可以让 Adapter 监听 `127.0.0.1:8099`，API/Worker 使用
`FRUX_MULTIMODAL_ENDPOINT=http://127.0.0.1:8099` 并设置 `allow_insecure_local=true`。单机生产 Compose
使用独立的 `allow_insecure_private_network=true`，只接受精确地址
`http://multimodal-provider:8099`；Adapter 只连接 backend 网络且不发布宿主机端口。任意其他容器、私网或
公网 HTTP 主机仍会被拒绝，跨主机部署必须在 Adapter 前终止 TLS。请求和响应继续使用独立 HMAC 做完整性
校验，但内部 HTTP 本身不提供机密性，因此不能把 Adapter 端口映射到宿主机或公网。

百炼 API Key 只注入 Adapter，不能注入 API/Worker。Worker 接收 Adapter Endpoint 和独立 HMAC 来生成
视频向量；启用 Query Embedding/Hybrid Search 时，API 也接收同一私网 Endpoint 和独立 HMAC 来生成查询
向量，但仍不接收百炼 API Key。`FRUX_MULTIMODAL_PROFILE` 必须在 API、Worker 和 Adapter 中保持一致；
当前允许选择带日期的原生融合档位或无日期的本地均值融合档位。

原生仓库开发可把配置保存为 `apps/.env.multimodal`，三个 Go 命令会自动发现它，且只有 Adapter 会加载
其中的 DashScope 凭证。容器部署不会自动读取宿主机上的该文件，仍需使用 Compose/Kubernetes 的环境变量
注入机制；不要把 `.env.multimodal` 复制进镜像。

生产环境的精确变量、成本、验证和回滚步骤见 [Prod 部署](operations/prod.md#启用生产多模态链路)。
