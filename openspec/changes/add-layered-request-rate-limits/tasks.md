## 1. Policy and Local Limiter

- [ ] 1.1 Add typed registered endpoint-group policies with bounds, identity dimension, distributed mode, and fallback mode.
- [ ] 1.2 Implement the bounded in-memory token bucket with idle expiry and conservative capacity behavior.
- [ ] 1.3 Add local limiter tests for refill, rejection, expiry, map saturation, and concurrency.

## 2. Trusted Identity and Redis Coordination

- [ ] 2.1 Add trusted proxy-aware client IP normalization and server-derived user identity selection.
- [ ] 2.2 Implement the atomic Redis token-bucket script and narrow infrastructure adapter.
- [ ] 2.3 Add short deadlines and explicit local-fallback or fail-closed handling per policy.
- [ ] 2.4 Add Redis integration tests for cross-instance quota, expiry, script errors, and fallback behavior.

## 3. HTTP Middleware and Compatibility

- [ ] 3.1 Add shared route middleware that performs local-first and optional distributed enforcement.
- [ ] 3.2 Add stable 429 API errors, retry metadata, and safe response headers.
- [ ] 3.3 Register initial policies for playback telemetry and selected expensive public or authenticated endpoints.
- [ ] 3.4 Replace the dedicated playback telemetry limiter while preserving its effective configured quota.
- [ ] 3.5 Integrate only predeclared emergency profiles with runtime degradation controls.

## 4. Observability and Verification

- [ ] 4.1 Add low-cardinality metrics for endpoint group, layer, allow, reject, fallback, saturation, and backend error.
- [ ] 4.2 Add Prometheus alerts and Grafana panels for rejection spikes, Redis fallback, and limiter saturation.
- [ ] 4.3 Add middleware and API-flow tests for user, IP, spoofed header, 429, fallback, and compatibility behavior.
- [ ] 4.4 Update governance, playback, monitoring, product, architecture, and engineering documentation.
- [ ] 4.5 Run targeted limiter/playback tests, the full Go suite, Compose config validation, and strict OpenSpec validation.
