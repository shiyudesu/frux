## Why

GCFeed stores only first-frame time, stutter count, and watch duration, and measures first frame at `loadeddata` rather than actual rendered output. This cannot distinguish startup delay, rebuffer duration, media/CDN failures, source selection, or client regressions.

## What Changes

- Define a versioned client playback telemetry schema covering load, first rendered frame, play success/failure, rebuffer count and duration, seek, quality/source changes, dropped frames, completion, and terminal error.
- Measure first rendered frame with browser rendering APIs when supported and document a compatible fallback.
- Batch and flush telemetry with bounded payloads, retry-safe report IDs, keepalive/page-exit delivery, and privacy-safe dimensions.
- Enrich server-side playback logs with scene, request ID, player/source type, selected rendition, network class, browser family, and CDN host without storing signed URLs or sensitive tokens.
- Export low-cardinality Prometheus metrics and add Grafana views and alert thresholds for startup, rebuffering, failure, and reporting health.
- Keep the existing QoS endpoint compatible while introducing the expanded event/batch contract.

## Capabilities

### New Capabilities

- `playback-observability`: Defines accurate client playback telemetry, privacy-safe ingestion, aggregation, dashboards, and alerting.

### Modified Capabilities

## Impact

- Affects Web player instrumentation, playback DTOs and persistence, metrics, monitoring dashboards, alert configuration, migrations, idempotency handling, and playback tests.
- Adds bounded telemetry volume and retention requirements but does not introduce third-party monitoring services.
- Requires updates to playback, monitoring, performance-testing, optimization, privacy, and engineering documentation.
