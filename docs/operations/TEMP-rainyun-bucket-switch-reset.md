# 临时操作手册：切换雨云 Bucket 并重置 Frux Prod

> 本文件用于当前一次性迁移，完成后可删除。
>
> 适用前提：Prod 目前没有真实用户，现有账号、视频、互动、消息和推荐数据均可放弃；新 Bucket
> 为空；旧 Bucket 暂时保留，不尝试迁移旧视频。

## 1. 最终结果

执行完成后：

- Prod 使用新的雨云 Bucket、AccessKey 和 SecretKey。
- PostgreSQL、Redis、Kafka 和本地兼容上传目录从空状态重新创建。
- PostgreSQL 备份 Volume 和手工备份文件保留。
- 旧 Bucket 不再被新服务使用，但暂时不删除，方便回滚。
- 新上传视频使用单次最终对象上传、数据库公开 exposure 和 25/30 分钟 HTTP 缓存。

不要只修改 AccessKey/SecretKey 后直接重启。数据库中的媒体对象键不包含 Bucket 身份；旧数据库接到
空的新 Bucket 后，所有旧视频都会指向不存在的对象。

## 2. 重要限制

本手册假设新实例继续使用：

```text
Endpoint  https://cn-zj1.rains3.com
Region    us-east-1
```

如果新实例的 Endpoint 不同，先停止操作并同步修改
`apps/api/configs/config.prod.yaml` 的 endpoint 和 presign endpoint。

以下命令会永久删除当前 Prod 的 PostgreSQL、Redis、Kafka 和 `api_uploads` Volume。执行删除步骤后，
只能通过本手册生成的 PostgreSQL 备份恢复业务数据库。

**不会删除：**

- 旧雨云 Bucket。
- 新雨云 Bucket。
- `frux-prod_postgres_backups` Volume。
- `/opt/frux/manual-backups` 下的手工备份。

## 3. 阶段一：合并并部署支持新 Bucket 的版本

### 3.1 在开发机合并分支

在仓库根目录执行：

```bash
cd /home/shiyu/Frux
git switch feat/reduce-media-egress
git pull --ff-only

gh pr view feat/reduce-media-egress --json url,state \
  || gh pr create \
    --base main \
    --head feat/reduce-media-egress \
    --title "feat(media): reduce object storage egress" \
    --body "Reduce Rainyun media egress and make the Prod Bucket configurable."

gh pr checks feat/reduce-media-egress --watch
gh pr merge feat/reduce-media-egress --merge
```

如果 Pull Request 已经合并，跳过本节。

### 3.2 在服务器提前补齐当前 Bucket 变量

新版本要求 `/opt/frux/.env.prod` 存在 `FRUX_S3_BUCKET`。部署新版本前先写入当前旧 Bucket，避免自动
部署因为缺少变量失败。

```bash
sudo -i

cp -a /opt/frux/.env.prod \
  "/opt/frux/.env.prod.before-bucket-variable-$(date -u +%Y%m%dT%H%M%SZ)"

if ! grep -q '^FRUX_S3_BUCKET=' /opt/frux/.env.prod; then
  printf '\nFRUX_S3_BUCKET=frux1\n' >> /opt/frux/.env.prod
fi

grep '^FRUX_S3_BUCKET=' /opt/frux/.env.prod
exit
```

此时不要填写新 Bucket 的密钥，正在运行的旧服务仍应保持原配置。

### 3.3 发布并部署新版本

1. 等 main 的 CI 通过。
2. 打开 GitHub Actions 的 `Publish Prod`。
3. 批准 `production` Environment。
4. 在服务器立即触发部署：

```bash
sudo systemctl start frux-deploy.service
sudo journalctl -u frux-deploy.service -n 200 -o cat
```

确认部署成功：

```bash
sudo -i

release=$(readlink -f /opt/frux/current)
test -n "$release"
test -f "$release/apps/docker-compose.prod.yml"

grep -F 'bucket: "${FRUX_S3_BUCKET}"' \
  "$release/apps/api/configs/config.prod.yaml"

set -a
. /opt/frux/.env.prod
set +a

frux_compose() {
  docker compose \
    --env-file /opt/frux/.env.prod \
    --env-file "$release/apps/.env.release" \
    -p frux-prod \
    -f "$release/apps/docker-compose.prod.yml" \
    "$@"
}

frux_compose ps
curl -fsS "http://127.0.0.1:${FRUX_API_PORT:-18081}/health"

api_id=$(frux_compose ps -q api)
docker inspect \
  -f '{{range .Config.Env}}{{println .}}{{end}}' \
  "$api_id" |
  grep '^FRUX_S3_BUCKET=frux1$'

exit
```

只有上述检查全部通过，才继续重置数据。

## 4. 阶段二：生成独立手工备份

### 4.1 停止自动部署定时器

```bash
sudo systemctl stop frux-deploy.timer
sudo systemctl stop frux-deploy.service || true
systemctl is-active frux-deploy.timer || true
```

### 4.2 进入 root 运维 Shell

```bash
sudo -i
set -Eeuo pipefail

FRUX_ROOT=/opt/frux
release=$(readlink -f "$FRUX_ROOT/current")
test -n "$release"

set -a
. "$FRUX_ROOT/.env.prod"
set +a

frux_compose() {
  docker compose \
    --env-file "$FRUX_ROOT/.env.prod" \
    --env-file "$release/apps/.env.release" \
    -p frux-prod \
    -f "$release/apps/docker-compose.prod.yml" \
    "$@"
}

frux_compose ps
```

后续服务器命令均在这个 root Shell 中执行。

### 4.3 导出 PostgreSQL

```bash
install -d -m 700 "$FRUX_ROOT/manual-backups"

timestamp=$(date -u +%Y%m%dT%H%M%SZ)
backup="$FRUX_ROOT/manual-backups/frux-before-bucket-switch-$timestamp.dump"

frux_compose exec -T postgres \
  pg_dump \
  -U "$FRUX_POSTGRES_USER" \
  -d "$FRUX_POSTGRES_DATABASE" \
  -Fc > "$backup"

test -s "$backup"
if command -v pg_restore >/dev/null 2>&1; then
  pg_restore --list "$backup" >/dev/null
else
  cat "$backup" |
    frux_compose exec -T postgres pg_restore --list >/dev/null
fi
sha256sum "$backup" | tee "$backup.sha256"
ls -lh "$backup" "$backup.sha256"
```

记录备份路径：

```bash
printf 'BACKUP=%s\n' "$backup"
```

## 5. 阶段三：停止当前 Prod 并修改新 Bucket

### 5.1 停止完整 Compose

```bash
frux_compose --profile worker down

remaining=$(
  docker ps -aq \
    --filter 'label=com.docker.compose.project=frux-prod'
)
test -z "$remaining"
```

这里不要加 `-v`。

### 5.2 备份并编辑 Prod 环境文件

```bash
env_backup="$FRUX_ROOT/.env.prod.before-bucket-switch-$timestamp"
cp -a "$FRUX_ROOT/.env.prod" "$env_backup"
chmod 600 "$env_backup"

editor "$FRUX_ROOT/.env.prod"
```

只修改以下三项：

```dotenv
FRUX_S3_ACCESS_KEY=新实例AccessKey
FRUX_S3_SECRET_KEY=新实例SecretKey
FRUX_S3_BUCKET=新Bucket名
```

不要修改 JWT、HMAC、Internal Token、PostgreSQL 或 Redis 密码。

重新加载环境：

```bash
unset FRUX_S3_ACCESS_KEY FRUX_S3_SECRET_KEY FRUX_S3_BUCKET
set -a
. "$FRUX_ROOT/.env.prod"
set +a

test -n "$FRUX_S3_ACCESS_KEY"
test -n "$FRUX_S3_SECRET_KEY"
test -n "$FRUX_S3_BUCKET"
printf 'New Bucket: %s\n' "$FRUX_S3_BUCKET"

frux_compose config --quiet
```

不要输出完整 `.env.prod`，也不要把密钥复制到聊天或截图。

## 6. 阶段四：验证新雨云 Bucket

本节使用一次性 AWS CLI 容器，不在服务器保存雨云密钥。

```bash
export AWS_ACCESS_KEY_ID="$FRUX_S3_ACCESS_KEY"
export AWS_SECRET_ACCESS_KEY="$FRUX_S3_SECRET_KEY"
export AWS_DEFAULT_REGION=us-east-1
export AWS_EC2_METADATA_DISABLED=true

aws_s3() {
  docker run --rm -i \
    -e AWS_ACCESS_KEY_ID \
    -e AWS_SECRET_ACCESS_KEY \
    -e AWS_DEFAULT_REGION \
    -e AWS_EC2_METADATA_DISABLED \
    amazon/aws-cli:2 \
    --endpoint-url https://cn-zj1.rains3.com \
    "$@"
}
```

### 6.1 检查访问权限

```bash
aws_s3 s3api head-bucket --bucket "$FRUX_S3_BUCKET"
```

命令无输出且退出码为 0 表示凭据和 Bucket 正确。

### 6.2 确认新 Bucket 为空

```bash
object_count=$(
  aws_s3 s3api list-objects-v2 \
    --bucket "$FRUX_S3_BUCKET" \
    --max-keys 10 \
    --query 'KeyCount' \
    --output text
)

printf 'Object count: %s\n' "$object_count"
test "$object_count" = "0"
```

如果不是 0，立即停止，不要删除本地数据卷。先确认是否选错 Bucket。

### 6.3 验证 PUT、HEAD 和 DELETE

```bash
probe_key="frux-switch-probe-$timestamp.txt"

printf 'frux bucket switch probe\n' |
  aws_s3 s3 cp - "s3://$FRUX_S3_BUCKET/$probe_key" \
    --content-type text/plain

aws_s3 s3api head-object \
  --bucket "$FRUX_S3_BUCKET" \
  --key "$probe_key"

aws_s3 s3api delete-object \
  --bucket "$FRUX_S3_BUCKET" \
  --key "$probe_key"

after_probe=$(
  aws_s3 s3api list-objects-v2 \
    --bucket "$FRUX_S3_BUCKET" \
    --max-keys 10 \
    --query 'KeyCount' \
    --output text
)

test "$after_probe" = "0"

unset AWS_ACCESS_KEY_ID
unset AWS_SECRET_ACCESS_KEY
unset AWS_DEFAULT_REGION
unset AWS_EC2_METADATA_DISABLED
unset -f aws_s3
```

只有新 Bucket 权限验证和空桶检查全部通过，才执行下一节。

## 7. 阶段五：删除旧本地状态

### 7.1 再次确认备份和容器状态

```bash
test -s "$backup"
test -f "$backup.sha256"
sha256sum -c "$backup.sha256"

remaining=$(
  docker ps -aq \
    --filter 'label=com.docker.compose.project=frux-prod'
)
test -z "$remaining"

docker volume inspect frux-prod_postgres_backups >/dev/null
```

### 7.2 检查将要删除的 Volume

```bash
reset_volumes=(
  frux-prod_postgres_data
  frux-prod_redis_data
  frux-prod_kafka_data
  frux-prod_api_uploads
)

for volume in "${reset_volumes[@]}"; do
  if docker volume inspect "$volume" >/dev/null 2>&1; then
    docker volume inspect \
      -f 'DELETE {{.Name}} created={{.CreatedAt}} mount={{.Mountpoint}}' \
      "$volume"
  else
    printf 'SKIP missing volume %s\n' "$volume"
  fi
done

printf 'PRESERVE frux-prod_postgres_backups\n'
printf 'PRESERVE %s\n' "$backup"
```

核对输出后，执行删除：

```bash
for volume in "${reset_volumes[@]}"; do
  if docker volume inspect "$volume" >/dev/null 2>&1; then
    docker volume rm "$volume"
  fi
done
```

验证：

```bash
for volume in "${reset_volumes[@]}"; do
  if docker volume inspect "$volume" >/dev/null 2>&1; then
    printf 'Volume was not deleted: %s\n' "$volume" >&2
    exit 1
  fi
done

docker volume inspect frux-prod_postgres_backups >/dev/null
```

## 8. 阶段六：使用新 Bucket 启动空白 Prod

当前 `/opt/frux/current` 必须已经是支持 `FRUX_S3_BUCKET` 和媒体流量优化的新版本。

```bash
grep -F 'bucket: "${FRUX_S3_BUCKET}"' \
  "$release/apps/api/configs/config.prod.yaml"

frux_compose config --quiet
frux_compose --profile worker up -d
```

等待容器健康：

```bash
for attempt in $(seq 1 60); do
  if frux_compose ps --format json |
    grep -q '"Health":"unhealthy"'; then
    frux_compose ps
    exit 1
  fi

  if curl -fsS \
    "http://127.0.0.1:${FRUX_API_PORT:-18081}/health" |
    grep -q '"ready":true'; then
    break
  fi

  sleep 5
done

curl -fsS "http://127.0.0.1:${FRUX_API_PORT:-18081}/health"
frux_compose ps
```

查看启动日志：

```bash
frux_compose logs --no-color --tail=200 api
frux_compose logs --no-color --tail=200 worker
frux_compose logs --no-color --tail=100 postgres
frux_compose logs --no-color --tail=100 kafka
```

确认 API 和 Worker 使用新 Bucket：

```bash
api_id=$(frux_compose ps -q api)
worker_id=$(frux_compose ps -q worker)

for container in "$api_id" "$worker_id"; do
  docker inspect \
    -f '{{range .Config.Env}}{{println .}}{{end}}' \
    "$container" |
    grep "^FRUX_S3_BUCKET=$FRUX_S3_BUCKET$"
done
```

确认数据库为空且迁移已完成：

```bash
frux_compose exec -T postgres \
  psql \
  -U "$FRUX_POSTGRES_USER" \
  -d "$FRUX_POSTGRES_DATABASE" \
  -v ON_ERROR_STOP=1 <<'SQL'
SELECT 'account' AS table_name, COUNT(*) FROM account
UNION ALL
SELECT 'video', COUNT(*) FROM video
UNION ALL
SELECT 'media_asset', COUNT(*) FROM media_asset
UNION ALL
SELECT 'media_variant', COUNT(*) FROM media_variant
UNION ALL
SELECT 'media_processing_job', COUNT(*) FROM media_processing_job;
SQL
```

各业务表应为 0。迁移记录、系统配置或内部表不要求为空。

确认 Worker 指标可读取：

```bash
docker exec "$worker_id" \
  wget -qO- http://127.0.0.1:9091/metrics |
  grep -E 'frux_kafka_broker_healthy|frux_kafka_consumer_workflow_healthy'
```

## 9. 阶段七：浏览器完整验证

1. 注册一个新的测试账号。
2. 上传一张封面和一个短 MP4。
3. 等待视频状态从“处理中”变为“待审核”或“已完成”。
4. 在后台批准视频。
5. 确认 Feed、详情页和作者主页能够播放。
6. 拖动播放进度，确认 Range 请求正常。
7. 下架视频，确认新页面请求不再获得播放地址。
8. 恢复视频，确认媒体 URL 中的 v3 generation 发生变化。

查看媒体处理任务：

```bash
frux_compose exec -T postgres \
  psql \
  -U "$FRUX_POSTGRES_USER" \
  -d "$FRUX_POSTGRES_DATABASE" \
  -x -c "
SELECT v.id, v.title, j.state, j.attempts, j.error_code,
       j.error_message, j.processing_step, j.progress_bps, j.updated_at
FROM media_processing_job j
LEFT JOIN video v ON v.media_asset_id = j.asset_id
ORDER BY j.updated_at DESC
LIMIT 20;"
```

## 10. 阶段八：检查新 Bucket 对象形状

重新创建只在当前 Shell 生效的 AWS CLI 包装：

```bash
export AWS_ACCESS_KEY_ID="$FRUX_S3_ACCESS_KEY"
export AWS_SECRET_ACCESS_KEY="$FRUX_S3_SECRET_KEY"
export AWS_DEFAULT_REGION=us-east-1
export AWS_EC2_METADATA_DISABLED=true

aws_s3() {
  docker run --rm -i \
    -e AWS_ACCESS_KEY_ID \
    -e AWS_SECRET_ACCESS_KEY \
    -e AWS_DEFAULT_REGION \
    -e AWS_EC2_METADATA_DISABLED \
    amazon/aws-cli:2 \
    --endpoint-url https://cn-zj1.rains3.com \
    "$@"
}
```

列出对象：

```bash
aws_s3 s3api list-objects-v2 \
  --bucket "$FRUX_S3_BUCKET" \
  --query 'Contents[].{Key:Key,Size:Size}' \
  --output table
```

新上传正常情况下应出现：

```text
uploads/.../source.mp4
uploads/.../source.jpg
processed/.../<checksum>/source.mp4
```

不应为新视频出现：

```text
tmp/media/*
media/v2/*
```

检查：

```bash
tmp_count=$(
  aws_s3 s3api list-objects-v2 \
    --bucket "$FRUX_S3_BUCKET" \
    --prefix 'tmp/media/' \
    --query 'KeyCount' \
    --output text
)

v2_count=$(
  aws_s3 s3api list-objects-v2 \
    --bucket "$FRUX_S3_BUCKET" \
    --prefix 'media/v2/' \
    --query 'KeyCount' \
    --output text
)

printf 'tmp/media objects: %s\n' "$tmp_count"
printf 'new media/v2 objects: %s\n' "$v2_count"
test "$tmp_count" = "0"
test "$v2_count" = "0"
```

查看出站字节指标：

```bash
docker exec "$api_id" \
  wget -qO- http://127.0.0.1:8080/metrics |
  grep '^frux_media_object_outbound_bytes_total'
```

清理临时 AWS 环境：

```bash
unset AWS_ACCESS_KEY_ID
unset AWS_SECRET_ACCESS_KEY
unset AWS_DEFAULT_REGION
unset AWS_EC2_METADATA_DISABLED
unset -f aws_s3
```

## 11. 恢复自动部署

全部验证通过后：

```bash
exit

sudo systemctl enable --now frux-deploy.timer
systemctl list-timers frux-deploy.timer
sudo journalctl -u frux-deploy.service -n 100 -o cat
```

至少保留旧 Bucket、环境文件备份和手工数据库备份 7 天。确认新视频持续正常、雨云新实例出站计量符合
预期后，再手工删除旧 Bucket。

## 12. 回滚

如果已经退出之前的 root Shell，先重新建立变量和 Compose 函数：

```bash
sudo -i
set -Eeuo pipefail

FRUX_ROOT=/opt/frux
release=$(readlink -f "$FRUX_ROOT/current")

read -r -p "数据库备份完整路径: " backup
read -r -p "旧 .env.prod 备份完整路径: " env_backup

test -s "$backup"
test -f "$env_backup"

set -a
. "$FRUX_ROOT/.env.prod"
set +a

frux_compose() {
  docker compose \
    --env-file "$FRUX_ROOT/.env.prod" \
    --env-file "$release/apps/.env.release" \
    -p frux-prod \
    -f "$release/apps/docker-compose.prod.yml" \
    "$@"
}
```

### 12.1 删除 Volume 之前

如果尚未执行第 7 节，只需恢复旧环境文件并重新启动：

```bash
cp -a "$env_backup" "$FRUX_ROOT/.env.prod"

unset FRUX_S3_ACCESS_KEY FRUX_S3_SECRET_KEY FRUX_S3_BUCKET
set -a
. "$FRUX_ROOT/.env.prod"
set +a

frux_compose --profile worker up -d
```

### 12.2 删除 Volume 之后

这会放弃新建的空白数据，并恢复旧数据库。旧雨云 Bucket 的流量限制仍然存在，因此旧视频可能依旧
无法播放。

```bash
frux_compose --profile worker down

cp -a "$env_backup" "$FRUX_ROOT/.env.prod"

unset FRUX_S3_ACCESS_KEY FRUX_S3_SECRET_KEY FRUX_S3_BUCKET
set -a
. "$FRUX_ROOT/.env.prod"
set +a

for volume in \
  frux-prod_postgres_data \
  frux-prod_redis_data \
  frux-prod_kafka_data \
  frux-prod_api_uploads; do
  if docker volume inspect "$volume" >/dev/null 2>&1; then
    docker volume rm "$volume"
  fi
done

frux_compose up -d postgres redis kafka
```

等待 PostgreSQL：

```bash
for attempt in $(seq 1 60); do
  if frux_compose exec -T postgres \
    pg_isready \
    -U "$FRUX_POSTGRES_USER" \
    -d "$FRUX_POSTGRES_DATABASE" >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
```

恢复数据库：

```bash
frux_compose exec -T postgres \
  pg_restore \
  -U "$FRUX_POSTGRES_USER" \
  -d "$FRUX_POSTGRES_DATABASE" \
  --clean \
  --if-exists \
  --no-owner \
  --no-privileges < "$backup"
```

启动完整服务：

```bash
frux_compose --profile worker up -d
frux_compose ps
curl -fsS "http://127.0.0.1:${FRUX_API_PORT:-18081}/health"
```

退出 root Shell 并恢复定时器：

```bash
exit
sudo systemctl enable --now frux-deploy.timer
```
