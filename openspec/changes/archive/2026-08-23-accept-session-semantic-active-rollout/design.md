## Context

The default Docker config keeps all multimodal flags false. `LoadConfig` expands environment values
inside YAML, but the boolean fields are literals, so operators currently must edit tracked YAML to
enable Session Semantic. The API needs only the selected profile, existing facts/projections, Builder,
and Exact repository for Session-only operation; it does not need the external Adapter.

The existing Session Semantic acceptance Runner already proves the real request path, but its policy
stage always creates a temporary highest version and later deletes it. The rollout workflow instead
needs to prove the persisted v3 target itself and leave its historical row disabled.

## Goals / Non-Goals

**Goals:**

- Enable Session-only runtime through strict explicit environment switches while preserving false
  defaults and avoiding Adapter credentials/endpoints.
- Reuse the existing acceptance Runner against one exact enabled rollout version.
- Generate deterministic target and fallback cohort evidence from persisted enabled policies.
- Guarantee exact target disable on success and best-effort failure recovery; never delete v3.
- Keep temporary-policy acceptance behavior unchanged.

**Non-Goals:**

- Enabling video jobs, query embedding, Hybrid Search, Similar Videos, Shadow, or external model calls.
- Automatically activating the target, provisioning production accounts, changing rollout percentage,
  or retaining v3 enabled after acceptance.
- Adding database schema, public API, Web UI, HNSW, training, or causal evaluation.

## Decisions

### 1. Apply two optional boolean overrides before multimodal validation

`LoadConfig` checks `FRUX_MULTIMODAL_ENABLED` and
`FRUX_MULTIMODAL_SESSION_RECOMMENDATION_ENABLED` after YAML unmarshalling. Missing or whitespace-only
values preserve YAML. Non-empty values must parse with `strconv.ParseBool`; invalid values return
`ErrInvalidMultimodalConfig`. Existing normalization then enforces that session recommendation cannot
be enabled without the parent runtime and complete profile/Exact composition.

The variables are added to the Frux env-file allowlist and Compose service environments. A separate
`.env.session-semantic-runtime.example` contains only profile and booleans, preventing accidental
injection of the native loopback Adapter endpoint/HMAC into containers.

Alternative considered: interpolate booleans directly in YAML. Rejected because empty Compose values
do not provide a reliable typed default and can make normal startup fail.

### 2. Add exact existing-policy mode to the current Runner

`FRUX_SESSION_SEMANTIC_ACCEPTANCE_POLICY_VERSION` defaults to zero. Zero preserves temporary-policy
mode. A positive value selects existing-policy mode and requires the target to be enabled, registered
as `session-semantic-rollout-v1`, contract-compatible, and accompanied by another enabled 100%
baseline.

The store uses the normal `PolicyService.Select` contract to find a bounded target request ID and a
non-target fallback request ID. It reports both cohort buckets and the selected fallback version.

Alternative considered: create a second acceptance implementation. Rejected because duplicating login,
facts, Feed, Snapshot, metrics, and report logic would drift from the already accepted path.

### 3. Distinguish managed policy from created policy

Runner state gains `policyManaged`. Temporary mode sets both `policyCreated` and `policyManaged`.
Existing mode sets only `policyManaged`. The disable stage and failure recovery act on every managed
policy; cleanup deletes only a created temporary policy. Therefore v3 is disabled but retained.

Alternative considered: leave v3 enabled after a successful run. Rejected because this low-traffic
development acceptance must end in a safe known state and the operator can reactivate explicitly.

### 4. Keep the real request scoped to the target cohort

The existing first-page request uses the generated target request ID and validates Request Log policy
version, `semantic_session`, positive `semantic_similarity`, Snapshot reuse, and zero Adapter operation
delta. Fallback evidence is selection-only and does not create another user-visible session; policy
selection/fallback already has unit/PostgreSQL coverage.

### 5. Preserve secret-free reporting

The report adds policy mode, target/fallback request cohort percentages, fallback policy version,
managed/created state, and final disabled/deleted flags. Account, password, Token, DSN, raw request IDs,
vectors, and candidate payloads remain excluded or bounded as in the existing technical report.

## Risks / Trade-offs

- **[Environment typo silently enables a feature]** → Only explicit parseable booleans apply; invalid
  non-empty values fail startup and blanks preserve defaults.
- **[Existing target is incompatible]** → Fail before behavior mutation and do not disable another row.
- **[Acceptance fails after activation]** → Best-effort exact disable runs with a bounded recovery context;
  the report retains the exact recovery command/version.
- **[Fallback request is not sent through HTTP]** → Persisted selector evidence plus existing selection
  tests prove routing; the real billed/behavior-sensitive request remains limited to one target session.
- **[Dedicated account accumulates immutable view facts]** → Use a local acceptance-only account and
  document that facts remain; favorites are reverted.

## Migration Plan

1. Add override parsing/Compose wiring and keep all examples/defaults false.
2. Extend acceptance config/store/runner/report with backward-compatible existing-policy mode.
3. Build/restart Docker with the session-only env file and verify runtime-ready=1 and Adapter absent.
4. Create a dedicated local account, activate v3 through the existing rollout command, execute the
   acceptance Runner with policy version3, and verify v3 ends disabled/not deleted.
5. Remove the runtime env override or restart default Compose so readiness returns to0.

## Open Questions

None. Keeping v3 enabled beyond acceptance remains a separate explicit operator decision.
