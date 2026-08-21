## MODIFIED Requirements

### Requirement: Multi-Source Candidate Recall
The recommendation service SHALL merge bounded candidates from configurable fresh, hot,
content-similar, followed-author, hash session-continuation, and optional active-contract semantic
session recall providers. The semantic session Provider SHALL be selected only by a complete
versioned policy and runtime and SHALL remain independent from the existing hash session path.

#### Scenario: One recall provider fails
- **WHEN** a provider exceeds its deadline or returns an infrastructure error
- **THEN** the service continues with healthy providers and marks the request degraded

#### Scenario: Providers return the same video
- **WHEN** multiple providers return one video
- **THEN** the pool contains one candidate retaining its applicable recall reasons and features

#### Scenario: Candidate is no longer public
- **WHEN** a recalled video becomes private, deleted, or non-published before response assembly
- **THEN** it is removed from the response

#### Scenario: Semantic session lacks sufficient evidence
- **WHEN** the optional semantic session Provider has insufficient trusted signals, compatible vectors, or confidence
- **THEN** it returns a healthy empty result without disabling hash session or other healthy Providers

### Requirement: Versioned Ranking Policy
Recommendation ranking SHALL use a validated versioned policy defining recall budgets, feature
weights, decay, exposure windows, diversity, rollout, and any optional session-semantic builder and
contract identity. Registered semantic Provider and feature fields SHALL be complete and mutually
consistent, and policies that omit them SHALL preserve their existing meaning.

#### Scenario: Invalid policy is submitted
- **WHEN** a policy contains unknown features, invalid bounds, unsupported values, or partial semantic-session configuration
- **THEN** it cannot become active

#### Scenario: Policy rollback occurs
- **WHEN** operators select a previous valid policy version
- **THEN** new recommendation requests use that version without redeploying the API

#### Scenario: Existing policy omits semantic session
- **WHEN** an existing stored or bootstrap policy has no semantic session block, Provider, or feature
- **THEN** its normalized configuration and request behavior remain backward compatible

### Requirement: Recommendation Evaluation Records
Frux SHALL record sampled privacy-bounded request, policy, candidate-reason, outcome linkage, and
optional session-semantic summary data using recommendation request IDs. Semantic summaries SHALL be
bounded to registered identity, confidence, counts, digest, closed outcome, and degradation fields
and SHALL exclude raw vectors and raw event payloads.

#### Scenario: Sampled request is served
- **WHEN** a request is selected by the configured evaluation sampling policy
- **THEN** its policy version, bounded context, ordered candidates, reasons, degraded flags, and any bounded semantic summary are retained for the configured period

#### Scenario: Outcome event arrives
- **WHEN** an exposure, progress, completion, skip, or interaction carries a recorded recommendation request ID
- **THEN** offline evaluation can link the outcome without storing client tokens or signed media URLs

#### Scenario: Semantic evidence is retained
- **WHEN** a sampled request evaluated session semantic interest
- **THEN** evidence records the builder/contract identity, closed result, confidence band, bounded counts, and candidate score components without recording vector components

### Requirement: Operational Model Fallback
Recommendation SHALL preserve local hash-vector and non-vector fallback behind versioned interfaces
when optional multimodal session vectors, active-contract projections, Exact retrieval, or semantic
runtime dependencies are unavailable.

#### Scenario: Preferred embedding is unavailable
- **WHEN** the preferred semantic model or vectors are unavailable
- **THEN** ranking can use the supported local hash-vector or non-vector fallback without failing the Feed

#### Scenario: Session semantic path degrades
- **WHEN** session signal construction, contract validation, confidence, or Exact retrieval cannot complete within bounds
- **THEN** the request continues through healthy configured Providers and records a bounded semantic degradation reason
