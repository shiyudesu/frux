## 1. Telemetry Contract and Storage

- [x] 1.1 Define versioned playback telemetry batch, common context, event types, normalized dimensions, and validation limits.
- [x] 1.2 Add telemetry event persistence models, migrations, unique event constraints, and retention-friendly indexes.
- [x] 1.3 Implement batch repository writes with duplicate accounting and no partial unbounded processing.
- [x] 1.4 Add tests for valid batches, duplicates, unsupported versions, payload limits, and prohibited fields.

## 2. Ingestion API

- [x] 2.1 Add `/api/playback-telemetry-batches` DTOs, handler, route, authentication/anonymous policy, and response summary.
- [x] 2.2 Normalize browser, OS, network, viewport, source, codec, quality, and CDN-host dimensions.
- [x] 2.3 Reject or strip signed URLs, tokens, cookies, free-form metadata, and high-cardinality metric labels.
- [x] 2.4 Add keepalive/beacon-compatible request handling and bounded rate limits.
- [x] 2.5 Preserve and test the legacy playback QoS endpoint.

## 3. Web Instrumentation

- [x] 3.1 Add strict telemetry event builders, playback session IDs, monotonic offsets, batching, and stable event IDs.
- [x] 3.2 Measure source load start and first rendered frame with `requestVideoFrameCallback` plus documented fallbacks.
- [x] 3.3 Track play success/failure, rebuffer intervals, seek intervals, source/quality changes, pause, end, and terminal errors.
- [x] 3.4 Collect bounded frame-quality totals and selected source/rendition context when supported.
- [x] 3.5 Flush on batch size, interval, terminal state, visibility change, and page exit with bounded retry.
- [x] 3.6 Ensure telemetry failure never changes playback state or user-visible success.

## 4. Metrics and Operations

- [x] 4.1 Add Prometheus startup/first-frame histograms with scene, network, player, and measurement-method labels.
- [x] 4.2 Add rebuffer count/duration/ratio, playback failure/recovery, quality/source, and telemetry-health metrics.
- [x] 4.3 Verify all metric labels are low cardinality and exclude user, video, request, and session identifiers.
- [x] 4.4 Add configurable telemetry retention, cleanup, accepted/rejected/duplicate, and delivery-delay metrics.

## 5. Dashboards and Alerts

- [x] 5.1 Add Grafana panels for startup percentiles, rebuffer ratio, error rate, quality distribution, and network/scene breakdown.
- [x] 5.2 Add telemetry ingestion volume, rejection, duplicate, and client-delivery-delay panels.
- [x] 5.3 Add minimum-sample sustained alerts for startup regression, rebuffering, playback failure, and telemetry outage.
- [x] 5.4 Document dashboard interpretation and alert investigation runbooks.

## 6. Verification and Documentation

- [x] 6.1 Add browser tests for exact/fallback first-frame measurement, buffering classification, seek exclusion, retries, and page exit.
- [x] 6.2 Add API-flow and metrics tests for batching, privacy sanitization, duplicate aggregation, and legacy compatibility.
- [x] 6.3 Update playback, monitoring, performance-testing, optimization, privacy, engineering, and architecture documentation.
- [x] 6.4 Compare new telemetry with legacy QoS during staged rollout and define the legacy-client support window.
- [x] 6.5 Run targeted Go tests, Web build, Grafana/Prometheus config checks, Windows Chrome telemetry checks, and strict OpenSpec validation.
