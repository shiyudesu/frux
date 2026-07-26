## Context

The Web player measures first frame on `loadeddata`, increments a stutter counter on `waiting`, and reports elapsed wall time from first playing until flush. The backend stores one flat QoS row per report. Prometheus exposes HTTP, Feed, cache, upload, processing, and worker metrics, but not playback startup, rebuffer duration, media failure, selected source, or telemetry health.

The new telemetry must stay low overhead, avoid signed URLs and sensitive identifiers, and work with both the native and future adaptive player.

## Goals / Non-Goals

**Goals:**

- Measure actual rendered startup and detailed playback lifecycle quality.
- Batch retry-safe client telemetry with bounded volume.
- Store privacy-safe diagnostic dimensions and aggregate low-cardinality metrics.
- Provide dashboards and alerts for startup, rebuffering, errors, and ingestion health.
- Preserve the existing QoS endpoint during migration.

**Non-Goals:**

- Sending full browser traces, media URLs, request headers, or user-generated text.
- Adopting a third-party observability SaaS.
- Replacing behavior events used for history and recommendation.
- Exposing per-user playback logs in the initial admin UI.

## Decisions

### 1. Separate behavior facts from telemetry events

Behavior events describe user intent and history (`play`, `progress`, `complete`, `skip`). Playback telemetry describes technical quality. They share playback session, video, scene, and request identifiers but use different endpoints, schemas, retention, and consumers.

### 2. Use a versioned batch envelope

`POST /api/playback-telemetry-batches` accepts:

- schema version,
- batch/report ID,
- playback session ID,
- bounded common context,
- up to 50 ordered telemetry events.

Events include load start, metadata ready, first rendered frame, play success/failure, rebuffer start/end, seek start/end, source/quality change, pause, end, and terminal error. Each has event ID, monotonic offset, media position, and typed fields. `(user_id, event_id)` is unique; batch replay returns the accepted result.

Anonymous reports can be accepted with a server-issued anonymous session identifier and stricter rate limits, without creating personal history.

### 3. Measure first rendered frame accurately

The player records load start immediately before source assignment. The first frame timestamp uses `requestVideoFrameCallback` when supported. Fallback order is first advancing `currentTime` while `readyState >= HAVE_CURRENT_DATA`, then `playing`; `loadeddata` alone is not considered an exact rendered-frame measurement and is tagged as fallback.

### 4. Track rebuffer duration, not only count

The client opens a rebuffer interval when `waiting` or `stalled` occurs during expected playback and closes it on `playing`, pause, seek, source change, or termination. It reports count, total duration, maximum duration, and time-to-recovery. Intentional pause and seek buffering are classified separately.

Additional optional metrics include dropped/total frames from `getVideoPlaybackQuality`, selected rendition, source type, startup retry count, and media error category.

### 5. Sanitize context before ingestion

Allowed dimensions are enums or bounded normalized values: scene, player adapter, source type, rendition label, codec family, network class, save-data, browser family/major, OS family, viewport class, and CDN hostname. Full URLs, signatures, cookies, tokens, titles, descriptions, and arbitrary client maps are rejected or discarded.

### 6. Persist raw events with retention and aggregate immediately

`playback_telemetry_event` stores normalized events in a time-partition-friendly shape. Retention is configurable and shorter than durable behavior facts. Ingestion also updates Prometheus counters/histograms with low-cardinality labels:

- startup/first-frame duration,
- rebuffer ratio/count/duration,
- playback failure and recovery,
- source/quality selection,
- telemetry accepted/rejected/duplicate,
- client-to-server delivery delay.

### 7. Add dashboards and actionable alerts

Grafana panels cover p50/p95/p99 startup, rebuffer ratio, error rate, quality distribution, scene/network breakdown, and telemetry health. Initial alerts require a minimum sample count and sustained windows to avoid sparse-data noise.

### 8. Buffer locally and flush safely

The Web client stores only the current in-memory batch. It flushes on size, time, terminal player state, `visibilitychange`, and `pagehide` using keepalive/beacon fallback. Failures retry with the same event IDs during the current page session; telemetry failures never block playback.

## Risks / Trade-offs

- [Telemetry volume overloads PostgreSQL] -> Bound events, batch writes, sample optional high-frequency data, partition/retain, and monitor ingestion.
- [High-cardinality metrics harm Prometheus] -> Use normalized enums and keep request/video/user identifiers out of metric labels.
- [Browser APIs differ] -> Record measurement method and provide explicit fallback hierarchy.
- [Telemetry and behavior events diverge] -> Share session identifiers but validate each pipeline independently.
- [Page exit loses final batch] -> Flush earlier at terminal transitions and use idempotent keepalive/beacon delivery.

## Migration Plan

1. Add the new event tables, batch endpoint, validators, and ingestion metrics.
2. Instrument the existing native player behind a Web flag while continuing old QoS reports.
3. Add Grafana panels and compare new metrics with legacy QoS.
4. Integrate the adaptive player adapter and source dimensions.
5. Stop primary Web use of the legacy QoS endpoint after parity; keep it for compatible clients.
6. Roll back by disabling new client batching; additive storage and old endpoint remain valid.

## Open Questions

- Raw telemetry retention and sampling rates for optional frame statistics.
- Anonymous telemetry policy and rate limits for public Feed traffic.
