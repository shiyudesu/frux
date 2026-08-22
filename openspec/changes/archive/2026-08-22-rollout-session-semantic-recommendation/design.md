## Context

Frux already supports multiple enabled recommendation policies. Selection sorts by descending
version and applies each policy's stable cohort percentage, so a higher 1% policy naturally falls
back to lower enabled policies for the other 99%. Session Semantic is implemented, accepted with real
vectors, and Shadow-isolated, but checked-in policies still omit it and checked-in runtime flags remain
off.

The current policy repository can create and enable a version, while its broad `RollbackPolicy`
operation disables every policy in a scene and promotes one version to 100%. That operation is useful
for emergency scene rollback but is too broad for the routine Kill Switch of one semantic canary.
There is also no operator workflow tying policy activation to Shadow evidence or proving that the API
actually composed Session Semantic dependencies.

## Goals / Non-Goals

**Goals:**

- Construct one conservative, versioned semantic rollout policy from an explicit persisted baseline.
- Require complete Shadow evidence, exact contract identity, runtime readiness, and a 100% enabled
  fallback before active mutation.
- Create the target disabled first, then activate only the exact target version under double mutation
  acknowledgement.
- Disable only the target semantic version as an immediate Kill Switch without changing v1/v2.
- Make plan/status/create/activate/disable idempotent and produce safe local operator evidence.
- Verify stable cohort selection, non-cohort fallback, semantic degradation fallback, and exact rollback.

**Non-Goals:**

- Automatically increasing rollout percentage, measuring causal lift, or choosing promotion thresholds.
- Editing `recommend/v1` or `recommend/v2`, using broad scene rollback as the normal semantic Kill Switch,
  or deleting policy history.
- Adding a public/Admin HTTP endpoint, Web console, scheduler, database audit table, A/B framework,
  HNSW, long-term semantic profile, training, or learned weights.
- Enabling multimodal/session runtime in checked-in configuration or calling an external model.

## Decisions

### 1. Register one conservative rollout profile in Go

`session-semantic-rollout-v1` clones an explicit source policy and adds:

- `semantic_session` budget 50 and deadline 250ms;
- `semantic_similarity` weight 0.25;
- pool limit 500 and fixed Provider order ending in `semantic_session`;
- zero baseline reservations plus semantic reservation 10, leaving common fill available;
- `session-semantic-v1`, the selected active contract, 24h lookback, 21 seeds, two positive signals,
  and minimum confidence 0.25;
- default rollout 1%, bounded to 1-5%, and 100% request-log sampling for diagnostic evidence.

All other ranking, suppression, diversity, retention, Snapshot, and baseline Provider fields come from
the source policy. The builder returns a new disabled policy and a deterministic config SHA-256;
source and bootstrap policies are never mutated.

Alternative considered: hand-author arbitrary JSON in the operator command. Rejected because a
registered profile gives tests, stable diffs, and a smaller unsafe configuration surface.

### 2. Expose explicit API runtime readiness

The API sets `frux_recommendation_session_semantic_runtime_ready` to 1 only after it has successfully
composed the active contract, trusted-fact Builder, Exact repository, and Semantic Provider. It is 0
otherwise. Activation reads the configured API metrics endpoint and requires exactly one ready sample;
ordinary `/health` is not sufficient.

Alternative considered: infer readiness from configuration files. Rejected because the operator may
run outside the API host and configuration presence does not prove successful dependency composition.

### 3. Use a standalone operator command with action-specific gates

`cmd/session-semantic-rollout` supports:

- `plan`: read-only policy/evidence/runtime diff and cohort probes;
- `create`: create the exact target version disabled;
- `activate`: enable the exact compatible target after all gates;
- `status`: read-only persisted/selected state;
- `disable`: exact target Kill Switch that is allowed even when Shadow evidence or runtime is unavailable.

Mutating actions require both `--execute` and
`FRUX_SESSION_SEMANTIC_ROLLOUT_ALLOW_MUTATION=true`. The command loads an ignored local env file or
normal environment, never accepts credentials in flags, and has bounded PostgreSQL/HTTP/report I/O.

Alternative considered: expose policy mutation over HTTP. Rejected because the project has no
approved policy-admin authorization/audit surface and a local operator command is smaller and safer.

### 4. Treat the Shadow report as a promotion prerequisite, not causal proof

Plan/create/activate parse the canonical `session-semantic-shadow-report/v1`, verify its SHA-256,
tool/schema versions, `status=complete`, zero external model calls, at least five available/labeled
cases, non-zero unique contribution and rank survival, and simulated NDCG no worse than active NDCG.
The report still declares non-causal limitations. Disable does not depend on this file.

Alternative considered: accept Prometheus samples alone. Rejected because low-volume metrics may lack
stable denominators and the canonical report already provides deterministic evidence.

### 5. Add an exact idempotent disable operation

Extend the policy repository with `DisablePolicy(scene, version)`. PostgreSQL locks the exact row,
validates its policy payload, and sets only that row to `enabled=false`; already-disabled is a successful
replay. No other policy is updated. The normal selector then falls through to lower enabled policies.

Activation remains exact-version enablement, but the operator preflight additionally requires another
enabled policy in the same scene with `rollout_percentage=100`. This prevents non-cohort users from
having no selected policy.

Alternative considered: reuse `RollbackPolicy(scene, 1)`. Rejected because it disables all other
staged policies and changes the target baseline to 100%, which is unnecessarily broad.

### 6. Make command replay deterministic against persisted state

Create treats an existing target with the same registered profile/config hash as replay; a different
payload is a conflict. Activate and disable treat the already-desired enabled state as replay. The
command never deletes policy rows.

Every run writes a `0600` JSON report containing action/mode/result, source/target versions, rollout
percentage, contract/config/evidence digests, bounded preflight outcomes, policy-field diff, synthetic
cohort bucket counts, mutation replay state, and exact recovery command. It excludes DSN, credentials,
headers, vectors, candidates, real user/request IDs, and raw errors.

Alternative considered: add an audit table. Rejected for this phase because the immutable policy row,
deterministic hashes, and permission-restricted operator report provide sufficient local-project
evidence without another runtime schema/retention system.

### 7. Keep expansion manual and bounded

The first profile permits 1-5%, but the command changes no existing target percentage. A larger cohort
requires creating a new higher policy version from a separate approved plan/evidence set. There is no
in-place ramp or automated promotion.

## Risks / Trade-offs

- **[Metrics endpoint is unavailable during activation]** → Fail closed; create/plan remain available
  and disable remains available for recovery.
- **[A stale Shadow report is reused]** → Bind report SHA, tool/schema, contract/profile inputs, policy
  config hash, and operator report; require explicit operator review for each target version.
- **[1% yields little organic traffic]** → Stable synthetic cohort probes and existing deterministic
  fixtures prove routing; no statistical-lift claim is made.
- **[Higher semantic version masks v2 for overlapping users]** → This is intended descending-version
  selection; report the exact target cohort and preserve v1 100% fallback.
- **[Exact query degrades after activation]** → Existing Provider failure isolation returns healthy
  baseline candidates; exact disable removes future semantic selection.
- **[Local report can be lost]** → It is permission-restricted and reproducible from immutable policy
  state/evidence hashes; production-grade centralized audit remains future work.

## Migration Plan

1. Add registered policy construction, exact disable, readiness metric, report parsing, and tests with
   all checked-in policies/runtime unchanged.
2. Add the operator command and examples; run `plan` against local Docker and verify activation is
   blocked while Session Semantic runtime is disabled.
3. Create a disabled target policy only after canonical Shadow evidence passes.
4. In an explicitly configured environment, enable Session Semantic runtime, verify readiness=1, then
   activate the 1% target with both mutation gates.
5. Observe fixed metrics and request logs. Roll back with exact `disable`; broad scene rollback remains
   emergency-only.

## Open Questions

No automatic expansion threshold is selected. Any move beyond 1%, production causal experiment, or
Web/Admin control requires a separate change.
