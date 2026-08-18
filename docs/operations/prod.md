# Prod 操作手册

Prod服务器不Clone仓库。GitHub Actions发布公开GHCR镜像，服务器用systemd每小时检查一次。

```text
main CI通过
    ↓ 检查部署相关路径
    ├─ 只有文档/模板变化：结束
    └─ 代码或Prod配置变化：构建API/Web
                              ↓ 人工批准production
                              发布frux-deploy:prod
                                      ↓
                              服务器拉取并部署
```

当前方案是在一台 NAT 主机上全新部署演示环境，适合个人项目和小流量试运行。它只有一台服务器、
一个 Kafka Broker 和单节点 MinIO，不是高可用架构。本流程不迁移旧 PostgreSQL、Redis、Kafka 或
雨云对象；旧部署用于短期回滚。

## NAT、DNS 与公开 Origin

开始前向主机提供商确认固定 TCP 映射：

```text
公网分配的 HTTPS 高端口 -> 主机 443
公网分配的 SSH 高端口   -> 主机 22
```

创建两条指向 NAT 公网地址的 DNS 记录：

```text
FRUX_DOMAIN     应用、API 和 /media
FRUX_S3_DOMAIN MinIO S3 API
```

两个值必须是不同的裸主机名。公开地址始终包含同一个 `FRUX_PUBLIC_HTTPS_PORT`：

```text
https://FRUX_DOMAIN:<public-port>
https://FRUX_S3_DOMAIN:<public-port>
```

主机 Caddy 仍监听本地 443，部署代理也继续通过 `127.0.0.1:443` 检查路由。不要把 Caddy 改为监听
公网高端口。

## 一次性设置GitHub

### main分支

仓库当前规则：

- Backend、Web和Repository三项CI必须通过。
- 外部贡献者通过Pull Request提交。
- 仓库管理员可以直接Push。
- 禁止Force Push和删除main。
- 只允许普通Merge Commit，不使用Squash或Rebase Merge。

`.github/CODEOWNERS` 标记了工作流、Dockerfile、Prod配置和部署脚本。以后增加可信协作者后，可开启
强制Code Owner审核。

### production Environment

在GitHub打开：

```text
Settings → Environments → production
```

确认：

- Required reviewers中有你。
- 只允许受保护的main分支部署。
- 管理员不能绕过审批。

CI通过后，只有这些路径发生变化才会构建镜像并等待审批：

```text
apps/api/**
apps/web/**
apps/docker-compose.prod.yml
apps/.env.prod.example
apps/.env.release.example
scripts/postgres-backup.sh
.github/workflows/deploy.yml
```

README、`docs/**`、Issue/PR模板和普通OpenSpec改动只运行CI，不触发CD。

比较基线是上一次成功的main CI，不是上一次已批准的Prod。这样纯文档提交不会因为之前有未发布代码
而再次触发审批。某次代码发布被取消后，需要发布该版本时，在Actions里重新运行原来的
`Publish Prod` Run。

### GHCR Packages

第一次成功发布后会生成：

```text
frux-api
frux-web
frux-deploy
```

进入三个Package的设置，将Visibility改为Public。服务器匿名拉取，不保存GitHub Token。

公共仓库不要使用自托管Runner，也不要添加 `pull_request_target` 工作流。外部Fork只在GitHub托管
Runner上执行无Secret CI。

## 一次性设置服务器

需要安装：

- Docker
- Docker Compose
- curl
- OpenSSL
- systemd
- Caddy（由 systemd 管理）

同时确认 Docker 可用、数据盘持久，并为镜像、PostgreSQL、Kafka、MinIO 和备份预留足够空间。
Docker 日志必须配置有界轮转，避免单机磁盘被容器日志耗尽。

### 创建Prod环境文件

建议把URL中的 `main` 换成你检查过的Commit SHA：

```bash
sudo install -d -m 700 /opt/frux

sudo curl -fsSL \
  https://raw.githubusercontent.com/shiyudesu/frux/main/apps/.env.prod.example \
  -o /opt/frux/.env.prod

sudo chmod 600 /opt/frux/.env.prod
sudo editor /opt/frux/.env.prod
```

填写：

| 配置 | 内容 |
| --- | --- |
| `FRUX_DOMAIN` | Frux域名，例如 `frux.example.com` |
| `FRUX_S3_DOMAIN` | 独立的 MinIO S3 域名，例如 `s3.frux.example.com` |
| `FRUX_PUBLIC_HTTPS_PORT` | 提供商分配并转发到本地 443 的 HTTPS 高端口 |
| `FRUX_JWT_CONSUMER_SECRET` | 至少 32 字节的消费端 JWT 随机密钥 |
| `FRUX_JWT_ADMIN_SECRET` | 与消费端不同的至少 32 字节后台 JWT 随机密钥 |
| `FRUX_JWT_LEGACY_SECRET` | 可选；升级前旧共享密钥，仅迁移窗口使用 |
| `FRUX_JWT_LEGACY_ISSUED_UNTIL` | 可选；旧服务停止签发 Token 的 RFC3339 时间 |
| `FRUX_JWT_LEGACY_ACCEPT_UNTIL` | 可选；旧 Token 接受截止时间，RFC3339 |
| `FRUX_HMAC_SECRET` | 与 JWT 密钥不同的应用内部签名密钥，用于游标和短期媒体 URL |
| `FRUX_INTERNAL_TOKEN` | 随机字符串 |
| `FRUX_POSTGRES_PASSWORD` | PostgreSQL密码 |
| `FRUX_REDIS_PASSWORD` | Redis密码 |
| `FRUX_MINIO_ROOT_USER` | 新 MinIO 管理用户名，只供 MinIO 和初始化器使用 |
| `FRUX_MINIO_ROOT_PASSWORD` | 新 MinIO 管理密码，不注入 API/Worker |
| `FRUX_S3_ACCESS_KEY` | 新 Bucket-scoped 应用 Access Key |
| `FRUX_S3_SECRET_KEY` | 新 Bucket-scoped 应用 Secret Key |
| `FRUX_S3_BUCKET` | 新建的私有 MinIO Bucket 名 |

以下命令适合生成密码、Token 和签名密钥：

```bash
openssl rand -base64 48 | tr -d '\n'
```

MinIO 用户名和 Access Key 另行生成符合 MinIO 约束的独立随机标识。消费端、后台、应用 HMAC、
MinIO Root 和 MinIO 应用凭据必须分别生成，Root 与应用凭据不得相同。
应用凭据只拥有 Frux 四个对象前缀所需的数据权限，不能修改 Bucket policy、CORS 或匿名访问；
更换 `FRUX_S3_ACCESS_KEY` 会由初始化器撤销旧的 Frux 应用身份。
首次从旧共享密钥升级时，先把原 `FRUX_JWT_SECRET` 填入
`FRUX_JWT_LEGACY_SECRET`，把切换时刻写入 `FRUX_JWT_LEGACY_ISSUED_UNTIL`，并将
`FRUX_JWT_LEGACY_ACCEPT_UNTIL` 设置为该时刻加旧 Token 最大 TTL 与 clock leeway 之后（当前至少
30 分 30 秒）。截止时间可以自然过期，后续重启只会关闭旧 Token 校验，不会阻止启动；
确认新 Web 与新 key ring 正常后清空两个 legacy 变量。截止时间后旧无 `kid` Token 会被拒绝，回滚时
必须同时恢复兼容校验配置，数据库中的 Refresh Session 表可保留。

`FRUX_DOMAIN` 和 `FRUX_S3_DOMAIN` 只填域名，不要加协议、端口、引号或末尾斜杠：

```dotenv
FRUX_DOMAIN=frux.example.com
FRUX_S3_DOMAIN=s3.frux.example.com
FRUX_PUBLIC_HTTPS_PORT=<public-port>
```

Prod 的运行时 S3 Endpoint 固定为 Compose 网络中的 `http://minio:9000`；浏览器预签名 Endpoint 为
`https://FRUX_S3_DOMAIN:<public-port>`。保持 path-style、非空 Region、私有 Bucket，并关闭应用侧
自动建 Bucket；`minio-init` 负责幂等创建 Bucket、应用身份、Bucket policy 和精确 CORS。

### 签发 DNS-01 证书

HTTP-01 和 TLS-ALPN-01 无法通过公网 80/443 到达此 NAT 主机。使用 DNS 服务商 API 通过 DNS-01
签发一张同时覆盖 `FRUX_DOMAIN` 和 `FRUX_S3_DOMAIN` 的证书，并把证书与私钥放在仅 root/Caddy
可读的位置。DNS API 凭据必须使用最小 Zone 权限、保存在仓库和部署包之外。

可以使用支持该 DNS 服务商的 ACME 客户端手动签发并自动续期。续期 Hook 必须原子替换证书文件、
执行 `caddy validate`，再 reload Caddy；同时监控证书到期时间。Caddy模板只读取最终证书文件，不
需要持有 DNS API Token。

### 配置主机 Caddy

Prod 默认回环端口：

```text
Web            127.0.0.1:18080
API            127.0.0.1:18081
MinIO S3 API   127.0.0.1:19000
MinIO Console  127.0.0.1:19001
```

先确认没有占用：

```bash
sudo ss -ltnp | grep -E ':(443|18080|18081|19000|19001)\s' || true
```

如 Web/API 端口有占用，修改 `.env.prod` 中对应值并同步修改 Caddy。MinIO API 必须与模板中的
`19000` 一致，Console 保持回环 `19001`。

安装仓库模板：

```bash
sudo install -d -m 755 /etc/caddy
sudo curl -fsSL \
  https://raw.githubusercontent.com/shiyudesu/frux/main/deploy/caddy/frux-nat-minio.Caddyfile \
  -o /etc/caddy/Caddyfile
```

创建 `/etc/caddy/frux.env`：

```dotenv
FRUX_DOMAIN=frux.example.com
FRUX_S3_DOMAIN=s3.frux.example.com
FRUX_TLS_CERT_FILE=/etc/caddy/certs/frux/fullchain.pem
FRUX_TLS_KEY_FILE=/etc/caddy/certs/frux/privkey.pem
```

该文件不保存 MinIO 或 DNS API 凭据。让 systemd Caddy 读取环境文件：

```bash
sudo chmod 600 /etc/caddy/frux.env
sudo systemctl edit caddy
```

写入：

```ini
[Service]
EnvironmentFile=/etc/caddy/frux.env
```

模板在本地 443 服务两个主机名：

- 应用主机的 `/api/*`、`/uploads/*`、`/media/*`、`/health` 到 `127.0.0.1:18081`。
- 应用主机的其他路径到 `127.0.0.1:18080`。
- S3 主机全部请求到 `127.0.0.1:19000`，不改写 Host、path、query、method 或 Range。
- 没有 MinIO Console 公开路由。

加载前验证：

```bash
sudo sh -c 'set -a; . /etc/caddy/frux.env; set +a; caddy validate --config /etc/caddy/Caddyfile'
sudo systemctl daemon-reload
sudo systemctl reload caddy
```

自动部署不会修改Caddyfile。

本地主机验证仍走 443：

```bash
sudo -i
set -a
. /opt/frux/.env.prod
set +a

curl --resolve "$FRUX_DOMAIN:443:127.0.0.1" \
  "https://$FRUX_DOMAIN/health"

curl --resolve "$FRUX_S3_DOMAIN:443:127.0.0.1" \
  "https://$FRUX_S3_DOMAIN/minio/health/live"
exit
```

从外部网络验证 NAT 高端口：

```bash
curl "https://FRUX_DOMAIN:<public-port>/health"
curl "https://FRUX_S3_DOMAIN:<public-port>/minio/health/live"
```

MinIO Console 只能通过 SSH 映射访问：

```bash
ssh -p <public-ssh-port> \
  -L 29001:127.0.0.1:19001 \
  <user>@<nat-public-address>
```

然后在本机打开 `http://127.0.0.1:29001`。不要给 Console 增加 DNS 或 Caddy 路由。

### 安装拉取代理

```bash
sudo curl -fsSL \
  https://raw.githubusercontent.com/shiyudesu/frux/main/scripts/prod-deploy.sh \
  -o /usr/local/sbin/frux-deploy

sudo curl -fsSL \
  https://raw.githubusercontent.com/shiyudesu/frux/main/deploy/systemd/frux-deploy.service \
  -o /etc/systemd/system/frux-deploy.service

sudo curl -fsSL \
  https://raw.githubusercontent.com/shiyudesu/frux/main/deploy/systemd/frux-deploy.timer \
  -o /etc/systemd/system/frux-deploy.timer

sudo chmod 755 /usr/local/sbin/frux-deploy
sudo systemctl daemon-reload
sudo systemctl enable --now frux-deploy.timer
```

Timer配置：

```ini
OnBootSec=2min
OnUnitActiveSec=1h
RandomizedDelaySec=5min
Persistent=true
```

首次拉取镜像可能较慢，Service允许运行1小时。Docker会缓存已下载的镜像层。

如果服务器原来手动运行过 `frux-prod`，启用代理前先停止旧容器：

```bash
FRUX_API_IMAGE=unused \
FRUX_WEB_IMAGE=unused \
FRUX_IMAGE_TAG=unused \
docker compose \
  --env-file .env.prod \
  -p frux-prod \
  -f docker-compose.prod.yml \
  down
```

不要加 `-v`。Volume会保留。代理检测到未纳管的 `frux-prod` 容器时会拒绝接管，不会擅自停服务。

## 发布

代码或Prod配置进入main后：

1. 等CI通过。
2. 打开Actions中的 `Publish Prod`。
3. 批准 `production` Environment。
4. 等下一次每小时检查，或立即执行：

```bash
sudo systemctl start frux-deploy.service
```

部署流程：

1. 拉取 `frux-deploy:prod`。
2. 验证文件白名单和SHA-256。
3. 拉取固定Digest的API/Web镜像。
4. 更新Compose并启动MinIO、初始化器、API、Web和Worker。
5. 检查API、Web、本地443 Caddy路由、MinIO依赖、数据库备份和Worker Kafka状态。
6. 失败时恢复上一版本。

服务器只保留：

```text
/opt/frux/.env.prod
/opt/frux/current
/opt/frux/releases/当前和上一版本
/usr/local/sbin/frux-deploy
/etc/systemd/system/frux-deploy.service
/etc/systemd/system/frux-deploy.timer
Docker镜像和Volumes
```

## Worker

Worker是生产核心服务，会随API和Web一起启动。此 NAT 主机是 fresh deployment：
`FRUX_S3_BUCKET`、PostgreSQL、Redis 和 Kafka 都从空状态初始化，不复制雨云或旧主机数据。
不要让空数据库连接已有数据的Bucket，也不要让同一数据库同时连接雨云和MinIO两个活动Bucket。

当前媒体策略是单并发、最大源时长 180 分钟、单次 FFmpeg 命令超时 360 分钟和 `veryfast` preset。
`processing` 表示正在执行；`pending`、`retryable` 表示排队；`completed`、`failed` 表示终态。
Worker下载一次源文件并把输出直接写入确定性的 `processed/*` 最终键。公开发布、下架和恢复只切换
PostgreSQL exposure generation，不复制MinIO对象。公开307缓存25分钟、签名媒体响应缓存30分钟；
下架立即停止新授权，但已缓存播放最多延迟30分钟结束。

H.264/AAC源可能走stream copy，但FFmpeg仍负责探测、封装、音频规范化和其他输入的转码。当前媒体
状态机、Worker镜像和验收流程都要求FFmpeg，不能因上传不频繁而关闭或绕过。

查看当前任务和失败原因：

```bash
sudo -i
set -a
. /opt/frux/.env.prod
set +a
release=$(readlink -f /opt/frux/current)

docker compose \
  --env-file /opt/frux/.env.prod \
  --env-file "$release/apps/.env.release" \
  -p frux-prod \
  -f "$release/apps/docker-compose.prod.yml" \
  exec -T postgres psql \
  -U "$FRUX_POSTGRES_USER" \
  -d "$FRUX_POSTGRES_DATABASE" \
  -x -c "
SELECT v.id, v.title, j.state, j.attempts, j.max_attempts,
       j.error_code, j.error_message, j.lease_until, j.updated_at
FROM media_processing_job j
LEFT JOIN video v ON v.media_asset_id = j.asset_id
ORDER BY j.updated_at DESC
LIMIT 20;"
```

`lease_expired` 表示 Worker 中断后由 reconciliation 回收；`duration_limit` 是源视频超过配置上限；
`probe_timeout`、`remux_timeout`、`transcode_timeout` 表示对应命令超过预算。retryable 任务会由
数据库 polling 自动重新领取，不需要恢复 Kafka 消息。

部署包含视频运营处理视图后，日常查看和重新处理失败视频应优先使用 `/admin/videos` 的“处理进度”，
不再要求运维人员直接执行 SQL。服务器命令保留为后台不可用时的诊断手段。

## 设置后台管理员

先在Frux网页注册账号，然后执行：

```bash
sudo -i
set -a
. /opt/frux/.env.prod
set +a

read -r -p "Frux账号: " ACCOUNT
cd /opt/frux/current/apps

docker compose \
  --env-file /opt/frux/.env.prod \
  --env-file .env.release \
  -p frux-prod \
  -f docker-compose.prod.yml \
  exec -T postgres \
  psql \
  -U "$FRUX_POSTGRES_USER" \
  -d "$FRUX_POSTGRES_DATABASE" \
  -v a="$ACCOUNT" <<'SQL'
UPDATE account
SET role = 'admin',
    updated_at = NOW()
WHERE account = lower(:'a')
RETURNING id, account, role;
SQL

unset ACCOUNT
exit
```

返回账号、ID和 `admin` 说明设置成功。

## 常用命令

立即检查新版本：

```bash
sudo systemctl start frux-deploy.service
```

查看部署日志：

```bash
sudo journalctl -u frux-deploy.service -n 200 -o cat
```

查看Timer：

```bash
systemctl list-timers frux-deploy.timer
```

查看容器：

```bash
cd /opt/frux/current/apps

docker compose \
  --env-file /opt/frux/.env.prod \
  --env-file .env.release \
  -p frux-prod \
  -f docker-compose.prod.yml \
  ps
```

## 数据

PostgreSQL、Redis、Kafka、MinIO `minio_data`、兼容上传目录和PostgreSQL备份保存在Docker Volume中。
Caddy证书由主机上的DNS-01续期流程管理。

不要执行：

```bash
docker compose -p frux-prod -f docker-compose.prod.yml down -v
```

这会删除持久数据。数据库、备份和MinIO Volume仍在同一台服务器，不构成异地灾备：

- 保留现有PostgreSQL定时备份，并复制到独立位置。
- 为MinIO数据盘配置云厂商快照，或把Bucket镜像到外部MinIO/S3故障域。
- 记录最近一次成功快照/镜像和恢复演练结果。
- 单机Kafka没有备份、复制或严格灾备保证。

## 首次验收、切换与回滚

在切换公开链接前完成：

1. 注册、登录和后台账号设置。
2. 视频与封面预签名PUT，确认精确Origin CORS和API `HeadObject`校验。
3. Worker下载、FFmpeg处理、确定性输出写入和审核发布。
4. `https://FRUX_DOMAIN:<public-port>/media/...` 的307、Range、HEAD、ETag和拖动播放。
5. 匿名访问 `uploads/*`、`processed/*`、`moderation/*` 和存储 `media/*` 返回拒绝。
6. 重启Compose但不删除Volume，确认数据库、Kafka、Redis和MinIO对象仍可用。
7. PostgreSQL备份成功，并确认MinIO快照或外部镜像状态。
8. 观察内存、磁盘、Worker readiness、MinIO流量和错误日志。

验收通过后，把对外文档和入口切换到完整地址 `https://FRUX_DOMAIN:<public-port>`。旧主机和旧雨云
Bucket保持不变并至少保留72小时。

验收失败时恢复旧公开入口，不删除旧主机或雨云Bucket，也不尝试把新主机期间的数据库或对象写入
合并回旧部署。必要时先保存新主机最后一份PostgreSQL备份用于诊断；回滚意味着明确接受fresh deployment
期间的新数据不会出现在旧系统。

该拓扑没有服务器级HA、多节点MinIO、Kafka复制、独立媒体故障域或内置监控Dashboard。需要关键生产
可用性和耐久性时，应另行设计多故障域架构，而不是继续扩展此单机演示方案。
