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

当前方案适合个人项目和小流量试运行。它只有一台服务器和一个Kafka，不是高可用架构。

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
- systemd
- Caddy（服务器已在使用）

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
| `FRUX_JWT_SECRET` | 随机字符串 |
| `FRUX_INTERNAL_TOKEN` | 随机字符串 |
| `FRUX_POSTGRES_PASSWORD` | PostgreSQL密码 |
| `FRUX_REDIS_PASSWORD` | Redis密码 |
| `FRUX_S3_ACCESS_KEY` | 雨云AccessKey |
| `FRUX_S3_SECRET_KEY` | 雨云SecretKey |

生成随机值：

```bash
openssl rand -base64 48 | tr -d '\n'
```

`FRUX_DOMAIN` 只填域名，不要加协议、引号或末尾斜杠：

```dotenv
FRUX_DOMAIN=frux.example.com
```

### 配置现有Caddy

Prod默认使用：

```text
Web  127.0.0.1:18080
API  127.0.0.1:18081
```

先确认没有占用：

```bash
sudo ss -ltnp | grep -E ':(18080|18081)\s' || true
```

如有占用，修改 `.env.prod` 中的 `FRUX_WEB_PORT` 和 `FRUX_API_PORT`。

在 `/etc/caddy/Caddyfile` 中加入：

```caddyfile
frux.example.com {
	@api path /api/* /uploads/* /media/* /health

	handle @api {
		reverse_proxy 127.0.0.1:18081
	}

	handle {
		reverse_proxy 127.0.0.1:18080
	}
}
```

替换域名和端口，然后执行：

```bash
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

自动部署不会修改Caddyfile。

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
  --profile worker \
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
4. 保留Worker当前启用状态。
5. 更新Compose。
6. 检查API、Web、Caddy路由、数据库备份和Worker Kafka状态。
7. 失败时恢复上一版本。

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

Worker默认不启动。确认 `frux1` 是空Bucket，或者已恢复匹配的PostgreSQL后：

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

后续发布会保持Worker现有状态。

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
  --profile worker \
  ps
```

## 数据

PostgreSQL、Redis、Kafka、兼容上传目录和PostgreSQL备份保存在Docker Volume中。Caddy证书由服务器
现有Caddy管理。

不要执行：

```bash
docker compose -p frux-prod -f docker-compose.prod.yml down -v
```

这会删除持久数据。数据库和备份Volume仍在同一台服务器，重要备份还要复制到别处。
