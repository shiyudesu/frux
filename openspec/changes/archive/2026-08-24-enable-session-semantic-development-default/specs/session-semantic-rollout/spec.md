## MODIFIED Requirements

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
- **THEN** it succeeds as an idempotent replay without changing unrelated policies

## ADDED Requirements

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
