# 部署与媒体交付

Frux 有两套现成的 Docker Compose：

| 环境 | Compose | 对象存储 | 用途 |
| --- | --- | --- | --- |
| 本地开发 | `apps/docker-compose.yml` | MinIO | 开发、测试、调试 |
| 当前 Prod | `apps/docker-compose.prod.yml` | 雨云 `frux1` | 个人项目、小流量试运行 |

当前 Prod 是单服务器方案。PostgreSQL、Redis 和 Kafka 都只有一个实例，不具备高可用，也没有
生产级 Kafka TLS、认证和复制。需要严格生产架构时，再迁移到托管数据库、托管 Kafka 或多机环境。

实际部署命令见：

- [Prod 部署](operations/prod.md)
- [雨云对象存储](operations/rainyun-object-storage.md)

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

## 当前 Prod 结构

```text
公网 80/443
    ↓
服务器现有 Caddy
    ├─ /api、/uploads、/media、/health → 127.0.0.1:18081
    └─ 其他请求                       → 127.0.0.1:18080

Docker Compose
    ├─ Web
    ├─ API
    ├─ Worker（默认不启动）
    ├─ PostgreSQL
    ├─ Redis
    ├─ 单节点 Kafka
    └─ PostgreSQL backup

API / Worker → 雨云 frux1
```

只有Web和API绑定宿主机回环地址。PostgreSQL、Redis、Kafka和Worker都没有宿主机端口，公网无法直接
连接。

Prod运行GHCR中的固定Digest镜像。服务器不Clone仓库，也不安装Go或Node。CI通过且你批准
`production` Environment后，GitHub发布新的部署包；服务器通过systemd每小时检查一次。

只有这些路径变化才会构建镜像并请求Prod审批：

```text
apps/api/**
apps/web/**
apps/docker-compose.prod.yml
apps/.env.prod.example
apps/.env.release.example
scripts/postgres-backup.sh
.github/workflows/deploy.yml
```

README、`docs/**`、Issue模板和普通OpenSpec修改仍运行CI，但不会触发CD。

## 雨云媒体流程

`frux1` 始终保持私有。雨云只提供整桶匿名访问开关，没有目录级公共读，所以不要开启公共访问。

上传流程：

```text
浏览器请求上传会话
    ↓
API返回短期签名PUT
    ↓
浏览器直传雨云
    ↓
API用HeadObject校验大小、类型和SHA-256
    ↓
Worker转码并写回雨云
```

公开视频播放：

```text
浏览器请求 Frux /media/*
    ↓
API确认对象仍属于当前可公开视频
    ├─ MPD清单和HEAD：API直接返回
    └─ MP4和DASH分片：307到最长60秒的雨云签名GET
```

视频字节由雨云提供，VPS只处理授权、小型MPD清单和重定向。原视频、私密视频、审核样本和未知对象
不会获得签名地址。

雨云网关默认允许跨域预检，因此面板中不需要配置CORS。详细验证方法见
[雨云对象存储](operations/rainyun-object-storage.md)。

## 数据和迁移

PostgreSQL是业务数据和媒体元数据的权威来源。一个 `frux1` 只能对应一套Frux PostgreSQL。

换服务器时：

1. 停止旧环境写入。
2. 备份并恢复PostgreSQL。
3. 确认媒体表和视频表已经恢复。
4. 启动API。
5. 最后启动Worker。

不要让空数据库直接连接已有数据的 `frux1` 后启动Worker。当前Prod已关闭未知对象自动清理，避免误删
旧对象，但数据库不知道的旧视频仍然无法使用。

Redis可以重建。Kafka包含事件、重试记录和Consumer Offset；当前单节点方案不提供Kafka灾备，不能
当作严格生产环境。

## 发布和回滚

Prod部署包固定API和Web镜像Digest。部署代理会：

1. 验证部署包文件和SHA-256。
2. 拉取镜像。
3. 保留Worker当前启用状态。
4. 更新Compose。
5. 检查API、Web、现有Caddy路由、数据库备份和Worker Kafka状态。
6. 失败时恢复上一版本。

数据库迁移不会自动回滚。新迁移必须兼容上一版应用，避免镜像回滚后无法读取数据库。

## 升级为严格生产环境

当前单机方案不满足以下要求：

- 多故障域PostgreSQL和Redis。
- 至少3个Kafka Broker，replication factor至少3，`min.insync.replicas`至少2。
- Kafka TLS、认证、ACL和预建Topic。
- 独立监控、告警和异地备份。
- 多实例API/Worker和滚动发布。

Kafka的完整要求见 [Kafka event backbone](kafka.md)，故障处理见
[Kafka故障恢复](modules/kafka-failure-recovery.md)。

外部审核网关的配置和上线阶段见 [审核模块](modules/review.md)。默认
`moderation.mode=disabled`，未配置真实网关时不要开启自动审核。
