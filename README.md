<div align="center">

# Frux

**面向短视频场景的完整 Feed 系统工程**

![Go](https://img.shields.io/badge/Go-1.26.1-00ADD8?style=flat-square&logo=go&logoColor=white)
![React](https://img.shields.io/badge/React-18.3-61DAFB?style=flat-square&logo=react&logoColor=111827)
![TypeScript](https://img.shields.io/badge/TypeScript-6.0-3178C6?style=flat-square&logo=typescript&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-17-4169E1?style=flat-square&logo=postgresql&logoColor=white)
![Redis](https://img.shields.io/badge/Redis-7.4-DC382D?style=flat-square&logo=redis&logoColor=white)
![Apache Kafka](https://img.shields.io/badge/Apache%20Kafka-4.0-231F20?style=flat-square&logo=apachekafka&logoColor=white)
![Docker Compose](https://img.shields.io/badge/Docker-Compose-2496ED?style=flat-square&logo=docker&logoColor=white)

Frux 使用 Go、React、PostgreSQL、Redis、Kafka 和 S3 兼容对象存储，
实现短视频从上传、审核、分发到播放互动的完整链路。

[在线体验](#在线体验) · [功能概览](#功能概览) · [快速启动](#快速启动) · [开发与验证](#开发与验证) · [文档](#文档)

</div>

## 在线体验

公开 NAT 演示地址使用完整高端口 Origin：`https://frux.shiyudesu.com:<public-port>`。

**郑重说明：**

线上实例中的部分视频素材整理或转载自公开网络，仅用于展示 Frux 的上传、审核、Feed、播放与
互动等功能，不作商业用途。本人不主张相关素材的著作权，相关权利归原作者及其他合法权利人所有。
若展示内容侵犯了您的著作权、肖像权或其他合法权益，请通过
[GitHub Issues](https://github.com/shiyudesu/frux/issues/new) 联系并注明具体内容；收到通知后，
我会第一时间下架并删除相关素材。

## 功能概览

| 能力 | 说明 |
| --- | --- |
| 视频供给 | 预签名直传、异步处理、原分辨率 MP4、封面处理和保护媒体访问 |
| 内容治理 | 自动审核、人工复审、版本化策略、下架/恢复、普通用户账号管理、后台权限与不可变审计 |
| Feed 分发 | 最新、热门、关注和推荐流，稳定游标、缓存校验、预加载与播放反馈 |
| 用户互动 | 点赞、收藏、关注、两级评论、消息通知、观看历史和稍后再看 |
| 播放体验 | 全屏连续播放、倍速与连播偏好、历史多源兼容、QoS 指标和版本化播放遥测 |
| 稳定性 | PostgreSQL 耐久任务、Kafka 重试/DLQ、Redis 协调限流、Prometheus 与 Grafana |

## 技术栈

| 层次 | 技术 |
| --- | --- |
| API / Worker | Go、CloudWeGo Hertz、GORM |
| Web | React、TypeScript、Vite、原生 HTMLVideoElement、dash.js |
| 数据 | PostgreSQL、Redis |
| 事件 | Apache Kafka（KRaft） |
| 媒体 | MinIO / S3、FFmpeg、原分辨率 MP4 |
| 可观测性 | Prometheus、Grafana |
| 本地编排 | Docker Compose |

## 快速启动

### 前置依赖

- Docker
- Docker Compose
- OpenSSL

### 启动完整环境

内部接口要求一个至少 32 字符、包含至少三类字符的随机 Token：

```bash
export FRUX_INTERNAL_TOKEN="$(openssl rand -base64 48 | tr -d '\n')"
cd apps
docker compose up --build
```

主要入口：

| 服务 | 地址 |
| --- | --- |
| Web | `http://127.0.0.1:5173` |
| API 健康检查 | `http://127.0.0.1:8080/health` |
| 后台登录 | `http://127.0.0.1:5173/admin/login` |
| MinIO Console | `http://127.0.0.1:9001` |
| Grafana | `http://127.0.0.1:3000/d/frux-overview/frux-overview` |

常用命令：

| 操作 | 命令 |
| --- | --- |
| 后台启动 | `cd apps && docker compose up -d --build` |
| 查看日志 | `cd apps && docker compose logs -f api worker web` |
| 停止服务 | `cd apps && docker compose down` |
| 校验配置 | `cd apps && docker compose config` |
| 清空本地数据 | `cd apps && docker compose down -v` |

> `docker compose down -v` 会删除 PostgreSQL、Redis、Kafka 和 MinIO 的本地数据卷。

该 Compose 文件只用于本地开发，并始终使用 MinIO。Prod 使用 NAT 高端口入口和私有自托管
MinIO，操作见 [Prod操作手册](docs/operations/prod.md) 与
[自托管MinIO](docs/operations/self-hosted-minio.md)。旧部署的雨云设置保留在
[雨云对象存储（旧部署）](docs/operations/rainyun-object-storage.md)。
Prod由GitHub Actions构建并推送公开GHCR镜像；服务器不Clone仓库，也不开放部署Webhook或保存
GitHub部署SSH Key，而是通过systemd每小时主动检查已批准的部署包。

### 启用本地后台账号

先在 Web 注册账号，再将该账号设置为兼容管理员：

```bash
docker exec frux-postgres \
  psql -U frux -d frux \
  -c "UPDATE account SET role='admin' WHERE account='ops';"
```

后台使用独立的 `admin_access` Token，不会覆盖普通用户登录态。新视频默认进入待审核状态，
只有审核批准且媒体处理完成后才会进入 Feed、搜索和公共媒体交付。

## 本地开发

依赖服务可用后，在仓库根目录启动 API、Worker 和 Vite：

```bash
./scripts/start.sh
```

两个 Go 进程使用相对路径读取 `apps/api/configs/config.yaml`；直接运行 Go 命令时应进入
`apps/api` 目录。脚本不会自动创建 PostgreSQL、Redis、Kafka 或 MinIO。

## 开发与验证

Pull Request、`main` 分支 Push 和手动触发会运行 GitHub Actions CI，分别检查后端、Web 和仓库配置。

```bash
# 后端
cd apps/api
test -z "$(gofmt -l .)"
go vet ./...
go test ./...
go build ./cmd/feed ./cmd/worker

# 前端
pnpm -C apps/web install --frozen-lockfile
pnpm -C apps/web run lint
pnpm -C apps/web run test
pnpm -C apps/web run build

# OpenSpec
openspec validate --all --strict
```

更多验证入口：

- [性能测试与 Grafana 指标](docs/performance-testing.md)
- [部署、MinIO/S3 与回滚](docs/deployment.md)
- [安全与媒体访问边界](docs/security.md)

## 架构摘要

- `cmd/feed` 提供 HTTP API，组合仓储、应用服务、Handler、中间件和监控。
- `cmd/worker` 消费 Kafka 事件并执行媒体、审核、Feed、嵌入和恢复任务。
- PostgreSQL 是业务事实和长任务状态的最终来源。
- Redis 保存 Feed 页、热榜、互动状态和短期索引。
- Kafka 负责可回放事件与短时唤醒，不承担长期租约和延迟重试。
- MinIO/S3 保存原始媒体、处理结果和受保护审核样本。

后端依赖方向保持为：

```text
Domain <- Application <- Infrastructure / Interfaces
```

## 文档

| 文档 | 内容 |
| --- | --- |
| [产品说明](docs/product.md) | 产品范围、模块地图和功能状态 |
| [快速阅读](docs/quickread.md) | 新读者代码阅读路线 |
| [系统架构](docs/architecture.md) | 分层、核心链路和数据模型 |
| [工程规范](docs/engineering.md) | 目录规则、API 风格和测试约定 |
| [UI/UX](docs/uiux.md) | Web 页面、交互和响应式规格 |
| [优化说明](docs/optimization.md) | Feed、缓存、媒体和播放优化 |
| [模块设计](docs/modules/README.md) | 各业务模块的详细设计 |
| [OpenSpec](openspec/) | 项目基线和变更规格 |

新增能力优先创建 OpenSpec change，并保持代码、测试和相关文档同步。
