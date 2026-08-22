## ADDED Requirements

### Requirement: Recommendation Shadow work is production-invariant
Optional recommendation Shadow evaluation SHALL execute only after a complete active ordering has
been copied and SHALL have no mutation or return path into active recall, ranking, degradation,
Snapshot/cursor state, request logs, served-candidate evidence, attribution, policy selection, or
response completion.

#### Scenario: Shadow succeeds with unique candidates
- **WHEN** Shadow finds candidates absent from the active ordering
- **THEN** the response candidates, order, reasons, scores, degradation, policy version, cursor, Snapshot, request log, and delivery evidence remain exactly those of Shadow-disabled execution

#### Scenario: Shadow times out or fails
- **WHEN** any Shadow dependency times out, returns an error, panics defensively, or ignores cancellation
- **THEN** the active recommendation response completes under its existing behavior without a Shadow degradation or error

#### Scenario: Active policy omits semantic fields
- **WHEN** `recommend/v1`, `recommend/v2`, or another active policy does not select `semantic_session`
- **THEN** Shadow may simulate its independent registered profile but MUST NOT modify, persist, activate, or reinterpret the active policy
