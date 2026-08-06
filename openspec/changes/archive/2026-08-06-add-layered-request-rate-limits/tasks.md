## 1. Policy and Local Limiter

- [x] 1.1 Add typed registered endpoint-group policies with bounds, identity dimension, distributed mode, and fallback mode.
- [x] 1.2 Implement the bounded in-memory token bucket with idle expiry and conservative capacity behavior.
- [x] 1.3 Add local limiter tests for refill, rejection, expiry, map saturation, and concurrency.

## 2. Trusted Identity and Redis Coordination

- [x] 2.1 Add trusted proxy-aware client IP normalization and server-derived user identity selection.
- [x] 2.2 Implement the atomic Redis token-bucket script and narrow infrastructure adapter.
- [x] 2.3 Add short deadlines and explicit local-fallback or fail-closed handling per policy.
- [x] 2.4 Add Redis integration tests for cross-instance quota, expiry, script errors, and fallback behavior.

## 3. HTTP Middleware and Compatibility

- [x] 3.1 Add shared route middleware that performs local-first and optional distributed enforcement.
- [x] 3.2 Add stable 429 API errors, retry metadata, and safe response headers.
- [x] 3.3 Register initial policies for playback telemetry and selected expensive public or authenticated endpoints.
- [x] 3.4 Replace the dedicated playback telemetry limiter while preserving its effective configured quota.
- [x] 3.5 Integrate only predeclared emergency profiles with runtime degradation controls.

## 4. Observability and Verification

- [x] 4.1 Add low-cardinality metrics for endpoint group, layer, allow, reject, fallback, saturation, and backend error.
- [x] 4.2 Add Prometheus alerts and Grafana panels for rejection spikes, Redis fallback, and limiter saturation.
- [x] 4.3 Add middleware and API-flow tests for user, IP, spoofed header, 429, fallback, and compatibility behavior.
- [x] 4.4 Update governance, playback, monitoring, product, architecture, and engineering documentation.
- [x] 4.5 Run targeted limiter/playback tests, the full Go suite, Compose config validation, and strict OpenSpec validation.

## 5. Review Finding Corrections

- [x] 5.1 Use a dedicated deadline-aware, no-command-retry Redis client and test one Lua execution per request.
- [x] 5.2 Preserve playback telemetry's exact fixed 60-second window with fake-clock compatibility coverage.
- [x] 5.3 Replace full-map local expiry scans with bounded indexed reclamation and strict capacity/concurrency tests.
- [x] 5.4 Re-run targeted, full, race, Compose, and strict OpenSpec validation (targeted race passes; full race still exposes the unrelated existing `TestPublicMediaImmutableRangeHeadAndETag` stub race).
