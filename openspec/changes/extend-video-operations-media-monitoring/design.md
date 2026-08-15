## Context

`/admin/videos` currently searches video lifecycle state and performs audited takedown/restoration.
It receives only the video-level `media_status`; processing-job state, error detail, elapsed time,
and Worker activity are unavailable. Operators therefore use Docker and direct SQL outside the
application.

PostgreSQL `media_processing_job` is already the authoritative processing state machine. Worker
claims and finalization are fenced by a unique owner token and unexpired lease. The media
repository, however, exposes only execution and reconciliation methods, not an admin read model or
audited retry operation. The Web page loads once and has no refresh scheduler.

## Goals / Non-Goals

**Goals:**

- Add a media-processing view inside existing video operations.
- Use plain Chinese labels in the main UI and hide infrastructure terminology by default.
- Show truthful current step and step progress for download, inspection, conversion, upload, and
  finalization.
- Refresh often while work is active without polling unnecessarily when idle or hidden.
- Provide safe, idempotent, audited single and bounded bulk retry of failed processing.
- Keep durable job fencing and Worker recovery authoritative.

**Non-Goals:**

- Display CPU, memory, container, Kafka, database, object-key, lease-token, or storage credentials.
- Provide shell execution or arbitrary SQL from the admin UI.
- Estimate an artificial overall percentage across unlike processing steps.
- Change video review, publication, takedown, or restoration semantics.
- Add WebSocket infrastructure solely for this page.

## Decisions

### Extend the existing video operations route with two views

`/admin/videos` keeps one permission boundary and adds “视频列表” and “处理进度” tabs. The current
video list remains unchanged. The processing view has:

- summary cards for waiting, processing, failed, completed, and oldest waiting age;
- a bounded active-work table;
- cursor-paginated recent failure/completion history;
- an expandable diagnostic section;
- manual refresh and retry actions.

The main table uses user-facing fields:

```text
视频 | 当前状态 | 当前步骤 | 步骤进度 | 已等待/处理 | 已尝试 | 最后更新 | 操作
```

Technical identifiers, profile version, stored error code, and bounded raw diagnostic are visible
only under “诊断信息”.

Alternative: create a separate `/admin/media` application. Rejected because the operator is
managing videos, existing authorization is `content.enforce`, and a separate destination would
fragment one workflow.

### Use separate active snapshot and stable history reads

Frequently changing active rows do not mix well with cursor pagination. The API therefore separates:

- an overview response containing summary plus at most 100 active waiting/processing rows;
- terminal history using stable `(completed_at DESC, id DESC)` pagination and filters.

The media persistence layer reads media jobs and assets. A narrow video catalog hydrates video ID,
title, and author in one bounded batch; handlers do not join repositories or issue SQL.

Alternative: join video and media tables directly in the HTTP handler. Rejected because it violates
layering and makes authorization/query behavior hard to test.

### Persist step progress on the durable job

Add nullable progress fields to `media_processing_job`:

- registered processing step;
- optional step progress in basis points (`0..10000`);
- progress update time.

Registered internal steps map to human labels:

| Internal step | Admin label |
| --- | --- |
| `waiting` | 等待处理 |
| `downloading` | 正在下载视频 |
| `inspecting` | 正在检查视频 |
| `remuxing` | 正在整理视频格式 |
| `transcoding` | 正在转换视频格式 |
| `uploading` | 正在上传处理结果 |
| `finalizing` | 正在完成处理 |
| `completed` | 处理完成 |
| `failed` | 处理失败 |

Download and upload progress use transferred bytes. Remux/transcode progress uses ffmpeg
`out_time_ms / source_duration_ms`. Inspection and finalization remain indeterminate rather than
showing invented percentages.

Worker owns a throttled progress reporter. It publishes step changes immediately and otherwise
persists at most once every five seconds and only after meaningful progress. Progress updates use
the same claim token and unexpired lease fence as heartbeats; a stale attempt cannot report progress.

Alternative: keep progress only in Worker memory. Rejected because deployments, page refreshes, and
multiple API instances would lose or disagree about progress.

### Use adaptive HTTP polling

The Web scheduler chooses the next refresh from the latest overview:

- 5 seconds when any item is processing;
- 10 seconds when work is waiting but none is processing;
- 30 seconds when visible work is terminal;
- no timer while `document.visibilityState !== "visible"`;
- immediate refresh when the page becomes visible, after manual refresh, and after retry.

Each refresh uses an AbortController/request generation so older responses cannot overwrite newer
state. Appended history pages are not silently replaced by overview polling.

Alternative: WebSocket or Server-Sent Events. Rejected because only a few administrators use the
page and adaptive polling is operationally simpler.

### Keep retry ownership in media and make projection repair durable

`application/media` gains an admin service and a media-owned repository contract. Retrying a failed
job:

1. locks the failed job and source asset;
2. verifies the source is not deleted and no ready result already exists;
3. resets attempts and terminal fields and schedules immediate retry;
4. records an idempotency receipt;
5. appends the success audit fact in the same PostgreSQL transaction;
6. writes a durable retry-notification outbox entry.

The outbox updates the video-facing media state to processing through the existing narrow notifier.
If projection update fails, it remains retryable without undoing the durable processing retry.

Single retry requires `Idempotency-Key`, a registered reason, and optional bounded note. Bulk retry
accepts at most 50 job IDs and processes each independently with a derived per-job idempotency key;
the response reports success, conflict, and rejection per item instead of hiding partial outcomes.

Only terminal failed jobs can be retried. Processing, waiting, completed, deleted-source, or already
requeued jobs return stable conflicts. Error classification can recommend whether retry is useful,
but the UI never claims that retry guarantees success.

### Reuse `content.enforce` and add a registered audit action

Read and retry endpoints require the existing `content.enforce` permission. Add
`media_processing.retry` targeting `media_processing_job`. Audit detail contains only job ID target,
video ID, registered reason, previous/new state, previous attempt count, route, and method. It does
not contain title, filename, object key, raw error, note, or credentials.

## Risks / Trade-offs

- **Frequent progress writes increase PostgreSQL traffic** → combine reporting with heartbeat,
  throttle to five seconds, skip insignificant changes, and never index progress percentage.
- **The UI can show stale data for several seconds** → display last-updated time and provide manual
  refresh; durable actions still use server-side state checks.
- **A retry can be scheduled while video-facing state update is delayed** → use a durable outbox and
  show processing state from the job view immediately.
- **Bulk retry can partially succeed** → return explicit per-item results and audit every successful
  retry independently.
- **Historical jobs have no progress data** → map their existing state normally and show progress as
  unavailable until a current Worker reports it.

## Migration Plan

1. Add nullable/default-safe progress and retry/outbox persistence fields and indexes.
2. Deploy API-compatible read paths before relying on Worker progress.
3. Deploy Worker progress reporting and retry-outbox dispatch.
4. Enable the processing tab and actions in the Web image.
5. Existing jobs retain their state; active jobs begin reporting progress on their next heartbeat,
   and historical terminal rows remain readable.
6. Rollback ignores additive fields; newly requeued jobs continue under the existing durable poller.

## Open Questions

None.
