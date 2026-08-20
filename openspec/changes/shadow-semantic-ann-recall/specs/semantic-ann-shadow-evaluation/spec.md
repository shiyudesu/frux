## ADDED Requirements

### Requirement: Shadow-First Prerequisites and Disabled Default
Semantic ANN shadow evaluation SHALL depend on the exact-first/HNSW query contract, immutable semantic profile contract, and dormant registered provider contract from the three prerequisite changes. Shadow sampling SHALL default to `0` PPM, MUST NOT require an active semantic policy, and SHALL complete before any later active gray proposal.

#### Scenario: Shadow configuration is absent
- **WHEN** the API starts without explicit shadow sampling
- **THEN** sampling is zero, no shadow work occurs, and selected policies remain unchanged

#### Scenario: A prerequisite is unavailable
- **WHEN** sampling is greater than zero but a compatible prerequisite is unavailable
- **THEN** startup fails before partial shadow execution

#### Scenario: Active rollout is requested first
- **WHEN** operators propose semantic ANN production activation without completed shadow evidence
- **THEN** the rollout is blocked and this shadow capability remains the required prior step

### Requirement: Deterministic Bounded Sampling
The evaluator SHALL use a versioned deterministic hash of positive user ID, canonical request ID, and normalized scene. Sampling PPM SHALL be configurable from `0` through `1,000,000`; budget, deadline, in-flight, comparison, simulated-pool, and simulated-top-K bounds SHALL be finite and validated. The tuple/hash MUST NOT be persisted or logged.

#### Scenario: Sampling is disabled
- **WHEN** sampling is `0` PPM
- **THEN** no admission, goroutine, profile read, or semantic query occurs

#### Scenario: Request is retried
- **WHEN** the same tuple and sampler version are evaluated on multiple instances
- **THEN** each instance makes the same sampling decision

#### Scenario: Configuration is invalid
- **WHEN** any configured value is negative, above its bound, zero where a positive limit is required, or unlimited
- **THEN** startup rejects shadow configuration

### Requirement: Production-Artifact Isolation
For an admitted request, the evaluator SHALL receive only copied bounded context and provider-local facts. The response MUST NOT await shadow completion. Shadow candidates, scores, failures, simulated mixing, and simulated ranking MUST NOT enter production merge, visibility filtering, suppression, ranking, diversity, degraded state, snapshots, recommendation request logs, served-candidate evidence, outcomes, attribution, cursors, or response fields.

#### Scenario: Shadow provider succeeds
- **WHEN** semantic shadow recall returns candidates
- **THEN** only aggregate shadow observations are emitted and production behavior/artifacts equal shadow-disabled execution

#### Scenario: Shadow provider fails or blocks
- **WHEN** shadow has no profile, is empty, fails, times out, reaches capacity, is cancelled, or ignores cancellation
- **THEN** the response and every production artifact remain unchanged

#### Scenario: Future mixing is simulated
- **WHEN** copied candidates are processed by reservation, pool-limit, and semantic-ranking simulation
- **THEN** simulated candidates remain confined to request-local shadow memory

### Requirement: Capacity-Safe No-Queue Execution and Shutdown
Shadow execution SHALL use a distinct non-blocking admission bound from 1 through 16, preserve both baseline and registered-provider active permits, acquire all permits before goroutine creation, hold them until actual return, and MUST NOT queue or retry. Shutdown SHALL close admission, cancel cooperative work, and honor a caller-bounded drain.

#### Scenario: Shadow capacity is exhausted
- **WHEN** a selected request cannot immediately acquire shadow admission
- **THEN** no goroutine or provider work starts and a fixed capacity result is observed

#### Scenario: Provider ignores cancellation
- **WHEN** admitted work remains blocked after context cancellation
- **THEN** outstanding calls/goroutines remain bounded by `max_in_flight`

#### Scenario: Shutdown deadline expires
- **WHEN** context-ignoring work has not returned
- **THEN** shutdown reports incomplete drain without waiting indefinitely or admitting replacement work

### Requirement: Coverage, Staleness, and Operational Evaluation
Each selected request SHALL record bounded fixed-state observations for provider latency/result, timeout/error/capacity, session/recent/long-term component availability, fused-query availability, semantic profile age, exact/HNSW query mode, result fill, and authoritative projection state `current`, `missing`, or `stale`. A stale projection SHALL mean provider/model/revision/text-hash/vector-digest equality failed and MUST NOT be returned as a candidate.

#### Scenario: Semantic profile is old
- **WHEN** a compatible profile exists with an older materialization time
- **THEN** its age is observed in a fixed bucket without labeling the user

#### Scenario: Projection is stale
- **WHEN** projection metadata does not equal the authoritative versioned embedding
- **THEN** the candidate is excluded and fixed stale coverage is observed

#### Scenario: Query underfills
- **WHEN** the query returns fewer candidates than budget after readability/exclusion filters
- **THEN** returned-K/budget fill is observed separately from provider errors

### Requirement: Unique Contribution, Pool Survival, and Simulated Ranking
The evaluator SHALL use bounded copied provider-local outputs to calculate semantic unique contribution against the available baseline union, semantic survival through the planned reservations and pre-rank pool cap, and semantic survival in a simulated top-K using the planned explicit `semantic_similarity` component. It SHALL also calculate Fresh/Hot displacement relative to the baseline simulated top-K.

#### Scenario: Semantic candidate is unique
- **WHEN** an ANN ID appears in no available baseline provider set
- **THEN** it contributes to bounded unique-contribution observations

#### Scenario: Semantic candidate survives pool mixing
- **WHEN** planned reservations/mixing retain it within the simulated pool limit
- **THEN** pool-survival observations increase

#### Scenario: Semantic candidate survives simulated ranking
- **WHEN** explicit semantic ranking places it inside simulated top-K
- **THEN** rank-survival observations increase without changing production rank

#### Scenario: Fresh or Hot candidate is displaced
- **WHEN** a Fresh/Hot ID in baseline simulated top-K is absent after semantic simulation
- **THEN** displacement is observed by fixed provider label

### Requirement: Author Topic Diversity and Comparator Evaluation
The evaluator SHALL compute bounded distinct-author/topic counts and concentration/entropy summaries for semantic output and simulated top-K. It SHALL also compute intersection, Jaccard, and comparator coverage for fixed baseline providers and their union. Unknown topic metadata and unavailable comparators SHALL be reported explicitly; candidate, author, topic, and video IDs MUST NOT be persisted or logged.

#### Scenario: Semantic results diversify authors or topics
- **WHEN** bounded results include multiple known authors/topics
- **THEN** fixed-label diversity distributions reflect the result without identity labels

#### Scenario: Topic metadata is absent
- **WHEN** candidates lack allowlisted topic/category metadata
- **THEN** unknown coverage is observed rather than inventing a topic

#### Scenario: Comparator is unavailable
- **WHEN** a baseline provider was omitted or failed
- **THEN** its unavailable state is recorded and no undefined ratio is emitted

### Requirement: Fixed-Label Shadow Observability
Shadow metrics SHALL cover selection, terminal result, latency, capacity, component/profile coverage, profile age, projection state, query mode/fill, unique contribution, pool/rank survival, Fresh/Hot displacement, diversity, and overlap. Each selected request SHALL produce at most one terminal result. Labels SHALL use documented fixed enums only.

#### Scenario: Sensitive values exist
- **WHEN** evaluation handles user/request/session/video/author/topic IDs, vectors, scores, models, policies, SQL/index details, or errors
- **THEN** those values do not appear in metric labels, normal logs, recommendation request logs, evidence, or attribution

#### Scenario: Completion races cancellation
- **WHEN** completion and deadline/shutdown cancellation race
- **THEN** exactly one terminal result and latency observation are emitted

### Requirement: Low-Volume Golden and Human Semantic Relevance
The runbook SHALL define a versioned golden set with at least 30 representative session/profile contexts and bounded relevance labels, including sparse, session-shift, recent-shift, stable long-term, negative-feedback, and Fresh/Hot-heavy cases. It SHALL report precision@K, recall@K where judgments are complete, nDCG@K, unique relevant contribution, and diversity. At least 20 percent of contexts SHALL receive independent second review with disagreement/adjudication reporting.

#### Scenario: Traffic volume is small
- **WHEN** production shadow samples are insufficient for stable aggregate inference
- **THEN** deterministic operational tests plus golden/human relevance still produce bounded evidence without claiming causal lift

#### Scenario: Unsafe or unreadable candidate is judged
- **WHEN** a candidate is private, unpublished, media-unready, or otherwise unsafe
- **THEN** it is labeled unacceptable and retrieval is treated as a correctness failure

#### Scenario: Reviewers disagree
- **WHEN** relevance labels differ
- **THEN** disagreement and adjudication are reported rather than hidden

### Requirement: Manual Inconclusive-Aware Acceptance Without Activation
The operator report SHALL record exact denominators and uncertainty and MUST NOT require a large fixed traffic floor such as 10,000 requests. It SHALL evaluate operational safety, coverage/staleness, unique contribution, pool/rank survival, displacement, diversity, overlap, and golden/human relevance. Insufficient evidence SHALL be `inconclusive`; a completed report SHALL NOT create/select a policy or claim causal online lift.

#### Scenario: Evidence is operationally safe and semantically plausible
- **WHEN** bounded operational checks, deterministic stress tests, and golden/human review meet documented gates
- **THEN** the report may recommend a separate active-rollout proposal

#### Scenario: Evidence is insufficient
- **WHEN** denominators are too small or coverage/relevance review is incomplete
- **THEN** the report states `inconclusive` without assuming zero failures or activating a policy

#### Scenario: Shadow metrics look favorable
- **WHEN** overlap, unique contribution, diversity, or simulated survival is high
- **THEN** the report explicitly states that observational evidence does not prove causal engagement lift

### Requirement: Focused Safety and Invariance Verification
Implementation SHALL include sampler, configuration, capacity, cancellation, shutdown, staleness, simulation, diversity, golden-evaluation, metric-bound, and production-invariance tests.

#### Scenario: Shadow modes are compared with baseline
- **WHEN** success, empty, no-profile, stale, error, timeout, capacity, cancellation, and blocked-provider cases run with shadow disabled and selected
- **THEN** production candidates/order/reasons/scores, degraded state, cursors, snapshots, request logs, evidence, attribution, and response completion are identical

#### Scenario: Capacity stress runs
- **WHEN** sampled requests exceed `max_in_flight` while providers ignore cancellation
- **THEN** only bounded calls start, excess requests observe capacity, and active recommendation retains baseline behavior
