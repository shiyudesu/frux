## ADDED Requirements

### Requirement: Acceptance may target an exact existing rollout policy
The Session Semantic acceptance Runner SHALL support an optional exact existing policy version. The
target MUST be enabled, registered as the accepted rollout profile, compatible with the active
contract, and accompanied by an enabled 100% baseline policy.

#### Scenario: Existing rollout target is valid
- **WHEN** the configured version is an enabled compatible Session Semantic rollout policy
- **THEN** the Runner uses that policy without creating another policy row

#### Scenario: Target is missing, disabled, incompatible, or lacks baseline fallback
- **WHEN** any existing-policy prerequisite fails
- **THEN** acceptance stops before creating behavior facts or changing policy state

#### Scenario: Existing-policy option is absent
- **WHEN** the configured policy version is zero
- **THEN** the previous temporary-policy creation, disable, and optional deletion workflow remains unchanged

### Requirement: Existing-policy cohort evidence is deterministic
The Runner SHALL generate one stable request identity selected by the exact rollout target and one
stable fallback identity that selects another enabled policy. It SHALL report bounded cohort buckets
and fallback policy version without exposing raw request IDs.

#### Scenario: Target cohort identity is generated
- **WHEN** the target rollout percentage is valid
- **THEN** the normal policy selector chooses the exact target version for the generated acceptance request

#### Scenario: Fallback identity is generated
- **WHEN** another enabled 100% baseline exists
- **THEN** the normal selector chooses a non-target version for the generated fallback identity

### Requirement: Managed existing policy is disabled but never deleted
An existing rollout target used by acceptance SHALL be considered managed but not created. The Runner
SHALL exact-disable it after successful verification and SHALL attempt the same bounded recovery after
failure. Cleanup MUST NOT delete the existing policy row.

#### Scenario: Existing-policy acceptance succeeds
- **WHEN** real Feed, evidence, Snapshot, metrics, and zero-model-call checks pass
- **THEN** the exact target is disabled, reported disabled, retained in PostgreSQL, and v1/v2 remain unchanged

#### Scenario: Existing-policy acceptance fails after management begins
- **WHEN** a later stage fails or times out
- **THEN** bounded recovery attempts exact disable and reports whether the target reached the safe state

#### Scenario: Cleanup is requested
- **WHEN** existing-policy mode reverts the acceptance favorite
- **THEN** the target policy remains present with `deleted=false`
