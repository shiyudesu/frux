## Why

Operators currently need server shell access and direct SQL to understand why videos are waiting,
processing, or failing. Video operations already owns discovery and enforcement, so media-processing
visibility and safe recovery should be available in the same permission-protected workspace using
plain user-facing language.

## What Changes

- Extend `/admin/videos` with separate “视频列表” and “处理进度” views.
- Add summary cards for waiting, processing, failed, completed, and oldest waiting time.
- Show video title, current processing step, step progress, elapsed time, retry count, last activity,
  and a plain-language failure reason; keep raw diagnostics inside an optional detail section.
- Persist bounded Worker progress for downloading, checking, converting, uploading, and finalizing,
  with write throttling so progress reporting does not overload PostgreSQL.
- Refresh adaptively: every 5 seconds while processing exists, 10 seconds while work is only waiting,
  30 seconds when all visible work is terminal, immediately after an operator action, and not while
  the browser tab is hidden.
- Add stable filters and cursor pagination for processing tasks.
- Add audited, idempotent single and bounded bulk “重新处理” actions for eligible failed tasks.
- Keep PostgreSQL processing jobs authoritative; the admin page does not execute shell commands or
  access database credentials.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `content-operations-console`: Add the media-processing view, human-readable states, adaptive
  refresh, details, and recovery actions to video operations.
- `durable-media-work-jobs`: Persist bounded processing-step progress and support fenced,
  operator-requested retries without weakening Worker ownership.
- `admin-audit-trail`: Register and validate successful and denied media-retry operations.

## Impact

Affected areas include media job/domain models and migrations, Worker progress reporting, media
administration application and persistence, video-title hydration, admin routes and API errors,
audit action registration, the video operations Web page, admin API types/tests, metrics, and
operations/product documentation. Existing creator and public video APIs remain compatible.
