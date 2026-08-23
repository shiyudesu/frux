## ADDED Requirements

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
