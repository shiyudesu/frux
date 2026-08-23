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
Activation SHALL enable only the exact compatible target policy. The existing descending-version,
stable-cohort selector SHALL choose it only for its configured 1-5% cohort and SHALL fall through to
an enabled lower 100% baseline for every non-matching request.

#### Scenario: Request is inside the semantic cohort
- **WHEN** the stable user/scene/request bucket is below the target rollout percentage
- **THEN** the target semantic policy is selected and its complete Session Semantic Provider/ranking configuration applies

#### Scenario: Request is outside the semantic cohort
- **WHEN** the target bucket does not match
- **THEN** selection continues to the existing enabled baseline without changing its configuration or response semantics

#### Scenario: Target is already enabled
- **WHEN** activation is repeated for the same compatible target
- **THEN** it succeeds as an idempotent replay without changing other policies

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
