## ADDED Requirements

### Requirement: Shadow Recall Isolation
Recommendation MAY execute sampled semantic ANN recall for aggregate shadow evaluation only when the shadow evaluator is explicitly configured. Shadow execution SHALL remain outside production candidate merge and MUST NOT change candidate IDs, recall reasons, source scores, degraded state, ranking, diversity, snapshots, request logs, served-candidate evidence, outcomes, attribution, cursors, response fields, or response latency.

#### Scenario: Sampled shadow recall returns candidates
- **WHEN** semantic ANN shadow evaluation returns candidate IDs for a recommendation request
- **THEN** the request produces the same production result and durable recommendation artifacts it would have produced without shadow evaluation

#### Scenario: Sampled shadow recall fails
- **WHEN** semantic ANN shadow evaluation has no profile, is empty, fails, times out, is cancelled, reaches capacity, or returns invalid data
- **THEN** the request retains the same production degraded state, fallback behavior, ordering, snapshots, logs, evidence, attribution, and response timing

#### Scenario: Active policy omits semantic ANN
- **WHEN** a sampled request uses a policy without the active `semantic_ann` provider
- **THEN** shadow evaluation remains observational and does not add, modify, or activate a policy entry

#### Scenario: Shadow evaluation is disabled
- **WHEN** shadow sampling is zero
- **THEN** recommendation execution performs no shadow admission or provider work and retains its previous behavior
