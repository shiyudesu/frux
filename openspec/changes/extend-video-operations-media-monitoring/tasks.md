## 1. Durable Progress Model

- [x] 1.1 Add registered media-processing steps, optional basis-point progress, progress timestamps, and validation to the media domain
- [x] 1.2 Add processing-job progress columns plus retry receipt and retry-notification outbox models and migration registration
- [x] 1.3 Add indexes for active overview, terminal history, retry idempotency, and retry-notification dispatch
- [x] 1.4 Add migration and domain tests for existing jobs, nullable historical progress, and invalid progress values

## 2. Worker Progress Reporting

- [x] 2.1 Add a Worker-owned throttled progress reporter fenced by job ID, claim token, state, and unexpired lease
- [x] 2.2 Report bounded source-download and output-upload byte progress
- [x] 2.3 Parse ffmpeg progress for remux/transcode media-time percentage without changing ffprobe output handling
- [x] 2.4 Report inspection, finalization, completion, failure, retry, and expired-attempt step transitions truthfully
- [x] 2.5 Add unit and integration tests for throttling, progress fencing, byte progress, ffmpeg progress, and historical jobs

## 3. Media Operations Application

- [x] 3.1 Add media-admin domain query, summary, history cursor, retry command, per-item result, and stable error contracts
- [x] 3.2 Implement bounded active overview and stable terminal-history queries in media persistence
- [x] 3.3 Add a narrow video catalog and router adapter that batch-hydrates video ID, title, and author without Handler aggregation
- [x] 3.4 Implement idempotent single retry with failed-state/source checks, attempt reset, audit fact, receipt, and retry-notification outbox in one transaction
- [x] 3.5 Implement bounded bulk retry with explicit per-item outcomes and derived per-job idempotency
- [x] 3.6 Implement retry-notification outbox dispatch through the existing media-repairing notifier with bounded leases and retries
- [x] 3.7 Add application and PostgreSQL tests for filters, pagination, summaries, conflicts, idempotent replay, audit rollback, partial bulk results, and projection recovery

## 4. Admin Audit and HTTP API

- [x] 4.1 Register `media_processing.retry` and `media_processing_job` audit validation with privacy-bounded detail
- [x] 4.2 Add denied-attempt audit wiring for processing retry routes
- [x] 4.3 Add content-enforce-protected overview, history, single-retry, and bulk-retry HTTP routes and DTOs
- [x] 4.4 Add strict query/body limits, Idempotency-Key validation, registered retry reasons, and stable API error mappings
- [x] 4.5 Add API-flow tests for authorized reads, permission denial, adaptive-data response shape, retry success, conflicts, replay, and bulk outcomes

## 5. Video Operations Web

- [x] 5.1 Add typed media-processing API client, summary/history/retry types, response guards, and user-facing state mappings
- [x] 5.2 Split `/admin/videos` into “视频列表” and “处理进度” views without changing its permission boundary
- [x] 5.3 Add summary cards, active task table, stable history pagination, filters, manual refresh, last-updated time, and expandable diagnostics
- [x] 5.4 Implement visible-page adaptive polling at 5, 10, or 30 seconds with cancellation and immediate refresh after visibility/action changes
- [x] 5.5 Add single and selected bulk “重新处理” confirmation flows with registered reasons, optional note, idempotency keys, and explicit partial outcomes
- [x] 5.6 Clear processing data on authoritative 403/401 and preserve truthful loading, empty, unavailable, stale, and conflict states
- [x] 5.7 Add component and admin-shell tests for plain-language labels, polling cadence, hidden-tab pause, stale-response rejection, diagnostics, retry, and bulk partial results

## 6. Metrics and Documentation

- [x] 6.1 Add low-cardinality metrics for progress updates, overview backlog, retry outcomes, outbox backlog, and projection repair
- [x] 6.2 Update content operations, media, engineering, architecture, monitoring, UI/UX, deployment, and operations documentation

## 7. Validation

- [x] 7.1 Run targeted media, video-admin, audit, router, migration, API-flow, and Web tests
- [x] 7.2 Run Go formatting/vet, build API and Worker, lint/build Web, validate Compose, and run strict OpenSpec validation
