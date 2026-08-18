# 自托管 MinIO

本文说明 NAT 主机 Prod 的私有 MinIO 运维边界。本地开发 Compose 仍使用独立的开发 MinIO；旧雨云
部署见 [雨云对象存储（旧部署）](rainyun-object-storage.md)。

## 拓扑与端点

| 用途 | 地址 | 调用方 |
| --- | --- | --- |
| 运行时 S3 | `http://minio:9000` | Compose 内的 API、Worker、初始化器 |
| 浏览器预签名 S3 | `https://FRUX_S3_DOMAIN:<public-port>` | 上传、签名下载和媒体 Range 请求 |
| 主机 MinIO API | `127.0.0.1:19000` | Caddy 的 S3 主机名反向代理 |
| 主机 MinIO Console | `127.0.0.1:19001` | 仅 SSH 隧道 |

`FRUX_DOMAIN` 与 `FRUX_S3_DOMAIN` 必须是不同的裸主机名。浏览器 Origin 和预签名 URL 都包含
`FRUX_PUBLIC_HTTPS_PORT`；API/Worker 运行时请求不经过 NAT 或 Caddy。S3 保持 path-style、非空
Region 和私有 Bucket，应用不自动创建 Bucket。

## 凭据与 Bucket 权限

Prod 使用两组互不相同的凭据：

```dotenv
FRUX_MINIO_ROOT_USER=<new-root-user>
FRUX_MINIO_ROOT_PASSWORD=<new-long-random-password>
FRUX_S3_ACCESS_KEY=<new-bucket-scoped-access-key>
FRUX_S3_SECRET_KEY=<new-bucket-scoped-secret-key>
FRUX_S3_BUCKET=<private-bucket-name>
```

- Root 凭据只供 MinIO 服务和 `minio-init` 管理使用，不注入 API 或 Worker。
- 应用凭据只允许列出 Bucket，并在 `uploads/`、`processed/`、`moderation/` 和 `media/`
  前缀下读取、写入、HEAD 和删除对象；不能修改 Bucket policy、CORS 或匿名访问。
- 初始化器可以重复运行；它创建或复用 Bucket、更新应用身份与 Bucket-scoped policy，并保持匿名访问关闭。
- 修改 `FRUX_S3_ACCESS_KEY` 时，初始化器会撤销前一个由 Frux 管理的应用身份；只修改
  `FRUX_S3_SECRET_KEY` 则更新同一身份的 Secret。
- 不在仓库、部署包、截图、命令历史或日志中保存真实凭据。

直接匿名访问 `uploads/*`、`processed/*`、`moderation/*` 和历史 `media/*` 对象都必须返回拒绝。公开
播放由 Frux 校验 v3 exposure 后签发短期 GET，不依赖公共 Bucket、公共 ACL 或物理公开副本。

## CORS

MinIO 只允许一个精确 Origin：

```text
https://FRUX_DOMAIN:<public-port>
```

策略只开放浏览器上传和播放所需的方法与头部，包括 `PUT`、`GET`、`HEAD`、`Content-Type`、
`Cache-Control`、`Range`、`x-amz-checksum-sha256` 和 `x-amz-meta-sha256`；诊断和播放所需响应头
包括 `ETag`、`Accept-Ranges`、`Content-Range`、`Content-Length` 和校验和头。不要使用 `*` Origin，
也不要遗漏公共高端口。

可从应用 Origin 验证预检：

```bash
export FRUX_APP_ORIGIN="https://FRUX_DOMAIN:<public-port>"
export FRUX_S3_ORIGIN="https://FRUX_S3_DOMAIN:<public-port>"

curl -i -X OPTIONS \
  "$FRUX_S3_ORIGIN/$FRUX_S3_BUCKET/uploads/cors-test" \
  -H "Origin: $FRUX_APP_ORIGIN" \
  -H "Access-Control-Request-Method: PUT" \
  -H "Access-Control-Request-Headers: content-type,cache-control,x-amz-checksum-sha256,x-amz-meta-sha256"
```

响应只能回显配置的应用 Origin。再用另一个 Origin 重试，响应不得授予 CORS 权限。

## Caddy 与签名请求

使用仓库模板 `deploy/caddy/frux-nat-minio.Caddyfile`。S3 站点直接代理到
`127.0.0.1:19000`，不重写 Host、path、query、method 或 Range；这些值可能参与 AWS Signature V4。
MinIO Console 不得添加 Caddy 路由。

通过 SSH 高端口临时访问 Console：

```bash
ssh -p <public-ssh-port> \
  -L 29001:127.0.0.1:19001 \
  <user>@<nat-public-address>
```

然后只在本机打开 `http://127.0.0.1:29001`。结束 SSH 会话即关闭隧道。

## 持久化、备份与恢复

`minio_data` 是媒体对象的持久卷。普通容器重建、镜像升级和不带 `-v` 的
`docker compose down` 必须保留该 Volume。绝不要在生产主机执行 `docker compose down -v`。

PostgreSQL 定时备份只覆盖业务数据和媒体元数据，不包含 MinIO 对象。上线前至少选择一种主机丢失
恢复方式：

1. 云厂商对 MinIO 数据盘执行定期快照，并记录保留期与恢复步骤。
2. 将私有 Bucket 定期镜像到独立故障域的外部 MinIO/S3，并监控复制失败。

恢复时必须使用彼此匹配的 PostgreSQL 备份和 MinIO 快照/镜像。不要让空数据库连接旧 Bucket，也
不要让一个数据库同时连接两个活动 Bucket；这会造成引用缺失、错误清理或新旧对象交叉写入。

## 验收

- 重复运行初始化器后，Bucket、应用身份、policy、CORS 和私有状态保持正确。
- 未签名对象请求返回拒绝。
- 浏览器能经 `https://FRUX_S3_DOMAIN:<public-port>` 完成视频与封面 PUT。
- API `HeadObject` 校验大小、类型、SHA-256 和 metadata 成功。
- Worker 能通过 `http://minio:9000` 下载源文件、写入并校验确定性输出。
- v3 播放保留 25 分钟 307、最长 30 分钟签名媒体缓存、Range、HEAD 和 ETag。
- 重启容器但不删除 Volume 后，对象仍可读取。
- PostgreSQL 备份成功，并明确记录 MinIO 快照或外部镜像的最近成功时间。

## 限制

这是单主机、单数据盘、非 HA 的个人演示拓扑，没有多节点 MinIO、独立媒体故障域或自动跨区恢复。
FFmpeg 和 Worker 仍是当前媒体状态机的必需部分；H.264/AAC 输入可能 stream copy，但不能关闭
FFmpeg 或绕过处理任务。
