## Why

The existing playback telemetry limiter is process-local and endpoint-specific, while the planned remote rate-limit decision endpoint would add latency and a failure dependency to every protected request. Frux needs a bounded layered approach that keeps enforcement on the request path local.

## What Changes

- Add declarative rate-limit policies for a small registered set of public and authenticated endpoint groups.
- Enforce cheap IP or anonymous limits at the edge-facing HTTP layer and user/resource limits in local middleware.
- Use Redis only where cross-instance quota coordination is required, with explicit fail-open or fail-closed behavior per endpoint class.
- Return stable `429` errors and retry metadata, and emit low-cardinality allow, reject, fallback, and backend-error metrics.
- Replace the standalone playback telemetry limiter with the shared mechanism while preserving its effective quota.
- Exclude billing quotas, arbitrary operator-defined expressions, service-mesh deployment, and adaptive overload shedding.

## Capabilities

### New Capabilities

- `layered-request-rate-limiting`: Local-first endpoint quotas with bounded distributed coordination and observable failure modes.

### Modified Capabilities

None.

## Impact

This affects configuration, Redis scripts or adapters, HTTP middleware and routing, playback telemetry integration, API errors, Prometheus alerts/dashboards, tests, and governance documentation. It follows `add-runtime-degradation-controls` so emergency bypass or tightening behavior has an established control path.
