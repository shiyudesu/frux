## Why

Frux needs a safe way to disable optional behavior during incidents without redeploying, but a generic JSON configuration table or synchronous per-request governance service would create weak validation and a new failure dependency. Degradation controls should be versioned centrally and evaluated locally.

## What Changes

- Add typed, versioned degradation policies with explicit keys, scope, enabled state, reason, expiry, and revision.
- Provide permission-protected create/update/rollback APIs with optimistic concurrency and mandatory audit attribution.
- Distribute or poll bounded policy snapshots so API and worker processes evaluate controls locally with a last-known-good value.
- Define fail-safe defaults for each registered control and metrics for stale, invalid, or unavailable snapshots.
- Start with registered optional capabilities rather than arbitrary runtime expressions.
- Exclude request-rate limiting, experimentation, recommendation-policy editing, infrastructure deployment controls, and a Web console.

## Capabilities

### New Capabilities

- `runtime-degradation-controls`: Versioned kill switches, local evaluation, expiry, rollback, and operational safety.

### Modified Capabilities

None.

## Impact

This adds a governance domain and persistence model, application reader/writer ports, API and worker snapshot adapters, protected admin/internal endpoints, metrics, migrations, tests, and governance documentation. It depends on admin authorization and audit.
