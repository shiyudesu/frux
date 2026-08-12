# 雨云对象存储怎么配

Frux 使用的雨云信息已经写进配置：

```text
Endpoint: https://cn-zj1.rains3.com
Bucket: frux1
Region: us-east-1
```

你只需要在雨云控制台做下面几步。

## 1. 保持 Bucket 私有

进入雨云控制台，打开对象存储 `frux1`。

访问权限选择“私有”。不要开启整个 Bucket 公共读，更不能开启公共写。

Frux 的原视频和处理中间文件也在这个 Bucket 里，整个 Bucket 公开会把这些文件一起暴露出去。

## 2. CORS 不用配置

雨云面板目前没有Bucket级CORS设置。`cn-zj1.rains3.com` 的网关已经统一返回：

```text
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: *
Access-Control-Allow-Headers: *
Access-Control-Expose-Headers: *
```

所以浏览器直传不需要你在控制台增加规则。我们已经用 `frux1` 路径做过OPTIONS预检，返回200。

上线后如果浏览器出现跨域错误，可再次检查：

```bash
curl -i -X OPTIONS \
  "https://cn-zj1.rains3.com/frux1/uploads/cors-test" \
  -H "Origin: https://你的Frux域名" \
  -H "Access-Control-Request-Method: PUT" \
  -H "Access-Control-Request-Headers: content-type,cache-control,x-amz-checksum-sha256,x-amz-meta-sha256"
```

雨云允许所有Origin跨域调用，但上传URL仍是Frux API签发的短期单对象URL。不要泄露签名URL。

## 3. 不要开启任何公共访问

雨云公开API只提供整个Bucket的匿名访问开关，没有目录或前缀级公共读。`frux1` 必须一直保持私有，
不要开启Bucket公共读。

公开视频使用下面的流程：

```text
浏览器请求 https://你的Frux域名/media/...
        ↓
Frux确认该对象属于当前可公开视频
        ↓
返回最多60秒有效的雨云签名下载地址
        ↓
浏览器直接从雨云读取视频
```

视频字节仍由雨云提供，不会经过VPS。原视频、私密视频、审核样本和未知对象不会获得签名地址。

## 4. 填写两个 Key

在服务器的 `/opt/frux/.env.prod` 中填写：

```dotenv
FRUX_S3_ACCESS_KEY=你的AccessKey
FRUX_S3_SECRET_KEY=你的SecretKey
```

不要把真实 Key 提交到 Git，也不要发到聊天或截图里。

## 5. 最后检查

上线后确认：

- OPTIONS预检和浏览器向雨云发送的PUT请求成功。
- 已发布的 `media/*` 可以播放。
- `uploads/*` 和 `processed/*` 直接用浏览器访问时仍然提示无权限。

一个 `frux1` 只能配一套 Frux PostgreSQL 数据库。换服务器时可以继续使用原来的 Bucket，但要先恢复
对应数据库，再启动 Worker。
