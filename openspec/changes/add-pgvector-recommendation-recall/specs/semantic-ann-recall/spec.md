## ADDED Requirements

### Requirement: Application-Level Semantic ANN Contracts
Frux SHALL define `semantic_ann` as an application-level recommendation `RecallProvider`. The provider SHALL depend only on a compatible semantic recent/long-term profile source supplied by `project-semantic-user-interest` and a validated bounded semantic ANN query interface supplied by `enable-pgvector-recommendation-index`; neither interface SHALL expose pgvector, SQL, projection, index, or persistence types to the recommendation application.

#### Scenario: Compatible prerequisites are composed
- **WHEN** semantic ANN enablement is true and both compatible prerequisite interfaces are available
- **THEN** the API registers one `semantic_ann` recall provider without constructing projection, index, backfill, or reconciliation infrastructure in this capability

#### Scenario: Enabled prerequisite is unavailable
- **WHEN** semantic ANN enablement is true but either prerequisite interface is missing or incompatible
- **THEN** API startup fails with a bounded prerequisite/configuration error before registering a partial provider

#### Scenario: Semantic ANN is disabled
- **WHEN** semantic ANN enablement is false
- **THEN** the provider is not registered, prerequisite interfaces are not required by recommendation composition, and existing recommendation behavior remains available

### Requirement: Recent-Then-Long-Term Profile Selection
The `semantic_ann` provider SHALL load one compatible semantic profile for the authenticated user, select a finite recent vector with norm at least `1e-6` before considering long-term interest, select a finite long-term vector with norm at least `1e-6` only when recent interest is unusable, and normalize a defensive request-local copy. It MUST NOT combine vectors, use a negative vector, mutate stored profile data, call an embedding service, or reinterpret the hash profile.

#### Scenario: Recent interest is usable
- **WHEN** the compatible profile contains a usable recent vector
- **THEN** the provider normalizes a copy of the recent vector and does not use the long-term vector

#### Scenario: Recent interest is empty
- **WHEN** recent interest is unusable and the compatible long-term vector is usable
- **THEN** the provider normalizes a copy of the long-term vector for the ANN query

#### Scenario: Profile is absent or empty
- **WHEN** no compatible profile exists or both positive vectors are unusable
- **THEN** the provider returns a successful empty candidate list without querying the ANN index

#### Scenario: Profile read is invalid
- **WHEN** the profile source returns malformed, non-finite, incompatible, or failed data
- **THEN** the provider returns no semantic candidates and reports normal bounded provider degradation

### Requirement: Bounded Active Semantic ANN Execution
An active `semantic_ann` provider SHALL perform one profile read and at most one ANN query. The ANN query SHALL use the selected policy budget from 1 to 100, the existing provider context with a selected policy deadline from 25 to 500 milliseconds, and at most 20 bounded current/recent session video exclusions. The provider SHALL use the existing shared provider-concurrency admission and MUST NOT retry, widen its budget, scan the corpus, or continue detached work after cancellation.

#### Scenario: Active provider is selected
- **WHEN** the selected policy contains valid `semantic_ann` budget and deadline entries and shared admission succeeds
- **THEN** one bounded ANN query receives the normalized selected vector, policy budget, bounded exclusions, and cancellable provider context

#### Scenario: Session exclusions exceed the provider bound
- **WHEN** recommendation context contains more than 20 current/recent video IDs
- **THEN** the provider copies at most 20 IDs into the ANN query without unbounded allocation or query input

#### Scenario: Deadline or capacity is exhausted
- **WHEN** the provider deadline expires or shared provider capacity is unavailable
- **THEN** no partial semantic candidate set is merged and existing provider degradation behavior applies

### Requirement: Cosine Recall Annotation and Merge
Every accepted semantic ANN neighbor SHALL become one recommendation candidate with exactly one `RecallReason` whose provider is `semantic_ann` and one `SourceScores["semantic_ann"]` value equal to finite positive cosine similarity clamped to `[0,1]`. Vectors, distances, profile source, model metadata, and index metadata MUST NOT become response fields or ranking features.

#### Scenario: Valid neighbor is returned
- **WHEN** the ANN interface returns a readable neighbor with finite positive cosine similarity
- **THEN** the candidate receives matching bounded `semantic_ann` reason and source scores

#### Scenario: Neighbor score is invalid
- **WHEN** a returned cosine score is non-finite or non-positive
- **THEN** the neighbor is omitted and no invalid score reaches merge, ranking, metrics, or logs

#### Scenario: Existing provider returns the same video
- **WHEN** semantic ANN and another provider recall one video
- **THEN** the normal merge keeps one candidate with both applicable provider reasons and source scores

### Requirement: Healthy Empty and Failure Isolation
Missing profiles and empty ANN results SHALL be healthy empty outcomes. Profile-source errors, ANN-interface errors, cancellation, timeout, or capacity exhaustion SHALL produce no semantic candidates and SHALL use the existing bounded degraded-provider path. Semantic ANN failure MUST NOT make Feed, attribution, snapshots, healthy providers, or hash/non-vector fallback unavailable.

#### Scenario: ANN has no neighbors
- **WHEN** the selected profile is valid but the bounded ANN query returns no candidates
- **THEN** semantic ANN completes as healthy empty and recommendation continues normally

#### Scenario: ANN query fails
- **WHEN** the ANN interface returns an error or exceeds its context
- **THEN** healthy providers continue and the request records only the bounded semantic provider degradation

#### Scenario: All semantic data is absent
- **WHEN** the user has no compatible profile or the index has no matching videos
- **THEN** existing cold-start, hash, and non-vector recommendation paths retain their prior behavior

### Requirement: Explicit Policy Opt-In
Policy validation SHALL recognize `semantic_ann` only as an optional recall provider. Its recall budget SHALL be 1 to 100 and its matching provider deadline SHALL be 25 to 500 milliseconds; both entries MUST be present together. `semantic_ann` SHALL NOT be a feature-weight key. Bootstrap `recommend/v1` and `recommend/v2` SHALL remain byte-for-byte free of `semantic_ann`.

#### Scenario: New policy opts in
- **WHEN** operators create and select a valid policy containing matching `semantic_ann` budget and deadline entries
- **THEN** the registered provider may contribute candidates through the existing provider executor

#### Scenario: Policy omits semantic ANN
- **WHEN** the provider is registered but the selected policy has no `semantic_ann` entries
- **THEN** semantic ANN does not run or affect candidate ordering

#### Scenario: Semantic policy configuration is invalid
- **WHEN** a policy has only one semantic entry, an out-of-range budget or deadline, or a `semantic_ann` feature weight
- **THEN** the policy cannot become active

#### Scenario: Bootstrap policies are ensured
- **WHEN** initial policy creation or migration runs after this capability is deployed
- **THEN** serialized `recommend/v1` and `recommend/v2` provider budgets, deadlines, features, weights, and rollout remain unchanged

### Requirement: Unchanged Ranking and Recommendation Semantics
Semantic ANN candidates SHALL pass through the existing visibility recheck, suppression, duplicate merge, ranking, diversity, snapshot, evaluation, and attribution behavior. This capability SHALL NOT add or change ranking features or weights, train a model, invoke request-path inference, remove hash behavior, or automatically activate a policy.

#### Scenario: Semantic candidates are merged
- **WHEN** an active policy receives semantic ANN candidates
- **THEN** the unchanged ranker orders them using only the existing policy feature set and weights

#### Scenario: Candidate becomes unreadable
- **WHEN** a semantic candidate is no longer public, published, or media-ready before response assembly
- **THEN** the existing final visibility check removes it

#### Scenario: Provider support is deployed
- **WHEN** code and enablement support exist but no new policy is selected
- **THEN** production ranking, bootstrap rollout, snapshots, attribution, and hash fallback remain unchanged

### Requirement: Bounded Active Provider Observability
Semantic ANN SHALL expose bounded-cardinality metrics for active provider attempts, duration, result, candidate count, and selected profile source. Result labels SHALL be limited to `success`, `empty`, `no_profile`, `timeout`, `capacity`, `invalid_profile`, and `index_error`; profile-source labels SHALL be limited to `recent`, `long_term`, and `none`.

#### Scenario: Active provider completes
- **WHEN** semantic ANN succeeds, returns empty, times out, reaches capacity, or fails a prerequisite call
- **THEN** active-provider metrics use only the fixed result and profile-source labels

#### Scenario: Sensitive or high-cardinality data exists
- **WHEN** provider work involves user, video, request, vector, candidate, model, SQL/index, or raw error data
- **THEN** those values do not appear in metric labels or normal logs

### Requirement: Policy-Controlled Rollout, Rollback, and Verification
Semantic ANN rollout SHALL enable and verify compatible provider composition before selecting a policy that contains `semantic_ann`. Rollback SHALL select a policy without `semantic_ann` before disabling provider composition. Implementation SHALL include focused provider, policy, composition, merge/degradation, metrics, regression, and documentation verification without adding pgvector infrastructure or shadow acceptance tests.

#### Scenario: Provider is prepared before rollout
- **WHEN** semantic ANN composition is enabled while the selected policy omits the provider
- **THEN** prerequisite compatibility can be verified without executing semantic ANN for production requests

#### Scenario: Active rollout is rolled back
- **WHEN** operators select a valid policy without `semantic_ann`
- **THEN** new requests stop executing the provider without changing prerequisite data or redeploying the API

#### Scenario: Focused validation runs
- **WHEN** implementation validation is executed
- **THEN** tests prove recent-then-long selection, bounds, exclusions, scores, merge, degradation, policy validation, unchanged bootstrap policies/ranking, enablement, metrics, and rollback behavior
