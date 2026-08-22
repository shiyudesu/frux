## Why

Frux now has real Session Semantic acceptance and an isolated Shadow path, but it still lacks a safe
way to promote the semantic Provider into a small active cohort. Rollout must be operator-controlled,
evidence-gated, reversible by exact policy version, and incapable of silently activating when the API
runtime is not ready.

## What Changes

- Add a registered `session-semantic-rollout-v1` policy builder that clones an explicit baseline
  policy and adds bounded `semantic_session`, `semantic_similarity`, Quota Merge order/reservations,
  active contract identity, and a default 1% stable cohort without modifying v1/v2.
- Add a runtime-readiness metric that proves the API actually composed the Session Semantic Builder,
  Provider, active contract, and Exact repository; ordinary health alone is insufficient for activation.
- Add a standalone operator command with `plan`, `create`, `activate`, `status`, and `disable` actions.
  All mutations require both `--execute` and an environment acknowledgement.
- Require a compatible complete Session Semantic Shadow report, exact active contract, source policy,
  target version, and runtime readiness before creating or activating a rollout policy.
- Create rollout policies disabled first. Activation enables only the exact target version and requires
  an already enabled 100% baseline policy so non-cohort users always retain a fallback.
- Add an exact-version Kill Switch that disables only the target semantic policy and leaves v1/v2 and
  every other policy unchanged; do not use broad scene rollback for routine semantic rollback.
- Produce a permission-restricted operator report containing action, evidence/config digests, policy
  diff, cohort probes, preflight results, mutation result, and recovery command without credentials,
  vectors, candidate IDs, raw Shadow rows, or DSN.
- Add idempotent create/activate/disable behavior, deterministic policy hashing, fixed-label metrics,
  PostgreSQL integration tests, and runtime selection/fallback tests.
- Keep active expansion manual. This change adds no automatic percentage ramp, A/B framework,
  statistical lift claim, long-term profile, HNSW, training, public API, or Web control.

## Capabilities

### New Capabilities

- `session-semantic-rollout`: Defines evidence-gated policy construction, disabled-first creation,
  exact activation/disable semantics, stable cohort probes, runtime readiness, operator reports,
  mutation gates, observability, and rollback verification.

### Modified Capabilities

- `contextual-recommendation`: Adds active semantic cohort selection and exact-version fallback rules
  while preserving existing v1/v2 and non-cohort behavior.
- `session-semantic-recommendation`: Extends dormant activation requirements with Shadow evidence,
  runtime readiness, disabled-first policy lifecycle, and exact Kill Switch behavior.

## Impact

- Affects recommendation domain/application policy helpers, policy repository operations, API runtime
  metrics/composition, a new Go operator command, tests, and recommendation/operations documentation.
- Uses the existing `recommendation_policy` table and adds no candidate, vector, profile, training, or
  experiment persistence. Operator reports are local files, not runtime request records.
- Adds no public HTTP endpoint or Web behavior. `recommend/v1` and `recommend/v2` serialization and
  rows remain unchanged; rollout is default-off until an operator explicitly creates and activates a
  higher version.
