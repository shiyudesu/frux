## ADDED Requirements

### Requirement: Active rollout requires accepted evidence and an exact Kill Switch
Session Semantic SHALL enter an active recommendation cohort only through a disabled-first,
registered rollout policy whose Shadow evidence, contract, runtime readiness, stable cohort, baseline
fallback, and exact-version disable operation have all passed validation.

#### Scenario: Dormant implementation exists without rollout evidence
- **WHEN** Session Semantic code/runtime exists but the rollout gates have not passed
- **THEN** bootstrap policies remain unchanged and no active policy selects the semantic Provider

#### Scenario: Rollout gates pass
- **WHEN** a compatible disabled target, complete Shadow report, ready runtime, and enabled 100% baseline are confirmed
- **THEN** an operator may explicitly enable only the exact bounded target cohort

#### Scenario: Kill Switch is used
- **WHEN** operational error, latency, capacity, relevance, or fallback evidence requires rollback
- **THEN** the exact target policy is disabled while existing non-semantic policies, vector facts, request logs, and outcomes remain intact
