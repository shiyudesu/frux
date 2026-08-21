## Why

Frux has now verified real active-contract multimodal video embeddings, Exact retrieval, Hybrid Search,
Similar Videos, complete candidate scoring, and deterministic Provider quota merge, but Recommendation
still uses only the existing hash-based user/session vectors. The next useful step is a bounded
session-only semantic path that works at low traffic, needs no model training or long-term user
profile, and turns the validated content representation into explainable recommendation value.

## What Changes

- Add a versioned session semantic interest builder that combines only bounded Recommendation
  context and trusted server-side current/recent behavior, interaction, and feedback facts with
  current active-contract video vectors.
- Add an optional `semantic_session` Recall Provider that performs active-contract Exact retrieval,
  excludes seed/suppressed videos, emits finite positive similarity evidence, and never invokes the
  external embedding Provider on the recommendation request path.
- Add a registered `semantic_similarity` ranking component whose effective contribution and
  achievable quota representation are bounded by session confidence; insufficient evidence returns
  a healthy empty Provider result and releases quota capacity through existing underfill handling.
- Extend versioned Recommendation policy validation and sampled request evidence with a closed
  session-semantic policy version, active contract key, confidence/result summary, and degradation
  reason while preserving stable Snapshot pagination and deterministic replay.
- Add fixed-cardinality metrics, deterministic Golden Set fixtures, PostgreSQL integration coverage,
  and end-to-end recommendation tests for positive signals, early skip, explicit negative feedback,
  missing vectors, contract mismatch, timeout, and fallback.
- Keep the capability disabled by default. Existing `recommend/v1`, `recommend/v2`, hash-based
  `session_continuation`, Fresh, Hot, Following, content similarity, Snapshot, and Feed behavior stay
  unchanged until a later independent rollout change selects the new Provider.
- Explicitly exclude long-term multimodal profiles, historical Backfill, HNSW/ANN, model training,
  provider calls during recommendation, Shadow traffic, and production Rollout.

## Capabilities

### New Capabilities

- `session-semantic-recommendation`: Bounded session signal construction, confidence, exact semantic
  recall, ranking evidence, fallback, observability, and deterministic evaluation.

### Modified Capabilities

- `contextual-recommendation`: Register the optional `semantic_session` Provider and
  `semantic_similarity` feature in versioned policies, and retain bounded semantic evidence in
  sampled Recommendation request records without changing existing active policies.

## Impact

- Backend recommendation domain/application code for policy registries, Recall Provider execution,
  candidate score components, ranking, request logging, and Snapshot-compatible evidence.
- PostgreSQL read adapters over existing recommendation behavior, interaction, feedback, multimodal
  vector fact/projection, and readable-video facts; no new external service or training dependency.
- Multimodal configuration validation and API composition so session recommendation is independently
  disabled and requires a complete active contract plus Exact retrieval when enabled.
- Fixed-label metrics, package tests, optional isolated PostgreSQL tests, recommendation flow tests,
  module documentation, and the recommendation roadmap.
- Public Recommendation request and Feed response schemas remain backward compatible.
