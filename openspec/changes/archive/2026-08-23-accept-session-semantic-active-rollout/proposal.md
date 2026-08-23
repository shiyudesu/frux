## Why

Frux can create a disabled 1% Session Semantic rollout policy, but checked-in Docker configuration
cannot enable the Session Semantic runtime without editing YAML, and the existing acceptance Runner
always creates its own temporary policy. A reproducible active-rollout acceptance needs explicit
default-off environment switches and the ability to exercise the already-created v3 policy safely.

## What Changes

- Add optional strict environment overrides for only `multimodal.enabled` and
  `multimodal.session_recommendation_enabled`; absent/blank values preserve YAML defaults, invalid
  non-blank values fail startup, and checked-in defaults remain false.
- Pass the two overrides through development and production Compose without enabling them, and add a
  dedicated session-runtime env example containing no Adapter endpoint, HMAC, or API key.
- Extend the existing Session Semantic acceptance configuration with an optional exact existing policy
  version.
- When an existing version is selected, verify it is enabled, is a registered Session Semantic rollout
  policy, matches the active contract, has an enabled 100% baseline, and generate a stable request ID
  that deterministically enters its configured cohort.
- Reuse the existing normal login, trusted playback/favorite facts, Feed request, Request Log,
  Snapshot, metrics, and zero-model-call checks against that exact policy rather than creating a new
  temporary policy.
- Treat the existing rollout policy as managed but not created: disable it exactly on success or
  failure, never delete it during cleanup, and preserve v1/v2.
- Record existing-policy mode, target/fallback cohort evidence, runtime-ready=1, exact policy version,
  and final disabled/not-deleted state in the acceptance report without credentials or raw vectors.
- Keep the legacy temporary-policy acceptance mode backward compatible.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `multimodal-provider-runtime`: Adds strict default-preserving environment overrides for Session-only
  runtime activation without requiring an Adapter endpoint.
- `session-semantic-acceptance-runner`: Adds existing-rollout-policy verification, stable cohort
  targeting, exact failure/success disable, and non-deleting cleanup semantics.
- `session-semantic-rollout`: Requires the real acceptance workflow to use and finally disable the
  exact active rollout target while retaining baseline policies.

## Impact

- Affects configuration loading, env-file allowlists/examples, development/production Compose,
  Session acceptance config/store/runner/report/tests, and rollout documentation.
- Adds no database schema, public API, model call, vector generation, policy creation during existing
  mode, automatic expansion, or checked-in feature enablement.
- The actual local run will temporarily enable Session Semantic runtime, explicitly activate v3,
  create trusted facts through normal APIs, verify v3, and leave v3 disabled afterward.
