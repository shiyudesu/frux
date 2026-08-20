## MODIFIED Requirements

### Requirement: Multi-Source Candidate Recall
The recommendation service SHALL collect bounded candidates from configurable fresh, hot, content-similar, followed-author, session-continuation, and optional semantic-ANN recall providers. Before ranking, it SHALL merge duplicate IDs, satisfy validated per-provider unique-candidate reservations, and fill remaining pool capacity by deterministic provider mixing. It MUST NOT globally sort all provider outputs by `published_at` and truncate before reservations/mixing. Semantic ANN SHALL use a separate bounded capacity slot and MUST NOT consume baseline-provider admission.

#### Scenario: One recall provider fails
- **WHEN** a provider exceeds its deadline or returns an infrastructure error
- **THEN** the service continues with healthy providers and marks the request degraded

#### Scenario: Providers return the same video
- **WHEN** multiple providers recall one video
- **THEN** the pool contains one candidate retaining its applicable recall reasons and features

#### Scenario: Global pool capacity is smaller than all provider outputs
- **WHEN** bounded provider outputs exceed the pre-rank pool capacity
- **THEN** validated provider reservations are satisfied first and remaining slots are filled deterministically rather than by a global `published_at` truncation

#### Scenario: Candidate is no longer public
- **WHEN** a recalled video becomes private, deleted, non-published, or media-unready before response assembly
- **THEN** it is removed from the response

#### Scenario: Selected policy omits semantic ANN
- **WHEN** the `semantic_ann` provider is registered but the selected policy has no matching semantic budget and deadline
- **THEN** semantic ANN does not run, no semantic candidates are merged, and production ordering is unchanged

#### Scenario: Semantic ANN fails
- **WHEN** future semantic profile or neighbor work fails, times out, is cancelled, or reaches separate semantic capacity
- **THEN** no partial semantic candidate set is merged and healthy existing providers continue through the normal degraded path

#### Scenario: Future policy includes semantic ANN
- **WHEN** a future policy selects `semantic_ann`
- **THEN** it also retains at least one baseline provider with non-zero budget/reservation and reserves a bounded semantic contribution before ranking

### Requirement: Versioned Ranking Policy
Recommendation ranking SHALL use a validated versioned policy defining recall budgets, provider deadlines, provider reservations, feature weights, decay, exposure windows, diversity, and rollout. Policy validation MAY recognize `semantic_ann` only as an optional future recall provider with budget 1 to 100, deadline 25 to 500 milliseconds, reservation 1 through its budget, and a positive finite `semantic_similarity` feature weight. Any semantic policy SHALL retain at least one baseline provider with non-zero budget/reservation. Bootstrap `recommend/v1` and `recommend/v2` SHALL remain byte-for-byte unchanged.

#### Scenario: Invalid policy is submitted
- **WHEN** a policy contains unknown features, invalid bounds, incomplete semantic budget/deadline/reservation/feature entries, no retained baseline provider, or a non-positive semantic feature weight
- **THEN** it cannot become active

#### Scenario: Policy rollback occurs
- **WHEN** operators select a previous valid policy version
- **THEN** new recommendation requests use that version without redeploying the API

#### Scenario: Existing bootstrap policies are ensured
- **WHEN** migrations run after semantic ANN support is deployed
- **THEN** serialized provider budgets, deadlines, reservations, weights, and rollout of `recommend/v1` and `recommend/v2` remain byte-for-byte free of `semantic_ann` and `semantic_similarity`

#### Scenario: New policy opts into semantic ANN
- **WHEN** a later accepted rollout creates a valid policy containing complete `semantic_ann` entries, semantic reservation, positive `semantic_similarity` weight, and a retained baseline provider
- **THEN** the provider may contribute through deterministic pre-rank mixing and explicit semantic ranking

#### Scenario: This change is deployed
- **WHEN** provider support is registered by `add-pgvector-recommendation-recall`
- **THEN** no semantic policy is created, selected, or actively canaried and shadow evaluation remains the required next gate
