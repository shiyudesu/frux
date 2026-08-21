# Session Semantic Recommendation Specification

## Purpose

Define bounded, server-verified, active-contract session interest construction and Exact semantic
recommendation that remains dormant by default, makes no request-time model call, and preserves all
existing recommendation fallbacks.

## Requirements

### Requirement: Session semantic inputs are bounded and server-verified
Frux SHALL derive session-semantic signals only for the authenticated user's bounded current/recent
Recommendation context and SHALL require trusted server encounter, playback, interaction, or accepted
feedback facts before a submitted video ID contributes. Duplicate or reordered facts MUST NOT create
additional signal weight.

#### Scenario: Current and recent videos have trusted facts
- **WHEN** a Recommendation request supplies bounded current/recent video IDs that have recent server-issued encounter or behavior evidence for the user
- **THEN** the builder may classify only those IDs into registered session signal kinds

#### Scenario: Client submits an arbitrary video ID
- **WHEN** a current or recent video ID lacks qualifying server evidence for that user and lookback
- **THEN** the ID contributes no semantic weight and is counted only through a bounded untrusted-or-missing result

#### Scenario: Playback facts are retried or reordered
- **WHEN** duplicate progress, completion, or skip facts exist for the same playback identity
- **THEN** the builder uses the canonical server ordering and applies at most one current contribution per video and signal kind

### Requirement: Session vector construction is versioned and contract-exact
Frux SHALL construct session interest through a code-registered builder version using only current
active-contract video vectors, bounded signal weights, time decay, capped negative contribution, and
L2 normalization. The builder SHALL require positive evidence and SHALL reject non-finite, near-zero,
dimension-mismatched, stale, or contract-incompatible results.

#### Scenario: Positive session signals are compatible
- **WHEN** current, completion, sustained-watch, like, or favorite signals resolve to compatible active-contract vectors
- **THEN** the builder produces one finite unit session vector under the policy-pinned contract

#### Scenario: Explicit negative overrides implicit context
- **WHEN** a recent video has accepted `not_interested` feedback in addition to an implicit current/recent contribution
- **THEN** implicit positive weight for that video is removed and its bounded negative direction and exclusion semantics are applied

#### Scenario: Already seen feedback is present
- **WHEN** a video has accepted `already_seen` feedback
- **THEN** it is excluded from recall without being treated as a disliked semantic direction

#### Scenario: Compatible positive evidence is absent
- **WHEN** every eligible signal lacks a compatible vector, only negative evidence remains, or the composed norm is below the registered epsilon
- **THEN** the builder returns a healthy unavailable result and does not fabricate a session vector

### Requirement: Confidence bounds semantic contribution
The registered builder SHALL calculate deterministic confidence from compatible-vector coverage,
positive evidence strength, directional coherence, and freshness. Confidence SHALL be finite and
bounded to `[0,1]`, SHALL control semantic eligibility and output size, and MUST NOT increase another
Provider's configured reservation.

#### Scenario: Session evidence is coherent and covered
- **WHEN** several fresh reinforcing positive signals have compatible vectors and limited contradiction
- **THEN** the builder returns the corresponding registered confidence band and permits a bounded semantic result count

#### Scenario: Session evidence is sparse or contradictory
- **WHEN** vector coverage, evidence strength, coherence, or freshness falls below the registered gate
- **THEN** `semantic_session` returns healthy empty or a confidence-reduced prefix and existing quota underfill reallocates unused capacity

#### Scenario: Confidence calculation is replayed
- **WHEN** the same normalized facts, builder version, contract, and evaluation time are supplied
- **THEN** vector direction, confidence, confidence band, exclusions, and output bound are identical

### Requirement: Semantic session recall uses Exact retrieval without model calls
The `semantic_session` Provider SHALL use the composed active-contract vector with bounded Exact
multimodal retrieval, exclude session seeds and hard-suppressed videos, retain only finite positive
similarities, and emit a confidence-scaled `semantic_similarity` component. It SHALL NOT call query
embedding or any external model during Recommendation.

#### Scenario: Compatible semantic candidates exist
- **WHEN** an eligible session vector and current active-contract projections are available
- **THEN** the Provider returns a deterministic bounded candidate prefix with `semantic_session` reason and finite positive confidence-scaled scores

#### Scenario: External embedding provider is unavailable
- **WHEN** video vector facts already exist but the upstream multimodal Provider or query embedding path is unavailable
- **THEN** session semantic recall can still execute using PostgreSQL Exact retrieval without an external call

#### Scenario: Exact retrieval times out
- **WHEN** the policy deadline expires or PostgreSQL Exact retrieval fails
- **THEN** only `semantic_session` is marked degraded and healthy non-semantic Providers continue

#### Scenario: Candidate lacks current readability
- **WHEN** a semantic result becomes private, deleted, non-published, media-unready, source-stale, or contract-stale
- **THEN** it does not enter the ranked response and cannot consume a final readable slot

### Requirement: Activation is complete, policy-controlled, and dormant by default
Session semantic recommendation SHALL require an independently enabled runtime, a complete active
contract, Exact retrieval, a registered builder version, policy-pinned contract key, Provider
budget/deadline/quota fields, and a positive `semantic_similarity` feature weight. Checked-in
configuration and bootstrap policies SHALL leave the capability inactive.

#### Scenario: Existing bootstrap policy is used
- **WHEN** `recommend/v1` or `recommend/v2` is selected without session-semantic fields
- **THEN** Frux preserves existing Provider selection, ranking, serialization, Snapshot, and fallback behavior

#### Scenario: Runtime configuration is partial
- **WHEN** session recommendation is enabled without a complete contract, Exact repository, or valid bounded configuration
- **THEN** API startup fails instead of registering a partial Provider

#### Scenario: Policy configuration is partial
- **WHEN** a policy selects `semantic_session` or `semantic_similarity` without the complete matching session-semantic block
- **THEN** policy validation rejects it before activation

#### Scenario: Runtime is enabled but policy does not select semantic session
- **WHEN** dependencies are ready but the selected policy omits `semantic_session`
- **THEN** the request performs no session semantic build or Exact semantic recall

### Requirement: Snapshot, fallback, and suppression semantics remain stable
Session semantic interest SHALL be evaluated only while building the first bounded Recommendation
ordering. Existing Snapshot pages SHALL reuse that ordering, and feedback, exposure, author, and
visibility suppression SHALL remain authoritative over semantic scores.

#### Scenario: Caller requests another Snapshot page
- **WHEN** a first page used session semantic recall and the Snapshot remains valid
- **THEN** later pages use the stored ordering without recomputing a session vector or Exact query

#### Scenario: Explicit suppression matches a high semantic score
- **WHEN** a candidate is semantically close but is hard-suppressed by accepted video/author feedback
- **THEN** existing suppression rules remove it according to the active policy

#### Scenario: Semantic session is unavailable
- **WHEN** signals, vectors, confidence, or Exact retrieval cannot produce candidates
- **THEN** hash session, Fresh, Hot, Following, content similarity, and non-vector fallback remain available under their existing rules

### Requirement: Semantic evidence is bounded and privacy-safe
Frux SHALL expose fixed-cardinality metrics and sampled bounded request evidence for builder version,
closed result, confidence, confidence band, signal/vector counts, contract identity, Provider outcome,
candidate count, and duration. Normal metrics, logs, API responses, and request evidence MUST NOT
contain raw vectors, raw event bodies, query text, credentials, or identifiers in metric labels.

#### Scenario: Semantic session succeeds
- **WHEN** the Provider contributes candidates to a sampled Recommendation request
- **THEN** request evidence retains the registered Provider reason, score component, builder/contract identity, bounded summary, and degradation state needed for diagnosis

#### Scenario: Semantic session returns empty or degraded
- **WHEN** confidence is insufficient, vectors are missing, contract mismatches, or Exact retrieval fails
- **THEN** fixed labels record the closed outcome without logging user, request, session, or video IDs as labels

#### Scenario: Evidence payload is inspected
- **WHEN** a request log or normal log is serialized
- **THEN** it contains no session vector components, upstream payload, signed URL, token, or raw playback event body

### Requirement: Deterministic Golden Set covers interest direction and fallback
Frux SHALL maintain versioned deterministic session-semantic fixtures covering positive direction,
negative direction, exclusion, confidence, contract compatibility, candidate ordering, and fallback.
These fixtures SHALL be labeled technical/offline evidence and MUST NOT claim online causal lift.

#### Scenario: Golden Set is evaluated
- **WHEN** the registered fixture suite is run
- **THEN** it reports builder version, contract, expected confidence band, included/excluded candidate IDs, ordering result, and fallback outcome reproducibly

#### Scenario: Quality has not been evaluated on public or human data
- **WHEN** only synthetic deterministic fixtures have passed
- **THEN** Frux may claim orchestration and regression coverage but not improved CTR, retention, watch time, or statistically significant relevance
