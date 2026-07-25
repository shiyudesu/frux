## 1. Telemetry Contract and Storage

- [ ] 1.1 Define versioned playback telemetry batch, common context, event types, normalized dimensions, and validation limits.
- [ ] 1.2 Add telemetry event persistence models, migrations, unique event constraints, and retention-friendly indexes.
- [ ] 1.3 Implement batch repository writes with duplicate accounting and no partial unbounded processing.
- [ ] 1.4 Add tests for valid batches, duplicates, unsupported versions, payload limits, and prohibited fields.

## 2. Ingestion API

- [ ] 2.1 Add `/api/playback-telemetry-batches` DTOs, handler, route, authentication/anonymous policy, and response summary.
- [ ] 2.2 Normalize browser, OS, network, viewport, source, codec, quality, and CDN-host dimensions.
- [ ] 2.3 Reject or strip signed URLs, tokens, cookies, free-form metadata, and high-cardinality metric labels.
- [ ] 2.4 Add keepalive/beacon-compatible request handling and bounded rate limits.
- [ ] 2.5 Preserve and test the legacy playback QoS endpoint.

## 3. Web Instrumentation

- [ ] 3.1 Add strict telemetry event builders, playback session IDs, monotonic offsets, batching, and stable event IDs.
- [ ] 3.2 Measure source load start and first rendered frame with `requestVideoFrameCallback` plus documented fallbacks.
- [ ] 3.3 Track play success/failure, rebuffer intervals, seek intervals, source/quality changes, pause, end, and terminal errors.
- [ ] 3.4 Collect bounded frame-quality totals and selected source/rendition context when supported.
- [ ] 3.5 Flush on batch size, interval, terminal state, visibility change, and page exit with bounded retry.
- [ ] 3.6 Ensure telemetry failure never changes playback state or user-visible success.

## 4. Metrics and Operations

- [ ] 4.1 Add Prometheus startup/first-frame histograms with scene, network, player, and measurement-method labels.
- [ ] 4.2 Add rebuffer count/duration/ratio, playback failure/recovery, quality/source, and telemetry-health metrics.
- [ ] 4.3 Verify all metric labels are low cardinality and exclude user, video, request, and session identifiers.
- [ ] 4.4 Add configurable telemetry retention, cleanup, accepted/rejected/duplicate, and delivery-delay metrics.

## 5. Dashboards and Alerts

- [ ] 5.1 Add Grafana panels for startup percentiles, rebuffer ratio, error rate, quality distribution, and network/scene breakdown.
- [ ] 5.2 Add telemetry ingestion volume, rejection, duplicate, and client-delivery-delay panels.
- [ ] 5.3 Add minimum-sample sustained alerts for startup regression, rebuffering, playback failure, and telemetry outage.
- [ ] 5.4 Document dashboard interpretation and alert investigation runbooks.

## 6. Verification and Documentation

- [ ] 6.1 Add browser tests for exact/fallback first-frame measurement, buffering classification, seek exclusion, retries, and page exit.
- [ ] 6.2 Add API-flow and metrics tests for batching, privacy sanitization, duplicate aggregation, and legacy compatibility.
- [ ] 6.3 Update playback, monitoring, performance-testing, optimization, privacy, engineering, and architecture documentation.
- [ ] 6.4 Compare new telemetry with legacy QoS during staged rollout and define the legacy-client support window.
- [ ] 6.5 Run targeted Go tests, Web build, Grafana/Prometheus config checks, Windows Chrome telemetry checks, and strict OpenSpec validation.
