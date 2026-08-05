## MODIFIED Requirements

### Requirement: Multi-Source Candidate Recall
The recommendation service SHALL merge bounded candidates from configurable fresh, hot, content-similar, followed-author, session-continuation, and optional semantic-ANN recall providers. Every selected provider SHALL use its validated policy budget and deadline and the shared global provider-concurrency limit. Providers absent from the selected policy MUST NOT run or affect candidates.

#### Scenario: One recall provider fails
- **WHEN** a provider exceeds its deadline or returns an infrastructure error
- **THEN** the service continues with healthy providers and marks the request degraded

#### Scenario: Providers return the same video
- **WHEN** multiple providers recall one video
- **THEN** the pool contains one candidate retaining its applicable recall reasons and features

#### Scenario: Candidate is no longer public
- **WHEN** a recalled video becomes private, deleted, non-published, or media-unready before response assembly
- **THEN** it is removed from the response

#### Scenario: Selected policy omits semantic ANN
- **WHEN** the `semantic_ann` provider is registered but the selected policy has no matching semantic budget and deadline
- **THEN** semantic ANN does not run, no semantic candidates are merged, and production ordering is unchanged

#### Scenario: Semantic ANN fails
- **WHEN** active semantic profile or ANN work fails, times out, is cancelled, or reaches shared capacity
- **THEN** no partial semantic candidate set is merged and healthy existing providers continue through the normal degraded path

### Requirement: Versioned Ranking Policy
Recommendation ranking SHALL use a validated versioned policy defining recall budgets, feature weights, decay, exposure windows, diversity, and rollout. Policy validation MAY recognize `semantic_ann` only as an optional recall provider with budget 1 to 100 and deadline 25 to 500 milliseconds; `semantic_ann` SHALL NOT introduce a ranking feature or weight. Bootstrap `recommend/v1` and `recommend/v2` SHALL remain unchanged.

#### Scenario: Invalid policy is submitted
- **WHEN** a policy contains unknown features, invalid bounds, unsupported values, a semantic-ANN budget without a matching deadline, a semantic-ANN deadline without a matching budget, or a semantic-ANN feature weight
- **THEN** it cannot become active

#### Scenario: Policy rollback occurs
- **WHEN** operators select a previous valid policy version
- **THEN** new recommendation requests use that version without redeploying the API

#### Scenario: Existing bootstrap policies are ensured
- **WHEN** migrations run after semantic ANN support is deployed
- **THEN** serialized provider budgets, deadlines, weights, and rollout of `recommend/v1` and `recommend/v2` remain byte-for-byte free of `semantic_ann`

#### Scenario: New policy opts into semantic ANN
- **WHEN** operators create a valid new policy containing matching `semantic_ann` budget and deadline entries
- **THEN** the provider may contribute recall reasons while the existing ranking feature set and weights remain unchanged
