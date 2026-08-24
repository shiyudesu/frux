## Context

The accepted v3 Session Semantic policy is a one-percent rollout and development Compose currently
needs a special env file to assemble the runtime. That is appropriate for production rollout safety,
but it means a personal development environment almost never exercises the completed feature during
normal browsing. The policy configuration is immutable by version, so changing v3 from1% to100% in
place would invalidate its digest and historical evidence.

## Goals / Non-Goals

**Goals:**

- Make ordinary development Compose startup assemble the Session-only runtime without an Adapter.
- Ensure every development recommendation request selects a compatible Session Semantic policy.
- Preserve policy immutability by creating v4 at100% and retaining v1/v2/v3 history.
- Keep startup and policy reconciliation idempotent across restarts and fresh databases.
- Keep production runtime and full-rollout reconciliation disabled by default.

**Non-Goals:**

- Enabling video embedding jobs, Hybrid Search, Similar Videos, query embedding, or model calls.
- Enabling Session Semantic by default in production.
- Rewriting v3, removing baselines, claiming online metric lift, or adding ANN/training.

## Decisions

### 1. Use an explicit development full-rollout configuration

Add `multimodal.session.development_full_rollout_enabled` and a strict environment override. The
option is valid only when the parent and Session runtimes are enabled and Kafka environment is
`local` or `test`. Development Compose defaults the profile and these three flags on; production
passes no enabling defaults and rejects the development flag outside local/test.

Alternative considered: change all YAML defaults to true. Rejected because native and production
processes without a selected profile would fail startup and production intent would become ambiguous.

### 2. Create immutable v4 instead of editing v3

The registered policy builder accepts staged1-5% policies and an explicitly authorized100% full
rollout. Development reconciliation builds v4 from semantic-free v2 and the active multimodal
contract, creates it disabled if absent, then activates it. A conflicting v4 causes startup failure
rather than overwrite. Once v4 is active, any other enabled registered semantic target is exact-disabled;
v1/v2 remain unchanged as emergency baselines.

Alternative considered: update v3 `rollout_percentage` directly in PostgreSQL. Rejected because it
breaks immutable version history, report digests, and replay safety.

### 3. Reconcile only after Session runtime composition succeeds

The API router invokes the development reconciler only after Builder, Exact Provider, and contract
composition succeeds. Reconciliation failure aborts startup, preventing a configuration that claims
full semantic operation while silently serving only baseline policies. Worker startup does not mutate
recommendation policies.

### 4. Keep staged operator safety and add explicit full-rollout acknowledgement

Ordinary rollout configuration remains limited to1-5%. A100% operator plan is accepted only when
`FRUX_SESSION_SEMANTIC_ROLLOUT_ALLOW_FULL=true`; create/activate still require the existing independent
mutation acknowledgement and `--execute`. This supports manual recovery/reproduction without making
production expansion accidental.

### 5. Treat fallback as retained recovery, not a selectable cohort at100%

A full target still requires another enabled non-semantic100% baseline so exact disable has an
immediate fallback. Existing-policy acceptance generates a target request but omits non-target cohort
evidence when the target itself covers100%, because no such request can exist.

## Risks / Trade-offs

- **[Development startup mutates policy state]** → The behavior requires an explicit local/test-only
  flag, uses exact versions, is idempotent, and fails on conflicts.
- **[v4 hides v1/v2 for all requests]** → Baselines remain enabled and exact-disable of v4 restores
  them immediately.
- **[Missing compatible vectors causes weak results]** → Existing Session Builder confidence and
  fallback behavior remain active; no model call is introduced on the request path.
- **[Production accidentally enables full rollout]** → Configuration rejects the development flag in
  staging/production and the operator path requires a separate full-rollout acknowledgement.

## Migration Plan

1. Add and validate the development full-rollout flag and development Compose defaults.
2. Extend registered policy construction and add the idempotent v4 reconciler.
3. Restart development API; it creates/activates v4 and exact-disables v3.
4. Verify runtime readiness is1 and deterministic policy selection returns v4 for all buckets.
5. Roll back by setting the development flag/runtime overrides false and exact-disabling v4.

## Open Questions

None. A future production ramp remains a separate explicit decision.
