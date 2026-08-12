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

## 3. 只公开 `media/*`

Bucket 仍然保持私有，只让已经发布的视频文件可以被浏览器读取。

如果雨云控制台有“Bucket 策略”或“自定义策略”，填入：

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": "*",
      "Action": "s3:GetObject",
      "Resource": "arn:aws:s3:::frux1/media/*"
    }
  ]
}
```

配置后应该是：

```text
media/*       可以公开读取
uploads/*     私有
processed/*   私有
moderation/*  私有
```

如果控制台只有“整个 Bucket 公共读”按钮，不要点。需要通过 S3 Bucket Policy 或只开放
`media/*` 的 CDN 来处理。

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
