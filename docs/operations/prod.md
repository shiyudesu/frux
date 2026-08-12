# Prod 部署

这套配置适合个人项目和小流量试运行。PostgreSQL、Redis、Kafka、API、Worker、Web 和 Caddy 都在
一台服务器的 Docker 里，视频文件放在雨云。

它不是高可用方案：服务器宕机后，整个系统都会停止；单 Kafka 也没有复制和认证。

## 准备这些东西

- 一台装好 Docker、Docker Compose 和 Git 的服务器。
- 一个已经解析到服务器 IP 的域名。
- 雨云对象存储的 AccessKey 和 SecretKey。

服务器需要开放：

```text
80/tcp
443/tcp
443/udp
```

## 填写 Prod 配置

进入 `apps` 目录：

```bash
cp .env.prod.example .env.prod
chmod 600 .env.prod
```

打开 `.env.prod`，填写这些空项：

| 配置 | 填什么 |
| --- | --- |
| `FRUX_DOMAIN` | 访问 Frux 的域名，例如 `frux.example.com` |
| `FRUX_ACME_EMAIL` | 用于申请 HTTPS 证书的联系邮箱 |
| `FRUX_JWT_SECRET` | 自己生成的随机字符串 |
| `FRUX_INTERNAL_TOKEN` | 自己生成的随机字符串 |
| `FRUX_POSTGRES_PASSWORD` | PostgreSQL 密码 |
| `FRUX_REDIS_PASSWORD` | Redis 密码 |
| `FRUX_S3_ACCESS_KEY` | 雨云 AccessKey |
| `FRUX_S3_SECRET_KEY` | 雨云 SecretKey |

随机字符串和密码可以用这条命令生成：

```bash
openssl rand -base64 48 | tr -d '\n'
```

有默认值的项目不用改。

## 配置雨云

按照[雨云对象存储怎么配](rainyun-object-storage.md)设置 `frux1` 的 CORS 和公开读取范围。

## 启动

先检查配置有没有写错：

```bash
docker compose --env-file .env.prod \
  -p frux-prod \
  -f docker-compose.prod.yml \
  config --quiet
```

启动数据库、Kafka、API 和 Web：

```bash
docker compose --env-file .env.prod \
  -p frux-prod \
  -f docker-compose.prod.yml \
  up -d --build
```

Worker 默认不启动。这样即使数据库是空的，也不会直接处理或清理雨云里的旧文件。

如果 `frux1` 是新建的空 Bucket，可以启动 Worker：

```bash
docker compose --env-file .env.prod \
  -p frux-prod \
  -f docker-compose.prod.yml \
  --profile worker \
  up -d worker
```

如果 `frux1` 已经存过 Frux 的视频，先恢复对应的 PostgreSQL 数据，再启动 Worker。

## 查看状态

```bash
docker compose --env-file .env.prod \
  -p frux-prod \
  -f docker-compose.prod.yml \
  --profile worker \
  ps
```

查看 API 和 Worker 日志：

```bash
docker compose --env-file .env.prod \
  -p frux-prod \
  -f docker-compose.prod.yml \
  --profile worker \
  logs --tail=200 api worker
```

容器正常后，打开：

```text
https://你的域名
```

## 数据和备份

数据库、Redis、Kafka、上传兼容目录、Caddy 证书和 PostgreSQL 备份都保存在 Docker volume 中。

不要在服务器上执行：

```bash
docker compose -p frux-prod -f docker-compose.prod.yml down -v
```

这个命令会删掉数据库和其他持久数据。

查看已经生成的 PostgreSQL 备份：

```bash
docker run --rm \
  -v frux-prod_postgres_backups:/backups:ro \
  alpine:3.22 \
  ls -lh /backups
```

Docker volume 仍然在同一台服务器上。重要数据还要定期复制到别的机器。
