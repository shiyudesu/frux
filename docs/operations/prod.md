# Prod 部署

Prod服务器不Clone仓库，也不接收GitHub的SSH部署或Webhook。GitHub只负责发布公开的GHCR镜像，
服务器每小时主动检查一次。

```text
PR → 无Secret CI
       ↓ 合并main并通过CI
构建API/Web镜像
       ↓ 你批准 production Environment
发布 frux-deploy:prod
       ↓ 每小时
服务器主动拉取、更新、检查、失败回滚
```

## GitHub仓库设置

### 1. 保护main

在仓库Rulesets或Branch protection中保护 `main`：

- 必须通过Pull Request合并。
- 必须通过CI中的Backend、Web和Repository检查。
- 必须通过CODEOWNERS审核。
- 新提交后撤销旧批准。
- 禁止Force Push和删除分支。

`.github/CODEOWNERS` 已把工作流、Dockerfile、Prod Compose和部署脚本交给仓库所有者审核。

### 2. 配置production Environment

打开：

```text
Settings → Environments → New environment → production
```

设置：

- Required reviewers：选择你自己。
- Deployment branches：只允许 `main`。
- 不需要配置服务器密码或SSH Key。

CI成功后，API和Web镜像会先构建。只有你批准Environment后，
`ghcr.io/shiyudesu/frux-deploy:prod` 才会更新。

### 3. 公共仓库安全

- 不使用自托管Runner。公共仓库的Fork PR可能在自托管机器上执行恶意代码。
- 不使用 `pull_request_target`。
- Fork PR只运行GitHub托管Runner上的无Secret CI。
- 仓库默认 `GITHUB_TOKEN` 权限保持只读；只有镜像发布Job单独申请 `packages: write`。
- `.github/workflows/deploy.yml` 中有权限的Action固定到了具体Commit SHA。

### 4. 将GHCR Package设为Public

第一次发布后，GitHub Packages中会出现：

```text
frux-api
frux-web
frux-deploy
```

进入每个Package的设置，把Visibility改为Public。服务器之后可以匿名拉取，不保存GHCR Token。

## 服务器第一次准备

服务器需要：

- Docker和Docker Compose。
- curl。
- systemd。

创建目录和Prod环境文件：

```bash
sudo install -d -m 700 /opt/frux

sudo curl -fsSL \
  https://raw.githubusercontent.com/shiyudesu/frux/main/apps/.env.prod.example \
  -o /opt/frux/.env.prod

sudo chmod 600 /opt/frux/.env.prod
sudo editor /opt/frux/.env.prod
```

需要填写：

| 配置 | 填什么 |
| --- | --- |
| `FRUX_DOMAIN` | Frux域名，例如 `frux.example.com` |
| `FRUX_ACME_EMAIL` | HTTPS证书联系邮箱 |
| `FRUX_JWT_SECRET` | 随机字符串 |
| `FRUX_INTERNAL_TOKEN` | 随机字符串 |
| `FRUX_POSTGRES_PASSWORD` | PostgreSQL密码 |
| `FRUX_REDIS_PASSWORD` | Redis密码 |
| `FRUX_S3_ACCESS_KEY` | 雨云AccessKey |
| `FRUX_S3_SECRET_KEY` | 雨云SecretKey |

随机值可用下面的命令生成：

```bash
openssl rand -base64 48 | tr -d '\n'
```

安装部署脚本和systemd配置。生产使用时建议把下面URL中的 `main` 换成你已经检查过的Commit SHA：

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

如果服务器之前已经用 `docker-compose.prod.yml` 手动运行过 `frux-prod`，先在旧Compose目录执行：

```bash
FRUX_API_IMAGE=unused \
FRUX_WEB_IMAGE=unused \
FRUX_IMAGE_TAG=unused \
docker compose \
  --env-file .env.prod \
  -p frux-prod \
  -f docker-compose.prod.yml \
  --profile worker \
  down
```

这些值只用于让 `compose down` 解析旧文件，不会拉取镜像。不要加 `-v`，数据库和其他Volume会保留。
拉取代理发现已有容器但没有自己的 `current` 记录时会拒绝接管，不会擅自停止现有服务。

服务器最终只多出：

```text
/opt/frux/.env.prod
/opt/frux/releases/当前和上一版本
/opt/frux/current
/usr/local/sbin/frux-deploy
/etc/systemd/system/frux-deploy.service
/etc/systemd/system/frux-deploy.timer
Docker镜像和Volumes
```

## 检查频率

Timer在开机两分钟后检查一次，之后每小时检查：

```ini
OnBootSec=2min
OnUnitActiveSec=1h
RandomizedDelaySec=5min
Persistent=true
```

镜像没有变化时只查询GHCR清单，不会重启容器。

需要立即检查时：

```bash
sudo systemctl start frux-deploy.service
```

查看Timer：

```bash
systemctl list-timers frux-deploy.timer
```

查看部署日志：

```bash
journalctl -u frux-deploy.service -n 200 --no-pager
```

## 第一次发布

1. Push或合并到 `main`。
2. 等CI通过。
3. 打开GitHub Actions中的 `Publish Prod`。
4. 批准 `production` Environment。
5. 等服务器下一次检查，或手动启动Service。

部署脚本会：

1. 拉取公开的 `frux-deploy:prod`。
2. 验证部署包文件和SHA-256。
3. 拉取固定Digest的API/Web镜像。
4. 更新Compose。
5. 等待API、Web、Caddy公网入口和数据库备份健康。
6. 如果Worker原来已启用，确认Kafka Broker和Consumer工作流均已就绪。
7. 失败时恢复上一套配置和镜像。

## Worker

Worker默认不启动。确认 `frux1` 是空Bucket，或者已经恢复匹配的PostgreSQL后：

```bash
cd /opt/frux/current/apps

docker compose \
  --env-file /opt/frux/.env.prod \
  --env-file .env.release \
  -p frux-prod \
  -f docker-compose.prod.yml \
  --profile worker \
  up -d worker
```

后续自动部署会记住Worker是否已经运行。

## 查看运行状态

```bash
cd /opt/frux/current/apps

docker compose \
  --env-file /opt/frux/.env.prod \
  --env-file .env.release \
  -p frux-prod \
  -f docker-compose.prod.yml \
  --profile worker \
  ps
```

## 数据

PostgreSQL、Redis、Kafka、兼容上传目录、Caddy证书和PostgreSQL备份都在Docker Volume中。

不要执行：

```bash
docker compose -p frux-prod -f docker-compose.prod.yml down -v
```

这会删除持久数据。
