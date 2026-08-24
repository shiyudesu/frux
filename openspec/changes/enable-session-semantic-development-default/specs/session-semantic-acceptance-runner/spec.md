## MODIFIED Requirements

### Requirement: Existing-policy cohort evidence is deterministic
The Runner SHALL generate one stable request identity selected by the exact rollout target. For a
staged target it SHALL also generate one stable fallback identity that selects another enabled policy.
For a100% target it SHALL retain verification of an enabled non-semantic100% baseline but SHALL report
that no non-target cohort exists. Raw request IDs MUST NOT be exposed.

#### Scenario: Target cohort identity is generated
- **WHEN** the target rollout percentage is valid
- **THEN** the normal policy selector chooses the exact target version for the generated acceptance request

#### Scenario: Staged fallback identity is generated
- **WHEN** the target percentage is below100% and another enabled100% baseline exists
- **THEN** the normal selector chooses a non-target version for the generated fallback identity

#### Scenario: Full rollout target is used
- **WHEN** the target percentage is100% and another enabled non-semantic100% baseline exists
- **THEN** the target request selects the exact version and fallback cohort fields remain absent because every bucket selects the target
