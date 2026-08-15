# 雨云对象存储

Frux Prod 固定使用：

```text
Endpoint  https://cn-zj1.rains3.com
Bucket    frux1
Region    us-east-1
```

你在雨云只需要做两件事：保持Bucket私有，把AccessKey和SecretKey填进服务器。

## 保持 frux1 私有

雨云控制台中，`frux1` 的匿名访问必须关闭。

不要开启整桶公共读或公共写。这个Bucket同时保存：

```text
uploads/*      用户上传的原文件
processed/*    唯一的转码结果和受保护输出
moderation/*   审核样本
media/v2/*     迁移期保留的历史公开副本；新发布不再创建
```

雨云公开API只有整桶匿名访问开关，没有目录级公共读。Frux不会依赖Bucket Policy或公开ACL。

## 填写Key

在服务器 `/opt/frux/.env.prod` 中填写：

```dotenv
FRUX_S3_ACCESS_KEY=你的AccessKey
FRUX_S3_SECRET_KEY=你的SecretKey
```

不要把真实Key提交到Git、发到聊天或放进截图。

## CORS

雨云面板没有Bucket级CORS设置，不需要配置。

`cn-zj1.rains3.com` 网关当前统一返回：

```text
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: *
Access-Control-Allow-Headers: *
Access-Control-Expose-Headers: *
```

浏览器上传权限不靠CORS限制，而是靠Frux API签发的短期、单对象签名URL。签名URL在过期前相当于临时
凭证，不要泄露。

需要排查跨域时：

```bash
curl -i -X OPTIONS \
  "https://cn-zj1.rains3.com/frux1/uploads/cors-test" \
  -H "Origin: https://你的Frux域名" \
  -H "Access-Control-Request-Method: PUT" \
  -H "Access-Control-Request-Headers: content-type,cache-control,x-amz-checksum-sha256,x-amz-meta-sha256"
```

正常响应包含状态码200和上述 `Access-Control-*` 响应头。

## 上传和播放

上传：

```text
浏览器请求上传会话
    ↓
API签发雨云PUT URL
    ↓
浏览器直传雨云
    ↓
API校验大小、类型和SHA-256
```

公开视频：

```text
浏览器请求 https://你的Frux域名/media/...
    ↓
Frux校验v3 generation、variant和视频当前公开资格
    ├─ MPD和HEAD：Frux返回
    └─ MP4和DASH分片：可缓存25分钟的307，目标为30分钟雨云签名GET
```

视频字节由雨云发送，不经过VPS。新视频只有一个 `processed/*` 文件；发布、下架和恢复只修改数据库，
不会再下载并上传公开副本。历史 `media/v2/*` 仅在 protected counterpart 缺失时读取一次进行修复，
随后至少保留30分钟再延迟清理。原文件、私密视频、审核样本和未知对象不会获得公开下载签名。

## 上线检查

- 浏览器直传PUT成功。
- 视频处理完成后可以播放。
- Range拖动和HEAD请求正常。
- 同一v3地址重复请求可复用307/签名地址；恢复发布后URL generation发生变化。
- 直接匿名访问雨云中的 `uploads/*`、`processed/*` 和 `media/*` 均返回无权限。

一个 `frux1` 只能对应一套Frux PostgreSQL。换服务器时先恢复数据库，再启动Worker。
