# 隔离压测环境

这套环境用于在现有 8 核 24 GiB、50 GiB 磁盘的 NAT 主机上测试 Feed、缓存、Kafka
和关注流分发。默认复用当前线上API镜像的不可变Digest；需要验证尚未发布的代码修复时，可以基于该
Digest构建只替换API/Worker二进制的benchmark运行镜像。两种模式都不复用 `frux-prod` 的
PostgreSQL、Redis、Kafka、媒体Volume、推荐日志或多模态凭据。

## 隔离边界

- Compose project 固定为 `frux-benchmark`，不会操作 `frux-prod` 容器和 Volume。
- PostgreSQL、Redis、Kafka、API uploads 和 Prometheus 都使用独立命名 Volume。
- API 和可选 Prometheus 只绑定主机 `127.0.0.1`，不需要新增 NAT 公网端口。
- k6 优先使用挂载盘中的静态二进制访问回环 API；容器方式作为备用。两种方式都不会拉取
  视频文件，也不经过公网带宽。
- 不启动 `multimodal-provider`，不传入 `DASHSCOPE_API_KEY` 或 Provider Endpoint。
- API/Worker 显式关闭视频向量、Query Embedding、Hybrid Search、Similar Videos 和
  Session Semantic Recommendation。
- Kafka 只保留两小时，Prometheus 最多保留 48 小时/512 MB，容器日志轮转为 3 x 10 MB。
- 默认资源上限合计约 4.5 CPU 和 11 GiB（不含短时 k6/seed），避免压垮现网栈。

因此这套环境测到的是“同一台 8 核 24 GiB 服务器上，受声明资源上限约束的隔离压测栈”，
简历和报告不能把结果表述成独占整机 8 核 24 GiB。

## 安装

服务器不需要 Clone 仓库。将以下文件按原目录结构上传到一个临时目录：

```text
apps/docker-compose.benchmark.yml
apps/api/configs/config.benchmark.yaml
apps/benchmark/prometheus.yml
apps/benchmark/Dockerfile.runtime
apps/benchmark/seed.sql
apps/benchmark/following_index.sql
apps/benchmark/k6/feed.js
apps/benchmark/summarize-fanout-ab.mjs
apps/benchmark/summarize-feed-cache.mjs
scripts/benchmark-deploy.sh
```

部署脚本默认从 `/opt/frux/current/apps/.env.release` 读取线上正在运行的不可变 API 镜像
Digest，只复制镜像引用，不读取或复制 `.env.prod`。

在临时目录的仓库根路径执行：

```bash
sudo scripts/benchmark-deploy.sh start
```

服务器当前 Docker Root 位于 `/data/data1/docker`。部署脚本默认把配置、结果和运行目录放在
同一块 50 GiB 挂载盘 `/data/data1/frux-benchmark`；Docker 命名 Volume 也由
`/data/data1/docker` 承载，不会把 benchmark 数据写入系统盘。

第一次运行会创建：

```text
/data/data1/frux-benchmark/.env.benchmark
/data/data1/frux-benchmark/.env.release
/data/data1/frux-benchmark/apps/...
/data/data1/frux-benchmark/results/
/data/data1/frux-benchmark/benchmark-deploy.sh
```

其中 `.env.benchmark` 使用自动生成的独立密码和签名密钥，权限为 `0600`。

验证状态和模型隔离：

```bash
sudo /data/data1/frux-benchmark/benchmark-deploy.sh status
sudo /data/data1/frux-benchmark/benchmark-deploy.sh verify
```

如果需要验证本地尚未发布的修复，先在可信构建机编译静态 `frux-api` 和 `frux-worker`，上传到：

```text
/data/data1/frux-benchmark/bin/frux-api
/data/data1/frux-benchmark/bin/frux-worker
```

随后构建可追溯的派生运行镜像。脚本会把镜像Tag写入只属于benchmark的 `.env.runtime`，不会修改
线上 `.env.release`：

```bash
sudo /data/data1/frux-benchmark/benchmark-deploy.sh build-runtime
```

核心压测环境不依赖 Prometheus 镜像。需要时间序列面板时再启动可选监控 Profile：

```bash
sudo /data/data1/frux-benchmark/benchmark-deploy.sh start-monitoring
```

## 造数

默认数据规模：

```text
12,000 用户
500 作者
50,000 视频
每用户 30 个基础关注关系
作者 2：大于 10,000 粉丝
作者 3：约 5,000 粉丝
```

执行：

```bash
sudo /data/data1/frux-benchmark/benchmark-deploy.sh seed
```

数据全部写入 benchmark PostgreSQL。Seed 随后为 `bench_viewer` 生成独立 Redis Inbox，
并为大 V 作者 2 生成 Author Outbox，因此 `following` 压测会覆盖真实的推拉归并读取，而不是
数据库兜底路径。视频使用 `benchmark.invalid` 占位 URL，k6 只请求 Feed JSON，不下载媒体内容。
Seed 是幂等的；如果调大规模会补充缺少的数据。如果需要缩小数据集，应销毁独立 benchmark
Volume 后重新 Seed，不能对现网数据库执行清理。

造数规模可以在 `/data/data1/frux-benchmark/.env.benchmark` 中调整：

```dotenv
FRUX_BENCH_SEED_USERS=12000
FRUX_BENCH_SEED_AUTHORS=500
FRUX_BENCH_SEED_VIDEOS=50000
FRUX_BENCH_SEED_FOLLOWS_PER_USER=30
```

## Feed 压测

命令格式：

```text
benchmark-deploy.sh run-feed <scene> <vus> <duration> <limit>
```

示例：

```bash
sudo /data/data1/frux-benchmark/benchmark-deploy.sh run-feed timeline 20 60s 20
sudo /data/data1/frux-benchmark/benchmark-deploy.sh run-feed timeline 50 120s 20
sudo /data/data1/frux-benchmark/benchmark-deploy.sh run-feed hot 50 120s 20
sudo /data/data1/frux-benchmark/benchmark-deploy.sh run-feed following 50 120s 20
sudo /data/data1/frux-benchmark/benchmark-deploy.sh run-feed recommend 20 60s 20
```

`following` 和 `recommend` 使用隔离数据库中的 `bench_viewer` 登录。密码只保存在
`/data/data1/frux-benchmark/.env.benchmark`。

k6 结果写入：

```text
/data/data1/frux-benchmark/results/feed-<scene>-vu<vus>-<duration>-<timestamp>.json
```

完整命令还支持运行标签和首屏比例：

```text
benchmark-deploy.sh run-feed <scene> <vus> <duration> <limit> <label> <first-page-percent>
```

`first-page-percent` 支持20、80和100，默认保持80%首屏、20%游标页。脚本分别记录首屏/游标
延迟，并校验跨页重复和发布时间顺序。`recommend` 会写入隔离数据库中的推荐日志和
候选证据，不会影响线上推荐数据，也不会调用外部向量模型。

Timeline Redis分层缓存完整A/B：

```bash
sudo /data/data1/frux-benchmark/benchmark-deploy.sh run-feed-cache-suite
```

该套件依次执行3轮缓存关闭、逐条GET和批量MGET对照，3轮10秒MGET短窗口对照，page size
10/50/100矩阵，以及3轮冷/预热缓存突发。每轮只删除独立Redis中的 `feed:page:*`、
`video:card:*` 和 `video:stat:*`，并在结束后恢复 `batch` 模式。单独切换和预热某种模式：

```bash
sudo /data/data1/frux-benchmark/benchmark-deploy.sh prepare-feed-cache batch true 20
sudo /data/data1/frux-benchmark/benchmark-deploy.sh run-feed-mget-short 3
```

每个Feed JSON结果均有同名的Redis、PostgreSQL和API指标前后快照：

```text
-redis-before.txt / -redis-after.txt
-postgres-before.txt / -postgres-after.txt
-api-before.prom / -api-after.prom
```

本地汇总缓存A/B结果：

```bash
node apps/benchmark/summarize-feed-cache.mjs /path/to/results
```

Kafka Fanout 突发测试使用挂载盘中的 `bin/benchmark-publish`：

```bash
sudo /data/data1/frux-benchmark/benchmark-deploy.sh run-fanout 100 5
```

含义是发布100条隔离的 `video-published` 事件，每5条中有1条来自大 V。结果记录发布速率、
生产Ack延迟、峰值Consumer Lag、积压恢复时间、事件到Redis索引可见延迟、Worker耗时、实际
Redis写命令和最终索引条目对账。

全写扩散与推拉结合的配对A/B：

```bash
sudo /data/data1/frux-benchmark/benchmark-deploy.sh \
  run-fanout-ab 100 5 20 60s 20 3
```

脚本保留作者2真实的11,999条粉丝关系，只把隔离数据库中的路由粉丝数临时设置为9,999或恢复为
真实值，从而让同一工作负载分别走全写扩散和推拉结合。每组开始前只执行benchmark Redis的
`FLUSHDB`，结束后恢复推拉路由并保留最后一组hybrid索引。严禁把该动作改成对生产Redis执行。

本地汇总已下载的结果：

```bash
node apps/benchmark/summarize-fanout-ab.mjs /path/to/results
```

## Prometheus

Prometheus 仅监听服务器回环地址：

```text
http://127.0.0.1:29090
```

需要从本机访问时，通过 NAT SSH 高端口建立隧道：

```bash
ssh -p <SSH公网高端口> \
  -L 29090:127.0.0.1:29090 \
  -L 28081:127.0.0.1:28081 \
  <user>@<NAT公网地址>
```

推荐查询：

```promql
histogram_quantile(
  0.99,
  sum(rate(frux_feed_request_duration_seconds_bucket[2m])) by (le, scene)
)
```

```promql
sum(rate(frux_feed_cache_requests_total{result="hit"}[2m])) by (area)
/
sum(rate(frux_feed_cache_requests_total{result=~"hit|miss"}[2m])) by (area)
```

```promql
max_over_time(frux_kafka_consumer_workflow_lag{group="feed_video_published_active"}[5m])
```

确认没有模型调用：

```promql
sum(frux_multimodal_provider_calls_total)
```

结果应保持为 0 或无时间序列。

## 启停与清理

停止容器但保留数据：

```bash
sudo /data/data1/frux-benchmark/benchmark-deploy.sh stop
```

移除容器和网络但保留 Volume：

```bash
sudo /data/data1/frux-benchmark/benchmark-deploy.sh down
```

只有明确需要重置全部 benchmark 数据时才删除独立 Volume：

```bash
sudo env FRUX_BENCH_CONFIRM_DESTROY=frux-benchmark \
  /data/data1/frux-benchmark/benchmark-deploy.sh destroy
```

该命令只针对 Compose project `frux-benchmark`，不会删除 `frux-prod` Volume。执行后数据不可恢复，
但可重新运行 `start` 和 `seed` 生成测试数据。

## 资源和磁盘检查

测试前后检查：

```bash
df -h
docker system df
docker stats --no-stream
```

不要在压测前运行全局 `docker system prune`。生产部署器会管理自身历史镜像；benchmark 数据由
本脚本按项目边界单独管理。
