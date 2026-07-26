# 性能测试指南

本文档说明如何用 k6、Prometheus 和 Grafana 测 GCFeed 的接口性能、QPS、P95 延迟、错误率和缓存效果。

## 前置准备

安装 k6：

```bash
brew install k6
```

启动项目：

```bash
cd apps
docker compose up -d --build
```

确认 API 正常：

```bash
curl http://127.0.0.1:8080/health
```

确认监控正常：

```bash
curl http://127.0.0.1:8080/metrics
curl http://127.0.0.1:9090/-/ready
curl http://127.0.0.1:3000/api/health
```

Grafana 面板：

```text
http://127.0.0.1:3000/d/gcfeed-overview/gcfeed-overview
```

默认账号密码：

```text
admin / admin
```

## 测最新视频流

目标：测公开视频 Feed 的 QPS、成功率、平均延迟和 P95 延迟。

```bash
SCENE=timeline VUS=20 DURATION=60s THINK_TIME=1 k6 run - <<'EOF'
import http from "k6/http";
import { check, sleep } from "k6";
import { Rate } from "k6/metrics";

const BASE_URL = (__ENV.BASE_URL || "http://127.0.0.1:8080").replace(/\/$/, "");
const SCENE = __ENV.SCENE || "timeline";
const LIMIT = Number(__ENV.LIMIT || 10);
const THINK_TIME = Number(__ENV.THINK_TIME || 1);
const successRate = new Rate("feed_success_rate");

export const options = {
  vus: Number(__ENV.VUS || 20),
  duration: __ENV.DURATION || "60s",
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<500"],
    feed_success_rate: ["rate>0.99"],
  },
};

export default function () {
  const url = `${BASE_URL}/api/feed-items?scene=${encodeURIComponent(SCENE)}&limit=${LIMIT}`;
  const res = http.get(url);
  const ok = check(res, {
    "status is 200": (r) => r.status === 200,
    "has feed items array": (r) => Array.isArray(r.json().items),
  });
  successRate.add(ok);
  sleep(THINK_TIME);
}
EOF
```

重点看：

- `http_reqs` 后面的 `/s`：QPS
- `http_req_duration avg`：平均延迟
- `http_req_duration p(95)`：P95 延迟
- `http_req_failed`：失败率
- `feed_success_rate`：业务成功率

## 测热门榜单

目标：测热榜读取链路，包括 Redis 热榜窗口和 Feed 卡片组装。

```bash
SCENE=hot VUS=20 DURATION=60s THINK_TIME=1 k6 run - <<'EOF'
import http from "k6/http";
import { check, sleep } from "k6";
import { Rate } from "k6/metrics";

const BASE_URL = (__ENV.BASE_URL || "http://127.0.0.1:8080").replace(/\/$/, "");
const SCENE = __ENV.SCENE || "hot";
const LIMIT = Number(__ENV.LIMIT || 10);
const THINK_TIME = Number(__ENV.THINK_TIME || 1);
const successRate = new Rate("feed_success_rate");

export const options = {
  vus: Number(__ENV.VUS || 20),
  duration: __ENV.DURATION || "60s",
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<500"],
    feed_success_rate: ["rate>0.99"],
  },
};

export default function () {
  const url = `${BASE_URL}/api/feed-items?scene=${encodeURIComponent(SCENE)}&limit=${LIMIT}`;
  const res = http.get(url);
  const ok = check(res, {
    "status is 200": (r) => r.status === 200,
    "has feed items array": (r) => Array.isArray(r.json().items),
  });
  successRate.add(ok);
  sleep(THINK_TIME);
}
EOF
```

## 测推荐流

推荐流需要登录态。先准备一个账号密码，再运行：

```bash
ACCOUNT="你的账号" PASSWORD="你的密码" VUS=20 DURATION=60s THINK_TIME=1 k6 run - <<'EOF'
import http from "k6/http";
import { check, sleep } from "k6";
import { Rate } from "k6/metrics";

const BASE_URL = (__ENV.BASE_URL || "http://127.0.0.1:8080").replace(/\/$/, "");
const ACCOUNT = __ENV.ACCOUNT || "";
const PASSWORD = __ENV.PASSWORD || "";
const LIMIT = Number(__ENV.LIMIT || 10);
const THINK_TIME = Number(__ENV.THINK_TIME || 1);
const successRate = new Rate("feed_success_rate");

export const options = {
  vus: Number(__ENV.VUS || 20),
  duration: __ENV.DURATION || "60s",
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<500"],
    feed_success_rate: ["rate>0.99"],
  },
};

export function setup() {
  const res = http.post(
    `${BASE_URL}/api/sessions`,
    JSON.stringify({ account: ACCOUNT, password: PASSWORD }),
    { headers: { "Content-Type": "application/json" } }
  );
  if (res.status !== 200) {
    throw new Error(`login failed: status=${res.status} body=${res.body}`);
  }
  return { token: res.json().access_token };
}

export default function (data) {
  const res = http.post(
    `${BASE_URL}/api/feed-queries`,
    JSON.stringify({
      scene: "recommend",
      limit: LIMIT,
      context: { request_id: `k6-recommend-${Date.now()}-${__VU}-${__ITER}` },
    }),
    {
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${data.token}`,
      },
    }
  );
  const ok = check(res, {
    "status is 200": (r) => r.status === 200,
    "has feed items array": (r) => Array.isArray(r.json().items),
  });
  successRate.add(ok);
  sleep(THINK_TIME);
}
EOF
```

## 测极限 QPS

普通压测会模拟用户停顿，`THINK_TIME=1` 时 20 VU 的理论 QPS 接近 20。测服务极限吞吐时，把等待时间设成 0：

```bash
SCENE=timeline VUS=50 DURATION=60s THINK_TIME=0 k6 run - <<'EOF'
import http from "k6/http";
import { check, sleep } from "k6";

const BASE_URL = (__ENV.BASE_URL || "http://127.0.0.1:8080").replace(/\/$/, "");
const SCENE = __ENV.SCENE || "timeline";
const THINK_TIME = Number(__ENV.THINK_TIME || 0);

export const options = {
  vus: Number(__ENV.VUS || 50),
  duration: __ENV.DURATION || "60s",
};

export default function () {
  const res = http.get(`${BASE_URL}/api/feed-items?scene=${SCENE}&limit=10`);
  check(res, { "status is 200": (r) => r.status === 200 });
  sleep(THINK_TIME);
}
EOF
```

逐步增加 `VUS`：

```bash
VUS=50
VUS=100
VUS=200
```

当 P95 明显升高、失败率上升或 CPU/数据库压力明显升高时，就接近当前本地环境上限。

## 如何解读结果

示例：

```text
http_reqs......................: 1200    19.85/s
http_req_duration..............: avg=5.35ms p(95)=17.96ms
http_req_failed................: 0.00%
feed_success_rate..............: 100.00%
```

含义：

- `http_reqs 19.85/s`：QPS 约 19.85
- `avg=5.35ms`：平均响应时间 5.35ms
- `p(95)=17.96ms`：95% 请求在 17.96ms 内完成
- `http_req_failed=0.00%`：HTTP 失败率为 0
- `feed_success_rate=100%`：业务检查全部通过

可写进简历：

```text
使用 k6 对 Feed 接口进行 20 VU / 60s 压测，完成 1200 次请求，吞吐量约 19.85 QPS，成功率 100%，错误率 0%，P95 延迟 17.96ms。
```

## 结合 Grafana 看指标

压测时打开：

```text
http://127.0.0.1:3000/d/gcfeed-overview/gcfeed-overview
```

重点观察：

- API QPS
- API 5xx Error Rate
- API P95 Latency
- Feed P95 Latency
- Feed Cache Hit Rate
- Upload and Video Processing P95
- Worker Success Rate

Prometheus 也可以直接查询：

```promql
sum(rate(gcfeed_http_requests_total[5m])) by (route)
histogram_quantile(0.95, sum(rate(gcfeed_http_request_duration_seconds_bucket[5m])) by (le, route))
histogram_quantile(0.95, sum(rate(gcfeed_feed_request_duration_seconds_bucket[5m])) by (le, scene))
sum(rate(gcfeed_feed_cache_requests_total{result="hit"}[5m])) by (area)
```

## 测试前的数据准备

为了让结果更接近真实场景，建议先准备：

- 20 到 50 个公开视频
- 多个用户
- 一些点赞、收藏、评论
- 至少一次推荐流访问，让推荐候选链路产生数据

当 Feed 返回空数组时，延迟指标仍然有效，业务场景说服力会弱一些。

## 测观看反馈闭环

观看事件压测需要先登录并取得公开视频 ID。单个播放会话应按真实顺序发送 `exposed -> play -> progress -> skip/complete`，每个事件使用唯一 `event_id`，同一事件重试时保持载荷不变。

重点验证：

- 相同 `event_id` 重试不会增加 `video_view_events`、曝光次数、历史更新或推荐消费次数。
- 同一 `event_id` 使用不同位置/时长时返回 409。
- `progress` 写入后观看历史的 `last_position_ms` 前进，迟到旧事件不能回退。
- `complete` 后到达的旧 `progress` 或 `skip` 不会取消完播状态。
- RabbitMQ 停止时 HTTP 仍接受事实，Outbox 积压上升；恢复后积压下降且下游只应用一次。
- 10 秒进度间隔下的写 QPS、PostgreSQL P95、Outbox 延迟和 Worker 成功率保持在目标范围。

浏览器检查必须使用 Windows 真 Chrome，覆盖自动播放、手动暂停、seek、滚轮/拖拽切换、页面隐藏、`pagehide/pageshow` 和 React Strict Mode 开发环境。网络面板中同一播放会话不应出现重复 `exposed`/`complete`，退出请求应使用 keepalive。

Feed 预加载检查同时覆盖：

- `timeline`、`hot`、`following`、`recommend` 的请求顺序与实际后续媒体请求顺序一致，Web 不再调用 `/api/preload-videos`。
- 前进后退切换会复用准备资源，活动窗口外的视频元素、监听器和请求被释放。
- DevTools 模拟 3G 时只准备下一条元数据；offline 或 save-data 时不主动请求后续视频字节；恢复网络后策略重新计算。
- 接近当前页末尾时只触发原 Feed 分页一次，追加项进入同一预加载窗口。
- 宽屏和移动视口切换后 `data-preload-resources` 不超过有效策略上限，失败只增加低基数调试计数且不让 Feed 进入错误状态。

## 生产媒体链路验证

1. 使用上传页提交代表性 360p、720p、1080p 视频，确认浏览器 PUT 直接发往 MinIO/S3，API 不承载文件正文。
2. 观察 `media_processing_job` 从 pending/processing 到 completed，输出不得上采样；同一处理事件重放不产生重复最终对象。
3. 对基线 MP4 发起 HEAD、Range 和 `If-None-Match`，确认 200/206/304、ETag 和 immutable Cache-Control；manifest 使用短缓存。
4. 处理期间公共 Feed、详情、推荐和预加载不得出现视频；基线完成后兼容 `media_url` 和有序 `playback_sources` 同时可用。
5. 删除视频后立即确认 API 不再发现播放源；缩短测试清理延迟后确认原始对象、封面、MP4、manifest 和 segment 被幂等删除。

压测时同时观察 `gcfeed_media_object_operation_duration_seconds`、`gcfeed_media_processing_results_total`、`gcfeed_media_renditions_total`、`gcfeed_media_reconciliation_issues_total` 和 `gcfeed_media_cleanup_backlog`。

## 播放遥测验证

1. 使用认证 Web 会话播放视频，确认 `/api/playback-telemetry-batches` 单批不超过 50 个事件和 64 KiB，退出请求带 keepalive。
2. 在 Windows Chrome DevTools 依次验证正常、Fast 3G、Offline、seek、暂停、后台切换和页面退出；seek 期间不得产生普通 rebuffer。
3. 支持 `requestVideoFrameCallback` 时首帧事件标记 `video_frame_callback`；禁用/模拟缺失 API 后依次验证 `advancing_time` 和 `playing` fallback。
4. 重放完全相同的 batch 返回原汇总且不重复增加质量指标；相同 batch/event ID 修改载荷返回 409。
5. 请求加入 `token`、`cookie`、完整签名 URL 或自由 metadata 时必须返回 400；Prometheus 标签不得出现用户、视频、请求或播放会话 ID。
6. 同时观察新遥测和旧 QoS 至少两个 Web 发布版本。比较首帧 p50/p95、卡顿次数/时长和上报覆盖率；趋势连续两周一致后才停止 Web 旧 QoS 主动上报。

重点指标：

```text
gcfeed_playback_first_frame_duration_seconds
gcfeed_playback_rebuffer_duration_seconds_total
gcfeed_playback_attempts_total
gcfeed_playback_telemetry_batches_total
gcfeed_playback_telemetry_events_total
gcfeed_playback_telemetry_delivery_delay_seconds
gcfeed_playback_telemetry_cleanup_runs_total
```

## 自适应播放器浏览器验证

Windows Chrome 桌面与移动视口至少覆盖：

1. 只有 legacy `media_url` 时不请求 DASH chunk，MP4 播放、seek、静音、全屏和快捷键保持兼容。
2. 有可用 manifest 时才加载 `dash.all.min` 独立 chunk；自动画质、手动画质和 0.5x-2x 速度反映有效状态。
3. manifest 失败或 DASH 网络错误时切换 MP4，位置、静音、速度和 intended-play 保持，UI 显示 fallback 而非正常播放假象。
4. previous/current/next 前后轮换时媒体元素复用，`data-preload-resources` 与 pool 均不超过 3；切换 scene、身份、代次或源 revision 后旧资源归零。
5. next slot 未达到 `buffer_ms` 时提交切换仍成功，新舞台显示 buffering；达到目标后不出现上一条媒体。
6. 连播默认关闭并保持 loop；开启后 ended 推进下一项，末项安全停留。
7. 图片项继续显示封面且没有进度、画质、速度或全屏视频控件。
