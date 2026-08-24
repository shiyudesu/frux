# Session Semantic Rollout Specification

## Purpose

Define evidence-gated, disabled-first Session Semantic cohort rollout with deterministic baseline
fallback, exact-version Kill Switch behavior, safe operator reports, and fixed observability.

## Requirements

### Requirement: Rollout policy construction is registered and baseline-preserving
Frux SHALL construct Session Semantic rollout policies only through the registered
`session-semantic-rollout-v1` profile from an explicit persisted source policy and active contract.
The source policy and bootstrap v1/v2 policies MUST remain unchanged.

#### Scenario: Valid baseline is planned
- **WHEN** an operator supplies a compatible baseline, target version, contract, and rollout percentage
- **THEN** Frux produces a deterministic disabled target policy and config digest with bounded semantic budget, deadline, reservation, weight, and Session Semantic configuration

#### Scenario: Source policy is inspected after planning
- **WHEN** rollout construction completes or fails
- **THEN** the source policy maps, semantic fields, enabled state, version, and serialized configuration remain unchanged

#### Scenario: Arbitrary semantic policy fields are requested
- **WHEN** input attempts to override unregistered budget, deadline, weight, reservation, builder, contract, or Provider order fields
- **THEN** the rollout plan is rejected rather than producing arbitrary policy JSON

### Requirement: Promotion is gated by compatible Shadow evidence and runtime readiness
Plan/create/activate SHALL verify the canonical Shadow report and exact contract. Activation SHALL
additionally require the API Session Semantic runtime-readiness metric to equal 1 and an enabled 100%
baseline policy in the same scene.

#### Scenario: Complete compatible evidence and runtime are ready
- **WHEN** the canonical report passes registered case/contribution/survival/relevance gates and the API exposes ready runtime
- **THEN** activation preflight may proceed for the exact target policy

#### Scenario: Runtime health exists without semantic readiness
- **WHEN** `/health` succeeds but Session Semantic Builder, Provider, contract, or Exact composition is absent
- **THEN** activation fails closed and the target remains disabled

#### Scenario: Evidence is incomplete or incompatible
- **WHEN** the report is inconclusive, malformed, model-calling, under minimum denominators, stale/incompatible, or worse than the registered relevance gate
- **THEN** plan/create/activate reports the failed gate and performs no mutation

### Requirement: Policy mutations are double-gated and disabled-first
Create, activate, and disable SHALL require both the action's explicit execute flag and the registered
environment acknowledgement. A newly created rollout policy SHALL be disabled until a separate
activation succeeds.

#### Scenario: Only one mutation acknowledgement is present
- **WHEN** a mutating action has `--execute` without the environment gate, or the environment gate without `--execute`
- **THEN** the command emits a planned/blocked report and does not mutate PostgreSQL

#### Scenario: Create is confirmed
- **WHEN** all create gates pass and both acknowledgements are present
- **THEN** the exact target version is inserted disabled and cannot affect policy selection

#### Scenario: Create is replayed
- **WHEN** the exact target already exists with the same registered configuration digest
- **THEN** create succeeds as an idempotent replay without another row or changed timestamp

### Requirement: Activation uses stable cohort selection with baseline fallback
Activation SHALL enable only the exact compatible target policy. Staged policies SHALL use the
existing descending-version stable-cohort selector for1-5% traffic and fall through to an enabled
lower100% baseline. An explicitly configured local/test development policy MAY use100%, in which case
every request selects the higher semantic version while the enabled baseline remains available after
exact disable.

#### Scenario: Request is inside a staged semantic cohort
- **WHEN** the stable user/scene/request bucket is below a1-5% target rollout percentage
- **THEN** the target semantic policy is selected and its complete Session Semantic Provider/ranking configuration applies

#### Scenario: Request is outside a staged semantic cohort
- **WHEN** the staged target bucket does not match
- **THEN** selection continues to the existing enabled baseline without changing its configuration or response semantics

#### Scenario: Development full rollout is enabled
- **WHEN** a compatible higher semantic policy is enabled at100% under the local/test development gate
- **THEN** every recommendation request selects that policy and the lower enabled baseline remains available for exact-disable recovery

#### Scenario: Target is already enabled
- **WHEN** activation is repeated for the same compatible target
- **THEN** it succeeds as an idempotent replay without changing other policies

### Requirement: Development full rollout is reconciled as an immutable policy version
When configured for local/test development, API startup SHALL ensure a compatible100% Session Semantic
v4 policy exists and is enabled. It SHALL NOT rewrite an existing policy configuration and SHALL
exact-disable every other enabled registered semantic rollout target after v4 activation.

#### Scenario: Development database contains only bootstrap policies
- **WHEN** the Session runtime composes successfully with development full rollout enabled
- **THEN** API startup creates v4 disabled, activates it, retains v1/v2, and serves v4 for every request

#### Scenario: Compatible v4 already exists
- **WHEN** API restarts with the same active contract and development full rollout enabled
- **THEN** creation and activation replay idempotently without a duplicate policy row

#### Scenario: Existing v4 is incompatible
- **WHEN** the persisted v4 configuration or contract differs from the registered development plan
- **THEN** API startup fails without overwriting the policy

#### Scenario: Another semantic rollout is enabled
- **WHEN** v4 activation succeeds while another registered semantic target remains enabled
- **THEN** each other semantic target is exact-disabled and non-semantic v1/v2 states remain unchanged

### Requirement: Production full rollout is explicit and environment-scoped
Frux SHALL support a production full-rollout flag that is valid only in staging/production, requires
the parent and Session runtimes, and is mutually exclusive with the development full-rollout flag.
When enabled, API startup SHALL idempotently reconcile the same immutable compatible v4=100% policy.

#### Scenario: Production full rollout is enabled
- **WHEN** Session runtime composes under production with the explicit production full-rollout flag
- **THEN** API ensures v4 exists and is enabled, exact-disables other semantic targets, and retains v1/v2

#### Scenario: Production flag is used locally
- **WHEN** local/test configuration enables the production full-rollout flag
- **THEN** configuration fails before startup

#### Scenario: Development and production flags are both set
- **WHEN** both environment-scoped full-rollout flags are true
- **THEN** configuration fails before startup or policy mutation

### Requirement: Full rollout requires an explicit operator acknowledgement
The rollout operator SHALL preserve the normal1-5% limit unless an explicit full-rollout
acknowledgement authorizes exactly100%. Create and activate MUST still require the existing mutation
environment acknowledgement and execution option.

#### Scenario: Full rollout acknowledgement is absent
- **WHEN** an operator configures a rollout percentage above5%
- **THEN** configuration is rejected before evidence loading or mutation

#### Scenario: Full rollout is explicitly acknowledged
- **WHEN** the operator configures exactly100% with the full-rollout acknowledgement
- **THEN** plan/create/activate may proceed through all existing evidence, readiness, baseline, and mutation gates

#### Scenario: Unsupported intermediate percentage is supplied
- **WHEN** the configured percentage is between6% and99%
- **THEN** configuration is rejected even when full rollout is acknowledged

### Requirement: Exact Kill Switch preserves all other policies
The semantic Kill Switch SHALL disable only the specified scene/version row and SHALL remain usable
when runtime readiness or Shadow evidence is unavailable. It MUST NOT delete the policy or call broad
scene rollback.

#### Scenario: Active semantic target is disabled
- **WHEN** the exact disable action is confirmed
- **THEN** only that target becomes disabled and new requests fall through to the existing enabled baseline

#### Scenario: Disable is replayed
- **WHEN** the exact target is already disabled
- **THEN** the operation succeeds idempotently without modifying another policy

#### Scenario: Wrong version is supplied
- **WHEN** the target scene/version does not exist or is not a registered Session Semantic rollout policy
- **THEN** disable fails closed and no policy row changes

### Requirement: Operator reports are bounded, reproducible, and secret-free
Every operator action SHALL emit a permission-restricted JSON report with bounded preflight results,
evidence/config digests, policy diff, synthetic cohort summary, mutation/replay result, and exact
recovery instructions. Reports MUST exclude credentials, DSN, headers, vectors, candidates, real
identifiers, and raw error text.

#### Scenario: Read-only plan is repeated
- **WHEN** persisted policy/evidence/runtime inputs are unchanged
- **THEN** the report's policy/evidence digests, diff, cohort summary, and gate outcomes remain identical

#### Scenario: Mutation succeeds
- **WHEN** create, activate, or disable completes
- **THEN** the report identifies the exact scene/version and whether the operation mutated or replayed state without exposing secrets

### Requirement: Rollout observability uses fixed labels
Frux SHALL expose Session Semantic runtime readiness and rollout operator outcomes using only fixed
action/result labels, while existing policy-version, Provider, degradation, Snapshot, and Session
Semantic metrics remain authoritative for active requests.

#### Scenario: API composes Session Semantic runtime
- **WHEN** Builder, Provider, active contract, and Exact dependencies are successfully registered
- **THEN** the readiness gauge is 1; otherwise it is 0

#### Scenario: Operator action is observed
- **WHEN** plan/create/activate/status/disable completes or fails
- **THEN** a fixed-label action/result counter changes without scene, version, user, request, contract, path, or error text labels

### Requirement: Active rollout acceptance ends in exact disabled state
Before an active Session Semantic rollout is considered technically accepted, Frux SHALL run the real
acceptance path against the exact enabled target version and SHALL finish with that target disabled
but retained. Existing baseline policies MUST remain enabled and unchanged.

#### Scenario: Real target request is accepted
- **WHEN** the exact cohort request proves policy version, semantic recall/ranking, Snapshot reuse, runtime readiness, and zero external model calls
- **THEN** the report records success and the exact target is disabled without broad scene rollback

#### Scenario: Acceptance cannot prove the target
- **WHEN** request evidence identifies another version or lacks valid Session Semantic evidence
- **THEN** acceptance fails and attempts exact target disable rather than declaring rollout success
