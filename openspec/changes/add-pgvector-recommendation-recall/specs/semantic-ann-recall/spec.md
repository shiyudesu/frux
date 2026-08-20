## ADDED Requirements

### Requirement: Dormant Application-Level Semantic Contracts
Frux SHALL define `semantic_ann` as an application-level recommendation `RecallProvider` backed only by compatible profile, bounded session-vector, and semantic-neighbor interfaces from the prerequisite changes. Those interfaces MUST NOT expose pgvector, SQL, projection, index, persistence, or embedding-service client types. Provider composition SHALL default disabled, and registration SHALL NOT create, select, or activate a semantic policy.

#### Scenario: Compatible prerequisites are composed
- **WHEN** semantic ANN enablement is true and all compatible prerequisite interfaces are available
- **THEN** the API registers one dormant `semantic_ann` provider without changing selected policies or constructing infrastructure owned by another capability

#### Scenario: Enabled prerequisite is unavailable
- **WHEN** enablement is true but a compatible profile, session-vector, or index interface is missing
- **THEN** API startup fails with a bounded prerequisite error before partial registration

#### Scenario: Semantic ANN is disabled
- **WHEN** enablement is false
- **THEN** the provider is not registered, prerequisites are not required by recommendation composition, and existing recommendation remains available

### Requirement: Fixed Session Recent Long-Term Fusion
The provider SHALL implement `semantic-query-v1` by combining usable finite compatible vectors with fixed weights session `0.50`, recent `0.30`, and long-term `0.20`. Missing components SHALL contribute zero and remaining configured weights SHALL be renormalized to one. A usable recent vector MUST NOT completely replace a usable long-term vector. The final request-local sum SHALL be normalized before search.

#### Scenario: All components are usable
- **WHEN** session, recent, and long-term vectors are valid with norm at least `1e-6`
- **THEN** the query vector uses the documented `0.50/0.30/0.20` blend

#### Scenario: Session is absent
- **WHEN** recent and long-term are usable but session is absent
- **THEN** their weights renormalize from `0.30/0.20` and both contribute

#### Scenario: Only long-term is usable
- **WHEN** session and recent are absent or unusable
- **THEN** a normalized defensive copy of long-term is used

#### Scenario: No semantic component is usable
- **WHEN** profile is absent and no bounded session vector can be built
- **THEN** semantic recall returns healthy empty and existing hash/non-vector providers remain the fallback

### Requirement: Separate Bounded Semantic Capacity
Semantic execution SHALL use a dedicated process-local non-blocking no-queue capacity bound from 1 through 16 and MUST NOT consume or reduce baseline-provider permits. A future invocation SHALL use budget 1 through 100, deadline 25 through 500 milliseconds, at most 20 exclusions, and at most one neighbor query. It MUST NOT retry, widen bounds, scan the corpus itself, or continue detached after cancellation.

#### Scenario: Semantic capacity is available
- **WHEN** an invocation acquires a semantic permit
- **THEN** bounded profile/session preparation and at most one query execute under the provider context

#### Scenario: Semantic capacity is exhausted
- **WHEN** no semantic permit is immediately available
- **THEN** no semantic work starts and baseline-provider admission remains unchanged

#### Scenario: Session exclusions exceed the bound
- **WHEN** recommendation context contains more than 20 current/recent video IDs
- **THEN** the provider copies at most 20 deterministic positive IDs

### Requirement: Deterministic Provider Reservation and Mixing
Before ranking a multi-provider pool, Frux SHALL merge duplicates, satisfy validated per-provider unique-candidate reservations, and fill remaining capacity by deterministic round-robin over fixed provider order and provider-local order. A future semantic policy SHALL have a separate semantic reservation and retain at least one baseline provider reservation. The service MUST NOT globally order all provider outputs by `published_at` and truncate before this mixing.

#### Scenario: Provider outputs exceed pool capacity
- **WHEN** total bounded outputs exceed the pre-rank pool cap
- **THEN** provider reservations are satisfied before deterministic fill and ranking

#### Scenario: Semantic candidates are older than Fresh candidates
- **WHEN** semantic candidates would have been removed by global `published_at` truncation
- **THEN** their configured reservation gives bounded candidates a chance to enter ranking

#### Scenario: Duplicate satisfies multiple providers
- **WHEN** one video is recalled by semantic ANN and a baseline provider
- **THEN** it occupies one global slot, retains all reasons/scores, and does not consume duplicate slots

#### Scenario: Provider underfills reservation
- **WHEN** a provider returns fewer unique candidates than reserved
- **THEN** unused capacity returns to deterministic round-robin fill

### Requirement: Explicit Semantic Similarity Ranking Component
Every accepted semantic neighbor SHALL receive one `semantic_ann` recall reason, one matching source score, and a candidate feature `semantic_similarity` equal to finite positive cosine similarity clamped to `[0,1]`. A future policy containing `semantic_ann` MUST assign a positive finite weight to `semantic_similarity`; semantic candidates MUST NOT be recalled and then ranked only by hash/recency features.

#### Scenario: Valid neighbor is returned
- **WHEN** the semantic-neighbor interface returns a readable video with finite positive cosine
- **THEN** reason, source score, and ranking feature carry the same bounded value

#### Scenario: Neighbor score is invalid
- **WHEN** cosine is non-finite or non-positive
- **THEN** the neighbor is omitted before mixing, ranking, metrics, or logs

#### Scenario: Future semantic policy lacks semantic weight
- **WHEN** a policy contains `semantic_ann` without a positive `semantic_similarity` feature weight
- **THEN** policy validation rejects it

### Requirement: Future Policy Baseline Preservation
Future policy validation MAY recognize `semantic_ann` only when budget, deadline, reservation, and positive semantic feature weight are present together and at least one baseline provider has non-zero budget and reservation. Bootstrap `recommend/v1` and `recommend/v2` SHALL remain byte-for-byte free of `semantic_ann`, `semantic_similarity`, and semantic reservations.

#### Scenario: Complete future policy is validated
- **WHEN** a later policy includes all semantic fields and retains Fresh, Hot, content similarity, followed author, or session continuation
- **THEN** it may be stored for a later accepted rollout

#### Scenario: Semantic policy removes every baseline
- **WHEN** a policy selects semantic ANN without a non-zero baseline provider reservation
- **THEN** it cannot become active

#### Scenario: Bootstrap policies are ensured
- **WHEN** initial policy creation or migration runs
- **THEN** v1/v2 serialized budgets, deadlines, reservations, features, weights, and rollout remain unchanged

### Requirement: Healthy Absence and Failure Isolation
Missing semantic profiles, missing session vectors, and empty search results SHALL be healthy empty outcomes. Profile/session/index errors, cancellation, timeout, invalid results, or semantic-capacity exhaustion SHALL produce no partial semantic candidates and MUST NOT make Feed, baseline providers, snapshots, evidence, attribution, or hash/non-vector fallback unavailable.

#### Scenario: All semantic data is absent
- **WHEN** a user has no compatible semantic profile and no usable session vector
- **THEN** existing cold-start, hash, Fresh, Hot, and other baseline paths retain their prior behavior

#### Scenario: Semantic query fails
- **WHEN** a prerequisite returns an error or exceeds context
- **THEN** baseline providers continue and only bounded semantic degradation is observed

#### Scenario: Candidate becomes unreadable
- **WHEN** a semantic candidate is no longer published, public, or media-ready before response assembly
- **THEN** the existing final readability check removes it

### Requirement: Bounded Registration Observability
Semantic provider support SHALL expose bounded metrics for invocation result/duration, fusion component availability, candidate count, semantic-capacity result, provider-reservation survival, and query mode. Labels SHALL use fixed enums only and MUST NOT contain user/video/request IDs, vectors, candidate lists, model strings, SQL/index details, or raw errors.

#### Scenario: Provider support is observed
- **WHEN** direct shadow or future active invocation completes
- **THEN** metrics use only documented fixed result, component, capacity, survival, and query-mode labels

#### Scenario: Sensitive values exist
- **WHEN** semantic work handles user context, vectors, IDs, policies, or infrastructure failures
- **THEN** those values do not appear in metric labels or normal logs

### Requirement: Registration Only and Shadow-First Activation
This change SHALL stop after disabled-by-default provider registration, future-policy validation, mixing/ranking support, tests, and documentation. It SHALL NOT create, select, or gray-rollout a semantic policy. `shadow-semantic-ann-recall` SHALL complete before any later active rollout proposal.

#### Scenario: Provider composition is enabled
- **WHEN** compatible provider support is registered while selected policies omit `semantic_ann`
- **THEN** production recommendation does not execute semantic recall

#### Scenario: Active gray rollout is requested
- **WHEN** operators want semantic ANN to affect production candidates
- **THEN** they first complete shadow evaluation and use a separate accepted rollout change

#### Scenario: Focused validation runs
- **WHEN** implementation verification executes
- **THEN** it proves fixed fusion, separate capacity, deterministic mixing, pool survival, explicit semantic ranking, policy guards, unchanged v1/v2, healthy fallback, registration-only behavior, and strict OpenSpec validation
