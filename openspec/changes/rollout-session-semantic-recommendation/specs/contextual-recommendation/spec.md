## ADDED Requirements

### Requirement: Active semantic cohorts preserve deterministic baseline fallback
When a higher-version Session Semantic rollout policy is enabled, recommendation selection SHALL use
the existing stable cohort contract and SHALL retain at least one enabled 100% baseline policy for
non-cohort requests. Exact disabling of the semantic version SHALL restore baseline-only selection
without modifying baseline configuration.

#### Scenario: Semantic cohort matches
- **WHEN** the highest compatible enabled semantic policy matches the stable request cohort
- **THEN** that exact policy version drives recall, ranking, Snapshot, evidence, and attribution for the request

#### Scenario: Semantic cohort does not match
- **WHEN** the highest semantic policy does not match the stable cohort
- **THEN** selection continues deterministically to a lower enabled policy and does not return a no-policy error

#### Scenario: Semantic version is killed
- **WHEN** an operator disables only the semantic target version
- **THEN** subsequent requests select from the remaining enabled policies with unchanged v1/v2 serialization and behavior
