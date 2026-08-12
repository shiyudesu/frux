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

## 3. `media/*` 公开读取需要先验证

雨云面板目前没有Bucket Policy入口。`cn-zj1.rains3.com` 会响应S3的 `?policy` 请求，但未登录时只会
返回403，这不能证明雨云允许你的账号写入Bucket Policy。

在确认之前：

- `frux1` 保持私有。
- 不要点击整个Bucket公共读。
- 不要把下面的策略当成已经支持。
- Prod公开视频播放仍属于上线阻塞项。

需要使用Bucket所有者的AccessKey，通过S3 API执行：

```bash
aws --endpoint-url https://cn-zj1.rains3.com \
  s3api get-bucket-policy \
  --bucket frux1
```

结果判断：

| 结果 | 含义 |
| --- | --- |
| `NoSuchBucketPolicy` | 接口可用，只是还没有策略 |
| 返回Policy JSON | 接口可用，Bucket已有策略 |
| `NotImplemented` | 雨云未开放该能力 |
| 使用Bucket所有者Key仍返回 `AccessDenied` | 凭据权限不足或雨云限制了该接口 |

只有确认 `get-bucket-policy` 可用后，才尝试应用：

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

应用命令：

```bash
aws --endpoint-url https://cn-zj1.rains3.com \
  s3api put-bucket-policy \
  --bucket frux1 \
  --policy file://rainyun-media-policy.json
```

应用后必须实际验证：

```text
media/*       匿名访问成功
uploads/*     匿名访问失败
processed/*   匿名访问失败
moderation/*  匿名访问失败
```

如果Policy接口不可用，只能改用支持前缀限制的CDN、对象级ACL或拆分公有/私有Bucket。当前Frux没有
实现这些替代方案，不能退化成整个 `frux1` 公共读。

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
